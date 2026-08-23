package translatorbot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"strings"
)

type ChatContextMessage struct {
	Author          string
	Content         string
	SourceChannelID string
	SourceMessageID string
	Attachments     []DiscordAttachment
	Images          []TranslationAttachment
}

type TranslationContext struct {
	GuildID               string
	MessageID             string
	ServerName            string
	ServerDescription     string
	ChannelName           string
	ChannelTopic          string
	ThreadName            string
	History               []ChatContextMessage
	HistoryFrozenCount    int
	ReplyChain            []ChatContextMessage
	StyleInstructions     string
	Author                string
	MentionedUsers        map[string]string // userID → display name
	MentionedChannels     map[string]string // channelID → channel name (source)
	MentionedRoles        map[string]string // roleID → role name
	SiteTitles            map[string]string // rawURL → page title
	SiteDescriptions      map[string]string // rawURL → page description
	SiteImages            map[string]string // rawURL → og:image URL
	Sites                 []SiteContextEntry
	Attachments           []TranslationAttachment
	TopicSummary          string
	PromptCacheLocation   string
	PromptCacheGeneration string
}

type MultiTranslationResult struct {
	Translations           map[string]string
	AttachmentDescriptions map[string][]string
	InputTokens            int
	OutputTokens           int
}

type PollTranslation struct {
	Question string
	Answers  []string
}

type PollMultiTranslationResult struct {
	Translations map[string]PollTranslation
	InputTokens  int
	OutputTokens int
}

type ThreadCreateTranslation struct {
	Name    string
	Message string
}

type ThreadCreateMultiTranslationResult struct {
	Translations map[string]ThreadCreateTranslation
	InputTokens  int
	OutputTokens int
}

type TopicSummaryResult struct {
	Summary      string
	InputTokens  int
	OutputTokens int
}

type Translator interface {
	TranslateMulti(ctx context.Context, prepared preparedTranslation) (MultiTranslationResult, error)
	TranslatePollMulti(ctx context.Context, prepared preparedTranslation) (PollMultiTranslationResult, error)
	TranslateThreadCreateMulti(ctx context.Context, prepared preparedTranslation) (ThreadCreateMultiTranslationResult, error)
}

// TopicSummarizer is implemented by translators that can compress discarded
// history into a short topic summary. Translation itself does not wait for it.
type TopicSummarizer interface {
	SummarizeTopic(ctx context.Context, prepared preparedTranslation) (TopicSummaryResult, error)
}

type preparedTranslation struct {
	targetLanguages    []string
	systemInstruction  string
	userPromptFrozen   string
	userPromptVariable string
	protector          *Protector
	guildID            string
	messageID          string
	answerCount        int  // set for poll translations; 0 for normal messages
	messageRequired    bool // set for thread-create translations when source message is non-empty
	content            string
	question           string
	answers            []string
	threadName         string
	threadMessage      string
	translationContext TranslationContext
	visionImages       []visionImage
	altCount           int
	promptCacheKey     string
	alwaysGlossary     []GlossaryEntry
	matchedGlossary    []GlossaryEntry
	writeSource        func(*strings.Builder)
}

func (p preparedTranslation) userPrompt() string {
	return p.userPromptFrozen + p.userPromptVariable
}

func (p *preparedTranslation) buildUserPrompt() {
	if p.writeSource == nil {
		return
	}
	frozen, variable := buildTranslationUserPromptParts(p.targetLanguages, p.translationContext, p.alwaysGlossary, p.matchedGlossary, p.writeSource)
	p.userPromptFrozen = frozen
	p.userPromptVariable = variable
}

func prepareMultiTranslation(targetLanguages []string, content string, translationContext TranslationContext, glossary []GlossaryEntry) (preparedTranslation, error) {
	normalized, p, err := beginPreparedTranslation(targetLanguages, &translationContext)
	if err != nil || len(normalized) == 0 {
		return preparedTranslation{}, err
	}

	protected := p.Protect(content)
	attachments := append([]TranslationAttachment(nil), translationContext.Attachments...)
	protectedAlts := make([]string, len(attachments))
	glossaryContent := content
	altCount := 0
	for i, attachment := range attachments {
		if !hasTranslatableText(attachment.Description) {
			continue
		}
		protectedAlts[i] = p.Protect(attachment.Description)
		glossaryContent += "\n" + protectedAlts[i]
		altCount++
	}
	translationContext.Attachments = attachments
	assignSiteContext(p, &translationContext)
	alwaysGlossary, matchedGlossary := splitGlossaryEntries(glossaryContent, glossary)
	prepared := preparedTranslation{
		targetLanguages:    normalized,
		systemInstruction:  buildTranslationSystemInstruction(messageTranslationTaskIntro, "<final_message>"),
		protector:          p,
		guildID:            translationContext.GuildID,
		messageID:          translationContext.MessageID,
		content:            content,
		translationContext: translationContext,
		altCount:           altCount,
		promptCacheKey:     translationPromptCacheKey(translationContext, ""),
		alwaysGlossary:     alwaysGlossary,
		matchedGlossary:    matchedGlossary,
		writeSource: func(b *strings.Builder) {
			writeAttachmentContext(b, attachments)
			writeAttachmentAlts(b, attachments, protectedAlts)
			writeAttributedElement(b, "final_message", translationContext.Author, protected)
		},
	}
	prepared.buildUserPrompt()
	return prepared, nil
}

type translationResponseItem struct {
	TranslatedText         string   `json:"translated_text"`
	AttachmentDescriptions []string `json:"attachment_descriptions"`
}

func decodeLanguageKeyedJSON[T any](raw string, targetLanguages []string) (map[string]T, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	var parsed map[string]json.RawMessage
	if err := decoder.Decode(&parsed); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, errors.New("multiple JSON values")
	}
	for _, lang := range targetLanguages {
		if _, ok := parsed[lang]; !ok {
			return nil, fmt.Errorf("missing language %q", lang)
		}
	}
	if len(parsed) != len(targetLanguages) {
		return nil, fmt.Errorf("got %d languages, want %d", len(parsed), len(targetLanguages))
	}
	out := make(map[string]T, len(targetLanguages))
	for _, lang := range targetLanguages {
		itemRaw, ok := parsed[lang]
		if !ok {
			return nil, fmt.Errorf("missing language %q", lang)
		}
		trimmedItem := bytes.TrimSpace(itemRaw)
		if len(trimmedItem) == 0 || trimmedItem[0] != '{' {
			return nil, fmt.Errorf("language %q is not an object", lang)
		}
		itemDecoder := json.NewDecoder(bytes.NewReader(itemRaw))
		itemDecoder.DisallowUnknownFields()
		var item T
		if err := itemDecoder.Decode(&item); err != nil {
			return nil, fmt.Errorf("language %q: %w", lang, err)
		}
		var itemTrailing any
		if err := itemDecoder.Decode(&itemTrailing); err != io.EOF {
			return nil, fmt.Errorf("language %q: multiple JSON values", lang)
		}
		out[lang] = item
	}
	return out, nil
}

func parseMultiTranslationResponse(raw string, targetLanguages []string, protector *Protector, sourceContent string, attachments []TranslationAttachment) (map[string]string, map[string][]string, error) {
	parsed, err := decodeLanguageKeyedJSON[translationResponseItem](raw, targetLanguages)
	if err != nil {
		return nil, nil, fmt.Errorf("parse translation response: %w", err)
	}

	altCount := translatableAttachmentCount(attachments)
	texts := make(map[string]string, len(targetLanguages))
	descriptions := make(map[string][]string, len(targetLanguages))
	for _, targetLanguage := range targetLanguages {
		item := parsed[targetLanguage]
		text := strings.TrimSpace(html.UnescapeString(item.TranslatedText))
		if text == "" {
			if hasTranslatableText(sourceContent) {
				return nil, nil, fmt.Errorf("parse translation response: empty translation for %q", targetLanguage)
			}
			texts[targetLanguage] = sourceContent
		} else {
			texts[targetLanguage] = protector.Restore(text)
		}
		if altCount == 0 {
			continue
		}
		alts, err := applyAttachmentDescriptions(item.AttachmentDescriptions, attachments, protector)
		if err != nil {
			return nil, nil, fmt.Errorf("parse translation response: language %q: %w", targetLanguage, err)
		}
		descriptions[targetLanguage] = alts
	}
	if len(descriptions) == 0 {
		return texts, nil, nil
	}
	return texts, descriptions, nil
}

func applyAttachmentDescriptions(translated []string, attachments []TranslationAttachment, protector *Protector) ([]string, error) {
	altCount := translatableAttachmentCount(attachments)
	if altCount == 0 {
		return nil, nil
	}
	if len(translated) < altCount {
		return nil, fmt.Errorf("%d attachment descriptions, want %d", len(translated), altCount)
	}
	out := make([]string, len(attachments))
	ti := 0
	for i, attachment := range attachments {
		if !hasTranslatableText(attachment.Description) {
			out[i] = attachment.Description
			continue
		}
		got := protector.Restore(strings.TrimSpace(html.UnescapeString(translated[ti])))
		if got == "" {
			return nil, fmt.Errorf("empty attachment description")
		}
		out[i] = got
		ti++
	}
	return out, nil
}

func normalizeTargetLanguages(targetLanguages []string) ([]string, error) {
	normalized := make([]string, 0, len(targetLanguages))
	seen := make(map[string]bool, len(targetLanguages))
	for _, lang := range targetLanguages {
		lang = normalizeLanguage(lang)
		if lang == "" || seen[lang] {
			continue
		}
		if !IsValidLanguageCode(lang) {
			return nil, fmt.Errorf("invalid target language %q", lang)
		}
		seen[lang] = true
		normalized = append(normalized, lang)
	}
	return normalized, nil
}

func preparePollTranslation(targetLanguages []string, question string, answers []string, translationContext TranslationContext, glossary []GlossaryEntry) (preparedTranslation, error) {
	normalized, p, err := beginPreparedTranslation(targetLanguages, &translationContext)
	if err != nil || len(normalized) == 0 {
		return preparedTranslation{}, err
	}

	protectedQuestion := p.Protect(question)
	protectedAnswers := make([]string, len(answers))
	for i, answer := range answers {
		protectedAnswers[i] = p.Protect(answer)
	}
	assignSiteContext(p, &translationContext)
	glossaryContent := question
	for _, answer := range answers {
		glossaryContent += "\n" + answer
	}
	alwaysGlossary, matchedGlossary := splitGlossaryEntries(glossaryContent, glossary)
	prepared := preparedTranslation{
		targetLanguages:    normalized,
		systemInstruction:  buildTranslationSystemInstruction(pollTranslationTaskIntro, "<poll>"),
		protector:          p,
		guildID:            translationContext.GuildID,
		messageID:          translationContext.MessageID,
		answerCount:        len(answers),
		question:           question,
		answers:            answers,
		translationContext: translationContext,
		promptCacheKey:     translationPromptCacheKey(translationContext, "poll"),
		alwaysGlossary:     alwaysGlossary,
		matchedGlossary:    matchedGlossary,
		writeSource: func(b *strings.Builder) {
			b.WriteString("<poll>")
			writeAttributedElement(b, "question", translationContext.Author, protectedQuestion)
			for _, answer := range protectedAnswers {
				writeXMLElement(b, "answer", answer)
			}
			b.WriteString("</poll>")
		},
	}
	prepared.buildUserPrompt()
	return prepared, nil
}

func prepareThreadCreateTranslation(targetLanguages []string, name, message string, translationContext TranslationContext, glossary []GlossaryEntry) (preparedTranslation, error) {
	// The thread name is a translation target here, not discord_context metadata.
	translationContext.ThreadName = ""
	normalized, p, err := beginPreparedTranslation(targetLanguages, &translationContext)
	if err != nil || len(normalized) == 0 {
		return preparedTranslation{}, err
	}

	protectedName := p.Protect(name)
	messageRequired := strings.TrimSpace(message) != ""
	var protectedMessage string
	if messageRequired {
		protectedMessage = p.Protect(message)
	}
	assignSiteContext(p, &translationContext)
	glossaryContent := name
	if messageRequired {
		glossaryContent += "\n" + message
	}
	alwaysGlossary, matchedGlossary := splitGlossaryEntries(glossaryContent, glossary)
	prepared := preparedTranslation{
		targetLanguages:    normalized,
		systemInstruction:  buildTranslationSystemInstruction(threadCreateTranslationTaskIntro, "<thread_create>"),
		protector:          p,
		guildID:            translationContext.GuildID,
		messageID:          translationContext.MessageID,
		messageRequired:    messageRequired,
		threadName:         name,
		threadMessage:      message,
		translationContext: translationContext,
		promptCacheKey:     translationPromptCacheKey(translationContext, "thread_create"),
		alwaysGlossary:     alwaysGlossary,
		matchedGlossary:    matchedGlossary,
		writeSource: func(b *strings.Builder) {
			b.WriteString("<thread_create>")
			writeXMLElement(b, "name", protectedName)
			if messageRequired {
				writeAttributedElement(b, "message", translationContext.Author, protectedMessage)
			}
			b.WriteString("</thread_create>")
		},
	}
	prepared.buildUserPrompt()
	return prepared, nil
}

func beginPreparedTranslation(targetLanguages []string, translationContext *TranslationContext) ([]string, *Protector, error) {
	normalized, err := normalizeTargetLanguages(targetLanguages)
	if err != nil {
		return nil, nil, err
	}
	if len(normalized) == 0 {
		return nil, nil, nil
	}
	p := NewProtector(NameMaps{
		Users:    translationContext.MentionedUsers,
		Channels: translationContext.MentionedChannels,
		Roles:    translationContext.MentionedRoles,
		Sites:    translationContext.SiteTitles,
	})
	p.SetSiteDescriptions(translationContext.SiteDescriptions)
	p.SetSiteImages(translationContext.SiteImages)
	return normalized, p, nil
}

func assignSiteContext(p *Protector, translationContext *TranslationContext) {
	incoming := translationContext.Sites
	translationContext.Sites = p.SiteContext()
	flags := make(map[string]bool, len(incoming))
	for _, site := range incoming {
		if site.HasVisionImage {
			flags[site.ID] = true
		}
	}
	for i := range translationContext.Sites {
		translationContext.Sites[i].HasVisionImage = flags[translationContext.Sites[i].ID]
	}
}

type pollTranslationResponseItem struct {
	Question string   `json:"question"`
	Answers  []string `json:"answers"`
}

func parsePollTranslationResponse(raw string, targetLanguages []string, answerCount int, protector *Protector) (map[string]PollTranslation, error) {
	parsed, err := decodeLanguageKeyedJSON[pollTranslationResponseItem](raw, targetLanguages)
	if err != nil {
		return nil, fmt.Errorf("parse poll translation response: %w", err)
	}

	out := make(map[string]PollTranslation, len(targetLanguages))
	for _, targetLanguage := range targetLanguages {
		item := parsed[targetLanguage]
		question := strings.TrimSpace(html.UnescapeString(item.Question))
		if question == "" {
			return nil, fmt.Errorf("parse poll translation response: empty question for %q", targetLanguage)
		}
		if len(item.Answers) != answerCount {
			return nil, fmt.Errorf("parse poll translation response: language %q has %d answers, want %d", targetLanguage, len(item.Answers), answerCount)
		}
		answers := make([]string, answerCount)
		for j, answer := range item.Answers {
			text := strings.TrimSpace(html.UnescapeString(answer))
			if text == "" {
				return nil, fmt.Errorf("parse poll translation response: empty answer %d for %q", j, targetLanguage)
			}
			answers[j] = protector.Restore(text)
		}
		out[targetLanguage] = PollTranslation{
			Question: protector.Restore(question),
			Answers:  answers,
		}
	}
	return out, nil
}

type threadCreateTranslationResponseItem struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

func parseThreadCreateTranslationResponse(raw string, targetLanguages []string, messageRequired bool, protector *Protector) (map[string]ThreadCreateTranslation, error) {
	parsed, err := decodeLanguageKeyedJSON[threadCreateTranslationResponseItem](raw, targetLanguages)
	if err != nil {
		return nil, fmt.Errorf("parse thread create translation response: %w", err)
	}

	out := make(map[string]ThreadCreateTranslation, len(targetLanguages))
	for _, targetLanguage := range targetLanguages {
		item := parsed[targetLanguage]
		name := strings.TrimSpace(html.UnescapeString(item.Name))
		if name == "" {
			return nil, fmt.Errorf("parse thread create translation response: empty name for %q", targetLanguage)
		}
		message := strings.TrimSpace(html.UnescapeString(item.Message))
		if messageRequired {
			if message == "" {
				return nil, fmt.Errorf("parse thread create translation response: empty message for %q", targetLanguage)
			}
		} else {
			message = ""
		}
		out[targetLanguage] = ThreadCreateTranslation{
			Name:    protector.Restore(name),
			Message: protector.Restore(message),
		}
	}
	return out, nil
}

func parseTopicSummaryResponse(raw string) (string, error) {
	var parsed struct {
		Summary string `json:"summary"`
	}
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&parsed); err != nil {
		return "", fmt.Errorf("parse topic summary response: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return "", fmt.Errorf("parse topic summary response: multiple JSON values")
	}
	summary := strings.TrimSpace(html.UnescapeString(parsed.Summary))
	if summary == "" {
		return "", errors.New("parse topic summary response: empty summary")
	}
	return truncateRunes(summary, topicSummaryMaxRunes, ""), nil
}
