package translatorbot

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type forwardedTargetContent struct {
	body        string
	jumpURL     string
	needsAssets bool
}

type forwardedMirrorPayload struct {
	content mirrorText
	files   []WebhookFile
}

// mirrorForwardedMessage mirrors a forwarded message to every destination.
// When the forwarded source already has a mirror in the destination, its
// translated body and jump URL are reused without calling the translator.
func (s *Service) mirrorForwardedMessage(ctx context.Context, m DiscordMessage, groupID, sourceLanguage string, contextFn func() TranslationContext, dests []mirrorDestination, scope translationScope) error {
	contents, pendingAlts, err := s.forwardedContents(ctx, m, contextFn, dests, scope)
	if err != nil {
		return s.translationFailure(ctx, m, sourceLanguage, err, scope)
	}
	var posted []postedWebhookMirror
	var errs []error
	for _, dest := range dests {
		payload := contents[dest.targetID]
		result, err := s.sendMirror(ctx, m, groupID, dest, payload.content, nil, payload.files, m.ForwardedMessage.Content)
		if err != nil {
			errs = append(errs, fmt.Errorf("target %s: %w", dest.targetID, err))
			continue
		}
		if pendingAlts != nil && len(payload.files) > 0 {
			posted = append(posted, postedWebhookMirror{
				channel: dest.channel, threadID: dest.threadID(), messageID: result.MessageID,
				attachmentIDs: result.AttachmentIDs, language: dest.channel.Language,
			})
		}
	}
	s.applyReadyAttachmentAlts(pendingAlts, posted)
	return errors.Join(errs...)
}

// forwardedContents prepares the outgoing content per destination: a
// localized forwarded header plus either a reused mirror body or a fresh
// translation of the forwarded snapshot.
func (s *Service) forwardedContents(ctx context.Context, m DiscordMessage, contextFn func() TranslationContext, dests []mirrorDestination, scope translationScope) (map[string]forwardedMirrorPayload, <-chan attachmentAltOutcome, error) {
	forwarded := m.ForwardedMessage

	prepared := make(map[string]forwardedTargetContent, len(dests))
	translateDests := make([]mirrorDestination, 0, len(dests))
	for _, dest := range dests {
		mirrorLink, mirrorChannelID, mirrorMessageID, ok, err := s.store.MessageQuoteTarget(ctx, forwarded.ChannelID, forwarded.MessageID, dest.targetID)
		if err != nil {
			return nil, nil, err
		}
		if ok && mirrorChannelID == dest.targetID && mirrorMessageID != "" {
			needsAssets := mirrorChannelID == forwarded.ChannelID && mirrorMessageID == forwarded.MessageID
			body := forwarded.Content
			if !needsAssets {
				if mirrorLink.MirrorBody == "" {
					return nil, nil, fmt.Errorf("mirror %s/%s has no recorded body for forward reuse; re-mirror the message", mirrorChannelID, mirrorMessageID)
				}
				body = mirrorLink.MirrorBody
			} else {
				body = mirroredMessageBody(body)
			}
			prepared[dest.targetID] = forwardedTargetContent{
				body: body, jumpURL: MessageJumpURL(m.GuildID, mirrorChannelID, mirrorMessageID),
				needsAssets: needsAssets,
			}
			continue
		}
		translateDests = append(translateDests, dest)
	}

	var loaded []loadedImageAttachment
	if len(translateDests) > 0 || anyForwardNeedsAssets(prepared) {
		loaded = s.loadImageAttachments(ctx, imageAttachmentsOnly(forwarded.Attachments))
	}

	var altCh <-chan attachmentAltOutcome
	if len(loaded) > 0 {
		languages := make([]string, 0, len(dests))
		for _, dest := range dests {
			languages = append(languages, dest.channel.Language)
		}
		altCh = s.startAttachmentAltTranslation(ctx, m.GuildID, loaded, languages, contextFn, scope)
	}

	if len(translateDests) > 0 {
		languages := make([]string, 0, len(translateDests))
		for _, dest := range translateDests {
			languages = append(languages, dest.channel.Language)
		}
		translations, err := s.translateWithLimit(ctx, m.GuildID, forwarded.Content, loaded, languages, contextFn, scope)
		if err != nil {
			return nil, nil, err
		}
		jumpGuildID := forwarded.GuildID
		if jumpGuildID == "" {
			jumpGuildID = m.GuildID
		}
		for _, dest := range translateDests {
			prepared[dest.targetID] = forwardedTargetContent{
				body:        s.postProcessContent(ctx, m.GuildID, translations.Translations[dest.channel.Language], dest.channel.Language),
				jumpURL:     MessageJumpURL(jumpGuildID, forwarded.ChannelID, forwarded.MessageID),
				needsAssets: true,
			}
		}
	}

	alts, pendingAlts := takeReadyAttachmentAlts(altCh)
	if altCh == nil {
		pendingAlts = nil
	}

	contents := make(map[string]forwardedMirrorPayload, len(dests))
	for _, dest := range dests {
		item := prepared[dest.targetID]
		body := item.body
		var files []WebhookFile
		var err error
		if item.needsAssets {
			body, err = messageContentWithLoadedImages(body, forwarded.Attachments, forwarded.Stickers, loaded)
			if err != nil {
				return nil, nil, err
			}
			files = webhookFilesForImages(loaded, alts[dest.channel.Language])
		}
		header := fmt.Sprintf("-# %s · %s", localizedUIString(dest.channel.Language, uiKeyForwarded), item.jumpURL)
		content := header
		if strings.TrimSpace(body) != "" {
			content = header + "\n" + body
		}
		final, err := finishMirrorContent(m.ID, content, dest.timestamp)
		if err != nil {
			return nil, nil, err
		}
		contents[dest.targetID] = forwardedMirrorPayload{content: final, files: files}
	}
	return contents, pendingAlts, nil
}

func anyForwardNeedsAssets(prepared map[string]forwardedTargetContent) bool {
	for _, item := range prepared {
		if item.needsAssets {
			return true
		}
	}
	return false
}
