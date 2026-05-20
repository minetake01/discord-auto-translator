package translatorbot

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

type Service struct {
	store      *Store
	discord    DiscordAPI
	translator Translator
	httpClient *http.Client
}

func NewService(store *Store, discord DiscordAPI, translator Translator) *Service {
	return &Service{store: store, discord: discord, translator: translator, httpClient: http.DefaultClient}
}

func (s *Service) HandleMessageCreate(ctx context.Context, m DiscordMessage) error {
	if m.Bot || m.WebhookID != "" || m.ThreadSystemMessage || strings.TrimSpace(m.Content) == "" {
		return nil
	}
	if err := s.handleThreadMessageCreate(ctx, m); err != nil {
		return err
	}
	groups, err := s.store.ChannelsByChannel(ctx, m.GuildID, m.ChannelID)
	if err != nil {
		return err
	}
	for _, source := range groups {
		channels, err := s.store.ChannelsInGroup(ctx, m.GuildID, source.GroupID)
		if err != nil {
			return err
		}
		for _, target := range channels {
			if target.ChannelID == m.ChannelID {
				continue
			}
			content, err := s.translator.Translate(ctx, target.Language, m.Content, nil)
			if err != nil {
				return err
			}
			content = ReplaceAlternateURLs(ctx, content, target.Language, s.httpClient)
			quote, err := s.replyQuote(ctx, m, target.ChannelID, target.Language)
			if err != nil {
				return err
			}
			if quote != "" {
				content = quote + "\n" + content
			}
			avatar := AvatarWithLanguageBadge(ctx, m.AuthorAvatarURL, target.Language)
			msgID, err := s.discord.SendWebhook(target.WebhookID, target.WebhookToken, WebhookSend{
				Content: content, Username: m.AuthorDisplayName, AvatarURL: avatar,
			})
			if err != nil {
				return err
			}
			err = s.store.SaveMessageLink(ctx, MessageLink{
				SourceMessageID: m.ID, SourceChannelID: m.ChannelID, GroupID: source.GroupID,
				TargetChannelID: target.ChannelID, TargetMessageID: msgID, TargetLanguage: target.Language,
				SourceAuthorID: m.AuthorID, SourceContentSnapshot: m.Content,
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) handleThreadMessageCreate(ctx context.Context, m DiscordMessage) error {
	threads, err := s.store.ThreadTargets(ctx, m.ChannelID)
	if err != nil {
		return err
	}
	for _, thread := range threads {
		targets, err := s.store.ChannelsInGroup(ctx, m.GuildID, thread.GroupID)
		if err != nil {
			return err
		}
		target := findChannel(targets, thread.TargetChannelID)
		if target == nil {
			continue
		}
		content, err := s.translator.Translate(ctx, target.Language, m.Content, nil)
		if err != nil {
			return err
		}
		content = ReplaceAlternateURLs(ctx, content, target.Language, s.httpClient)
		quote, err := s.replyQuote(ctx, m, thread.TargetThreadID, target.Language)
		if err != nil {
			return err
		}
		if quote != "" {
			content = quote + "\n" + content
		}
		avatar := AvatarWithLanguageBadge(ctx, m.AuthorAvatarURL, target.Language)
		msgID, err := s.discord.SendWebhook(target.WebhookID, target.WebhookToken, WebhookSend{
			Content: content, Username: m.AuthorDisplayName, AvatarURL: avatar, ThreadID: thread.TargetThreadID,
		})
		if err != nil {
			return err
		}
		err = s.store.SaveMessageLink(ctx, MessageLink{
			SourceMessageID: m.ID, SourceChannelID: m.ChannelID, GroupID: thread.GroupID,
			TargetChannelID: thread.TargetThreadID, TargetMessageID: msgID, TargetLanguage: target.Language,
			SourceAuthorID: m.AuthorID, SourceContentSnapshot: m.Content,
		})
		if err != nil {
			return err
		}
	}
	return nil
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
		content, err := s.translator.Translate(ctx, target.Language, m.Content, nil)
		if err != nil {
			return err
		}
		if err := s.discord.EditWebhook(target.WebhookID, target.WebhookToken, link.TargetMessageID, content); err != nil {
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
		if err := s.discord.DeleteWebhook(target.WebhookID, target.WebhookToken, link.TargetMessageID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) SyncReaction(ctx context.Context, guildID, sourceChannelID, sourceMessageID, emoji, userID string, add bool) error {
	links, err := s.store.MessagePeers(ctx, sourceChannelID, sourceMessageID)
	if err != nil {
		return err
	}
	for _, link := range links {
		if add {
			err = s.discord.AddReaction(link.TargetChannelID, link.TargetMessageID, emoji)
		} else {
			err = s.discord.RemoveReaction(link.TargetChannelID, link.TargetMessageID, emoji, userID)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) SyncPin(ctx context.Context, sourceChannelID, sourceMessageID string, pinned bool) error {
	links, err := s.store.MessagePeers(ctx, sourceChannelID, sourceMessageID)
	if err != nil {
		return err
	}
	for _, link := range links {
		if pinned {
			err = s.discord.PinMessage(link.TargetChannelID, link.TargetMessageID)
		} else {
			err = s.discord.UnpinMessage(link.TargetChannelID, link.TargetMessageID)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) replyQuote(ctx context.Context, m DiscordMessage, targetChannelID, targetLanguage string) (string, error) {
	if m.ReferencedMessageID == "" {
		return "", nil
	}
	authorID, content, quoteChannelID, quoteMessageID, ok, err := s.store.MessageQuoteTarget(ctx, m.ChannelID, m.ReferencedMessageID, targetChannelID)
	if err != nil || !ok {
		return "", err
	}
	snippet, err := s.translator.Translate(ctx, targetLanguage, firstLine(content), nil)
	if err != nil {
		return "", err
	}
	snippet = firstLine(snippet)
	if len([]rune(snippet)) > 20 {
		snippet = string([]rune(snippet)[:20]) + "..."
	}
	link := fmt.Sprintf("https://discord.com/channels/%s/%s/%s", m.GuildID, quoteChannelID, quoteMessageID)
	prefix := fmt.Sprintf("> %s\n-# [original message](%s)", snippet, link)
	if m.MentionAuthor {
		prefix = fmt.Sprintf("<@%s>\n%s", authorID, prefix)
	}
	return prefix, nil
}

func (s *Service) SyncThreadCreate(ctx context.Context, guildID, sourceChannelID, sourceThreadID, name string) error {
	groups, err := s.store.ChannelsByChannel(ctx, guildID, sourceChannelID)
	if err != nil {
		return err
	}
	for _, source := range groups {
		channels, err := s.store.ChannelsInGroup(ctx, guildID, source.GroupID)
		if err != nil {
			return err
		}
		for _, target := range channels {
			if target.ChannelID == sourceChannelID {
				continue
			}
			translatedName, err := s.translator.Translate(ctx, target.Language, name, nil)
			if err != nil {
				return err
			}
			threadID, err := s.createTargetThread(ctx, source.GroupID, sourceChannelID, sourceThreadID, target.ChannelID, translatedName)
			if err != nil {
				return err
			}
			err = s.store.SaveThreadLink(ctx, ThreadLink{
				GroupID: source.GroupID, SourceThreadID: sourceThreadID, SourceChannelID: sourceChannelID,
				TargetThreadID: threadID, TargetChannelID: target.ChannelID, TargetLanguage: target.Language,
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) createTargetThread(ctx context.Context, groupID, sourceChannelID, sourceThreadID, targetChannelID, name string) (string, error) {
	links, err := s.store.MessagePeers(ctx, sourceChannelID, sourceThreadID)
	if err != nil {
		return "", err
	}
	for _, link := range links {
		if link.GroupID == groupID && link.TargetChannelID == targetChannelID {
			return s.discord.CreateThreadFromMessage(targetChannelID, link.TargetMessageID, name)
		}
	}
	return s.discord.CreateThread(targetChannelID, name)
}

func findChannel(channels []GroupChannel, id string) *GroupChannel {
	for i := range channels {
		if channels[i].ChannelID == id {
			return &channels[i]
		}
	}
	return nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}
