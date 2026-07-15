package translatorbot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const translationHistoryLimit = 3
const translationHistoryMaxAge = 24 * time.Hour
const translationReplyChainLimit = 3

const mergeShortMessageMaxRunes = 60
const mergeMaxCombinedRunes = 150
const mergeMaxCount = 4
const mergeMaxInterval = 5 * time.Minute

var errTranslationRateLimited = errors.New("translation rate limit exceeded")

// Service implements the mirroring pipeline: it receives normalized Discord
// events, translates content through the Translator, and fans the result out
// to every peer channel of a translation group via webhooks.
type Service struct {
	store         *Store
	discord       DiscordAPI
	translator    Translator
	rateLimiter   *TokenRateLimiter
	alternateURLs *alternateURLReplacer
	publicBaseURL string
	selfBotUserID string
	threadMu      sync.Mutex
	messageLocks  sync.Map
}

func NewService(store *Store, discord DiscordAPI, translator Translator) *Service {
	return &Service{
		store:         store,
		discord:       discord,
		translator:    translator,
		rateLimiter:   NewTokenRateLimiter(defaultRateLimitTokensPerMinute),
		alternateURLs: newAlternateURLReplacer(http.DefaultClient, alternateURLDomainCacheTTL, time.Now),
	}
}

func (s *Service) SetRateLimiter(limiter *TokenRateLimiter) {
	s.rateLimiter = limiter
}

func (s *Service) SetPublicBaseURL(publicBaseURL string) {
	s.publicBaseURL = strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
}

func (s *Service) SetSelfBotUserID(selfBotUserID string) {
	s.selfBotUserID = selfBotUserID
}

// shouldProcessMessage is the single source policy for create and update.
// Human messages do not depend on SQLite. Automated sources fail closed when
// their guild-scoped allowlist lookup cannot be completed.
func (s *Service) shouldProcessMessage(ctx context.Context, m DiscordMessage) (bool, error) {
	if s.selfBotUserID != "" && m.AuthorID == s.selfBotUserID {
		return false, nil
	}
	if m.WebhookID != "" {
		return s.store.IsMessageSourceAllowed(ctx, m.GuildID, SourceTypeWebhook, m.WebhookID)
	}
	if !m.Bot {
		return true, nil
	}
	return s.store.IsMessageSourceAllowed(ctx, m.GuildID, SourceTypeBot, m.AuthorID)
}

// postProcessContent applies target-language link rewriting to translated
// content: hreflang alternate URLs first, then managed Discord references.
func (s *Service) postProcessContent(ctx context.Context, guildID, text, targetLanguage string) string {
	text = s.alternateURLs.Replace(ctx, text, targetLanguage)
	return ReplaceDiscordRefs(ctx, s.store, guildID, text, targetLanguage)
}

// notifyTranslationIssue posts a localized notice to the source channel when
// a message could not be mirrored. The language is the source channel's
// registered language, since that is where the notice is shown.
func (s *Service) notifyTranslationIssue(channelID, language string, err error) {
	key := uiKeyTranslationFailedNotice
	if errors.Is(err, errTranslationRateLimited) {
		key = uiKeyRateLimitNotice
	}
	_ = s.discord.SendChannelMessage(channelID, localizedUIString(language, key))
}

// translateWithLimit translates content into every requested language while
// enforcing the per-guild token rate limit. Content without translatable text
// is returned as-is for every language without calling the translator, in
// which case contextFn is never invoked (it may perform Discord API calls).
// Returns errTranslationRateLimited when the guild is over budget.
func (s *Service) translateWithLimit(ctx context.Context, guildID, content string, languages []string, contextFn func() TranslationContext) (map[string]string, error) {
	translations := make(map[string]string, len(languages))
	if strings.TrimSpace(content) == "" || !hasTranslatableText(content) {
		for _, language := range languages {
			translations[language] = content
		}
		return translations, nil
	}
	glossary, err := s.store.ListGlossaryEntries(ctx, guildID)
	if err != nil {
		return nil, err
	}
	translationContext := contextFn()
	if err := s.checkTranslationRateLimit(guildID, languages, content, translationContext, glossary); err != nil {
		return nil, err
	}
	result, err := s.translator.TranslateMulti(ctx, languages, content, translationContext, glossary)
	if err != nil {
		return nil, err
	}
	s.recordTranslationUsage(guildID, result.InputTokens, result.OutputTokens)
	for _, language := range languages {
		translated, ok := result.Translations[language]
		if !ok {
			return nil, fmt.Errorf("missing translation for %q", language)
		}
		translations[language] = translated
	}
	return translations, nil
}

func (s *Service) checkTranslationRateLimit(guildID string, targetLanguages []string, content string, translationContext TranslationContext, glossary []GlossaryEntry) error {
	if s.rateLimiter == nil {
		return nil
	}
	systemInstruction := BuildMultiTranslationSystemInstruction(content, glossary, len(translationContext.History) > 0, len(translationContext.ReplyChain) > 0, strings.TrimSpace(translationContext.StyleInstructions) != "")
	userPrompt := BuildMultiTranslationUserPrompt(targetLanguages, content, translationContext)
	estimate := EstimateTranslationTokens(systemInstruction+userPrompt, "") + 200*len(targetLanguages)
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

// groupTranslationContext gathers server/channel context, recent history, reply
// chain, and the group's style instructions for a translation request.
func (s *Service) groupTranslationContext(ctx context.Context, guildID, groupID, contextChannelID, historyChannelID, sourceLanguage, excludeMessageID, replyChannelID, replyMessageID, author, threadName string) TranslationContext {
	channelIDs := s.conversationLocations(ctx, guildID, groupID, historyChannelID, sourceLanguage)
	replyChain, replyKeys := s.replyChainContext(ctx, replyChannelID, replyMessageID)
	translationContext := s.translationContext(ctx, guildID, contextChannelID, channelIDs, excludeMessageID, replyKeys)
	translationContext.ReplyChain = replyChain
	translationContext.StyleInstructions = s.groupStyleInstructions(ctx, guildID, groupID)
	translationContext.Author = strings.TrimSpace(author)
	translationContext.ThreadName = strings.TrimSpace(threadName)
	return translationContext
}

func (s *Service) resolveThreadName(m DiscordMessage) string {
	if name := strings.TrimSpace(m.ThreadName); name != "" {
		return name
	}
	return bestEffortString(func() (string, error) {
		return s.discord.ChannelName(m.ChannelID)
	})
}

func (s *Service) groupStyleInstructions(ctx context.Context, guildID, groupID string) string {
	preset, custom, err := s.store.GroupStyle(ctx, guildID, groupID)
	if err != nil {
		return ""
	}
	return ResolveStyleInstructions(preset, custom)
}

func (s *Service) conversationLocations(ctx context.Context, guildID, groupID, historyChannelID, sourceLanguage string) []string {
	channels, err := s.store.ChannelsInGroup(ctx, guildID, groupID)
	if err != nil {
		return nil
	}
	if findChannel(channels, historyChannelID) != nil {
		channelIDs := make([]string, len(channels))
		for i, ch := range channels {
			channelIDs[i] = ch.ChannelID
		}
		return channelIDs
	}
	if historyChannelID == "" {
		return nil
	}
	channelIDs := []string{historyChannelID}
	threads, err := s.store.ThreadTargets(ctx, historyChannelID)
	if err != nil {
		return channelIDs
	}
	seen := map[string]bool{historyChannelID: true}
	for _, thread := range threads {
		if thread.SourceThreadID != "" && !seen[thread.SourceThreadID] {
			seen[thread.SourceThreadID] = true
			channelIDs = append(channelIDs, thread.SourceThreadID)
		}
		if thread.TargetThreadID != "" && !seen[thread.TargetThreadID] {
			seen[thread.TargetThreadID] = true
			channelIDs = append(channelIDs, thread.TargetThreadID)
		}
	}
	return channelIDs
}

func (s *Service) translationContext(ctx context.Context, guildID, channelID string, historyChannelIDs []string, excludeMessageID string, excludeReplyKeys map[string]bool) TranslationContext {
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
	if len(historyChannelIDs) == 0 {
		return translationContext
	}
	links, err := s.store.RecentMessageHistory(ctx, historyChannelIDs, excludeMessageID, translationHistoryLimit*mergeMaxCount)
	if err != nil {
		return translationContext
	}
	cutoff := time.Now().UTC().Add(-translationHistoryMaxAge)
	translationContext.History = mergeConsecutiveMessages(links, cutoff, excludeReplyKeys)
	return translationContext
}

type historyMergeSlot struct {
	author   string
	content  string
	lastTime time.Time
	count    int
}

func mergeConsecutiveMessages(links []MessageLink, cutoff time.Time, excludeReplyKeys map[string]bool) []ChatContextMessage {
	slots := make([]historyMergeSlot, 0, len(links))
	for _, link := range links {
		if excludeReplyKeys != nil && excludeReplyKeys[messageRefKey(link.SourceChannelID, link.SourceMessageID)] {
			continue
		}
		content := link.SourceContentSnapshot
		if strings.TrimSpace(content) == "" {
			continue
		}
		messageTime, hasTime := discordSnowflakeTime(link.SourceMessageID)
		if hasTime && !messageTime.After(cutoff) {
			continue
		}
		author := strings.TrimSpace(link.SourceAuthorDisplayName)
		contentRunes := len([]rune(content))
		if len(slots) > 0 {
			last := &slots[len(slots)-1]
			combinedRunes := len([]rune(last.content)) + 1 + contentRunes
			if last.author == author &&
				contentRunes <= mergeShortMessageMaxRunes &&
				combinedRunes <= mergeMaxCombinedRunes &&
				last.count < mergeMaxCount &&
				hasTime &&
				!last.lastTime.IsZero() &&
				messageTime.Sub(last.lastTime) <= mergeMaxInterval {
				last.content += "\n" + content
				if hasTime {
					last.lastTime = messageTime
				}
				last.count++
				continue
			}
		}
		slot := historyMergeSlot{
			author:  author,
			content: content,
			count:   1,
		}
		if hasTime {
			slot.lastTime = messageTime
		}
		slots = append(slots, slot)
	}
	limit := translationHistoryLimit
	if len(slots) < limit {
		limit = len(slots)
	}
	out := make([]ChatContextMessage, 0, limit)
	start := len(slots) - limit
	for i := start; i < len(slots); i++ {
		out = append(out, ChatContextMessage{
			Author:  slots[i].author,
			Content: slots[i].content,
		})
	}
	return out
}

type messageRef struct {
	channelID string
	messageID string
}

func messageRefKey(channelID, messageID string) string {
	return channelID + "\x00" + messageID
}

func (s *Service) replyChainContext(ctx context.Context, refChannelID, refMessageID string) ([]ChatContextMessage, map[string]bool) {
	sourceKeys := make(map[string]bool)
	if refMessageID == "" || refChannelID == "" {
		return nil, sourceKeys
	}
	collected := make([]ChatContextMessage, 0, translationReplyChainLimit)
	currentChannelID := refChannelID
	currentMessageID := refMessageID
	for len(collected) < translationReplyChainLimit {
		entry, sourceChannelID, sourceMessageID, nextRef, ok := s.resolveReplyChainEntry(ctx, currentChannelID, currentMessageID)
		if !ok {
			break
		}
		collected = append(collected, entry)
		sourceKeys[messageRefKey(sourceChannelID, sourceMessageID)] = true
		if nextRef.messageID == "" {
			break
		}
		currentChannelID = nextRef.channelID
		currentMessageID = nextRef.messageID
		if currentChannelID == "" {
			currentChannelID = sourceChannelID
		}
	}
	for i, j := 0, len(collected)-1; i < j; i, j = i+1, j-1 {
		collected[i], collected[j] = collected[j], collected[i]
	}
	return collected, sourceKeys
}

func (s *Service) resolveReplyChainEntry(ctx context.Context, channelID, messageID string) (entry ChatContextMessage, sourceChannelID, sourceMessageID string, nextRef messageRef, ok bool) {
	original, tracked, err := s.store.MessageOriginal(ctx, channelID, messageID)
	if err != nil {
		return entry, "", "", nextRef, false
	}
	fetchChannelID := channelID
	fetchMessageID := messageID
	if tracked {
		sourceChannelID = original.SourceChannelID
		sourceMessageID = original.SourceMessageID
		fetchChannelID = sourceChannelID
		fetchMessageID = sourceMessageID
		entry.Content = original.Snapshot
		entry.Author = strings.TrimSpace(original.SourceAuthorDisplayName)
	}
	fetched, fetchErr := s.discord.Message(fetchChannelID, fetchMessageID)
	if fetchErr != nil {
		if !tracked {
			return entry, "", "", nextRef, false
		}
		return entry, sourceChannelID, sourceMessageID, nextRef, strings.TrimSpace(entry.Content) != ""
	}
	if !tracked {
		entry.Content = fetched.Content
		entry.Author = strings.TrimSpace(fetched.AuthorDisplayName)
		sourceChannelID = channelID
		sourceMessageID = messageID
	} else if entry.Author == "" {
		entry.Author = strings.TrimSpace(fetched.AuthorDisplayName)
	}
	nextRef = messageRef{
		channelID: fetched.ReferencedChannelID,
		messageID: fetched.ReferencedMessageID,
	}
	if nextRef.channelID == "" && nextRef.messageID != "" {
		nextRef.channelID = fetchChannelID
	}
	return entry, sourceChannelID, sourceMessageID, nextRef, strings.TrimSpace(entry.Content) != ""
}

// lockMessage serializes concurrent handling of the same (channel, message).
func (s *Service) lockMessage(channelID, messageID string) func() {
	key := channelID + "\x00" + messageID
	mu := &sync.Mutex{}
	actual, _ := s.messageLocks.LoadOrStore(key, mu)
	m := actual.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}

func messageLinkProcessedKey(sourceChannelID, sourceMessageID, targetChannelID string) string {
	return "msglink:" + sourceChannelID + ":" + sourceMessageID + ":" + targetChannelID
}

// targetAlreadySynced reports whether a source message already has a mirror
// in the target channel, checking both processed-event markers and links.
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

// sendAndSaveLink posts a webhook message and persists its link. When the
// link cannot be saved, the just-posted message is deleted as compensation.
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
