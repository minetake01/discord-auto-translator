package translatorbot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// backfillItem is one message of a batch: the destination it goes to and the
// image attachments already fetched for it.
type backfillItem struct {
	m      DiscordMessage
	dest   mirrorDestination
	loaded []loadedImageAttachment
}

// backfillBatch collects consecutive normal messages for one request. The
// limits keep the response well under max_tokens and the images within the
// vision budget; a message's own images always travel with it.
type backfillBatch struct {
	items        []backfillItem
	outputTokens int
	images       int
	imageBytes   int
}

// fits reports whether item may join the batch. A batch never mixes
// destinations: the last item's destination is the batch destination.
func (b *backfillBatch) fits(item backfillItem) bool {
	if len(b.items) == 0 {
		return true
	}
	vision := visionFromLoaded(item.loaded)
	return len(b.items) < batchMaxMessages &&
		b.outputTokens+estimateMessageOutputTokens(item.m.Content) <= batchMaxOutputTokens &&
		b.images+len(vision) <= visionMaxImages &&
		b.imageBytes+visionBytesTotal(vision) <= visionMaxTotalBytes &&
		b.items[len(b.items)-1].dest.targetID == item.dest.targetID
}

func (b *backfillBatch) add(item backfillItem) {
	vision := visionFromLoaded(item.loaded)
	b.items = append(b.items, item)
	b.outputTokens += estimateMessageOutputTokens(item.m.Content)
	b.images += len(vision)
	b.imageBytes += visionBytesTotal(vision)
}

// flushBatch translates and posts a batch in order. A batch whose response
// breaks the contract is split in half and each half retried; a single
// message that still fails is skipped and recorded.
func (r *backfillRunner) flushBatch(ctx context.Context, items []backfillItem) error {
	if len(items) == 0 {
		return nil
	}
	var translations []string
	err := r.retry.do(ctx, func() error {
		var err error
		translations, err = r.translateBatch(ctx, items)
		return err
	})
	if err != nil {
		if !isPermanentBackfillError(err) {
			return err
		}
		if len(items) == 1 {
			return r.poster.skip(ctx, items[0].dest, items[0].m, err)
		}
		half := len(items) / 2
		if err := r.flushBatch(ctx, items[:half]); err != nil {
			return err
		}
		return r.flushBatch(ctx, items[half:])
	}
	alts := r.batchAlts(ctx, items)
	for i, item := range items {
		if err := r.poster.batchItem(ctx, item, translations[i], alts[i]); err != nil {
			return err
		}
	}
	return nil
}

// translateBatch sends one batch request. Messages without translatable
// text are passed through and left out of the request.
func (r *backfillRunner) translateBatch(ctx context.Context, items []backfillItem) ([]string, error) {
	out := make([]string, len(items))
	requestIndex := make(map[string]int) // channel\x00message → id position in the request
	var messages []BatchMessage
	var vision []visionImage
	var replyContext []ChatContextMessage
	replyRefs := map[string]string{}
	for i, item := range items {
		m := item.m
		out[i] = m.Content
		if !needsTranslation(m.Content) {
			continue
		}
		bm := BatchMessage{
			Author: m.AuthorDisplayName, Content: m.Content,
			MentionedUsers: m.MentionedUsers, MentionedChannels: m.MentionedChannels, MentionedRoles: m.MentionedRoles,
		}
		for _, image := range visionFromLoaded(item.loaded) {
			vision = append(vision, image)
			bm.Attachments = append(bm.Attachments, TranslationAttachment{Index: len(vision)})
		}
		if m.ReferencedMessageID != "" {
			refChannelID := m.ReferencedMessageChannelID
			if refChannelID == "" {
				refChannelID = m.ChannelID
			}
			key := messageRefKey(refChannelID, m.ReferencedMessageID)
			if pos, ok := requestIndex[key]; ok {
				bm.ReplyTo = batchMessageID(pos)
			} else if ref, ok := replyRefs[key]; ok {
				bm.ReplyTo = ref
			} else if len(replyContext) < batchReplyContextMax {
				entry, _, _, _, ok := r.s.resolveReplyChainEntry(ctx, refChannelID, m.ReferencedMessageID)
				if ok {
					entry.Ref = "r" + strconv.Itoa(len(replyContext)+1)
					replyContext = append(replyContext, entry)
					replyRefs[key] = entry.Ref
					bm.ReplyTo = entry.Ref
				}
			}
		}
		requestIndex[messageRefKey(m.ChannelID, m.ID)] = len(messages)
		messages = append(messages, bm)
	}
	if len(messages) == 0 {
		return out, nil
	}
	first := items[0].m
	language := items[0].dest.channel.Language
	contextFn := func() TranslationContext {
		contextChannelID, threadName := first.ChannelID, ""
		if first.ParentChannelID != "" {
			contextChannelID, threadName = first.ParentChannelID, first.ThreadName
		}
		tc := r.s.groupTranslationContext(ctx, translationContextRequest{
			guildID: first.GuildID, groupID: r.run.job.GroupID,
			contextChannelID: contextChannelID, historyChannelID: first.ChannelID,
			excludeMessageID: first.ID, threadName: threadName,
		}, r.scope)
		tc.ReplyChain = replyContext
		return tc
	}
	lookup := make([]string, len(messages))
	for i, m := range messages {
		lookup[i] = m.Content
	}
	prepared, err := r.s.prepareTranslation(ctx, first.GuildID, strings.Join(lookup, "\n"), []string{language}, contextFn, func(_ []string, tc TranslationContext, glossary []GlossaryEntry) (preparedTranslation, error) {
		return prepareBatchTranslation(language, messages, tc, glossary)
	}, vision, r.scope)
	if err != nil {
		return nil, err
	}
	result, err := r.s.translator.TranslateBatch(ctx, prepared)
	if err != nil {
		return nil, err
	}
	r.s.recordSuccessfulTranslation(first.GuildID, result.InputTokens, result.OutputTokens)
	if len(result.Translations) != len(messages) {
		return nil, fmt.Errorf("%w: got %d translations for %d messages", errBatchContent, len(result.Translations), len(messages))
	}
	for i, item := range items {
		if pos, ok := requestIndex[messageRefKey(item.m.ChannelID, item.m.ID)]; ok {
			out[i] = result.Translations[pos]
		}
	}
	return out, nil
}

// batchAlts translates the alt text of every image in the batch with one
// request. On failure the source alt text is kept, as in live mirroring.
func (r *backfillRunner) batchAlts(ctx context.Context, items []backfillItem) [][]string {
	out := make([][]string, len(items))
	var loaded []loadedImageAttachment
	for _, item := range items {
		loaded = append(loaded, item.loaded...)
	}
	if len(loaded) == 0 {
		return out
	}
	language := items[0].dest.channel.Language
	var descriptions map[string][]string
	err := r.retry.do(ctx, func() error {
		var err error
		descriptions, err = r.s.translateAttachmentAltsWithLimit(ctx, items[0].m.GuildID, loaded, []string{language}, func() TranslationContext {
			return TranslationContext{GuildID: items[0].m.GuildID, MessageID: items[0].m.ID}
		}, r.scope)
		return err
	})
	translated := descriptions[language]
	if err != nil || len(translated) != len(loaded) {
		return out
	}
	offset := 0
	for i, item := range items {
		out[i] = translated[offset : offset+len(item.loaded)]
		offset += len(item.loaded)
	}
	return out
}
