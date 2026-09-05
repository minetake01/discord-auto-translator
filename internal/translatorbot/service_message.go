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
// channel so the jump link stays within that channel.
func (s *Service) replyQuote(ctx context.Context, m DiscordMessage, targetChannelID, targetLanguage string) (string, error) {
	if m.ReferencedMessageID == "" {
		return "", nil
	}
	content := m.ReferencedMessageContent
	quoteChannelID := m.ReferencedMessageChannelID
	quoteMessageID := m.ReferencedMessageID
	if quoteChannelID == "" {
		quoteChannelID = m.ChannelID
	}

	dbOriginalContent, dbQuoteChannelID, dbQuoteMessageID, ok, err := s.store.MessageQuoteTarget(ctx, m.ChannelID, m.ReferencedMessageID, targetChannelID)
	if err != nil {
		return "", err
	}
	if ok {
		if dbQuoteChannelID != "" && dbQuoteMessageID != "" {
			quoteChannelID = dbQuoteChannelID
			quoteMessageID = dbQuoteMessageID
			if transferred, fetchErr := s.discord.Message(quoteChannelID, quoteMessageID); fetchErr == nil && strings.TrimSpace(transferred.Content) != "" {
				content = transferred.Content
			} else {
				content = dbOriginalContent
			}
		} else {
			content = dbOriginalContent
		}
	}
	snippet := firstLineWithoutPseudoReply(content)
	if snippet == "" {
		return "", nil
	}
	snippet = normalizeMarkdownHeaderSnippet(snippet)
	snippet = truncateRunes(snippet, replyQuoteMaxRunes, "...")
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
	if m.ThreadSystemMessage || (strings.TrimSpace(m.Content) == "" && len(m.Attachments) == 0 && len(m.Stickers) == 0 && m.ReferencedMessageID == "" && m.ForwardedMessage == nil && m.Poll == nil && m.PollResult == nil) {
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
		return s.translationContextForMessage(ctx, m, source.GroupID, m.ChannelID, m.ChannelID, "")
	}
	return s.mirrorMessage(ctx, m, source.GroupID, source.Language, contextFn, dests)
}

// mirrorMessage translates a source message once and sends it to every
// destination, handling forwarded messages, reply quotes, and asset URLs.
// Translation failures are reported to the source channel in its language.
func (s *Service) mirrorMessage(ctx context.Context, m DiscordMessage, groupID, sourceLanguage string, contextFn func() TranslationContext, dests []mirrorDestination) error {
	if m.ForwardedMessage != nil {
		return s.mirrorForwardedMessage(ctx, m, groupID, sourceLanguage, contextFn, dests)
	}
	if m.PollResult != nil {
		return s.mirrorPollResultMessage(ctx, m, groupID, dests)
	}
	if m.Poll != nil {
		return s.mirrorPollMessage(ctx, m, groupID, sourceLanguage, contextFn, dests)
	}

	languages := destinationLanguages(dests)
	loaded := s.loadImageAttachments(ctx, imageAttachmentsOnly(m.Attachments))
	altCh := s.startAttachmentAltTranslation(ctx, m.GuildID, loaded, languages, contextFn)
	translations, err := s.translateWithLimit(ctx, m.GuildID, m.Content, loaded, languages, contextFn)
	if err != nil {
		s.notifyTranslationIssue(m.ChannelID, m.ID, sourceLanguage, err)
		if errors.Is(err, errTranslationRateLimited) {
			return nil
		}
		return err
	}
	alts, pendingAlts := takeReadyAttachmentAlts(altCh)

	var posted []postedWebhookMirror
	var errs []error
	for _, dest := range dests {
		content := s.postProcessContent(ctx, m.GuildID, translations.Translations[dest.channel.Language], dest.channel.Language)
		quote, err := s.replyQuote(ctx, m, dest.targetID, dest.channel.Language)
		if err != nil {
			errs = append(errs, fmt.Errorf("target %s: %w", dest.targetID, err))
			continue
		}
		content = withQuote(quote, content)
		content, err = messageContentWithLoadedImages(content, m.Attachments, m.Stickers, loaded)
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

// sendMirror posts the prepared content to one destination and records the
// message link with the given source snapshot.
func (s *Service) sendMirror(ctx context.Context, m DiscordMessage, groupID string, dest mirrorDestination, content string, embeds []*discordgo.MessageEmbed, files []WebhookFile, snapshot string) (WebhookSendResult, error) {
	avatar := AvatarWithLanguageBadge(ctx, s.publicBaseURL, m.AuthorAvatarURL, dest.channel.Language, m.AuthorRoleColor)
	ref := MessageReference{MessageID: m.ReferencedMessageID, ChannelID: m.ReferencedMessageChannelID}
	if ref.MessageID != "" && ref.ChannelID == "" {
		ref.ChannelID = m.ChannelID
	}
	return s.sendAndSaveLink(ctx, dest.channel, dest.threadID(), WebhookSend{
		Content: content, Username: m.AuthorDisplayName, AvatarURL: avatar, TTS: m.TTS, ThreadID: dest.threadID(), Embeds: embeds, Files: files,
	}, MessageLink{
		SourceMessageID: m.ID, SourceChannelID: m.ChannelID, GroupID: groupID,
		TargetChannelID: dest.targetID, TargetLanguage: dest.channel.Language,
		SourceAuthorID: m.AuthorID, SourceAuthorDisplayName: m.AuthorDisplayName, SourceContentSnapshot: snapshot,
		SourceImageAttachments: sourceImageAttachments(m),
	}, ref)
}

func (s *Service) mirrorPollMessage(ctx context.Context, m DiscordMessage, groupID, sourceLanguage string, contextFn func() TranslationContext, dests []mirrorDestination) error {
	languages := destinationLanguages(dests)
	question := strings.TrimSpace(m.Poll.Question)
	answers := pollAnswerTexts(m.Poll)
	snapshot := formatPollSnapshot(m.Poll)
	if c := strings.TrimSpace(m.Content); c != "" {
		if snapshot != "" {
			snapshot = c + "\n\n" + snapshot
		} else {
			snapshot = c
		}
	}

	contentTranslations, err := s.translateWithLimit(ctx, m.GuildID, m.Content, nil, languages, contextFn)
	if err != nil {
		s.notifyTranslationIssue(m.ChannelID, m.ID, sourceLanguage, err)
		if errors.Is(err, errTranslationRateLimited) {
			return nil
		}
		return err
	}
	pollTranslations, err := s.translatePollWithLimit(ctx, m.GuildID, question, answers, languages, contextFn)
	if err != nil {
		s.notifyTranslationIssue(m.ChannelID, m.ID, sourceLanguage, err)
		if errors.Is(err, errTranslationRateLimited) {
			return nil
		}
		return err
	}

	var errs []error
	for _, dest := range dests {
		content := s.postProcessContent(ctx, m.GuildID, contentTranslations.Translations[dest.channel.Language], dest.channel.Language)
		quote, err := s.replyQuote(ctx, m, dest.targetID, dest.channel.Language)
		if err != nil {
			errs = append(errs, fmt.Errorf("target %s: %w", dest.targetID, err))
			continue
		}
		content = withQuote(quote, content)
		content, err = messageContentWithAllAssetURLs(content, m.Attachments, m.Stickers)
		if err != nil {
			errs = append(errs, fmt.Errorf("target %s: %w", dest.targetID, err))
			continue
		}
		content = withPollStartedHeader(content, dest.channel.Language, m.GuildID, m.ChannelID, m.ID, true)
		poll := pollTranslations[dest.channel.Language]
		translatedQuestion := s.postProcessContent(ctx, m.GuildID, poll.Question, dest.channel.Language)
		translatedAnswers := make([]string, len(poll.Answers))
		for i, answer := range poll.Answers {
			translatedAnswers[i] = s.postProcessContent(ctx, m.GuildID, answer, dest.channel.Language)
		}
		embed := buildPollEmbed(translatedQuestion, formatTranslatedPollAnswers(m.Poll, translatedAnswers), m.AuthorRoleColor)
		var embeds []*discordgo.MessageEmbed
		if embed != nil {
			embeds = []*discordgo.MessageEmbed{embed}
		}
		if _, err := s.sendMirror(ctx, m, groupID, dest, content, embeds, nil, snapshot); err != nil {
			errs = append(errs, fmt.Errorf("target %s: %w", dest.targetID, err))
		}
	}
	if m.Poll.Expiry != nil && !m.Poll.Expiry.IsZero() {
		for language, poll := range pollTranslations {
			answers := make([]string, len(poll.Answers))
			copy(answers, poll.Answers)
			if err := s.store.SavePollTranslationCache(ctx, m.ChannelID, m.ID, language, answers, *m.Poll.Expiry); err != nil {
				errs = append(errs, fmt.Errorf("poll translation cache %s: %w", language, err))
			}
		}
	}
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
		body := pollResultBody(dest.channel.Language, m.PollResult, victorLabel)
		quote, err := s.replyQuote(ctx, m, dest.targetID, dest.channel.Language)
		if err != nil {
			errs = append(errs, fmt.Errorf("target %s: %w", dest.targetID, err))
			continue
		}
		content := withQuote(quote, body)
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

type pendingMessageEdit struct {
	link   MessageLink
	target GroupChannel
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
	if !allowed {
		return nil
	}
	links, err := s.store.MessageTargets(ctx, m.ChannelID, m.ID)
	if err != nil {
		return err
	}
	byGroup := make(map[string][]MessageLink)
	for _, link := range links {
		if link.SourceContentSnapshot == m.Content {
			continue
		}
		byGroup[link.GroupID] = append(byGroup[link.GroupID], link)
	}
	for groupID, groupLinks := range byGroup {
		targets, err := s.store.ChannelsInGroup(ctx, m.GuildID, groupID)
		if err != nil {
			return err
		}
		pending := make([]pendingMessageEdit, 0, len(groupLinks))
		for _, link := range groupLinks {
			target, err := s.messageLinkTarget(ctx, targets, link)
			if err != nil {
				return err
			}
			if target == nil {
				continue
			}
			pending = append(pending, pendingMessageEdit{link: link, target: *target})
		}
		if len(pending) == 0 {
			continue
		}
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
			return s.translationContextForMessage(ctx, m, groupID, contextChannelID, historyChannelID, threadName)
		}
		languages := make([]string, 0, len(pending))
		for _, p := range pending {
			languages = append(languages, p.target.Language)
		}
		translations, err := s.translateWithLimit(ctx, m.GuildID, m.Content, nil, languages, contextFn)
		if err != nil {
			if errors.Is(err, errTranslationRateLimited) {
				continue
			}
			return err
		}
		for _, p := range pending {
			content := s.postProcessContent(ctx, m.GuildID, translations.Translations[p.target.Language], p.target.Language)
			content, err = messageContentWithAssetURLs(content, m.Attachments, m.Stickers)
			if err != nil {
				return err
			}
			if err := s.discord.EditWebhook(p.target.WebhookID, p.target.WebhookToken, p.link.TargetMessageID, threadIDForWebhook(p.link, &p.target), content); err != nil {
				return err
			}
			if err := s.store.UpdateMessageLinkSnapshot(ctx, p.link.SourceChannelID, p.link.SourceMessageID, p.link.TargetChannelID, m.Content); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) HandleMessageDelete(ctx context.Context, guildID, channelID, messageID string) error {
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

	byGroup := make(map[string][]MessageLink)
	for _, link := range links {
		byGroup[link.GroupID] = append(byGroup[link.GroupID], link)
	}
	for groupID, groupLinks := range byGroup {
		targets, err := s.store.ChannelsInGroup(ctx, guildID, groupID)
		if err != nil {
			return err
		}
		for _, link := range groupLinks {
			target, err := s.messageLinkTarget(ctx, targets, link)
			if err != nil {
				return err
			}
			if target == nil {
				continue
			}
			if err := s.discord.DeleteWebhook(target.WebhookID, target.WebhookToken, link.TargetMessageID, threadIDForWebhook(link, target)); err != nil {
				return err
			}
		}
	}
	return s.store.DeleteMessageData(ctx, channelID, messageID, links)
}

func (s *Service) messageTargetsReplyingToCopies(ctx context.Context, sourceChannelID, sourceMessageID string, copies []MessageLink) ([]MessageLink, error) {
	refs := make([]messageRef, 0, len(copies)+1)
	refs = append(refs, messageRef{channelID: sourceChannelID, messageID: sourceMessageID})
	for _, copy := range copies {
		refs = append(refs, messageRef{channelID: copy.TargetChannelID, messageID: copy.TargetMessageID})
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
	byGroup := make(map[string][]MessageLink)
	for _, link := range links {
		byGroup[link.GroupID] = append(byGroup[link.GroupID], link)
	}
	for groupID, groupLinks := range byGroup {
		targets, err := s.store.ChannelsInGroup(ctx, guildID, groupID)
		if err != nil {
			return err
		}
		for _, link := range groupLinks {
			target, err := s.messageLinkTarget(ctx, targets, link)
			if err != nil {
				return err
			}
			if target == nil {
				continue
			}
			message, err := s.discord.Message(link.TargetChannelID, link.TargetMessageID)
			if err != nil {
				return fmt.Errorf("fetch reply mirror %s/%s: %w", link.TargetChannelID, link.TargetMessageID, err)
			}
			content := withQuote(
				fmt.Sprintf("> -# %s", localizedUIString(link.TargetLanguage, uiKeyOriginalMessageDeleted)),
				mirroredMessageBody(message.Content),
			)
			if err := s.discord.EditWebhook(target.WebhookID, target.WebhookToken, link.TargetMessageID, threadIDForWebhook(link, target), content); err != nil {
				return err
			}
		}
	}
	return nil
}
