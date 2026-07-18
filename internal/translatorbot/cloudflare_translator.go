package translatorbot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	cloudflareModel                        = "@cf/google/gemma-4-26b-a4b-it"
	cloudflareAPIBaseURL                   = "https://api.cloudflare.com/client/v4"
	maxCloudflareResponseBytes             = 1 << 20
	translationCompletionBaseTokens        = 1024
	translationCompletionTokensPerLanguage = 5000
)

type CloudflareTranslator struct {
	baseURL   string
	accountID string
	apiToken  string
	gatewayID string
	client    *http.Client
}

func NewCloudflareTranslator(accountID, apiToken, gatewayID string) *CloudflareTranslator {
	return newCloudflareTranslator(cloudflareAPIBaseURL, accountID, apiToken, gatewayID, &http.Client{Timeout: 10 * time.Second})
}

func newCloudflareTranslator(baseURL, accountID, apiToken, gatewayID string, client *http.Client) *CloudflareTranslator {
	return &CloudflareTranslator{
		baseURL: strings.TrimRight(baseURL, "/"), accountID: accountID, apiToken: apiToken, gatewayID: gatewayID, client: client,
	}
}

type cloudflareChatRequest struct {
	Model               string                       `json:"model"`
	Messages            []cloudflareChatMessage      `json:"messages"`
	Temperature         float64                      `json:"temperature"`
	Stream              bool                         `json:"stream"`
	MaxCompletionTokens int                          `json:"max_completion_tokens"`
	ChatTemplateKwargs  cloudflareChatTemplateKwargs `json:"chat_template_kwargs"`
	ResponseFormat      cloudflareResponseFormat     `json:"response_format"`
}

type cloudflareChatTemplateKwargs struct {
	EnableThinking bool `json:"enable_thinking"`
}

type cloudflareChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type cloudflareResponseFormat struct {
	Type       string               `json:"type"`
	JSONSchema cloudflareJSONSchema `json:"json_schema"`
}

type cloudflareJSONSchema struct {
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

type cloudflareChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (t *CloudflareTranslator) TranslateMulti(ctx context.Context, targetLanguages []string, content string, translationContext TranslationContext, glossary []GlossaryEntry) (MultiTranslationResult, error) {
	prepared, err := prepareMultiTranslation(targetLanguages, content, translationContext, glossary)
	if err != nil {
		return MultiTranslationResult{}, err
	}
	if len(prepared.targetLanguages) == 0 {
		return MultiTranslationResult{Translations: map[string]string{}}, nil
	}
	payload := cloudflareChatRequest{
		Model: cloudflareModel,
		Messages: []cloudflareChatMessage{
			{Role: "system", Content: prepared.systemInstruction},
			{Role: "user", Content: prepared.userPrompt},
		},
		Temperature:         0.2,
		Stream:              false,
		MaxCompletionTokens: maxTranslationCompletionTokens(len(prepared.targetLanguages)),
		ChatTemplateKwargs:  cloudflareChatTemplateKwargs{EnableThinking: false},
		ResponseFormat: cloudflareResponseFormat{Type: "json_schema", JSONSchema: cloudflareJSONSchema{
			Name: "translation_response", Strict: true, Schema: multiTranslationJSONSchema(prepared.targetLanguages),
		}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return MultiTranslationResult{}, fmt.Errorf("encode Cloudflare request: %w", err)
	}
	endpoint := fmt.Sprintf("%s/accounts/%s/ai/v1/chat/completions", t.baseURL, t.accountID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return MultiTranslationResult{}, fmt.Errorf("create Cloudflare request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+t.apiToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("cf-aig-gateway-id", t.gatewayID)
	req.Header.Set("cf-aig-collect-log-payload", "false")
	metadata, err := json.Marshal(struct {
		GuildID   string `json:"guild_id"`
		MessageID string `json:"message_id"`
	}{GuildID: translationContext.GuildID, MessageID: translationContext.MessageID})
	if err != nil {
		return MultiTranslationResult{}, fmt.Errorf("encode Cloudflare metadata: %w", err)
	}
	req.Header.Set("cf-aig-metadata", string(metadata))

	resp, err := t.client.Do(req)
	if err != nil {
		return MultiTranslationResult{}, fmt.Errorf("Cloudflare translation request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxCloudflareResponseBytes))
		return MultiTranslationResult{}, fmt.Errorf("Cloudflare translation request failed with HTTP %d", resp.StatusCode)
	}
	limited := io.LimitReader(resp.Body, maxCloudflareResponseBytes+1)
	responseBody, err := io.ReadAll(limited)
	if err != nil {
		return MultiTranslationResult{}, fmt.Errorf("read Cloudflare response: %w", err)
	}
	if len(responseBody) > maxCloudflareResponseBytes {
		return MultiTranslationResult{}, fmt.Errorf("Cloudflare response exceeds %d bytes", maxCloudflareResponseBytes)
	}
	var parsed cloudflareChatResponse
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return MultiTranslationResult{}, fmt.Errorf("parse Cloudflare response envelope: %w", err)
	}
	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		return MultiTranslationResult{}, fmt.Errorf("Cloudflare response has no message content")
	}
	raw := strings.TrimSpace(parsed.Choices[0].Message.Content)
	translations, err := parseMultiTranslationResponse(raw, prepared.targetLanguages, prepared.protector)
	if err != nil {
		return MultiTranslationResult{}, err
	}
	result := MultiTranslationResult{Translations: translations, InputTokens: parsed.Usage.PromptTokens, OutputTokens: parsed.Usage.CompletionTokens}
	if result.InputTokens == 0 && result.OutputTokens == 0 {
		result.InputTokens = EstimateTranslationTokens(prepared.systemInstruction+prepared.userPrompt, raw)
	}
	return result, nil
}

// maxTranslationCompletionTokens reserves a fixed allowance for reasoning and
// the response envelope, plus enough room per language for a translation of a
// Discord Nitro-sized (4,000 character) source message and JSON overhead.
func maxTranslationCompletionTokens(languageCount int) int {
	return translationCompletionBaseTokens + translationCompletionTokensPerLanguage*languageCount
}

func multiTranslationJSONSchema(targetLanguages []string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"translations"},
		"properties": map[string]any{
			"translations": map[string]any{
				"type": "array", "minItems": len(targetLanguages), "maxItems": len(targetLanguages),
				"items": map[string]any{
					"type": "object", "additionalProperties": false,
					"required": []string{"language", "translated_text"},
					"properties": map[string]any{
						"language":        map[string]any{"type": "string", "enum": targetLanguages},
						"translated_text": map[string]any{"type": "string", "minLength": 1, "description": "The <final_message> translated into this item's language."},
					},
				},
			},
		},
	}
}
