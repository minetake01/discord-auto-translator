package main

import (
	"encoding/json"
	"sort"
	"strings"
)

func parseResponse(raw json.RawMessage) responsePayload {
	var payload responsePayload
	if len(raw) == 0 {
		return payload
	}
	_ = json.Unmarshal(raw, &payload)
	return payload
}

func firstFinishReason(resp responsePayload) string {
	if len(resp.Choices) == 0 {
		return ""
	}
	return strings.TrimSpace(resp.Choices[0].FinishReason)
}

func extractOutputText(resp responsePayload) string {
	for _, choice := range resp.Choices {
		if text := strings.TrimSpace(messageText(choice.Message.Content)); text != "" {
			return text
		}
	}
	return ""
}

func extractReasoningText(resp responsePayload) string {
	var parts []string
	for _, choice := range resp.Choices {
		if text := strings.TrimSpace(choice.Message.Reasoning); text != "" {
			parts = append(parts, text)
			continue
		}
		if text := strings.TrimSpace(choice.Message.ReasoningContent); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func parseTranslations(text string) []struct {
	Language       string
	TranslatedText string
} {
	trimmed := strings.TrimSpace(text)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)

	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil || len(payload) == 0 {
		return nil
	}
	languages := make([]string, 0, len(payload))
	texts := make(map[string]string, len(payload))
	for language, raw := range payload {
		var item struct {
			TranslatedText *string `json:"translated_text"`
		}
		if json.Unmarshal(raw, &item) != nil || item.TranslatedText == nil {
			continue
		}
		languages = append(languages, language)
		texts[language] = *item.TranslatedText
	}
	if len(languages) == 0 {
		return nil
	}
	sort.Strings(languages)
	out := make([]struct {
		Language       string
		TranslatedText string
	}, 0, len(languages))
	for _, language := range languages {
		out = append(out, struct {
			Language       string
			TranslatedText string
		}{Language: language, TranslatedText: texts[language]})
	}
	return out
}
