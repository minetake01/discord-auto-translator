package translatorbot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const translationHistoryLimit = 3
const translationHistoryMaxAge = 24 * time.Hour
const discordEpochMillis = 1420070400000
const discordMessageContentLimit = 2000

var errTranslationRateLimited = errors.New("translation rate limit exceeded")

type Service struct {
	store         *Store
	discord       DiscordAPI
	translator    Translator
	rateLimiter   *TokenRateLimiter
	httpClient    *http.Client
	publicBaseURL string
	threadMu      sync.Mutex
	messageLocks  sync.Map
}

func NewService(store *Store, discord DiscordAPI, translator Translator) *Service {
	return &Service{
		store:       store,
		discord:     discord,
		translator:  translator,
		rateLimiter: NewTokenRateLimiter(defaultRateLimitTokensPerMinute),
		httpClient:  http.DefaultClient,
	}
}

func (s *Service) SetRateLimiter(limiter *TokenRateLimiter) {
	s.rateLimiter = limiter
}

func (s *Service) postProcessContent(ctx context.Context, guildID, text, targetLanguage string) string {
	text = ReplaceAlternateURLs(ctx, text, targetLanguage, s.httpClient)
	return ReplaceDiscordRefs(ctx, s.store, guildID, text, targetLanguage)
}

func (s *Service) SetPublicBaseURL(publicBaseURL string) {
	s.publicBaseURL = strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
}

func messageLinkProcessedKey(sourceChannelID, sourceMessageID, targetChannelID string) string {
	return "msglink:" + sourceChannelID + ":" + sourceMessageID + ":" + targetChannelID
}

func (s *Service) lockMessage(channelID, messageID string) func() {
	key := channelID + "\x00" + messageID
	mu := &sync.Mutex{}
	actual, _ := s.messageLocks.LoadOrStore(key, mu)
	m := actual.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}

func (s *Service) targetAlreadySynced(ctx context.Context, sourceChannelID, sourceMessageID, targetChannelID string) (bool, error) {
	key := messageLinkProcessedKey(sourceChannelID, sourceMessageID, targetChannelID)
	if processed, err := s.store.IsEventProcessed(ctx, key); err != nil {
		return false, err
	} else if processed {
		return true, nil
	}
	links, err := s.store.MessageTargets(ctx, sourceChannelID, sourceMessageID)
	if err != nil {
		return false, err
	}
	for _, link := range links {
		if link.TargetChannelID == targetChannelID {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) sendAndSaveLink(ctx context.Context, target GroupChannel, threadID string, send WebhookSend, link MessageLink) error {
	msgID, err := s.discord.SendWebhook(target.WebhookID, target.WebhookToken, send)
	if err != nil {
		return err
	}
	link.TargetMessageID = msgID
	if err := s.store.SaveMessageLink(ctx, link); err != nil {
		_ = s.discord.DeleteWebhook(target.WebhookID, target.WebhookToken, msgID, threadID)
		return err
	}
	_, _ = s.store.MarkProcessed(ctx, messageLinkProcessedKey(link.SourceChannelID, link.SourceMessageID, link.TargetChannelID))
	return nil
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
	if m.ThreadSystemMessage || (strings.TrimSpace(m.Content) == "" && len(m.Attachments) == 0 && len(m.Stickers) == 0 && m.ReferencedMessageID == "") {
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
	var targets []GroupChannel
	targetLanguages := make([]string, 0, len(channels))
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
		targets = append(targets, target)
		targetLanguages = append(targetLanguages, target.Language)
	}
	if len(targets) == 0 {
		return nil
	}

	if strings.TrimSpace(m.Content) == "" {
		return s.mirrorEmptyContent(ctx, m, source, targets)
	}

	translations := make(map[string]string, len(targetLanguages))
	if hasTranslatableText(m.Content) {
		glossary, err := s.store.ListGlossaryEntries(ctx, m.GuildID)
		if err != nil {
			return err
		}
		translationContext := s.translationContext(ctx, m.GuildID, m.ChannelID, m.ChannelID, source.Language, m.ID)
		translationContext.StyleInstructions = s.groupStyleInstructions(ctx, m.GuildID, source.GroupID)
		if err := s.checkTranslationRateLimit(m.GuildID, targetLanguages, m.Content, translationContext, glossary); err != nil {
			if errors.Is(err, errTranslationRateLimited) {
				_ = s.discord.SendChannelMessage(m.ChannelID, "翻訳レート制限に達したため、このメッセージは翻訳されませんでした。")
				return nil
			}
			return err
		}

		result, err := s.translator.TranslateMulti(ctx, targetLanguages, m.Content, translationContext, glossary)
		if err != nil {
			_ = s.discord.SendChannelMessage(m.ChannelID, "翻訳に失敗したため、このメッセージはミラーリングされませんでした。")
			return err
		}
		s.recordTranslationUsage(m.GuildID, result.InputTokens, result.OutputTokens)
		translations = result.Translations
	} else {
		for _, language := range targetLanguages {
			translations[language] = m.Content
		}
	}

	var errs []error
	for _, target := range targets {
		content, ok := translations[target.Language]
		if !ok {
			_ = s.discord.SendChannelMessage(m.ChannelID, "翻訳に失敗したため、このメッセージはミラーリングされませんでした。")
			return fmt.Errorf("missing translation for %q", target.Language)
		}
		content = s.postProcessContent(ctx, m.GuildID, content, target.Language)
		quote, err := s.replyQuote(ctx, m, target.ChannelID, target.Language, s.groupStyleInstructions(ctx, m.GuildID, source.GroupID))
		if err != nil {
			errs = append(errs, fmt.Errorf("target %s: %w", target.ChannelID, err))
			continue
		}
		if quote != "" {
			content = quote + "\n" + content
		}
		content, err = messageContentWithAssetURLs(content, m.Attachments, m.Stickers)
		if err != nil {
			errs = append(errs, fmt.Errorf("target %s: %w", target.ChannelID, err))
			continue
		}
		avatar := AvatarWithLanguageBadge(ctx, s.publicBaseURL, m.AuthorAvatarURL, target.Language)
		err = s.sendAndSaveLink(ctx, target, "", WebhookSend{
			Content: content, Username: m.AuthorDisplayName, AvatarURL: avatar, TTS: m.TTS,
		}, MessageLink{
			SourceMessageID: m.ID, SourceChannelID: m.ChannelID, GroupID: source.GroupID,
			TargetChannelID: target.ChannelID, TargetLanguage: target.Language,
			SourceAuthorID: m.AuthorID, SourceAuthorDisplayName: m.AuthorDisplayName, SourceContentSnapshot: m.Content,
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("target %s: %w", target.ChannelID, err))
		}
	}
	return errors.Join(errs...)
}

func (s *Service) mirrorEmptyContent(ctx context.Context, m DiscordMessage, source GroupChannel, targets []GroupChannel) error {
	var errs []error
	for _, target := range targets {
		quote, err := s.replyQuote(ctx, m, target.ChannelID, target.Language, s.groupStyleInstructions(ctx, m.GuildID, source.GroupID))
		if err != nil {
			errs = append(errs, fmt.Errorf("target %s: %w", target.ChannelID, err))
			continue
		}
		content := ""
		if quote != "" {
			content = quote
		}
		content, err = messageContentWithAssetURLs(content, m.Attachments, m.Stickers)
		if err != nil {
			errs = append(errs, fmt.Errorf("target %s: %w", target.ChannelID, err))
			continue
		}
		avatar := AvatarWithLanguageBadge(ctx, s.publicBaseURL, m.AuthorAvatarURL, target.Language)
		err = s.sendAndSaveLink(ctx, target, "", WebhookSend{
			Content: content, Username: m.AuthorDisplayName, AvatarURL: avatar, TTS: m.TTS,
		}, MessageLink{
			SourceMessageID: m.ID, SourceChannelID: m.ChannelID, GroupID: source.GroupID,
			TargetChannelID: target.ChannelID, TargetLanguage: target.Language,
			SourceAuthorID: m.AuthorID, SourceAuthorDisplayName: m.AuthorDisplayName, SourceContentSnapshot: m.Content,
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("target %s: %w", target.ChannelID, err))
		}
	}
	return errors.Join(errs...)
}

func (s *Service) checkTranslationRateLimit(guildID string, targetLanguages []string, content string, translationContext TranslationContext, glossary []GlossaryEntry) error {
	if s.rateLimiter == nil {
		return nil
	}
	estimate := EstimateTranslationTokens(BuildMultiTranslationUserPrompt(targetLanguages, content, translationContext, glossary), "") + 200*len(targetLanguages)
	if !s.rateLimiter.Allow(guildID, estimate) {
		return errTranslationRateLimited
	}
	return nil
}

func (s *Service) recordTranslationUsage(guildID string, inputTokens, outputTokens int) {
	if s.rateLimiter != nil {
		s.rateLimiter.Record(guildID, inputTokens+outputTokens)
	}
}

func (s *Service) handleThreadMessageCreate(ctx context.Context, m DiscordMessage) error {
	threads, err := s.store.ThreadTargets(ctx, m.ChannelID)
	if err != nil {
		return err
	}
	var errs []error
	for _, thread := range threads {
		if err := s.mirrorThreadMessage(ctx, m, thread); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) mirrorThreadMessage(ctx context.Context, m DiscordMessage, thread ThreadLink) error {
	targets, err := s.store.ChannelsInGroup(ctx, m.GuildID, thread.GroupID)
	if err != nil {
		return err
	}
	target := findChannel(targets, thread.TargetChannelID)
	if target == nil {
		return nil
	}
	synced, err := s.targetAlreadySynced(ctx, m.ChannelID, m.ID, thread.TargetThreadID)
	if err != nil {
		return fmt.Errorf("thread target %s: %w", thread.TargetThreadID, err)
	}
	if synced {
		return nil
	}

	content := ""
	if strings.TrimSpace(m.Content) != "" && hasTranslatableText(m.Content) {
		glossary, err := s.store.ListGlossaryEntries(ctx, m.GuildID)
		if err != nil {
			return err
		}
		translationContext := s.translationContext(ctx, m.GuildID, thread.SourceChannelID, m.ChannelID, languageForChannel(targets, thread.SourceChannelID), m.ID)
		translationContext.StyleInstructions = s.groupStyleInstructions(ctx, m.GuildID, thread.GroupID)
		targetLanguages := []string{target.Language}
		if err := s.checkTranslationRateLimit(m.GuildID, targetLanguages, m.Content, translationContext, glossary); err != nil {
			if errors.Is(err, errTranslationRateLimited) {
				_ = s.discord.SendChannelMessage(m.ChannelID, "翻訳レート制限に達したため、このメッセージは翻訳されませんでした。")
				return nil
			}
			return err
		}
		result, err := s.translator.TranslateMulti(ctx, targetLanguages, m.Content, translationContext, glossary)
		if err != nil {
			_ = s.discord.SendChannelMessage(m.ChannelID, "翻訳に失敗したため、このメッセージはミラーリングされませんでした。")
			return err
		}
		s.recordTranslationUsage(m.GuildID, result.InputTokens, result.OutputTokens)
		translated, ok := result.Translations[target.Language]
		if !ok {
			_ = s.discord.SendChannelMessage(m.ChannelID, "翻訳に失敗したため、このメッセージはミラーリングされませんでした。")
			return fmt.Errorf("missing translation for %q", target.Language)
		}
		content = translated
	} else if strings.TrimSpace(m.Content) != "" {
		content = m.Content
	}
	content = s.postProcessContent(ctx, m.GuildID, content, target.Language)
	quote, err := s.replyQuote(ctx, m, thread.TargetThreadID, target.Language, s.groupStyleInstructions(ctx, m.GuildID, thread.GroupID))
	if err != nil {
		return fmt.Errorf("thread target %s: %w", thread.TargetThreadID, err)
	}
	if quote != "" {
		content = quote + "\n" + content
	}
	content, err = messageContentWithAssetURLs(content, m.Attachments, m.Stickers)
	if err != nil {
		return fmt.Errorf("thread target %s: %w", thread.TargetThreadID, err)
	}
	avatar := AvatarWithLanguageBadge(ctx, s.publicBaseURL, m.AuthorAvatarURL, target.Language)
	err = s.sendAndSaveLink(ctx, *target, thread.TargetThreadID, WebhookSend{
		Content: content, Username: m.AuthorDisplayName, AvatarURL: avatar, TTS: m.TTS, ThreadID: thread.TargetThreadID,
	}, MessageLink{
		SourceMessageID: m.ID, SourceChannelID: m.ChannelID, GroupID: thread.GroupID,
		TargetChannelID: thread.TargetThreadID, TargetLanguage: target.Language,
		SourceAuthorID: m.AuthorID, SourceAuthorDisplayName: m.AuthorDisplayName, SourceContentSnapshot: m.Content,
	})
	if err != nil {
		return fmt.Errorf("thread target %s: %w", thread.TargetThreadID, err)
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
		content := m.Content
		if hasTranslatableText(m.Content) {
			translationContext := s.translationContext(ctx, m.GuildID, m.ChannelID, m.ChannelID, languageForChannel(targets, m.ChannelID), m.ID)
			translationContext.StyleInstructions = s.groupStyleInstructions(ctx, m.GuildID, link.GroupID)
			glossary, err := s.store.ListGlossaryEntries(ctx, m.GuildID)
			if err != nil {
				return err
			}
			if err := s.checkTranslationRateLimit(m.GuildID, []string{target.Language}, m.Content, translationContext, glossary); err != nil {
				if errors.Is(err, errTranslationRateLimited) {
					continue
				}
				return err
			}
			result, err := s.translator.TranslateMulti(ctx, []string{target.Language}, m.Content, translationContext, glossary)
			if err != nil {
				return err
			}
			s.recordTranslationUsage(m.GuildID, result.InputTokens, result.OutputTokens)
			var ok bool
			content, ok = result.Translations[target.Language]
			if !ok {
				return fmt.Errorf("missing translation for %q", target.Language)
			}
		}
		content = s.postProcessContent(ctx, m.GuildID, content, target.Language)
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

func (s *Service) HandleMessagePinUpdate(ctx context.Context, channelID, messageID string, pinned bool) error {
	prevPinned, known, err := s.store.GetPinState(ctx, channelID, messageID)
	if err != nil {
		return err
	}
	if known && prevPinned == pinned {
		return nil
	}
	if err := s.SyncPin(ctx, channelID, messageID, pinned); err != nil {
		return err
	}
	return s.savePinStatesForPeers(ctx, channelID, messageID, pinned)
}

func (s *Service) savePinStatesForPeers(ctx context.Context, channelID, messageID string, pinned bool) error {
	if err := s.store.SavePinState(ctx, channelID, messageID, pinned); err != nil {
		return err
	}
	peers, err := s.store.MessagePeers(ctx, channelID, messageID)
	if err != nil {
		return err
	}
	for _, link := range peers {
		if link.TargetChannelID == channelID && link.TargetMessageID == messageID {
			if err := s.store.SavePinState(ctx, link.SourceChannelID, link.SourceMessageID, pinned); err != nil {
				return err
			}
			continue
		}
		if err := s.store.SavePinState(ctx, link.TargetChannelID, link.TargetMessageID, pinned); err != nil {
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

func (s *Service) SyncReaction(ctx context.Context, guildID, sourceChannelID, sourceMessageID, emoji string, add bool) error {
	links, err := s.store.MessagePeers(ctx, sourceChannelID, sourceMessageID)
	if err != nil {
		return err
	}
	for _, link := range links {
		if add {
			err = s.discord.AddReaction(link.TargetChannelID, link.TargetMessageID, emoji)
		} else {
			err = s.discord.RemoveOwnReaction(link.TargetChannelID, link.TargetMessageID, emoji)
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
	var errs []error
	for _, link := range links {
		if pinned {
			err = s.discord.PinMessage(link.TargetChannelID, link.TargetMessageID)
		} else {
			err = s.discord.UnpinMessage(link.TargetChannelID, link.TargetMessageID)
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) replyQuote(ctx context.Context, m DiscordMessage, targetChannelID, targetLanguage, styleInstructions string) (string, error) {
	if m.ReferencedMessageID == "" {
		return "", nil
	}
	authorID := m.ReferencedMessageAuthorID
	content := m.ReferencedMessageContent
	quoteChannelID := m.ReferencedMessageChannelID
	quoteMessageID := m.ReferencedMessageID
	if quoteChannelID == "" {
		quoteChannelID = m.ChannelID
	}

	dbAuthorID, dbContent, dbQuoteChannelID, dbQuoteMessageID, ok, err := s.store.MessageQuoteTarget(ctx, m.ChannelID, m.ReferencedMessageID, targetChannelID)
	if err != nil {
		return "", err
	}
	if ok {
		if dbQuoteChannelID != "" && dbQuoteMessageID != "" {
			quoteChannelID = dbQuoteChannelID
			quoteMessageID = dbQuoteMessageID
		}
		if authorID == "" {
			authorID = dbAuthorID
		}
		if strings.TrimSpace(content) == "" {
			content = dbContent
		}
	}
	if strings.TrimSpace(content) == "" {
		return "", nil
	}
	snippetSource := firstLine(content)
	snippet := snippetSource
	if hasTranslatableText(content) {
		var err error
		snippet, err = s.translateSnippet(ctx, m.GuildID, targetLanguage, snippetSource, styleInstructions)
		if err != nil {
			return "", err
		}
	} else {
		snippet = s.postProcessContent(ctx, m.GuildID, snippetSource, targetLanguage)
	}
	snippet = firstLine(snippet)
	if len([]rune(snippet)) > 20 {
		snippet = string([]rune(snippet)[:20]) + "..."
	}
	link := MessageJumpURL(m.GuildID, quoteChannelID, quoteMessageID)
	prefix := fmt.Sprintf("> %s\n-# [original message](%s)", snippet, link)
	if m.MentionAuthor && authorID != "" {
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
	InitialMessageStickers []DiscordSticker
	InitialMessageTTS      bool
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
			targetLanguages := []string{target.Language}
			glossary, err := s.store.ListGlossaryEntries(ctx, req.GuildID)
			if err != nil {
				return false, err
			}
			translationContext := s.translationContext(ctx, req.GuildID, req.SourceChannelID, req.SourceThreadID, source.Language, req.InitialMessageID)
			translationContext.StyleInstructions = s.groupStyleInstructions(ctx, req.GuildID, source.GroupID)
			if err := s.checkTranslationRateLimit(req.GuildID, targetLanguages, req.Name, translationContext, glossary); err != nil {
				return false, err
			}
			nameResult, err := s.translator.TranslateMulti(ctx, targetLanguages, req.Name, translationContext, glossary)
			if err != nil {
				return false, err
			}
			s.recordTranslationUsage(req.GuildID, nameResult.InputTokens, nameResult.OutputTokens)
			translatedName, ok := nameResult.Translations[target.Language]
			if !ok {
				return false, fmt.Errorf("missing translation for %q", target.Language)
			}
			translatedInitial := ""
			if strings.TrimSpace(req.InitialMessageText) != "" && hasTranslatableText(req.InitialMessageText) {
				if err := s.checkTranslationRateLimit(req.GuildID, targetLanguages, req.InitialMessageText, translationContext, glossary); err != nil {
					return false, err
				}
				initialResult, err := s.translator.TranslateMulti(ctx, targetLanguages, req.InitialMessageText, translationContext, glossary)
				if err != nil {
					return false, err
				}
				s.recordTranslationUsage(req.GuildID, initialResult.InputTokens, initialResult.OutputTokens)
				translatedInitial, ok = initialResult.Translations[target.Language]
				if !ok {
					return false, fmt.Errorf("missing translation for %q", target.Language)
				}
			} else if strings.TrimSpace(req.InitialMessageText) != "" {
				translatedInitial = req.InitialMessageText
			}
			translatedInitial = s.postProcessContent(ctx, req.GuildID, translatedInitial, target.Language)
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
			if req.InitialMessageID != "" && initialMessageID == "" && (translatedInitial != "" || len(req.InitialMessageFiles) > 0 || len(req.InitialMessageStickers) > 0) {
				synced, err := s.targetAlreadySynced(ctx, req.SourceThreadID, req.InitialMessageID, threadID)
				if err != nil {
					return false, err
				}
				if !synced {
					content, err := messageContentWithAssetURLs(translatedInitial, req.InitialMessageFiles, req.InitialMessageStickers)
					if err != nil {
						return false, err
					}
					avatar := AvatarWithLanguageBadge(ctx, s.publicBaseURL, req.InitialMessageAvatar, target.Language)
					if err := s.sendAndSaveLink(ctx, target, threadID, WebhookSend{
						Content: content, Username: req.InitialMessageUsername, AvatarURL: avatar, TTS: req.InitialMessageTTS, ThreadID: threadID,
					}, MessageLink{
						SourceMessageID: req.InitialMessageID, SourceChannelID: req.SourceThreadID, GroupID: source.GroupID,
						TargetChannelID: threadID, TargetLanguage: target.Language,
						SourceAuthorID: req.InitialMessageAuthor, SourceAuthorDisplayName: req.InitialMessageUsername, SourceContentSnapshot: req.InitialMessageText,
					}); err != nil {
						return false, err
					}
				}
				createdWithInitialMessage = true
			}
			if req.InitialMessageID != "" && initialMessageID != "" {
				synced, err := s.targetAlreadySynced(ctx, req.SourceThreadID, req.InitialMessageID, threadID)
				if err != nil {
					return false, err
				}
				if !synced {
					if err := s.store.SaveMessageLink(ctx, MessageLink{
						SourceMessageID: req.InitialMessageID, SourceChannelID: req.SourceThreadID, GroupID: source.GroupID,
						TargetChannelID: threadID, TargetMessageID: initialMessageID, TargetLanguage: target.Language,
						SourceAuthorID: req.InitialMessageAuthor, SourceAuthorDisplayName: req.InitialMessageUsername, SourceContentSnapshot: req.InitialMessageText,
					}); err != nil {
						return false, err
					}
					_, _ = s.store.MarkProcessed(ctx, messageLinkProcessedKey(req.SourceThreadID, req.InitialMessageID, threadID))
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
		req.InitialMessageStickers = m.Stickers
		req.InitialMessageTTS = m.TTS
	} else {
		req.SourceMessageID = m.ChannelID
	}
	return s.syncThreadCreate(ctx, req)
}

func (s *Service) translateMessageContent(ctx context.Context, guildID, targetLanguage, content string, translationContext TranslationContext) (string, error) {
	if strings.TrimSpace(content) == "" {
		return "", nil
	}
	if !hasTranslatableText(content) {
		return s.postProcessContent(ctx, guildID, content, targetLanguage), nil
	}
	glossary, err := s.store.ListGlossaryEntries(ctx, guildID)
	if err != nil {
		return "", err
	}
	result, err := s.translator.TranslateMulti(ctx, []string{targetLanguage}, content, translationContext, glossary)
	if err != nil {
		return "", err
	}
	translated, ok := result.Translations[targetLanguage]
	if !ok {
		return "", fmt.Errorf("missing translation for %q", targetLanguage)
	}
	return s.postProcessContent(ctx, guildID, translated, targetLanguage), nil
}

func (s *Service) translateSnippet(ctx context.Context, guildID, targetLanguage, content, styleInstructions string) (string, error) {
	if strings.TrimSpace(content) == "" {
		return "", nil
	}
	if !hasTranslatableText(content) {
		return s.postProcessContent(ctx, guildID, content, targetLanguage), nil
	}
	glossary, err := s.store.ListGlossaryEntries(ctx, guildID)
	if err != nil {
		return "", err
	}
	translationContext := TranslationContext{StyleInstructions: styleInstructions}
	if err := s.checkTranslationRateLimit(guildID, []string{targetLanguage}, content, translationContext, glossary); err != nil {
		if errors.Is(err, errTranslationRateLimited) {
			return content, nil
		}
		return "", err
	}
	result, err := s.translator.TranslateMulti(ctx, []string{targetLanguage}, content, translationContext, glossary)
	if err != nil {
		return "", err
	}
	s.recordTranslationUsage(guildID, result.InputTokens, result.OutputTokens)
	translated, ok := result.Translations[targetLanguage]
	if !ok {
		return "", fmt.Errorf("missing translation for %q", targetLanguage)
	}
	return s.postProcessContent(ctx, guildID, translated, targetLanguage), nil
}

const (
	stickerFormatPNG    = 1
	stickerFormatAPNG   = 2
	stickerFormatLottie = 3
	stickerFormatGIF    = 4
)

func stickerAssetURL(sticker DiscordSticker) string {
	switch sticker.FormatType {
	case stickerFormatGIF:
		return fmt.Sprintf("https://media.discordapp.net/stickers/%s.gif", sticker.ID)
	default:
		return fmt.Sprintf("https://cdn.discordapp.com/stickers/%s.png", sticker.ID)
	}
}

func messageContentWithAssetURLs(content string, attachments []DiscordAttachment, stickers []DiscordSticker) (string, error) {
	assetURLs := make([]string, 0, len(attachments)+len(stickers))
	for _, attachment := range attachments {
		unsignedURL, err := unsignedAssetURL(attachment.URL)
		if err != nil {
			return "", fmt.Errorf("attachment %q: %w", attachmentFileName(attachment), err)
		}
		assetURLs = append(assetURLs, unsignedURL)
	}
	for _, sticker := range stickers {
		if strings.TrimSpace(sticker.ID) == "" {
			return "", errors.New("sticker ID is required")
		}
		assetURLs = append(assetURLs, stickerAssetURL(sticker))
	}
	if len(assetURLs) > 0 {
		if strings.TrimSpace(content) != "" {
			content += "\n"
		}
		content += strings.Join(assetURLs, "\n")
	}
	if utf8.RuneCountInString(content) > discordMessageContentLimit {
		return "", fmt.Errorf("message content has %d characters; Discord limit is %d", utf8.RuneCountInString(content), discordMessageContentLimit)
	}
	return content, nil
}

func unsignedAssetURL(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("invalid HTTP URL %q", rawURL)
	}
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	return u.String(), nil
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
		glossary, err := s.store.ListGlossaryEntries(ctx, guildID)
		if err != nil {
			return err
		}
		translationContext := TranslationContext{StyleInstructions: s.groupStyleInstructions(ctx, guildID, thread.GroupID)}
		if err := s.checkTranslationRateLimit(guildID, []string{target.Language}, name, translationContext, glossary); err != nil {
			if errors.Is(err, errTranslationRateLimited) {
				continue
			}
			return err
		}
		result, err := s.translator.TranslateMulti(ctx, []string{target.Language}, name, translationContext, glossary)
		if err != nil {
			return err
		}
		s.recordTranslationUsage(guildID, result.InputTokens, result.OutputTokens)
		translatedName, ok := result.Translations[target.Language]
		if !ok {
			return fmt.Errorf("missing translation for %q", target.Language)
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
		content, err := messageContentWithAssetURLs(initialMessage, req.InitialMessageFiles, req.InitialMessageStickers)
		if err != nil {
			return "", "", err
		}
		if content == "" {
			if req.DeferWithoutSourceMsg {
				return "", "", nil
			}
			content = name
		}
		return s.discord.CreateThread(target.ChannelID, target.ChannelType, name, content)
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
	threadID, _, err := s.discord.CreateThread(target.ChannelID, target.ChannelType, name, "")
	return threadID, "", err
}

func (s *Service) groupStyleInstructions(ctx context.Context, guildID, groupID string) string {
	preset, custom, err := s.store.GroupStyle(ctx, guildID, groupID)
	if err != nil {
		return ""
	}
	return ResolveStyleInstructions(preset, custom)
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
			Author:   strings.TrimSpace(link.SourceAuthorDisplayName),
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
