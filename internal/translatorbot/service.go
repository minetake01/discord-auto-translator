package translatorbot

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const translationHistoryLimit = 3
const translationHistoryMaxAge = 24 * time.Hour
const discordEpochMillis = 1420070400000

type Service struct {
	store         *Store
	discord       DiscordAPI
	translator    Translator
	httpClient    *http.Client
	publicBaseURL string
	threadMu      sync.Mutex
}

func NewService(store *Store, discord DiscordAPI, translator Translator) *Service {
	return &Service{store: store, discord: discord, translator: translator, httpClient: http.DefaultClient}
}

func (s *Service) SetPublicBaseURL(publicBaseURL string) {
	s.publicBaseURL = strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
}

func (s *Service) HandleMessageCreate(ctx context.Context, m DiscordMessage) error {
	if m.Bot || m.WebhookID != "" {
		return nil
	}
	if m.ThreadStarterMessage {
		_, err := s.ensureThreadSynced(ctx, m)
		return err
	}
	if m.ThreadSystemMessage || (strings.TrimSpace(m.Content) == "" && len(m.Attachments) == 0) {
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
	for _, source := range groups {
		channels, err := s.store.ChannelsInGroup(ctx, m.GuildID, source.GroupID)
		if err != nil {
			return err
		}
		for _, target := range channels {
			if target.ChannelID == m.ChannelID {
				continue
			}
			content, err := s.translateMessageContent(ctx, target.Language, m.Content, s.translationContext(ctx, m.GuildID, m.ChannelID, m.ChannelID, source.Language, m.ID))
			if err != nil {
				return err
			}
			quote, err := s.replyQuote(ctx, m, target.ChannelID, target.Language)
			if err != nil {
				return err
			}
			if quote != "" {
				content = quote + "\n" + content
			}
			files, err := s.attachmentFiles(ctx, m.Attachments)
			if err != nil {
				return err
			}
			avatar := AvatarWithLanguageBadge(ctx, s.publicBaseURL, m.AuthorAvatarURL, target.Language)
			msgID, err := s.discord.SendWebhook(target.WebhookID, target.WebhookToken, WebhookSend{
				Content: content, Username: m.AuthorDisplayName, AvatarURL: avatar, Files: files,
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
		content, err := s.translateMessageContent(ctx, target.Language, m.Content, s.translationContext(ctx, m.GuildID, thread.SourceChannelID, m.ChannelID, languageForChannel(targets, thread.SourceChannelID), m.ID))
		if err != nil {
			return err
		}
		quote, err := s.replyQuote(ctx, m, thread.TargetThreadID, target.Language)
		if err != nil {
			return err
		}
		if quote != "" {
			content = quote + "\n" + content
		}
		files, err := s.attachmentFiles(ctx, m.Attachments)
		if err != nil {
			return err
		}
		avatar := AvatarWithLanguageBadge(ctx, s.publicBaseURL, m.AuthorAvatarURL, target.Language)
		msgID, err := s.discord.SendWebhook(target.WebhookID, target.WebhookToken, WebhookSend{
			Content: content, Username: m.AuthorDisplayName, AvatarURL: avatar, ThreadID: thread.TargetThreadID, Files: files,
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
		translationContext := s.translationContext(ctx, m.GuildID, m.ChannelID, m.ChannelID, languageForChannel(targets, m.ChannelID), m.ID)
		content, err := s.translator.Translate(ctx, target.Language, m.Content, translationContext)
		if err != nil {
			return err
		}
		content = ReplaceAlternateURLs(ctx, content, target.Language, s.httpClient)
		if err := s.discord.EditWebhook(target.WebhookID, target.WebhookToken, link.TargetMessageID, threadIDForWebhook(link, target), content); err != nil {
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
	snippet, err := s.translator.Translate(ctx, targetLanguage, firstLine(content), TranslationContext{})
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
	_, err := s.syncThreadCreate(ctx, threadCreateRequest{
		GuildID:         guildID,
		SourceChannelID: sourceChannelID,
		SourceThreadID:  sourceThreadID,
		SourceMessageID: sourceThreadID,
		Name:            name,
	})
	return err
}

func (s *Service) SyncThreadCreateFromGateway(ctx context.Context, guildID, sourceChannelID, sourceThreadID, name string) error {
	_, err := s.syncThreadCreate(ctx, threadCreateRequest{
		GuildID:               guildID,
		SourceChannelID:       sourceChannelID,
		SourceThreadID:        sourceThreadID,
		SourceMessageID:       sourceThreadID,
		Name:                  name,
		DeferWithoutSourceMsg: true,
	})
	return err
}

type threadCreateRequest struct {
	GuildID                string
	SourceChannelID        string
	SourceThreadID         string
	SourceMessageID        string
	Name                   string
	InitialMessageID       string
	InitialMessageAuthor   string
	InitialMessageUsername string
	InitialMessageAvatar   string
	InitialMessageText     string
	InitialMessageFiles    []DiscordAttachment
	DeferWithoutSourceMsg  bool
}

func (s *Service) syncThreadCreate(ctx context.Context, req threadCreateRequest) (bool, error) {
	s.threadMu.Lock()
	defer s.threadMu.Unlock()

	groups, err := s.store.ChannelsByChannel(ctx, req.GuildID, req.SourceChannelID)
	if err != nil {
		return false, err
	}
	existing, err := s.store.SourceThreadTargets(ctx, req.SourceThreadID)
	if err != nil {
		return false, err
	}
	createdWithInitialMessage := false
	for _, source := range groups {
		channels, err := s.store.ChannelsInGroup(ctx, req.GuildID, source.GroupID)
		if err != nil {
			return false, err
		}
		for _, target := range channels {
			if target.ChannelID == source.ChannelID {
				continue
			}
			if existingThreadTarget(existing, source.GroupID, target.ChannelID) {
				continue
			}
			translationContext := s.translationContext(ctx, req.GuildID, req.SourceChannelID, req.SourceThreadID, source.Language, req.InitialMessageID)
			translatedName, err := s.translator.Translate(ctx, target.Language, req.Name, translationContext)
			if err != nil {
				return false, err
			}
			translatedInitial, err := s.translateMessageContent(ctx, target.Language, req.InitialMessageText, translationContext)
			if err != nil {
				return false, err
			}
			threadID, initialMessageID, err := s.createTargetThread(ctx, source.GroupID, req, target, translatedName, translatedInitial)
			if err != nil {
				return false, err
			}
			if threadID == "" {
				continue
			}
			err = s.store.SaveThreadLink(ctx, ThreadLink{
				GroupID: source.GroupID, SourceThreadID: req.SourceThreadID, SourceChannelID: req.SourceChannelID,
				TargetThreadID: threadID, TargetChannelID: target.ChannelID, TargetLanguage: target.Language,
			})
			if err != nil {
				return false, err
			}
			if req.InitialMessageID != "" && initialMessageID == "" && (translatedInitial != "" || len(req.InitialMessageFiles) > 0) {
				files, err := s.attachmentFiles(ctx, req.InitialMessageFiles)
				if err != nil {
					return false, err
				}
				avatar := AvatarWithLanguageBadge(ctx, s.publicBaseURL, req.InitialMessageAvatar, target.Language)
				initialMessageID, err = s.discord.SendWebhook(target.WebhookID, target.WebhookToken, WebhookSend{
					Content: translatedInitial, Username: req.InitialMessageUsername, AvatarURL: avatar, ThreadID: threadID, Files: files,
				})
				if err != nil {
					return false, err
				}
			}
			if req.InitialMessageID != "" && initialMessageID != "" {
				err = s.store.SaveMessageLink(ctx, MessageLink{
					SourceMessageID: req.InitialMessageID, SourceChannelID: req.SourceThreadID, GroupID: source.GroupID,
					TargetChannelID: threadID, TargetMessageID: initialMessageID, TargetLanguage: target.Language,
					SourceAuthorID: req.InitialMessageAuthor, SourceContentSnapshot: req.InitialMessageText,
				})
				if err != nil {
					return false, err
				}
				createdWithInitialMessage = true
			}
		}
	}
	return createdWithInitialMessage, nil
}

func existingThreadTarget(links []ThreadLink, groupID, targetChannelID string) bool {
	for _, link := range links {
		if link.GroupID == groupID && link.TargetChannelID == targetChannelID {
			return true
		}
	}
	return false
}

func (s *Service) ensureThreadSynced(ctx context.Context, m DiscordMessage) (bool, error) {
	if m.ParentChannelID == "" || m.ThreadName == "" {
		return false, nil
	}
	if existing, err := s.store.SourceThreadTargets(ctx, m.ChannelID); err != nil {
		return false, err
	} else if len(existing) > 0 {
		return false, nil
	}
	req := threadCreateRequest{
		GuildID:         m.GuildID,
		SourceChannelID: m.ParentChannelID,
		SourceThreadID:  m.ChannelID,
		Name:            m.ThreadName,
	}
	if m.ThreadStarterMessage {
		req.SourceMessageID = m.ReferencedMessageID
		req.DeferWithoutSourceMsg = true
	} else if isThreadOnlySourceMessage(ctx, s.store, m.GuildID, m.ParentChannelID, m.ID, m.ChannelID) {
		req.InitialMessageID = m.ID
		req.InitialMessageAuthor = m.AuthorID
		req.InitialMessageUsername = m.AuthorDisplayName
		req.InitialMessageAvatar = m.AuthorAvatarURL
		req.InitialMessageText = m.Content
		req.InitialMessageFiles = m.Attachments
	} else {
		req.SourceMessageID = m.ChannelID
	}
	return s.syncThreadCreate(ctx, req)
}

func (s *Service) translateMessageContent(ctx context.Context, targetLanguage, content string, translationContext TranslationContext) (string, error) {
	if strings.TrimSpace(content) == "" {
		return "", nil
	}
	translated, err := s.translator.Translate(ctx, targetLanguage, content, translationContext)
	if err != nil {
		return "", err
	}
	return ReplaceAlternateURLs(ctx, translated, targetLanguage, s.httpClient), nil
}

func (s *Service) attachmentFiles(ctx context.Context, attachments []DiscordAttachment) ([]WebhookFile, error) {
	files := make([]WebhookFile, 0, len(attachments))
	for _, attachment := range attachments {
		url := strings.TrimSpace(attachment.URL)
		if url == "" {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := s.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("download attachment %s: %s", url, resp.Status)
		}
		files = append(files, WebhookFile{
			Name:        attachmentFileName(attachment),
			ContentType: attachment.ContentType,
			Reader:      bytes.NewReader(body),
		})
	}
	return files, nil
}

func attachmentFileName(attachment DiscordAttachment) string {
	if name := filepath.Base(strings.TrimSpace(attachment.Filename)); name != "." && name != "/" && name != "\\" {
		return name
	}
	if u, err := url.Parse(strings.TrimSpace(attachment.URL)); err == nil {
		if name := filepath.Base(u.Path); name != "." && name != "/" && name != "\\" {
			return name
		}
	}
	return "attachment"
}

func (s *Service) SyncThreadUpdate(ctx context.Context, guildID, sourceThreadID, name string) error {
	threads, err := s.store.SourceThreadTargets(ctx, sourceThreadID)
	if err != nil {
		return err
	}
	for _, thread := range threads {
		targets, err := s.store.ChannelsInGroup(ctx, guildID, thread.GroupID)
		if err != nil {
			return err
		}
		target := findChannel(targets, thread.TargetChannelID)
		if target == nil {
			continue
		}
		translatedName, err := s.translator.Translate(ctx, target.Language, name, TranslationContext{})
		if err != nil {
			return err
		}
		if err := s.discord.EditThread(thread.TargetThreadID, translatedName); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) SyncThreadDelete(ctx context.Context, sourceThreadID string) error {
	threads, err := s.store.SourceThreadTargets(ctx, sourceThreadID)
	if err != nil {
		return err
	}
	for _, thread := range threads {
		if err := s.discord.DeleteThread(thread.TargetThreadID); err != nil {
			return err
		}
	}
	return s.store.DeleteThreadLinks(ctx, sourceThreadID)
}

func (s *Service) createTargetThread(ctx context.Context, groupID string, req threadCreateRequest, target GroupChannel, name, initialMessage string) (string, string, error) {
	if isThreadOnlyChannelType(target.ChannelType) {
		files, err := s.attachmentFiles(ctx, req.InitialMessageFiles)
		if err != nil {
			return "", "", err
		}
		if initialMessage == "" && len(files) == 0 {
			if req.DeferWithoutSourceMsg {
				return "", "", nil
			}
			initialMessage = name
		}
		return s.discord.CreateThread(target.ChannelID, target.ChannelType, name, initialMessage, files)
	}
	if req.SourceMessageID != "" {
		links, err := s.store.MessagePeers(ctx, req.SourceChannelID, req.SourceMessageID)
		if err != nil {
			return "", "", err
		}
		for _, link := range links {
			if link.GroupID == groupID && link.TargetChannelID == target.ChannelID {
				threadID, err := s.discord.CreateThreadFromMessage(target.ChannelID, link.TargetMessageID, name)
				return threadID, "", err
			}
		}
		if req.DeferWithoutSourceMsg {
			return "", "", nil
		}
	}
	threadID, _, err := s.discord.CreateThread(target.ChannelID, target.ChannelType, name, "", nil)
	return threadID, "", err
}

func (s *Service) translationContext(ctx context.Context, guildID, channelID, historyChannelID, sourceLanguage, excludeMessageID string) TranslationContext {
	translationContext := TranslationContext{
		ServerName: bestEffortString(func() (string, error) {
			return s.discord.GuildName(guildID)
		}),
		ServerDescription: bestEffortString(func() (string, error) {
			return s.discord.GuildDescription(guildID)
		}),
		ChannelName: bestEffortString(func() (string, error) {
			return s.discord.ChannelName(channelID)
		}),
		ChannelTopic: bestEffortString(func() (string, error) {
			return s.discord.ChannelTopic(channelID)
		}),
	}
	if historyChannelID == "" {
		return translationContext
	}
	links, err := s.store.RecentMessageHistory(ctx, historyChannelID, excludeMessageID, translationHistoryLimit)
	if err != nil {
		return translationContext
	}
	cutoff := time.Now().UTC().Add(-translationHistoryMaxAge)
	for _, link := range links {
		if strings.TrimSpace(link.SourceContentSnapshot) == "" {
			continue
		}
		if messageTime, ok := discordSnowflakeTime(link.SourceMessageID); ok && !messageTime.After(cutoff) {
			continue
		}
		translationContext.History = append(translationContext.History, ChatContextMessage{
			Author:   link.SourceAuthorID,
			Language: sourceLanguage,
			Content:  link.SourceContentSnapshot,
		})
	}
	return translationContext
}

func discordSnowflakeTime(id string) (time.Time, bool) {
	if len(id) < 17 {
		return time.Time{}, false
	}
	snowflake, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	timestampMillis := int64(snowflake>>22) + discordEpochMillis
	return time.UnixMilli(timestampMillis).UTC(), true
}

func bestEffortString(fn func() (string, error)) string {
	value, err := fn()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func findChannel(channels []GroupChannel, id string) *GroupChannel {
	for i := range channels {
		if channels[i].ChannelID == id {
			return &channels[i]
		}
	}
	return nil
}

func languageForChannel(channels []GroupChannel, id string) string {
	if channel := findChannel(channels, id); channel != nil {
		return channel.Language
	}
	return ""
}

func threadIDForWebhook(link MessageLink, target *GroupChannel) string {
	if target == nil || link.TargetChannelID == target.ChannelID {
		return ""
	}
	return link.TargetChannelID
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

func isThreadOnlySourceMessage(ctx context.Context, store *Store, guildID, parentChannelID, messageID, threadID string) bool {
	if messageID == "" || messageID != threadID {
		return false
	}
	groups, err := store.ChannelsByChannel(ctx, guildID, parentChannelID)
	if err != nil {
		return false
	}
	for _, group := range groups {
		if isThreadOnlyChannelType(group.ChannelType) {
			return true
		}
	}
	return false
}
