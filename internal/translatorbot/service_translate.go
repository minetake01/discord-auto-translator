package translatorbot

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

type issueNoticeKind string

const (
	issueNoticeProvider  issueNoticeKind = "provider"
	issueNoticeRateLimit issueNoticeKind = "rate_limit"

	providerNoticeCooldown = time.Hour
)

// issueNoticeState suppresses repeat source-channel notices. Provider outages
// stay muted for one hour after a notice, even if some translations succeed in
// between. Guild token rate limits notify once until the limiter allows again.
type issueNoticeState struct {
	mu                 sync.Mutex
	now                func() time.Time
	providerNotifiedAt map[string]time.Time
	rateLimitNotified  map[string]struct{}
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

func (p *issueNoticeState) nowTime() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

func (p *issueNoticeState) allow(channelID string, err error) bool {
	kind, suppress := issueNoticeKindFor(err)
	if !suppress {
		return true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	switch kind {
	case issueNoticeProvider:
		if p.providerNotifiedAt == nil {
			p.providerNotifiedAt = make(map[string]time.Time)
		}
		now := p.nowTime()
		if last, ok := p.providerNotifiedAt[channelID]; ok && now.Sub(last) < providerNoticeCooldown {
			return false
		}
		p.providerNotifiedAt[channelID] = now
		return true
	case issueNoticeRateLimit:
		if p.rateLimitNotified == nil {
			p.rateLimitNotified = make(map[string]struct{})
		}
		if _, already := p.rateLimitNotified[channelID]; already {
			return false
		}
		p.rateLimitNotified[channelID] = struct{}{}
		return true
	default:
		return true
	}
}

func (p *issueNoticeState) clearRateLimit() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rateLimitNotified = nil
}

// notifyTranslationIssue posts a localized notice as a reply to the source
// message when it could not be mirrored. The language is the source channel's
// registered language, since that is where the notice is shown. Provider
// outages notify each source channel at most once per hour. Token rate limits
// notify each source channel once until the limiter allows again.
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

func (s *Service) prepareTranslation(ctx context.Context, guildID, lookupText string, languages []string, contextFn func() TranslationContext, prepare func([]string, TranslationContext, []GlossaryEntry) (preparedTranslation, error), reservedVision []visionImage, scope translationScope) (preparedTranslation, error) {
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
	if len(ogpVision) > 0 {
		prepared.buildUserPrompt()
	}
	prepared.visionImages = append(append(append([]visionImage{}, reservedVision...), contextVision...), ogpVision...)
	if err := s.checkPreparedTranslationRateLimit(guildID, prepared, scope); err != nil {
		return preparedTranslation{}, err
	}
	return prepared, nil
}

// translateWithLimit translates content into every requested language while
// enforcing the per-guild token rate limit.
// Messages without translatable text are returned as-is without calling the
// translator, in which case contextFn is never invoked.
// Returns errTranslationRateLimited when the guild is over budget.
func (s *Service) translateWithLimit(ctx context.Context, guildID, content string, loaded []loadedImageAttachment, languages []string, contextFn func() TranslationContext, scope translationScope) (MultiTranslationResult, error) {
	if !needsTranslation(content) {
		translations := make(map[string]string, len(languages))
		for _, language := range languages {
			translations[language] = content
		}
		return MultiTranslationResult{Translations: translations}, nil
	}
	prepared, err := s.prepareTranslation(ctx, guildID, content, languages, contextFn, func(langs []string, tc TranslationContext, glossary []GlossaryEntry) (preparedTranslation, error) {
		tc.Attachments = translationAttachmentsFromLoaded(loaded)
		return prepareMultiTranslation(langs, content, tc, glossary)
	}, visionFromLoaded(loaded), scope)
	if err != nil {
		return MultiTranslationResult{}, err
	}
	result, err := s.translator.TranslateMulti(ctx, prepared)
	if err != nil {
		return MultiTranslationResult{}, err
	}
	s.recordSuccessfulTranslation(guildID, result.InputTokens, result.OutputTokens)
	for _, language := range languages {
		if _, ok := result.Translations[language]; !ok {
			return MultiTranslationResult{}, fmt.Errorf("missing translation for %q", language)
		}
	}
	return result, nil
}

// translateAttachmentAltsWithLimit translates image alt texts. Alt texts carry
// no conversation, so the context is reduced to the identifiers the request
// still needs before it enters the shared preparation pipeline.
func (s *Service) translateAttachmentAltsWithLimit(ctx context.Context, guildID string, loaded []loadedImageAttachment, languages []string, contextFn func() TranslationContext, scope translationScope) (map[string][]string, error) {
	attachments := translationAttachmentsFromLoaded(loaded)
	if translatableAttachmentCount(attachments) == 0 {
		return nil, nil
	}
	prepared, err := s.prepareTranslation(ctx, guildID, "", languages, func() TranslationContext {
		tc := TranslationContext{GuildID: guildID}
		if contextFn != nil {
			tc = contextFn()
		}
		return tc.attachmentScope(attachments)
	}, prepareAttachmentAltTranslation, visionFromLoaded(loaded), scope)
	if err != nil {
		return nil, err
	}
	result, err := s.translator.TranslateAttachmentAlts(ctx, prepared)
	if err != nil {
		return nil, err
	}
	s.recordSuccessfulTranslation(guildID, result.InputTokens, result.OutputTokens)
	for _, language := range languages {
		if len(result.Descriptions[language]) != len(loaded) {
			return nil, fmt.Errorf("missing attachment descriptions for %q", language)
		}
	}
	return result.Descriptions, nil
}

type attachmentAltOutcome struct {
	Descriptions map[string][]string
	Err          error
}

func (s *Service) startAttachmentAltTranslation(ctx context.Context, guildID string, loaded []loadedImageAttachment, languages []string, contextFn func() TranslationContext, scope translationScope) <-chan attachmentAltOutcome {
	ch := make(chan attachmentAltOutcome, 1)
	if translatableAttachmentCount(translationAttachmentsFromLoaded(loaded)) == 0 {
		ch <- attachmentAltOutcome{}
		return ch
	}
	go func() {
		descriptions, err := s.translateAttachmentAltsWithLimit(ctx, guildID, loaded, languages, contextFn, scope)
		ch <- attachmentAltOutcome{Descriptions: descriptions, Err: err}
	}()
	return ch
}

func takeReadyAttachmentAlts(ch <-chan attachmentAltOutcome) (map[string][]string, <-chan attachmentAltOutcome) {
	select {
	case out := <-ch:
		if out.Err != nil {
			return nil, nil
		}
		return out.Descriptions, nil
	default:
		return nil, ch
	}
}

func (s *Service) applyReadyAttachmentAlts(pending <-chan attachmentAltOutcome, posted []postedWebhookMirror) {
	if pending == nil {
		return
	}
	out := <-pending
	if out.Err != nil || out.Descriptions == nil {
		return
	}
	for _, item := range posted {
		if err := s.patchWebhookAttachmentDescriptions(item, out.Descriptions[item.language]); err != nil {
			continue
		}
	}
}

type postedWebhookMirror struct {
	channel       GroupChannel
	threadID      string
	messageID     string
	attachmentIDs []string
	language      string
}

func (s *Service) patchWebhookAttachmentDescriptions(posted postedWebhookMirror, descriptions []string) error {
	if len(posted.attachmentIDs) == 0 || len(descriptions) != len(posted.attachmentIDs) {
		return nil
	}
	edits := make([]WebhookAttachmentEdit, len(posted.attachmentIDs))
	for i, id := range posted.attachmentIDs {
		edits[i] = WebhookAttachmentEdit{
			ID:          id,
			Description: truncateRunes(strings.TrimSpace(descriptions[i]), discordAttachmentDescriptionLimit, ""),
		}
	}
	return s.discord.EditWebhookAttachments(posted.channel.WebhookID, posted.channel.WebhookToken, posted.messageID, posted.threadID, edits)
}

func (s *Service) translatePollWithLimit(ctx context.Context, guildID, question string, answers []string, languages []string, contextFn func() TranslationContext, scope translationScope) (map[string]PollTranslation, error) {
	translations := make(map[string]PollTranslation, len(languages))
	needsTranslation := hasTranslatableText(question) || slices.ContainsFunc(answers, hasTranslatableText)
	if !needsTranslation {
		for _, language := range languages {
			copied := make([]string, len(answers))
			copy(copied, answers)
			translations[language] = PollTranslation{Question: question, Answers: copied}
		}
		return translations, nil
	}
	pollText := strings.Join(append([]string{question}, answers...), "\n")
	prepared, err := s.prepareTranslation(ctx, guildID, pollText, languages, contextFn, func(langs []string, tc TranslationContext, glossary []GlossaryEntry) (preparedTranslation, error) {
		return preparePollTranslation(langs, question, answers, tc, glossary)
	}, nil, scope)
	if err != nil {
		return nil, err
	}
	result, err := s.translator.TranslatePollMulti(ctx, prepared)
	if err != nil {
		return nil, err
	}
	s.recordSuccessfulTranslation(guildID, result.InputTokens, result.OutputTokens)
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
func (s *Service) translateThreadCreateWithLimit(ctx context.Context, guildID, name, message string, languages []string, contextFn func() TranslationContext, scope translationScope) (map[string]ThreadCreateTranslation, error) {
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
	}, nil, scope)
	if err != nil {
		return nil, err
	}
	result, err := s.translator.TranslateThreadCreateMulti(ctx, prepared)
	if err != nil {
		return nil, err
	}
	s.recordSuccessfulTranslation(guildID, result.InputTokens, result.OutputTokens)
	for _, language := range languages {
		translated, ok := result.Translations[language]
		if !ok {
			return nil, fmt.Errorf("missing thread create translation for %q", language)
		}
		translations[language] = translated
	}
	return translations, nil
}

// checkPreparedTranslationRateLimit admits a request against the guild token
// budget. Backfill shares the budget with live translation but only uses
// part of it, so a long backfill never starves live messages.
func (s *Service) checkPreparedTranslationRateLimit(guildID string, prepared preparedTranslation, scope translationScope) error {
	if s.rateLimiter == nil {
		return nil
	}
	fraction := 1.0
	if scope.backfill != nil {
		fraction = backfillRateLimitShare
	}
	if !s.rateLimiter.AllowShare(guildID, estimatePreparedTokens(prepared), fraction) {
		return errTranslationRateLimited
	}
	if fraction == 1 {
		s.issueNotices.clearRateLimit()
	}
	return nil
}

// translationFailure reports a failed live translation to the source channel
// and decides whether the caller should surface the error. Backfill never
// posts notices and needs rate-limit errors back so it can wait and retry.
func (s *Service) translationFailure(ctx context.Context, m DiscordMessage, sourceLanguage string, err error, scope translationScope) error {
	if scope.backfill != nil {
		return err
	}
	s.notifyTranslationIssue(m.ChannelID, m.ID, sourceLanguage, err)
	if errors.Is(err, errTranslationRateLimited) {
		return nil
	}
	return err
}

func (s *Service) recordTranslationUsage(guildID string, inputTokens, outputTokens int) {
	if s.rateLimiter != nil {
		s.rateLimiter.Record(guildID, inputTokens+outputTokens)
	}
}

func (s *Service) recordSuccessfulTranslation(guildID string, inputTokens, outputTokens int) {
	s.recordTranslationUsage(guildID, inputTokens, outputTokens)
}

const translationOutputTokenReserve = 200

func estimatePreparedTokens(prepared preparedTranslation) int {
	langs := len(prepared.targetLanguages)
	if langs == 0 {
		langs = 1
	}
	return EstimateTranslationTokens(prepared.systemInstruction+prepared.userPromptStable+prepared.userPromptHistory+prepared.userPromptVariable, "") +
		estimatePreparedOutputTokens(prepared, langs) +
		visionTokenOverheadPerImage*len(prepared.visionImages)
}

// estimatePreparedOutputTokens bounds one request's output estimate on the
// same per-content estimate the batch budget uses: every target language
// reserves the floor, and a single-language request with known source
// content takes the larger of that floor and the content's own bound.
func estimatePreparedOutputTokens(prepared preparedTranslation, langs int) int {
	reserve := translationOutputTokenReserve * langs
	if langs != 1 {
		return reserve
	}
	sources := prepared.batchSources
	if len(sources) == 0 && prepared.content != "" {
		sources = []string{prepared.content}
	}
	bounded := 0
	for _, source := range sources {
		bounded += estimateMessageOutputTokens(source)
	}
	if bounded > batchMaxOutputTokens {
		bounded = batchMaxOutputTokens
	}
	return max(reserve, bounded)
}
