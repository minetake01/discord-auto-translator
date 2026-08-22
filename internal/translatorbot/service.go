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

const historyIdleGap = 15 * time.Minute
const historyCountHigh = 16
const historyCountLow = 8
const historySpanHigh = 30 * time.Minute
const historySpanLow = 15 * time.Minute
const historyTokenHigh = 800
const historyTokenLow = 400
const historyFetchLimit = 512
const translationReplyChainLimit = 3
const topicSummaryTimeout = 2 * time.Minute
const topicSummarySourceTokenLimit = 1200

const mergeShortMessageMaxRunes = 60
const mergeMaxCombinedRunes = 150
const mergeMaxCount = 4
const mergeMaxInterval = 5 * time.Minute

var errTranslationRateLimited = errors.New("translation rate limit exceeded")

// Service implements the mirroring pipeline: it receives normalized Discord
// events, translates content through the Translator, and fans the result out
// to every peer channel of a translation group via webhooks.
type Service struct {
	store                *Store
	discord              DiscordAPI
	translator           Translator
	rateLimiter          *TokenRateLimiter
	urlPages             *urlPageCache
	httpClient           *http.Client
	publicBaseURL        string
	selfBotUserID        string
	threadMu             sync.Mutex
	messageLocks         sync.Map
	topicSummaryAttempts sync.Map
	runTopicSummary      func(func())
	issueNotices         issueNoticeState
}

func NewService(store *Store, discord DiscordAPI, translator Translator) *Service {
	return &Service{
		store:       store,
		discord:     discord,
		translator:  translator,
		rateLimiter: NewTokenRateLimiter(defaultRateLimitTokensPerMinute),
		urlPages:    newURLPageCache(http.DefaultClient, urlPageCacheTTL, time.Now),
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
// content: hreflang URLs from the page cache first, then managed Discord references.
func (s *Service) postProcessContent(ctx context.Context, guildID, text, targetLanguage string) string {
	text = s.urlPages.Replace(ctx, text, targetLanguage)
	return ReplaceDiscordRefs(ctx, s.store, guildID, text, targetLanguage)
}

type issueNoticeKind string

const (
	issueNoticeProvider  issueNoticeKind = "provider"
	issueNoticeRateLimit issueNoticeKind = "rate_limit"
)

// issueNoticeState suppresses repeat source-channel notices while the same
// provider outage or guild token rate limit continues. Each kind is tracked
// separately so one condition does not hide the other.
type issueNoticeState struct {
	mu       sync.Mutex
	notified map[issueNoticeKind]map[string]struct{}
}

func issueNoticeKindFor(err error) (issueNoticeKind, bool) {
	switch {
	case errors.Is(err, errTranslationProvider):
		return issueNoticeProvider, true
	case errors.Is(err, errTranslationRateLimited):
		return issueNoticeRateLimit, true
	default:
		return "", false
	}
}

func (p *issueNoticeState) allow(channelID string, err error) bool {
	kind, suppress := issueNoticeKindFor(err)
	if !suppress {
		return true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.notified == nil {
		p.notified = make(map[issueNoticeKind]map[string]struct{})
	}
	channels := p.notified[kind]
	if channels == nil {
		channels = make(map[string]struct{})
		p.notified[kind] = channels
	}
	if _, already := channels[channelID]; already {
		return false
	}
	channels[channelID] = struct{}{}
	return true
}

func (p *issueNoticeState) clear(kind issueNoticeKind) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.notified, kind)
}

// notifyTranslationIssue posts a localized notice as a reply to the source
// message when it could not be mirrored. The language is the source channel's
// registered language, since that is where the notice is shown. Provider
// outages and token rate limits notify each source channel once until that
// condition clears.
func (s *Service) notifyTranslationIssue(channelID, messageID, language string, err error) {
	if !s.issueNotices.allow(channelID, err) {
		return
	}
	key := uiKeyTranslationFailedNotice
	if errors.Is(err, errTranslationRateLimited) {
		key = uiKeyRateLimitNotice
	}
	content := localizedUIString(language, key)
	if errors.Is(err, errTranslationProvider) {
		content = content + " " + localizedUIString(language, uiKeyProviderMayContinueNotice)
	}
	_ = s.discord.SendChannelMessage(channelID, messageID, content)
}

// translateWithLimit translates content and existing image alt text into every
// requested language while enforcing the per-guild token rate limit.
// Messages without translatable text or existing image alt text are returned
// as-is without calling the translator, in which case contextFn is never invoked.
// Returns errTranslationRateLimited when the guild is over budget.
func (s *Service) translateWithLimit(ctx context.Context, guildID, content string, loaded []loadedImageAttachment, languages []string, contextFn func() TranslationContext) (MultiTranslationResult, error) {
	if !needsTranslation(content, attachmentsFromLoaded(loaded)) {
		translations := make(map[string]string, len(languages))
		for _, language := range languages {
			translations[language] = content
		}
		return MultiTranslationResult{Translations: translations}, nil
	}
	prepared, err := s.prepareTranslation(ctx, guildID, content, languages, contextFn, func(langs []string, tc TranslationContext, glossary []GlossaryEntry) (preparedTranslation, error) {
		tc.Attachments = translationAttachmentsFromLoaded(loaded)
		return prepareMultiTranslation(langs, content, tc, glossary)
	}, visionFromLoaded(loaded))
	if err != nil {
		return MultiTranslationResult{}, err
	}
	result, err := s.translator.TranslateMulti(ctx, prepared)
	if err != nil {
		return MultiTranslationResult{}, err
	}
	s.issueNotices.clear(issueNoticeProvider)
	s.recordTranslationUsage(guildID, result.InputTokens, result.OutputTokens)
	for _, language := range languages {
		if _, ok := result.Translations[language]; !ok {
			return MultiTranslationResult{}, fmt.Errorf("missing translation for %q", language)
		}
		if len(loaded) > 0 && len(result.AttachmentDescriptions[language]) != len(loaded) {
			return MultiTranslationResult{}, fmt.Errorf("missing attachment descriptions for %q", language)
		}
	}
	return result, nil
}

func (s *Service) translatePollWithLimit(ctx context.Context, guildID, question string, answers []string, languages []string, contextFn func() TranslationContext) (map[string]PollTranslation, error) {
	translations := make(map[string]PollTranslation, len(languages))
	needsTranslation := hasTranslatableText(question)
	if !needsTranslation {
		for _, answer := range answers {
			if hasTranslatableText(answer) {
				needsTranslation = true
				break
			}
		}
	}
	if !needsTranslation {
		for _, language := range languages {
			copied := make([]string, len(answers))
			copy(copied, answers)
			translations[language] = PollTranslation{Question: question, Answers: copied}
		}
		return translations, nil
	}
	pollText := question
	for _, answer := range answers {
		pollText += "\n" + answer
	}
	prepared, err := s.prepareTranslation(ctx, guildID, pollText, languages, contextFn, func(langs []string, tc TranslationContext, glossary []GlossaryEntry) (preparedTranslation, error) {
		return preparePollTranslation(langs, question, answers, tc, glossary)
	}, nil)
	if err != nil {
		return nil, err
	}
	result, err := s.translator.TranslatePollMulti(ctx, prepared)
	if err != nil {
		return nil, err
	}
	s.issueNotices.clear(issueNoticeProvider)
	s.recordTranslationUsage(guildID, result.InputTokens, result.OutputTokens)
	for _, language := range languages {
		translated, ok := result.Translations[language]
		if !ok {
			return nil, fmt.Errorf("missing poll translation for %q", language)
		}
		translations[language] = translated
	}
	return translations, nil
}

// translateThreadCreateWithLimit translates a thread name and optional initial
// message in one translator call. When neither field has translatable text,
// both are returned as-is without calling the translator.
func (s *Service) translateThreadCreateWithLimit(ctx context.Context, guildID, name, message string, languages []string, contextFn func() TranslationContext) (map[string]ThreadCreateTranslation, error) {
	translations := make(map[string]ThreadCreateTranslation, len(languages))
	if !hasTranslatableText(name) && !hasTranslatableText(message) {
		for _, language := range languages {
			translations[language] = ThreadCreateTranslation{Name: name, Message: message}
		}
		return translations, nil
	}
	lookupText := name
	if strings.TrimSpace(message) != "" {
		lookupText += "\n" + message
	}
	prepared, err := s.prepareTranslation(ctx, guildID, lookupText, languages, contextFn, func(langs []string, tc TranslationContext, glossary []GlossaryEntry) (preparedTranslation, error) {
		return prepareThreadCreateTranslation(langs, name, message, tc, glossary)
	}, nil)
	if err != nil {
		return nil, err
	}
	result, err := s.translator.TranslateThreadCreateMulti(ctx, prepared)
	if err != nil {
		return nil, err
	}
	s.issueNotices.clear(issueNoticeProvider)
	s.recordTranslationUsage(guildID, result.InputTokens, result.OutputTokens)
	for _, language := range languages {
		translated, ok := result.Translations[language]
		if !ok {
			return nil, fmt.Errorf("missing thread create translation for %q", language)
		}
		translations[language] = translated
	}
	return translations, nil
}

func visionFromLoaded(loaded []loadedImageAttachment) []visionImage {
	if len(loaded) == 0 {
		return nil
	}
	vision := make([]visionImage, 0, len(loaded))
	for _, item := range loaded {
		if item.Vision.DataURL == "" {
			continue
		}
		vision = append(vision, item.Vision)
	}
	return vision
}

func (s *Service) prepareTranslation(ctx context.Context, guildID, lookupText string, languages []string, contextFn func() TranslationContext, prepare func([]string, TranslationContext, []GlossaryEntry) (preparedTranslation, error), reservedVision []visionImage) (preparedTranslation, error) {
	glossary, err := s.store.ListGlossaryEntries(ctx, guildID)
	if err != nil {
		return preparedTranslation{}, err
	}
	translationContext := contextFn()
	attachURLPageMeta(&translationContext, s.urlPages.Lookup(ctx, lookupText))
	remainingSlots := visionMaxImages - len(reservedVision)
	remainingBytes := visionMaxTotalBytes - visionBytesTotal(reservedVision)
	contextVision := s.loadContextVisionImages(ctx, &translationContext, remainingSlots, remainingBytes)
	prepared, err := prepare(languages, translationContext, glossary)
	if err != nil {
		return preparedTranslation{}, err
	}
	remainingSlots -= len(contextVision)
	remainingBytes -= visionBytesTotal(contextVision)
	ogpVision := s.loadOGPVisionImages(ctx, prepared.translationContext.Sites, remainingSlots, remainingBytes)
	if hasSiteVisionImage(prepared.translationContext.Sites) {
		translationContext.Sites = prepared.translationContext.Sites
		prepared, err = prepare(languages, translationContext, glossary)
		if err != nil {
			return preparedTranslation{}, err
		}
	}
	prepared.visionImages = append(append(append([]visionImage{}, reservedVision...), contextVision...), ogpVision...)
	if err := s.checkPreparedTranslationRateLimit(guildID, prepared); err != nil {
		return preparedTranslation{}, err
	}
	return prepared, nil
}

func attachURLPageMeta(tc *TranslationContext, pages map[string]urlPageInfo) {
	if len(pages) == 0 {
		return
	}
	titles := make(map[string]string, len(pages))
	descriptions := make(map[string]string, len(pages))
	images := make(map[string]string, len(pages))
	for rawURL, page := range pages {
		if title := strings.TrimSpace(page.Title); title != "" {
			titles[rawURL] = title
		}
		if desc := strings.TrimSpace(page.Description); desc != "" {
			descriptions[rawURL] = desc
		}
		if imageURL := strings.TrimSpace(page.ImageURL); imageURL != "" {
			images[rawURL] = imageURL
		}
	}
	tc.SiteTitles = titles
	tc.SiteDescriptions = descriptions
	tc.SiteImages = images
}

func (s *Service) checkPreparedTranslationRateLimit(guildID string, prepared preparedTranslation) error {
	if s.rateLimiter == nil || len(prepared.targetLanguages) == 0 {
		return nil
	}
	estimate := EstimateTranslationTokens(prepared.systemInstruction+prepared.userPromptFrozen+prepared.userPromptVariable, "") + 200*len(prepared.targetLanguages) + visionTokenOverheadPerImage*len(prepared.visionImages)
	if !s.rateLimiter.Allow(guildID, estimate) {
		return errTranslationRateLimited
	}
	s.issueNotices.clear(issueNoticeRateLimit)
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
	channelIDs, locationKey := s.conversationScope(ctx, guildID, groupID, historyChannelID)
	replyChain, replyKeys := s.replyChainContext(ctx, replyChannelID, replyMessageID)
	translationContext := s.translationContext(ctx, guildID, contextChannelID, channelIDs, locationKey, historyChannelID, excludeMessageID, replyKeys)
	translationContext.ReplyChain = replyChain
	translationContext.StyleInstructions = s.groupStyleInstructions(ctx, guildID, groupID)
	translationContext.Author = strings.TrimSpace(author)
	translationContext.ThreadName = strings.TrimSpace(threadName)
	translationContext.PromptCacheLocation = locationKey
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
	channelIDs, _ := s.conversationScope(ctx, guildID, groupID, historyChannelID)
	return channelIDs
}

func (s *Service) conversationScope(ctx context.Context, guildID, groupID, historyChannelID string) (channelIDs []string, locationKey string) {
	groupLocation := guildID + ":" + groupID + ":group"
	channels, err := s.store.ChannelsInGroup(ctx, guildID, groupID)
	if err != nil {
		return nil, groupLocation
	}
	if findChannel(channels, historyChannelID) != nil {
		channelIDs := make([]string, len(channels))
		for i, ch := range channels {
			channelIDs[i] = ch.ChannelID
		}
		return channelIDs, groupLocation
	}
	if historyChannelID == "" {
		return nil, groupLocation
	}
	channelIDs = []string{historyChannelID}
	threads, err := s.store.ThreadTargets(ctx, historyChannelID)
	if err != nil {
		return channelIDs, guildID + ":" + groupID + ":thread:" + historyChannelID
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
	return channelIDs, guildID + ":" + groupID + ":thread:" + minStableID(channelIDs)
}

func minStableID(ids []string) string {
	min := ""
	for _, id := range ids {
		if id == "" {
			continue
		}
		if min == "" || snowflakeIDLess(id, min) {
			min = id
		}
	}
	return min
}

func snowflakeIDLess(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

func (s *Service) translationContext(ctx context.Context, guildID, channelID string, historyChannelIDs []string, locationKey, sourceChannelID, excludeMessageID string, excludeReplyKeys map[string]bool) TranslationContext {
	translationContext := TranslationContext{
		GuildID:   guildID,
		MessageID: excludeMessageID,
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
		translationContext.PromptCacheGeneration = historyGenerationID(nil, sourceChannelID, excludeMessageID)
		return translationContext
	}
	links, err := s.store.RecentMessageHistory(ctx, historyChannelIDs, excludeMessageID, historyFetchLimit)
	if err != nil {
		translationContext.PromptCacheGeneration = historyGenerationID(nil, sourceChannelID, excludeMessageID)
		return translationContext
	}
	history, frozenCount, discarded := selectRecentHistory(links, excludeMessageID, excludeReplyKeys)
	translationContext.History = history
	translationContext.HistoryFrozenCount = frozenCount
	translationContext.PromptCacheGeneration = historyGenerationID(history, sourceChannelID, excludeMessageID)
	if summary, err := s.store.TopicSummary(ctx, locationKey, translationContext.PromptCacheGeneration); err == nil {
		translationContext.TopicSummary = summary
	}
	if len(discarded) > 0 && translationContext.TopicSummary == "" {
		s.scheduleTopicSummary(guildID, locationKey, translationContext.PromptCacheGeneration, excludeMessageID, discarded)
	}
	return translationContext
}

type historyMergeSlot struct {
	author          string
	content         string
	firstTime       time.Time
	lastTime        time.Time
	sourceChannelID string
	sourceMessageID string
	keys            []string
	count           int
	attachments     []DiscordAttachment
}

func (slot historyMergeSlot) toMessage() ChatContextMessage {
	return ChatContextMessage{
		Author:          slot.author,
		Content:         slot.content,
		SourceChannelID: slot.sourceChannelID,
		SourceMessageID: slot.sourceMessageID,
		Attachments:     append([]DiscordAttachment(nil), slot.attachments...),
	}
}

func selectRecentHistory(links []MessageLink, currentMessageID string, excludeReplyKeys map[string]bool) ([]ChatContextMessage, int, []ChatContextMessage) {
	session := truncateIdleSession(links, currentMessageID)
	slots, discardedSlots := replayHistoryHysteresis(mergeConsecutiveMessages(session))
	history, frozenCount := splitFrozenHistory(slots, excludeReplyKeys)
	discarded := make([]ChatContextMessage, 0, len(discardedSlots))
	for _, slot := range discardedSlots {
		discarded = append(discarded, slot.toMessage())
	}
	return history, frozenCount, discarded
}

func truncateIdleSession(links []MessageLink, currentMessageID string) []MessageLink {
	filtered := make([]MessageLink, 0, len(links))
	for _, link := range links {
		if !historyLinkHasContext(link) {
			continue
		}
		filtered = append(filtered, link)
	}
	if len(filtered) == 0 {
		return nil
	}
	newest := len(filtered) - 1
	currentTime, hasCurrent := discordSnowflakeTime(currentMessageID)
	newestTime, hasNewest := discordSnowflakeTime(filtered[newest].SourceMessageID)
	if hasCurrent && hasNewest && currentTime.Sub(newestTime) > historyIdleGap {
		return nil
	}
	sessionStart := 0
	for i := newest; i > 0; i-- {
		newerTime, hasNewer := discordSnowflakeTime(filtered[i].SourceMessageID)
		olderTime, hasOlder := discordSnowflakeTime(filtered[i-1].SourceMessageID)
		if !hasNewer || !hasOlder {
			continue
		}
		if newerTime.Sub(olderTime) > historyIdleGap {
			sessionStart = i
			break
		}
	}
	return filtered[sessionStart:]
}

func mergeConsecutiveMessages(links []MessageLink) []historyMergeSlot {
	slots := make([]historyMergeSlot, 0, len(links))
	for _, link := range links {
		if !historyLinkHasContext(link) {
			continue
		}
		content := link.SourceContentSnapshot
		images := imageAttachmentsOnly(link.SourceImageAttachments)
		messageTime, hasTime := discordSnowflakeTime(link.SourceMessageID)
		author := strings.TrimSpace(link.SourceAuthorDisplayName)
		contentRunes := len([]rune(content))
		key := messageRefKey(link.SourceChannelID, link.SourceMessageID)
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
				if strings.TrimSpace(content) != "" {
					if strings.TrimSpace(last.content) != "" {
						last.content += "\n" + content
					} else {
						last.content = content
					}
				}
				last.attachments = append(last.attachments, images...)
				last.lastTime = messageTime
				last.keys = append(last.keys, key)
				last.count++
				continue
			}
		}
		slot := historyMergeSlot{
			author:          author,
			content:         content,
			sourceChannelID: link.SourceChannelID,
			sourceMessageID: link.SourceMessageID,
			keys:            []string{key},
			count:           1,
			attachments:     append([]DiscordAttachment(nil), images...),
		}
		if hasTime {
			slot.firstTime = messageTime
			slot.lastTime = messageTime
		}
		slots = append(slots, slot)
	}
	return slots
}

func replayHistoryHysteresis(slots []historyMergeSlot) (kept, discarded []historyMergeSlot) {
	if len(slots) == 0 {
		return nil, nil
	}
	start := 0
	for k := 1; k <= len(slots); k++ {
		if !historyExceedsHigh(slots[start:k]) {
			continue
		}
		for start < k-1 && historyExceedsLow(slots[start:k]) {
			start++
		}
	}
	if start == 0 {
		return slots, nil
	}
	return slots[start:], slots[:start]
}

func historyExceedsHigh(slots []historyMergeSlot) bool {
	return len(slots) > historyCountHigh || historySpan(slots) > historySpanHigh || historyTokens(slots) > historyTokenHigh
}

func historyExceedsLow(slots []historyMergeSlot) bool {
	return len(slots) > historyCountLow || historySpan(slots) > historySpanLow || historyTokens(slots) > historyTokenLow
}

func historySpan(slots []historyMergeSlot) time.Duration {
	if len(slots) == 0 {
		return 0
	}
	first := slots[0].firstTime
	last := slots[len(slots)-1].lastTime
	if first.IsZero() || last.IsZero() || last.Before(first) {
		return 0
	}
	return last.Sub(first)
}

func historyTokens(slots []historyMergeSlot) int {
	total := 0
	for _, slot := range slots {
		total += EstimateTranslationTokens(slot.content, "")
	}
	return total
}

func splitFrozenHistory(slots []historyMergeSlot, excludeReplyKeys map[string]bool) ([]ChatContextMessage, int) {
	n := len(slots)
	if n == 0 {
		return nil, 0
	}
	frozen := slots[:n-1]
	out := make([]ChatContextMessage, 0, n)
	for _, slot := range frozen {
		out = append(out, slot.toMessage())
	}
	tail := slots[n-1]
	if slotMatchesReply(tail, excludeReplyKeys) {
		return out, len(out)
	}
	out = append(out, tail.toMessage())
	return out, len(frozen)
}

func slotMatchesReply(slot historyMergeSlot, excludeReplyKeys map[string]bool) bool {
	if excludeReplyKeys == nil {
		return false
	}
	for _, key := range slot.keys {
		if excludeReplyKeys[key] {
			return true
		}
	}
	return false
}

func historyLinkHasContext(link MessageLink) bool {
	return strings.TrimSpace(link.SourceContentSnapshot) != "" || len(imageAttachmentsOnly(link.SourceImageAttachments)) > 0
}

func historyGenerationID(history []ChatContextMessage, currentChannelID, currentMessageID string) string {
	if len(history) > 0 {
		return history[0].SourceChannelID + history[0].SourceMessageID
	}
	channelID := strings.TrimSpace(currentChannelID)
	messageID := strings.TrimSpace(currentMessageID)
	if channelID != "" && messageID != "" {
		return channelID + messageID
	}
	if messageID != "" {
		return messageID
	}
	return "empty"
}

func (s *Service) scheduleTopicSummary(guildID, locationKey, generationID, messageID string, discarded []ChatContextMessage) {
	if _, ok := s.translator.(TopicSummarizer); !ok {
		return
	}
	if strings.TrimSpace(locationKey) == "" || strings.TrimSpace(generationID) == "" || generationID == "empty" || len(discarded) == 0 {
		return
	}
	attemptKey := locationKey + "\x00" + generationID
	if _, loaded := s.topicSummaryAttempts.LoadOrStore(attemptKey, struct{}{}); loaded {
		return
	}
	copied := append([]ChatContextMessage(nil), discarded...)
	run := func() {
		ctx, cancel := context.WithTimeout(context.Background(), topicSummaryTimeout)
		defer cancel()
		if err := s.generateAndStoreTopicSummary(ctx, guildID, locationKey, generationID, messageID, copied); err != nil && errors.Is(err, errTranslationRateLimited) {
			s.topicSummaryAttempts.Delete(attemptKey)
		}
	}
	if s.runTopicSummary != nil {
		s.runTopicSummary(run)
		return
	}
	go run()
}

func (s *Service) generateAndStoreTopicSummary(ctx context.Context, guildID, locationKey, generationID, messageID string, discarded []ChatContextMessage) error {
	summarizer, ok := s.translator.(TopicSummarizer)
	if !ok {
		return nil
	}
	if existing, err := s.store.TopicSummary(ctx, locationKey, generationID); err == nil && existing != "" {
		return nil
	}
	previous := ""
	if prevGeneration, prevSummary, err := s.store.TopicSummaryForLocation(ctx, locationKey); err == nil && prevGeneration != generationID {
		previous = prevSummary
	}
	req := TopicSummaryRequest{
		PreviousSummary: previous,
		Discarded:       capDiscardedForSummary(discarded),
		GuildID:         guildID,
		MessageID:       messageID,
	}
	prepared, err := prepareTopicSummary(req)
	if err != nil {
		return err
	}
	if s.rateLimiter != nil {
		estimate := EstimateTranslationTokens(prepared.systemInstruction+prepared.userPromptFrozen+prepared.userPromptVariable, "") + 200
		if !s.rateLimiter.Allow(guildID, estimate) {
			return errTranslationRateLimited
		}
	}
	result, err := summarizer.SummarizeTopic(ctx, req)
	if err != nil {
		return err
	}
	s.issueNotices.clear(issueNoticeProvider)
	s.recordTranslationUsage(guildID, result.InputTokens, result.OutputTokens)
	if strings.TrimSpace(result.Summary) == "" {
		return errors.New("empty topic summary")
	}
	return s.store.UpsertTopicSummary(ctx, guildID, locationKey, generationID, result.Summary)
}

func capDiscardedForSummary(messages []ChatContextMessage) []ChatContextMessage {
	if len(messages) == 0 {
		return nil
	}
	total := 0
	start := len(messages)
	for i := len(messages) - 1; i >= 0; i-- {
		n := EstimateTranslationTokens(messages[i].Content, "")
		if total+n > topicSummarySourceTokenLimit && start < len(messages) {
			break
		}
		total += n
		start = i
	}
	return messages[start:]
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
		entry.Attachments = append([]DiscordAttachment(nil), original.ImageAttachments...)
		entry.SourceChannelID = sourceChannelID
		entry.SourceMessageID = sourceMessageID
	}
	fetched, fetchErr := s.discord.Message(fetchChannelID, fetchMessageID)
	if fetchErr != nil {
		if !tracked {
			return entry, "", "", nextRef, false
		}
		return entry, sourceChannelID, sourceMessageID, nextRef, contextMessageHasContent(entry)
	}
	if !tracked {
		entry.Content = fetched.Content
		entry.Author = strings.TrimSpace(fetched.AuthorDisplayName)
		entry.Attachments = append([]DiscordAttachment(nil), fetched.Attachments...)
		sourceChannelID = channelID
		sourceMessageID = messageID
	} else if entry.Author == "" {
		entry.Author = strings.TrimSpace(fetched.AuthorDisplayName)
	}
	if len(fetched.Attachments) > 0 {
		entry.Attachments = append([]DiscordAttachment(nil), fetched.Attachments...)
	}
	entry.SourceChannelID = sourceChannelID
	entry.SourceMessageID = sourceMessageID
	nextRef = messageRef{
		channelID: fetched.ReferencedChannelID,
		messageID: fetched.ReferencedMessageID,
	}
	if nextRef.channelID == "" && nextRef.messageID != "" {
		nextRef.channelID = fetchChannelID
	}
	return entry, sourceChannelID, sourceMessageID, nextRef, contextMessageHasContent(entry)
}

func contextMessageHasContent(entry ChatContextMessage) bool {
	return strings.TrimSpace(entry.Content) != "" || len(imageAttachmentsOnly(entry.Attachments)) > 0
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
// ref may be zero when the source message is not a reply.
func (s *Service) sendAndSaveLink(ctx context.Context, target GroupChannel, threadID string, send WebhookSend, link MessageLink, ref MessageReference) error {
	msgID, err := s.discord.SendWebhook(target.WebhookID, target.WebhookToken, send)
	if err != nil {
		return err
	}
	link.TargetMessageID = msgID
	if err := s.store.SaveMessageLinkWithReference(ctx, link, ref); err != nil {
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
