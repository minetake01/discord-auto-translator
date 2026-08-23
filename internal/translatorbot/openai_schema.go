package translatorbot

import (
	"encoding/json"
	"errors"
)

func openaiObjectSchema(required []string, properties map[string]any) map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             required,
		"properties":           properties,
	}
}

func requireSchemaLanguages(targetLanguages []string) error {
	if len(targetLanguages) == 0 {
		return errors.New("translation JSON schema requires target languages")
	}
	for _, lang := range targetLanguages {
		if lang == "" {
			return errors.New("translation JSON schema has an empty language")
		}
	}
	return nil
}

func marshalOpenAIJSONSchema(schema map[string]any) (json.RawMessage, error) {
	encoded, err := json.Marshal(schema)
	if err != nil {
		return nil, errors.New("encode translation JSON schema")
	}
	return encoded, nil
}

func openaiLanguageKeyedSchema(targetLanguages []string, item map[string]any) (json.RawMessage, error) {
	if err := requireSchemaLanguages(targetLanguages); err != nil {
		return nil, err
	}
	properties := make(map[string]any, len(targetLanguages))
	required := make([]string, len(targetLanguages))
	for i, lang := range targetLanguages {
		properties[lang] = item
		required[i] = lang
	}
	return marshalOpenAIJSONSchema(openaiObjectSchema(required, properties))
}

func openaiMessageTranslationSchema(targetLanguages []string, altCount int) (json.RawMessage, error) {
	if altCount < 0 {
		return nil, errors.New("translation JSON schema has a negative alt count")
	}
	required := []string{"translated_text"}
	properties := map[string]any{
		"translated_text": map[string]any{
			"type":        "string",
			"description": "The <final_message> translated into this language. Empty when <final_message> is empty.",
		},
	}
	if altCount > 0 {
		required = append(required, "attachment_descriptions")
		properties["attachment_descriptions"] = map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"minItems":    altCount,
			"maxItems":    altCount,
			"description": "Exactly as many strings as <alt> elements in <attachment_alts>, in that order. Translate those existing alt texts. Never invent alt text. Do not describe background images from history, replies, or linked pages.",
		}
	}
	return openaiLanguageKeyedSchema(targetLanguages, openaiObjectSchema(required, properties))
}

func openaiPollTranslationSchema(targetLanguages []string) (json.RawMessage, error) {
	return openaiLanguageKeyedSchema(targetLanguages, openaiObjectSchema([]string{"question", "answers"}, map[string]any{
		"question": map[string]any{
			"type":        "string",
			"description": "The poll question translated into this language.",
		},
		"answers": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "The poll answers translated into this language, in source order.",
		},
	}))
}

func openaiThreadCreateTranslationSchema(targetLanguages []string) (json.RawMessage, error) {
	return openaiLanguageKeyedSchema(targetLanguages, openaiObjectSchema([]string{"name", "message"}, map[string]any{
		"name": map[string]any{
			"type":        "string",
			"description": "The thread <name> translated into this language.",
		},
		"message": map[string]any{
			"type":        "string",
			"description": "The initial thread <message> translated into this language. Empty when <message> was omitted.",
		},
	}))
}

func openaiTopicSummarySchema() (json.RawMessage, error) {
	return marshalOpenAIJSONSchema(openaiObjectSchema([]string{"summary"}, map[string]any{
		"summary": map[string]any{
			"type":        "string",
			"description": "A 2-4 sentence topic summary of the discarded conversation, for later translation background only.",
		},
	}))
}
