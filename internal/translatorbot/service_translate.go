package translatorbot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

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
	if len(ogpVision) > 0 {
		prepared.buildUserPrompt()
	}
	prepared.visionImages = append(append(append([]visionImage{}, reservedVision...), contextVision...), ogpVision...)
	if err := s.checkPreparedTranslationRateLimit(guildID, prepared); err != nil {
		return preparedTranslation{}, err
	}
	return prepared, nil
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
	s.recordSuccessfulTranslation(guildID, result.InputTokens, result.OutputTokens)
	for _, language := range languages {
		if _, ok := result.Translations[language]; !ok {
			return MultiTranslationResult{}, fmt.Errorf("missing translation for %q", language)
		}
		if translatableAttachmentCount(translationAttachmentsFromLoaded(loaded)) > 0 && len(result.AttachmentDescriptions[language]) != len(loaded) {
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

func (s *Service) checkPreparedTranslationRateLimit(guildID string, prepared preparedTranslation) error {
	if s.rateLimiter == nil {
		return nil
	}
	if !s.rateLimiter.Allow(guildID, estimatePreparedTokens(prepared)) {
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

func (s *Service) recordSuccessfulTranslation(guildID string, inputTokens, outputTokens int) {
	s.issueNotices.clear(issueNoticeProvider)
	s.recordTranslationUsage(guildID, inputTokens, outputTokens)
}

const translationOutputTokenReserve = 200

func estimatePreparedTokens(prepared preparedTranslation) int {
	langs := len(prepared.targetLanguages)
	if langs == 0 {
		langs = 1
	}
	return EstimateTranslationTokens(prepared.systemInstruction+prepared.userPromptStable+prepared.userPromptHistory+prepared.userPromptVariable, "") +
		translationOutputTokenReserve*langs +
		visionTokenOverheadPerImage*len(prepared.visionImages)
}
