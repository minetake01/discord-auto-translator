package translatorbot

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// mirrorDestination is one place a source message is mirrored to: either a
// registered channel itself, or a synced thread inside it. The webhook
// credentials always belong to the registered channel.
type mirrorDestination struct {
	channel  GroupChannel
	targetID string
	// timestamp puts the source posting time on the first line; backfill
	// sets it for every message it posts.
	timestamp bool
}

func destinationForChannel(channel GroupChannel) mirrorDestination {
	return mirrorDestination{channel: channel, targetID: channel.ChannelID}
}

func destinationForThread(channel GroupChannel, threadID string) mirrorDestination {
	return mirrorDestination{channel: channel, targetID: threadID}
}

func destinationLanguages(dests []mirrorDestination) []string {
	languages := make([]string, 0, len(dests))
	for _, dest := range dests {
		languages = append(languages, dest.channel.Language)
	}
	return languages
}

// threadID returns the thread_id webhook parameter, empty for channel sends.
func (d mirrorDestination) threadID() string {
	if d.targetID == d.channel.ChannelID {
		return ""
	}
	return d.targetID
}

// replyQuote builds the pseudo-reply quote line for a reply message,
// preferring the mirrored version of the referenced message in the target
// channel so the jump link stays within that channel. An attachment-only
// referenced message quotes a localized placeholder instead of a snippet.
func (s *Service) replyQuote(ctx context.Context, m DiscordMessage, targetChannelID, targetLanguage string) (string, error) {
	if m.ReferencedMessageID == "" {
		return "", nil
	}
	content := m.ReferencedMessageContent
	attachmentCount := len(m.ReferencedMessageAttachments)
	quoteChannelID := m.ReferencedMessageChannelID
	quoteMessageID := m.ReferencedMessageID
	if quoteChannelID == "" {
		quoteChannelID = m.ChannelID
	}

	source, dbQuoteChannelID, dbQuoteMessageID, ok, err := s.store.MessageQuoteTarget(ctx, m.ChannelID, m.ReferencedMessageID, targetChannelID)
	if err != nil {
		return "", err
	}
	if ok {
		attachmentCount = max(attachmentCount, len(source.SourceImageAttachments))
		if dbQuoteChannelID != "" && dbQuoteMessageID != "" {
			quoteChannelID = dbQuoteChannelID
			quoteMessageID = dbQuoteMessageID
			if transferred, fetchErr := s.discord.Message(quoteChannelID, quoteMessageID); fetchErr == nil && strings.TrimSpace(transferred.Content) != "" {
				content = transferred.Content
			} else {
				content = source.SourceContentSnapshot
			}
		} else {
			content = source.SourceContentSnapshot
		}
	}
	snippet := firstLineWithoutPseudoReply(content)
	if snippet == "" {
		if attachmentCount == 0 {
			return "", nil
		}
		snippet = localizedUIStringf(targetLanguage, uiKeyAttachmentOnly, attachmentCount)
	}
	snippet = normalizeMarkdownHeaderSnippet(snippet)
	snippet = truncateRunes(snippet, replyQuoteMaxRunes, "...")
	snippet = suppressSnippetEmbeds(snippet)
	link := MessageJumpURL(m.GuildID, quoteChannelID, quoteMessageID)
	label := localizedUIString(targetLanguage, uiKeyOriginalMessage)
	return fmt.Sprintf("> %s · [%s](%s)", snippet, label, link), nil
}

func (s *Service) HandleMessageCreate(ctx context.Context, m DiscordMessage) error {
	allowed, err := s.shouldProcessMessage(ctx, m)
	if err != nil {
		return fmt.Errorf("message source policy: %w", err)
	}
	if !allowed {
		return nil
	}
	unlock := s.lockMessage(m.ChannelID, m.ID)
	defer unlock()

	if m.ThreadStarterMessage {
		_, err := s.ensureThreadSynced(ctx, m)
		return err
	}
	if m.ThreadSystemMessage || m.hasNoPayload() {
		return nil
	}
	threadCreatedWithInitialMessage, err := s.ensureThreadSynced(ctx, m)
	if err != nil {
		return err
	}
	if threadCreatedWithInitialMessage {
		return nil
	}
	if err := s.handleThreadMessageCreate(ctx, m); err != nil {
		return err
	}
	groups, err := s.store.ChannelsByChannel(ctx, m.GuildID, m.ChannelID)
	if err != nil {
		return err
	}
	var errs []error
	for _, source := range groups {
		if err := s.mirrorMessageToGroup(ctx, m, source); err != nil {
			errs = append(errs, err)
		}
	}
	if m.PollResult != nil {
		pollChannelID := m.ReferencedMessageChannelID
		if pollChannelID == "" {
			pollChannelID = m.ChannelID
		}
		if err := s.store.DeletePollTranslationCache(ctx, pollChannelID, m.ReferencedMessageID); err != nil {
			errs = append(errs, fmt.Errorf("delete poll translation cache: %w", err))
		}
	}
	return errors.Join(errs...)
}

func (s *Service) mirrorMessageToGroup(ctx context.Context, m DiscordMessage, source GroupChannel) error {
	channels, err := s.store.ChannelsInGroup(ctx, m.GuildID, source.GroupID)
	if err != nil {
		return err
	}
	var dests []mirrorDestination
	for _, target := range channels {
		if target.ChannelID == m.ChannelID {
			continue
		}
		held, err := s.liveTargetHeld(ctx, source.GroupID, target.ChannelID, liveTranslation)
		if err != nil {
			return fmt.Errorf("target %s: %w", target.ChannelID, err)
		}
		if held {
			continue
		}
		synced, err := s.targetAlreadySynced(ctx, m.ChannelID, m.ID, target.ChannelID)
		if err != nil {
			return fmt.Errorf("target %s: %w", target.ChannelID, err)
		}
		if synced {
			continue
		}
		dests = append(dests, destinationForChannel(target))
	}
	if len(dests) == 0 {
		return nil
	}
	contextFn := func() TranslationContext {
		return s.translationContextForMessage(ctx, m, source.GroupID, m.ChannelID, m.ChannelID, "", liveTranslation)
	}
	return s.mirrorMessage(ctx, m, source.GroupID, source.Language, contextFn, dests, liveTranslation)
}

// mirrorMessage translates a source message once and sends it to every
// destination, handling forwarded messages, reply quotes, and asset URLs.
// Translation failures are reported to the source channel in its language.
func (s *Service) mirrorMessage(ctx context.Context, m DiscordMessage, groupID, sourceLanguage string, contextFn func() TranslationContext, dests []mirrorDestination, scope translationScope) error {
	if m.ForwardedMessage != nil {
		return s.mirrorForwardedMessage(ctx, m, groupID, sourceLanguage, contextFn, dests, scope)
	}
	if m.PollResult != nil {
		return s.mirrorPollResultMessage(ctx, m, groupID, dests)
	}
	if m.Poll != nil {
		return s.mirrorPollMessage(ctx, m, groupID, sourceLanguage, contextFn, dests, scope)
	}

	languages := destinationLanguages(dests)
	loaded := s.loadImageAttachments(ctx, imageAttachmentsOnly(m.Attachments))
	altCh := s.startAttachmentAltTranslation(ctx, m.GuildID, loaded, languages, contextFn, scope)
	translations, err := s.translateWithLimit(ctx, m.GuildID, m.Content, loaded, languages, contextFn, scope)
	if err != nil {
		return s.translationFailure(ctx, m, sourceLanguage, err, scope)
	}
	alts, pendingAlts := takeReadyAttachmentAlts(altCh)

	var posted []postedWebhookMirror
	var errs []error
	for _, dest := range dests {
		content, err := s.mirrorContent(ctx, m, dest.targetID, dest.channel.Language, translations.Translations[dest.channel.Language], imagesNotReuploaded(m.Attachments, loaded), dest.timestamp)
		if err != nil {
			errs = append(errs, fmt.Errorf("target %s: %w", dest.targetID, err))
			continue
		}
		files := webhookFilesForImages(loaded, alts[dest.channel.Language])
		result, err := s.sendMirror(ctx, m, groupID, dest, content, nil, files, m.Content)
		if err != nil {
			errs = append(errs, fmt.Errorf("target %s: %w", dest.targetID, err))
			continue
		}
		if pendingAlts != nil {
			posted = append(posted, postedWebhookMirror{
				channel: dest.channel, threadID: dest.threadID(), messageID: result.MessageID,
				attachmentIDs: result.AttachmentIDs, language: dest.channel.Language,
			})
		}
	}
	s.applyReadyAttachmentAlts(pendingAlts, posted)
	return errors.Join(errs...)
}

// mirrorContent builds the finished text of a mirrored message from its
// translation: poll header, reply quote, body, asset URLs, then the timestamp
// line. Sending and editing share the assembly, so an edit keeps every header
// the original send had and can never lose the timestamp line. The text is
// split into messages only at the point it is posted or edited. inlineImages
// are image attachments linked by URL instead of reuploaded.
func (s *Service) mirrorContent(ctx context.Context, m DiscordMessage, targetID, language, translated string, inlineImages []DiscordAttachment, timestamp bool) (mirrorText, error) {
	quote, err := s.replyQuote(ctx, m, targetID, language)
	if err != nil {
		return "", err
	}
	return s.assembleMirrorContent(ctx, m, language, translated, quote, inlineImages, timestamp)
}

// assembleMirrorContent applies a resolved reply quote and the fixed headers
// to a translated body: asset URLs, poll header, and timestamp line.
func (s *Service) assembleMirrorContent(ctx context.Context, m DiscordMessage, language, translated, quote string, inlineImages []DiscordAttachment, timestamp bool) (mirrorText, error) {
	content := s.postProcessContent(ctx, m.GuildID, translated, language)
	content = withQuote(quote, content)
	var err error
	if content, err = messageContentWithAssetURLs(content, m.Attachments, m.Stickers); err != nil {
		return "", err
	}
	if content, err = messageContentWithAllAssetURLs(content, inlineImages, nil); err != nil {
		return "", err
	}
	return finishMirrorContent(m.ID, withPollStartedHeader(content, language, m.GuildID, m.ChannelID, m.ID, m.Poll != nil), timestamp)
}

// sendMirror posts the prepared content to one destination and records the
// message link with the given source snapshot.
func (s *Service) sendMirror(ctx context.Context, m DiscordMessage, groupID string, dest mirrorDestination, content mirrorText, embeds []*discordgo.MessageEmbed, files []WebhookFile, snapshot string) (WebhookSendResult, error) {
	avatar := avatarProxyURL(s.publicBaseURL, m.AuthorAvatarURL, m.AuthorRoleColor)
	ref := MessageReference{MessageID: m.ReferencedMessageID, ChannelID: m.ReferencedMessageChannelID}
	if ref.MessageID != "" && ref.ChannelID == "" {
		ref.ChannelID = m.ChannelID
	}
	threadID := dest.threadID()
	return s.sendAndSaveLink(ctx, dest.channel, threadID, WebhookSend{
		Content: string(content), Username: m.AuthorDisplayName, AvatarURL: avatar, TTS: m.TTS, ThreadID: threadID, Embeds: embeds, Files: files,
	}, MessageLink{
		SourceMessageID: m.ID, SourceChannelID: m.ChannelID, GroupID: groupID,
		TargetChannelID: dest.targetID, TargetLanguage: dest.channel.Language,
		SourceAuthorID: m.AuthorID, SourceAuthorDisplayName: m.AuthorDisplayName,
		SourceAuthorAvatarURL: m.AuthorAvatarURL, SourceAuthorRoleColor: m.AuthorRoleColor,
		SourceContentSnapshot:  snapshot,
		SourceImageAttachments: sourceImageAttachments(m), TimestampLine: dest.timestamp,
	}, ref)
}

func (s *Service) mirrorPollMessage(ctx context.Context, m DiscordMessage, groupID, sourceLanguage string, contextFn func() TranslationContext, dests []mirrorDestination, scope translationScope) error {
	languages := destinationLanguages(dests)
	question := strings.TrimSpace(m.Poll.Question)
	answers := pollAnswerTexts(m.Poll)
	snapshot := pollMessageSnapshot(m.Content, m.Poll)

	contentTranslations, err := s.translateWithLimit(ctx, m.GuildID, m.Content, nil, languages, contextFn, scope)
	if err != nil {
		return s.translationFailure(ctx, m, sourceLanguage, err, scope)
	}
	pollTranslations, err := s.translatePollWithLimit(ctx, m.GuildID, question, answers, languages, contextFn, scope)
	if err != nil {
		return s.translationFailure(ctx, m, sourceLanguage, err, scope)
	}

	var errs []error
	for _, dest := range dests {
		content, err := s.mirrorContent(ctx, m, dest.targetID, dest.channel.Language, contentTranslations.Translations[dest.channel.Language], imageAttachmentsOnly(m.Attachments), dest.timestamp)
		if err != nil {
			errs = append(errs, fmt.Errorf("target %s: %w", dest.targetID, err))
			continue
		}
		embeds := s.translatedPollEmbeds(ctx, m.GuildID, m.Poll, pollTranslations[dest.channel.Language], dest.channel.Language, m.AuthorRoleColor)
		if _, err := s.sendMirror(ctx, m, groupID, dest, content, embeds, nil, snapshot); err != nil {
			errs = append(errs, fmt.Errorf("target %s: %w", dest.targetID, err))
		}
	}
	errs = append(errs, s.saveTranslatedPolls(ctx, m.ChannelID, m.ID, m.Poll.Expiry, pollTranslations))
	return errors.Join(errs...)
}

func (s *Service) mirrorPollResultMessage(ctx context.Context, m DiscordMessage, groupID string, dests []mirrorDestination) error {
	pollChannelID := m.ReferencedMessageChannelID
	if pollChannelID == "" {
		pollChannelID = m.ChannelID
	}
	var errs []error
	for _, dest := range dests {
		victorLabel, err := s.pollResultVictorLabel(ctx, pollChannelID, m.ReferencedMessageID, dest.channel.Language, m.PollResult)
		if err != nil {
			errs = append(errs, fmt.Errorf("target %s: %w", dest.targetID, err))
			continue
		}
		// The result body is built per language instead of translated, but it
		// is a mirror body like any other: reference resolution and the reply
		// quote come from mirrorContent. Poll messages carry no attachments,
		// and m.Poll is nil here, so the asset-URL lines and the poll-started
		// header are no-ops.
		body := pollResultBody(dest.channel.Language, m.PollResult, victorLabel)
		content, err := s.mirrorContent(ctx, m, dest.targetID, dest.channel.Language, body, nil, dest.timestamp)
		if err != nil {
			errs = append(errs, fmt.Errorf("target %s: %w", dest.targetID, err))
			continue
		}
		if _, err := s.sendMirror(ctx, m, groupID, dest, content, nil, nil, body); err != nil {
			errs = append(errs, fmt.Errorf("target %s: %w", dest.targetID, err))
		}
	}
	return errors.Join(errs...)
}

// pollResultVictorLabel resolves the display label for a poll winner, preferring
// the translated answer from poll_translation_cache and falling back to the
// source victor_answer_text from the poll_result embed.
func (s *Service) pollResultVictorLabel(ctx context.Context, pollChannelID, pollMessageID, language string, result *DiscordPollResult) (string, error) {
	if result == nil || !result.HasEmbed {
		return "", nil
	}
	answerText := ""
	if pollMessageID != "" && result.VictorAnswerID > 0 {
		answers, ok, err := s.store.PollTranslatedAnswers(ctx, pollChannelID, pollMessageID, language)
		if err != nil {
			return "", err
		}
		idx := result.VictorAnswerID - 1
		if ok && idx >= 0 && idx < len(answers) {
			answerText = strings.TrimSpace(answers[idx])
		}
	}
	if answerText == "" {
		answerText = strings.TrimSpace(result.VictorAnswerText)
	}
	return formatPollVictorLabel(answerText, result.VictorEmoji), nil
}

func (s *Service) messageLinkTarget(ctx context.Context, targets []GroupChannel, link MessageLink) (*GroupChannel, error) {
	target := findChannel(targets, link.TargetChannelID)
	if target != nil {
		return target, nil
	}
	parentID, ok, err := s.store.ThreadParentChannel(ctx, link.GroupID, link.TargetChannelID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return findChannel(targets, parentID), nil
}

func (s *Service) HandleMessageUpdate(ctx context.Context, m DiscordMessage) error {
	allowed, err := s.shouldProcessMessage(ctx, m)
	if err != nil {
		return fmt.Errorf("message source policy: %w", err)
	}
	if !allowed || m.Poll != nil || m.PollResult != nil || m.ForwardedMessage != nil {
		// Polls, poll results, and forwards cannot be edited; their updates
		// only carry vote counts or embeds.
		return nil
	}
	unlock := s.lockMessage(m.ChannelID, m.ID)
	defer unlock()
	links, err := s.store.MessageTargets(ctx, m.ChannelID, m.ID)
	if err != nil {
		return err
	}
	var edited []MessageLink
	for _, link := range links {
		if link.SourceContentSnapshot != m.Content {
			edited = append(edited, link)
		}
	}
	if len(edited) == 0 {
		return nil
	}
	if err := s.restoreReplyReference(ctx, &m); err != nil {
		return err
	}
	if err := forEachGroupEdit(ctx, s.store, m.GuildID, edited,
		func(link MessageLink) string { return link.GroupID },
		s.messageLinkTarget,
		func(ctx context.Context, groupID string, pending []editPair[MessageLink]) error {
			return s.editMessageMirrors(ctx, m, groupID, pending)
		},
	); err != nil {
		return err
	}
	// The mirrors of messages replying to this one quote its snippet; follow
	// the edit the way the delete pass follows a deletion.
	return s.refreshReplyingQuotes(ctx, m, links)
}

// refreshReplyingQuotes re-renders the quote header of every mirror of a
// message replying to the edited message, so the snippet and jump link follow
// the referenced message's current state. The quote is rebuilt on the mirror
// body the link records and the mirror is re-split like any other edit, so a
// longer quote still leaves every part within Discord's content limit. Parts
// whose text does not change are left untouched, and a quote that cannot be
// rebuilt leaves the quote as rendered.
func (s *Service) refreshReplyingQuotes(ctx context.Context, m DiscordMessage, copies []MessageLink) error {
	replies, err := s.messageTargetsReplyingToCopies(ctx, m.ChannelID, m.ID, copies)
	if err != nil {
		return err
	}
	return forEachGroupEdit(ctx, s.store, m.GuildID, replies,
		func(link MessageLink) string { return link.GroupID },
		s.messageLinkTarget,
		func(ctx context.Context, groupID string, pending []editPair[MessageLink]) error {
			for _, p := range pending {
				quote, err := s.replyingMessageQuote(ctx, m, p.link, p.target.Language)
				if err != nil {
					return err
				}
				if quote == "" {
					continue
				}
				rebuilt, err := s.requotedMirrorContent(p.link, quote)
				if err != nil {
					return err
				}
				primary, err := s.discord.Message(p.link.TargetChannelID, p.link.TargetMessageID)
				if err != nil {
					return fmt.Errorf("fetch reply mirror %s/%s: %w", p.link.TargetChannelID, p.link.TargetMessageID, err)
				}
				if splitMessageContent(string(rebuilt), discordMessageContentLimit)[0] == primary.Content {
					continue
				}
				previous, err := s.mirrorPartContents(ctx, p.link)
				if err != nil {
					return err
				}
				if err := s.replaceMirrorMessages(ctx, p.target, &p.link, rebuilt, previous); err != nil {
					return err
				}
			}
			return nil
		},
	)
}

// requotedMirrorContent rebuilds a mirror's text with quote on top, from the
// body the link records. A link without that body has nothing to re-quote:
// the mirror predates the body column or its record broke, and guessing a
// body would post a wrong, silently truncated mirror.
func (s *Service) requotedMirrorContent(link MessageLink, quote string) (mirrorText, error) {
	if link.MirrorBody == "" {
		return "", fmt.Errorf("mirror %s/%s has no recorded body; re-mirror the message before refreshing its quote", link.TargetChannelID, link.TargetMessageID)
	}
	return finishMirrorContent(link.SourceMessageID, withQuote(quote, link.MirrorBody), link.TimestampLine)
}

// mirrorPartContents returns the posted contents of a mirror in order, first
// message first, for comparison against a rebuild.
func (s *Service) mirrorPartContents(ctx context.Context, link MessageLink) ([]string, error) {
	primary, err := s.discord.Message(link.TargetChannelID, link.TargetMessageID)
	if err != nil {
		return nil, fmt.Errorf("fetch mirror %s/%s: %w", link.TargetChannelID, link.TargetMessageID, err)
	}
	previous := []string{primary.Content}
	for _, partID := range link.ContinuationMessageIDs {
		part, err := s.discord.Message(link.TargetChannelID, partID)
		if err != nil {
			return nil, fmt.Errorf("fetch mirror %s/%s: %w", link.TargetChannelID, partID, err)
		}
		previous = append(previous, part.Content)
	}
	return previous, nil
}

// replyingMessageQuote rebuilds the quote header for a mirror of a message
// replying to the edited one, from the reference the bot stored when it
// mirrored the reply. The edited message supplies the referenced message's
// current attachments.
func (s *Service) replyingMessageQuote(ctx context.Context, edited DiscordMessage, link MessageLink, language string) (string, error) {
	ref, ok, err := s.store.MessageReferenceOf(ctx, link.SourceChannelID, link.SourceMessageID)
	if err != nil || !ok {
		return "", err
	}
	return s.replyQuote(ctx, DiscordMessage{
		ID: link.SourceMessageID, ChannelID: link.SourceChannelID, GuildID: edited.GuildID,
		ReferencedMessageID: ref.MessageID, ReferencedMessageChannelID: ref.ChannelID,
		ReferencedMessageAttachments: edited.Attachments,
	}, link.TargetChannelID, language)
}

// editMessageMirrors translates the edited source once for the group's target
// languages and rewrites every mirror, then records the new snapshot. A
// rate-limited translation skips this group's mirrors until the next update.
func (s *Service) editMessageMirrors(ctx context.Context, m DiscordMessage, groupID string, pending []editPair[MessageLink]) error {
	contextFn := func() TranslationContext {
		contextChannelID, historyChannelID := m.ChannelID, m.ChannelID
		threadName := ""
		if threads, err := s.store.SourceThreadTargets(ctx, m.ChannelID); err == nil {
			for _, tl := range threads {
				if tl.GroupID == groupID {
					contextChannelID = tl.SourceChannelID
					threadName = s.resolveThreadName(m)
					break
				}
			}
		}
		return s.translationContextForMessage(ctx, m, groupID, contextChannelID, historyChannelID, threadName, liveTranslation)
	}
	languages := make([]string, 0, len(pending))
	for _, p := range pending {
		languages = append(languages, p.target.Language)
	}
	translations, err := s.translateWithLimit(ctx, m.GuildID, m.Content, nil, languages, contextFn, liveTranslation)
	if err != nil {
		if errors.Is(err, errTranslationRateLimited) {
			return nil
		}
		return err
	}
	for _, p := range pending {
		content, err := s.editMirrorContent(ctx, m, p.link, p.target, translations.Translations[p.target.Language])
		if err != nil {
			return err
		}
		if err := s.replaceMirrorMessages(ctx, p.target, &p.link, content, nil); err != nil {
			return err
		}
		if err := s.store.UpdateMessageLinkSnapshot(ctx, p.link.SourceChannelID, p.link.SourceMessageID, p.link.TargetChannelID, m.Content); err != nil {
			return err
		}
	}
	return nil
}

// editMirrorContent rebuilds a mirror's text for an edit. The reply quote is
// rebuilt from the reply target; when the target no longer yields one — the
// referenced message is gone or was never mirrored — the quote header already
// rendered on the mirror is kept, so an edit never drops the pseudo-reply the
// original send had.
func (s *Service) editMirrorContent(ctx context.Context, m DiscordMessage, link MessageLink, target GroupChannel, translated string) (mirrorText, error) {
	quote, err := s.replyQuote(ctx, m, link.TargetChannelID, target.Language)
	if err != nil {
		return "", err
	}
	if quote == "" {
		quote = s.renderedQuoteHeader(link)
	}
	return s.assembleMirrorContent(ctx, m, target.Language, translated, quote, nil, link.TimestampLine)
}

// renderedQuoteHeader returns the bot-generated quote header the mirror
// renders — a pseudo-reply line or the deleted-original notice — or "" when
// it renders none or cannot be fetched. The mirror is the last record of what
// the original send looked like.
func (s *Service) renderedQuoteHeader(link MessageLink) string {
	message, err := s.discord.Message(link.TargetChannelID, link.TargetMessageID)
	if err != nil {
		return ""
	}
	lines := dropTimestampLine(mirrorContentLines(message.Content))
	if len(lines) == 0 {
		return ""
	}
	line := strings.TrimSpace(lines[0])
	if isPseudoReplyLine(line) || line == deletedReplyQuoteLine(link.TargetLanguage) {
		return line
	}
	return ""
}

// deletedReplyQuoteLine is the pseudo-reply header that replaces the quote
// when the referenced message has been deleted.
func deletedReplyQuoteLine(language string) string {
	return fmt.Sprintf("> -# %s", localizedUIString(language, uiKeyOriginalMessageDeleted))
}

// restoreReplyReference fills the reply target of an edited message from
// the stored reference, so the rebuilt mirror keeps its reply quote.
func (s *Service) restoreReplyReference(ctx context.Context, m *DiscordMessage) error {
	if m.ReferencedMessageID != "" {
		return nil
	}
	ref, ok, err := s.store.MessageReferenceOf(ctx, m.ChannelID, m.ID)
	if err != nil || !ok {
		return err
	}
	m.ReferencedMessageID, m.ReferencedMessageChannelID = ref.MessageID, ref.ChannelID
	if fetched, err := s.discord.Message(ref.ChannelID, ref.MessageID); err == nil {
		m.ReferencedMessageContent = fetched.Content
		m.ReferencedMessageAttachments = fetched.Attachments
	}
	return nil
}

func (s *Service) HandleMessageDelete(ctx context.Context, guildID, channelID, messageID string) error {
	s.editDebounce.cancel(editDebounceKey(channelID, messageID))
	unlock := s.lockMessage(channelID, messageID)
	defer unlock()
	links, err := s.store.MessageTargets(ctx, channelID, messageID)
	if err != nil {
		return err
	}
	replies, err := s.messageTargetsReplyingToCopies(ctx, channelID, messageID, links)
	if err != nil {
		return err
	}
	if err := s.replaceDeletedReplyQuotes(ctx, guildID, replies); err != nil {
		return err
	}
	if err := forEachGroupEdit(ctx, s.store, guildID, links,
		func(link MessageLink) string { return link.GroupID },
		s.messageLinkTarget,
		func(ctx context.Context, groupID string, pending []editPair[MessageLink]) error {
			for _, p := range pending {
				for _, messageID := range append([]string{p.link.TargetMessageID}, p.link.ContinuationMessageIDs...) {
					if err := s.deleteWebhookMessageAt(p.target, p.link, messageID); err != nil && !isUnknownMessageError(err) {
						return err
					}
				}
			}
			return nil
		},
	); err != nil {
		return err
	}
	return s.store.DeleteMessageData(ctx, channelID, messageID, links)
}

func (s *Service) messageTargetsReplyingToCopies(ctx context.Context, sourceChannelID, sourceMessageID string, copies []MessageLink) ([]MessageLink, error) {
	refs := make([]messageRef, 0, len(copies)+1)
	refs = append(refs, messageRef{channelID: sourceChannelID, messageID: sourceMessageID})
	for _, copy := range copies {
		refs = append(refs, messageRef{channelID: copy.TargetChannelID, messageID: copy.TargetMessageID})
		for _, partID := range copy.ContinuationMessageIDs {
			refs = append(refs, messageRef{channelID: copy.TargetChannelID, messageID: partID})
		}
	}
	seen := make(map[string]bool)
	var replies []MessageLink
	for _, ref := range refs {
		links, err := s.store.MessageTargetsReplyingTo(ctx, ref.channelID, ref.messageID)
		if err != nil {
			return nil, err
		}
		for _, link := range links {
			key := link.SourceChannelID + "\x00" + link.SourceMessageID + "\x00" + link.TargetChannelID + "\x00" + link.TargetMessageID
			if !seen[key] {
				replies = append(replies, link)
				seen[key] = true
			}
		}
	}
	return replies, nil
}

func (s *Service) replaceDeletedReplyQuotes(ctx context.Context, guildID string, links []MessageLink) error {
	return forEachGroupEdit(ctx, s.store, guildID, links,
		func(link MessageLink) string { return link.GroupID },
		s.messageLinkTarget,
		func(ctx context.Context, groupID string, pending []editPair[MessageLink]) error {
			for _, p := range pending {
				rebuilt, err := s.requotedMirrorContent(p.link, deletedReplyQuoteLine(p.link.TargetLanguage))
				if err != nil {
					return err
				}
				primary, err := s.discord.Message(p.link.TargetChannelID, p.link.TargetMessageID)
				if err != nil {
					return fmt.Errorf("fetch reply mirror %s/%s: %w", p.link.TargetChannelID, p.link.TargetMessageID, err)
				}
				if splitMessageContent(string(rebuilt), discordMessageContentLimit)[0] == primary.Content {
					continue
				}
				previous, err := s.mirrorPartContents(ctx, p.link)
				if err != nil {
					return err
				}
				if err := s.replaceMirrorMessages(ctx, p.target, &p.link, rebuilt, previous); err != nil {
					return err
				}
			}
			return nil
		},
	)
}
