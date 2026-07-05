package translatorbot

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

// threadID returns the thread_id webhook parameter, empty for channel sends.
func (d mirrorDestination) threadID() string {
	if d.targetID == d.channel.ChannelID {
		return ""
	}
	return d.targetID
}

func (s *Service) HandleMessageCreate(ctx context.Context, m DiscordMessage) error {
	if m.Bot || m.WebhookID != "" {
		return nil
	}
	unlock := s.lockMessage(m.ChannelID, m.ID)
	defer unlock()

	if m.ThreadStarterMessage {
		_, err := s.ensureThreadSynced(ctx, m)
		return err
	}
	if m.ThreadSystemMessage || (strings.TrimSpace(m.Content) == "" && len(m.Attachments) == 0 && len(m.Stickers) == 0 && m.ReferencedMessageID == "" && m.ForwardedMessage == nil) {
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
		return s.groupTranslationContext(ctx, m.GuildID, source.GroupID, m.ChannelID, m.ChannelID, source.Language, m.ID)
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

	languages := make([]string, 0, len(dests))
	for _, dest := range dests {
		languages = append(languages, dest.channel.Language)
	}
	translations, err := s.translateWithLimit(ctx, m.GuildID, m.Content, languages, contextFn)
	if err != nil {
		s.notifyTranslationIssue(m.ChannelID, sourceLanguage, err)
		if errors.Is(err, errTranslationRateLimited) {
			return nil
		}
		return err
	}

	var errs []error
	for _, dest := range dests {
		content := s.postProcessContent(ctx, m.GuildID, translations[dest.channel.Language], dest.channel.Language)
		quote, err := s.replyQuote(ctx, m, dest.targetID, dest.channel.Language)
		if err != nil {
			errs = append(errs, fmt.Errorf("target %s: %w", dest.targetID, err))
			continue
		}
		switch {
		case quote != "" && content != "":
			content = quote + "\n\n" + content
		case quote != "":
			content = quote
		}
		content, err = messageContentWithAssetURLs(content, m.Attachments, m.Stickers)
		if err != nil {
			errs = append(errs, fmt.Errorf("target %s: %w", dest.targetID, err))
			continue
		}
		if err := s.sendMirror(ctx, m, groupID, dest, content, m.Content); err != nil {
			errs = append(errs, fmt.Errorf("target %s: %w", dest.targetID, err))
		}
	}
	return errors.Join(errs...)
}

// sendMirror posts the prepared content to one destination and records the
// message link with the given source snapshot.
func (s *Service) sendMirror(ctx context.Context, m DiscordMessage, groupID string, dest mirrorDestination, content, snapshot string) error {
	avatar := AvatarWithLanguageBadge(ctx, s.publicBaseURL, m.AuthorAvatarURL, dest.channel.Language, m.AuthorRoleColor)
	return s.sendAndSaveLink(ctx, dest.channel, dest.threadID(), WebhookSend{
		Content: content, Username: m.AuthorDisplayName, AvatarURL: avatar, TTS: m.TTS, ThreadID: dest.threadID(),
	}, MessageLink{
		SourceMessageID: m.ID, SourceChannelID: m.ChannelID, GroupID: groupID,
		TargetChannelID: dest.targetID, TargetLanguage: dest.channel.Language,
		SourceAuthorID: m.AuthorID, SourceAuthorDisplayName: m.AuthorDisplayName, SourceContentSnapshot: snapshot,
	})
}

func (s *Service) HandleMessageUpdate(ctx context.Context, m DiscordMessage) error {
	if m.Bot || m.WebhookID != "" {
		return nil
	}
	links, err := s.store.MessageTargets(ctx, m.ChannelID, m.ID)
	if err != nil {
		return err
	}
	for _, link := range links {
		if link.SourceContentSnapshot == m.Content {
			continue
		}
		targets, err := s.store.ChannelsInGroup(ctx, m.GuildID, link.GroupID)
		if err != nil {
			return err
		}
		target := findChannel(targets, link.TargetChannelID)
		if target == nil {
			if parentID, ok, err := s.store.ThreadParentChannel(ctx, link.GroupID, link.TargetChannelID); err != nil {
				return err
			} else if ok {
				target = findChannel(targets, parentID)
			}
		}
		if target == nil {
			continue
		}
		contextFn := func() TranslationContext {
			return s.groupTranslationContext(ctx, m.GuildID, link.GroupID, m.ChannelID, m.ChannelID, languageForChannel(targets, m.ChannelID), m.ID)
		}
		translations, err := s.translateWithLimit(ctx, m.GuildID, m.Content, []string{target.Language}, contextFn)
		if err != nil {
			if errors.Is(err, errTranslationRateLimited) {
				continue
			}
			return err
		}
		content := s.postProcessContent(ctx, m.GuildID, translations[target.Language], target.Language)
		content, err = messageContentWithAssetURLs(content, m.Attachments, m.Stickers)
		if err != nil {
			return err
		}
		if err := s.discord.EditWebhook(target.WebhookID, target.WebhookToken, link.TargetMessageID, threadIDForWebhook(link, target), content); err != nil {
			return err
		}
		if err := s.store.UpdateMessageLinkSnapshot(ctx, link.SourceChannelID, link.SourceMessageID, link.TargetChannelID, m.Content); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) HandleMessageDelete(ctx context.Context, guildID, channelID, messageID string) error {
	links, err := s.store.MessageTargets(ctx, channelID, messageID)
	if err != nil {
		return err
	}
	for _, link := range links {
		targets, err := s.store.ChannelsInGroup(ctx, guildID, link.GroupID)
		if err != nil {
			return err
		}
		target := findChannel(targets, link.TargetChannelID)
		if target == nil {
			if parentID, ok, err := s.store.ThreadParentChannel(ctx, link.GroupID, link.TargetChannelID); err != nil {
				return err
			} else if ok {
				target = findChannel(targets, parentID)
			}
		}
		if target == nil {
			continue
		}
		if err := s.discord.DeleteWebhook(target.WebhookID, target.WebhookToken, link.TargetMessageID, threadIDForWebhook(link, target)); err != nil {
			return err
		}
	}
	return nil
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
			if transferredContent, fetchErr := s.discord.MessageContent(quoteChannelID, quoteMessageID); fetchErr == nil && strings.TrimSpace(transferredContent) != "" {
				content = transferredContent
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
	snippet = truncateRunes(snippet, 40, "...")
	link := MessageJumpURL(m.GuildID, quoteChannelID, quoteMessageID)
	label := localizedUIString(targetLanguage, uiKeyOriginalMessage)
	return fmt.Sprintf("> %s · [%s](%s)", snippet, label, link), nil
}
