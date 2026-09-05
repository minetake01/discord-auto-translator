// OpenAI-compatible Chat Completions translation API client.
package translatorbot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testOpenAIBaseURL = "https://api.example.test/v1"
	testOpenAIAPIKey  = "test-api-key"
	testOpenAIModel   = "test-model"
)

type openaiRoundTripFunc func(*http.Request) (*http.Response, error)

func (f openaiRoundTripFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

func openaiContentText(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var parts []map[string]any
	if err := json.Unmarshal(raw, &parts); err != nil {
		t.Fatalf("content %s: %v", raw, err)
	}
	var b strings.Builder
	for _, part := range parts {
		if typ, _ := part["type"].(string); typ != "" && typ != "text" {
			continue
		}
		text, _ := part["text"].(string)
		b.WriteString(text)
	}
	return b.String()
}

func contentHasBreakpoint(t *testing.T, raw json.RawMessage) bool {
	t.Helper()
	var parts []map[string]any
	if err := json.Unmarshal(raw, &parts); err != nil {
		t.Fatalf("content %s: %v", raw, err)
	}
	for _, part := range parts {
		if _, ok := part["prompt_cache_breakpoint"]; ok {
			return true
		}
	}
	return false
}

func translateMulti(t testing.TB, ctx context.Context, translator *OpenAITranslator, targetLanguages []string, content string, translationContext TranslationContext, glossary []GlossaryEntry) (MultiTranslationResult, error) {
	t.Helper()
	prepared, err := prepareMultiTranslation(targetLanguages, content, translationContext, glossary)
	if err != nil {
		return MultiTranslationResult{}, err
	}
	return translator.TranslateMulti(ctx, prepared)
}

func translatePollMulti(t testing.TB, translator *OpenAITranslator, targetLanguages []string, question string, answers []string, translationContext TranslationContext, glossary []GlossaryEntry) (PollMultiTranslationResult, error) {
	t.Helper()
	prepared, err := preparePollTranslation(targetLanguages, question, answers, translationContext, glossary)
	if err != nil {
		return PollMultiTranslationResult{}, err
	}
	return translator.TranslatePollMulti(context.Background(), prepared)
}

func translateThreadCreateMulti(t testing.TB, translator *OpenAITranslator, targetLanguages []string, name, message string, translationContext TranslationContext, glossary []GlossaryEntry) (ThreadCreateMultiTranslationResult, error) {
	t.Helper()
	prepared, err := prepareThreadCreateTranslation(targetLanguages, name, message, translationContext, glossary)
	if err != nil {
		return ThreadCreateMultiTranslationResult{}, err
	}
	return translator.TranslateThreadCreateMulti(context.Background(), prepared)
}

func successfulOpenAIResponse(raw string, inputTokens, outputTokens int) string {
	encoded, _ := json.Marshal(raw)
	return `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":` + string(encoded) + `}}],"usage":{"prompt_tokens":` + fmtInt(inputTokens) + `,"completion_tokens":` + fmtInt(outputTokens) + `}}`
}

func fmtInt(value int) string { return strconv.Itoa(value) }

func testTranslator(client openaiHTTPClient) *OpenAITranslator {
	return newOpenAITranslator(client, testOpenAIAPIKey, testOpenAIModel, joinOpenAIChatCompletionsURL(testOpenAIBaseURL), "")
}

func requireJSONSchemaResponseFormat(t *testing.T, input openaiChatCompletionRequest, name string, schema json.RawMessage) {
	t.Helper()
	if input.ResponseFormat.Type != "json_schema" {
		t.Fatalf("response_format.type = %q", input.ResponseFormat.Type)
	}
	got := input.ResponseFormat.JSONSchema
	if got.Name != name || !got.Strict {
		t.Fatalf("json_schema name=%q strict=%t, want name=%q strict=true", got.Name, got.Strict, name)
	}
	var gotSchema, wantSchema any
	if err := json.Unmarshal(got.Schema, &gotSchema); err != nil {
		t.Fatalf("decode sent schema: %v", err)
	}
	if err := json.Unmarshal(schema, &wantSchema); err != nil {
		t.Fatalf("decode expected schema: %v", err)
	}
	gotJSON, err := json.Marshal(gotSchema)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, err := json.Marshal(wantSchema)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("json_schema.schema = %s, want %s", got.Schema, schema)
	}
}

func mustMessageSchema(t *testing.T, langs []string) json.RawMessage {
	t.Helper()
	schema, err := openaiMessageTranslationSchema(langs)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func mustAttachmentAltSchema(t *testing.T, langs []string, altCount int) json.RawMessage {
	t.Helper()
	schema, err := openaiAttachmentAltTranslationSchema(langs, altCount)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func mustPollSchema(t *testing.T, langs []string) json.RawMessage {
	t.Helper()
	schema, err := openaiPollTranslationSchema(langs)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func mustThreadCreateSchema(t *testing.T, langs []string) json.RawMessage {
	t.Helper()
	schema, err := openaiThreadCreateTranslationSchema(langs)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func mustTopicSummarySchema(t *testing.T) json.RawMessage {
	t.Helper()
	schema, err := openaiTopicSummarySchema()
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func schemaHasJSONSchemaDump(content string) bool {
	return strings.Contains(content, `"additionalProperties"`) || strings.Contains(content, `"enum"`)
}

func TestNewOpenAITranslatorRejectsInvalidConfig(t *testing.T) {
	for _, tt := range []struct {
		name    string
		baseURL string
		apiKey  string
		model   string
	}{
		{name: "empty base URL", apiKey: testOpenAIAPIKey, model: testOpenAIModel},
		{name: "invalid scheme", baseURL: "ftp://api.example.test/v1", apiKey: testOpenAIAPIKey, model: testOpenAIModel},
		{name: "empty api key", baseURL: testOpenAIBaseURL, model: testOpenAIModel},
		{name: "empty model", baseURL: testOpenAIBaseURL, apiKey: testOpenAIAPIKey},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewOpenAITranslator(context.Background(), tt.baseURL, tt.apiKey, tt.model, ""); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestNewOpenAIHTTPClientUsesKeepAliveTransport(t *testing.T) {
	client := newOpenAIHTTPClient()
	if client == http.DefaultClient {
		t.Fatal("expected dedicated client, got http.DefaultClient")
	}
	if client.Timeout != 0 {
		t.Fatalf("client Timeout = %s, want 0 (per-attempt context deadline owns limits)", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport == nil {
		t.Fatalf("Transport = %#v, want *http.Transport", client.Transport)
	}
	if transport.IdleConnTimeout != openaiHTTPIdleConnTimeout {
		t.Fatalf("IdleConnTimeout = %s, want %s", transport.IdleConnTimeout, openaiHTTPIdleConnTimeout)
	}
	if transport.DialContext == nil {
		t.Fatal("DialContext is nil")
	}
	translator, err := NewOpenAITranslator(context.Background(), testOpenAIBaseURL, testOpenAIAPIKey, testOpenAIModel, "")
	if err != nil {
		t.Fatal(err)
	}
	if translator.client == http.DefaultClient {
		t.Fatal("NewOpenAITranslator still uses http.DefaultClient")
	}
}

func TestOpenAITranslatorSendsVisionImagesAfterCachedText(t *testing.T) {
	client := openaiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		var input openaiChatCompletionRequest
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if len(input.Messages) != 2 {
			t.Fatalf("prompt shape = %#v", input.Messages)
		}
		if !contentHasBreakpoint(t, input.Messages[0].Content) {
			t.Fatalf("system missing prompt_cache_breakpoint: %s", input.Messages[0].Content)
		}
		var parts []map[string]any
		if err := json.Unmarshal(input.Messages[1].Content, &parts); err != nil {
			t.Fatalf("user content = %s", input.Messages[1].Content)
		}
		if len(parts) < 3 {
			t.Fatalf("parts = %#v", parts)
		}
		lastBreakpoint := -1
		imageIndex := -1
		variableHasBreakpoint := false
		for i, part := range parts {
			_, hasBP := part["prompt_cache_breakpoint"]
			if hasBP {
				lastBreakpoint = i
			}
			if part["type"] == "image_url" {
				imageIndex = i
			}
			text, _ := part["text"].(string)
			if strings.Contains(text, "<attachments>") && hasBP {
				variableHasBreakpoint = true
			}
		}
		if lastBreakpoint == -1 {
			t.Fatalf("missing prompt_cache_breakpoint: %#v", parts)
		}
		if imageIndex == -1 || imageIndex <= lastBreakpoint {
			t.Fatalf("image part must come after breakpoint: %#v", parts)
		}
		if variableHasBreakpoint {
			t.Fatalf("target message text must not be a cache breakpoint: %#v", parts)
		}
		if parts[0]["type"] != "text" {
			t.Fatalf("stable text should be first: %#v", parts)
		}
		imageURLObj, _ := parts[imageIndex]["image_url"].(map[string]any)
		imageURL, _ := imageURLObj["url"].(string)
		if !strings.HasPrefix(imageURL, "data:image/jpeg;base64,") {
			t.Fatalf("image_url = %#v", parts[imageIndex]["image_url"])
		}
		if !strings.Contains(openaiContentText(t, input.Messages[1].Content), "<attachments>") {
			t.Fatalf("text parts = %s", input.Messages[1].Content)
		}
		requireJSONSchemaResponseFormat(t, input, openaiMessageTranslationSchemaName, mustMessageSchema(t, []string{"en"}))
		if _, _, ok := schemaAttachmentDescriptionBounds(t, input.ResponseFormat.JSONSchema.Schema); ok {
			t.Fatal("message schema must not include attachment_descriptions")
		}
		if schemaHasJSONSchemaDump(openaiContentText(t, input.Messages[0].Content)) {
			t.Fatalf("schema must not be copied into the system instruction: %s", input.Messages[0].Content)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(successfulOpenAIResponse(`{"en":{"translated_text":"Hello"}}`, 1, 2)))}, nil
	})
	prepared, err := prepareMultiTranslation([]string{"en"}, "出口", TranslationContext{
		Attachments: []TranslationAttachment{{Index: 1, Filename: "sign.png", Description: "出口"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared.visionImages = []visionImage{{DataURL: jpegDataURL([]byte{0xff, 0xd8, 0xff})}}
	result, err := testTranslator(client).TranslateMulti(context.Background(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	if result.Translations["en"] != "Hello" {
		t.Fatalf("result = %#v", result)
	}
}

func TestOpenAITranslatorAttachmentAltsUseDedicatedSchema(t *testing.T) {
	client := openaiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		var input openaiChatCompletionRequest
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		requireJSONSchemaResponseFormat(t, input, openaiAttachmentAltTranslationSchemaName, mustAttachmentAltSchema(t, []string{"en"}, 1))
		minItems, maxItems, ok := schemaAttachmentDescriptionBounds(t, input.ResponseFormat.JSONSchema.Schema)
		if !ok || minItems != 1 || maxItems != 1 {
			t.Fatalf("attachment_descriptions bounds = %t %d %d", ok, minItems, maxItems)
		}
		if strings.Contains(openaiContentText(t, input.Messages[1].Content), "<final_message") {
			t.Fatalf("alt prompt must not include final_message: %s", input.Messages[1].Content)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(successfulOpenAIResponse(`{"en":{"attachment_descriptions":["Exit"]}}`, 1, 2)))}, nil
	})
	prepared, err := prepareAttachmentAltTranslation([]string{"en"}, TranslationContext{
		Attachments: []TranslationAttachment{{Index: 1, Filename: "sign.png", Description: "出口"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared.visionImages = []visionImage{{DataURL: jpegDataURL([]byte{0xff, 0xd8, 0xff})}}
	result, err := testTranslator(client).TranslateAttachmentAlts(context.Background(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	if result.Descriptions["en"][0] != "Exit" {
		t.Fatalf("result = %#v", result)
	}
}

func TestOpenAITranslatorIgnoresOGPAltsWhenThereAreNoAttachments(t *testing.T) {
	client := openaiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(successfulOpenAIResponse(`{"en":{"translated_text":"Hello"}}`, 1, 2)))}, nil
	})
	prepared, err := prepareMultiTranslation([]string{"en"}, "see this", TranslationContext{
		Sites: []SiteContextEntry{{ID: "1", Title: "Article", Description: "About", HasVisionImage: true}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared.visionImages = []visionImage{{DataURL: jpegDataURL([]byte{0xff, 0xd8, 0xff})}}
	result, err := testTranslator(client).TranslateMulti(context.Background(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	if result.Translations["en"] != "Hello" {
		t.Fatalf("result = %#v", result)
	}
}

func TestOpenAITranslatorRejectsAttachmentDescriptionsOnMessageResponse(t *testing.T) {
	client := openaiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(successfulOpenAIResponse(`{"en":{"translated_text":"Hello","attachment_descriptions":["Linked page hero image"]}}`, 1, 2)))}, nil
	})
	if _, err := translateMulti(t, context.Background(), testTranslator(client), []string{"en"}, "see this", TranslationContext{}, nil); err == nil {
		t.Fatal("expected unknown field error for attachment_descriptions on message translation")
	}
}

func TestOpenAITranslatorRequestContractAndResponseUsage(t *testing.T) {
	client := openaiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.String() != "https://api.example.test/v1/chat/completions" {
			t.Fatalf("request = %s %s", req.Method, req.URL)
		}
		if req.Header.Get("Content-Type") != "application/json" || req.Header.Get("Accept") != "application/json" {
			t.Fatalf("headers = %#v", req.Header)
		}
		if req.Header.Get("Authorization") != "Bearer "+testOpenAIAPIKey {
			t.Fatalf("Authorization = %q", req.Header.Get("Authorization"))
		}
		var input openaiChatCompletionRequest
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if input.Model != testOpenAIModel || input.MaxTokens != openaiMaxTokens {
			t.Fatalf("request config = %#v", input)
		}
		if len(input.Messages) != 2 || input.Messages[0].Role != "system" || input.Messages[1].Role != "user" {
			t.Fatalf("prompt shape = %#v", input.Messages)
		}
		requireJSONSchemaResponseFormat(t, input, openaiMessageTranslationSchemaName, mustMessageSchema(t, []string{"en"}))
		if schemaHasJSONSchemaDump(openaiContentText(t, input.Messages[0].Content)) {
			t.Fatalf("schema must not be copied into the system instruction: %s", input.Messages[0].Content)
		}
		if !strings.Contains(openaiContentText(t, input.Messages[1].Content), "<target_languages>en</target_languages>") {
			t.Fatalf("prompt contract = %#v", input.Messages)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(successfulOpenAIResponse(`{"en":{"translated_text":"Hello [USER:Alice]"}}`, 1, 2)))}, nil
	})
	result, err := translateMulti(t, context.Background(), testTranslator(client), []string{"en"}, "こんにちは <@42>", TranslationContext{
		GuildID: "guild-1", MessageID: "message-2", MentionedUsers: map[string]string{"42": "Alice"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Translations["en"] != "Hello <@42>" || result.InputTokens != 1 || result.OutputTokens != 2 {
		t.Fatalf("result = %#v", result)
	}
}

func TestOpenAITranslatorRejectsInvalidResponses(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "truncated", body: `{"choices":[{"finish_reason":"length","message":{"role":"assistant","content":"{}"}}],"usage":{"prompt_tokens":1,"completion_tokens":4096}}`},
		{name: "no choices", body: `{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1}}`},
		{name: "two choices", body: `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"x"}},{"finish_reason":"stop","message":{"role":"assistant","content":"y"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`},
		{name: "empty content", body: `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":""}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`},
		{name: "missing usage", body: `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"x"}}],"usage":null}`},
		{name: "malformed JSON", body: successfulOpenAIResponse("not-json", 1, 1)},
		{name: "missing language", body: successfulOpenAIResponse(`{"en":{"translated_text":"Hello"}}`, 1, 1)},
		{name: "extra language", body: successfulOpenAIResponse(`{"en":{"translated_text":"Hello"},"ja":{"translated_text":"こんにちは"},"fr":{"translated_text":"Bonjour"}}`, 1, 1)},
		{name: "empty translation", body: successfulOpenAIResponse(`{"en":{"translated_text":"Hello"},"ja":{"translated_text":" "}}`, 1, 1)},
		{name: "unknown translation field", body: successfulOpenAIResponse(`{"en":{"translated_text":"Hello"},"ja":{"translated_text":"こんにちは","extra":true}}`, 1, 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := openaiRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(tt.body))}, nil
			})
			_, err := translateMulti(t, context.Background(), testTranslator(client), []string{"en", "ja"}, "hello", TranslationContext{}, nil)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestOpenAITranslatorCallsOnceAndHonorsCancellation(t *testing.T) {
	var calls atomic.Int32
	client := openaiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := translateMulti(t, ctx, testTranslator(client), []string{"en"}, "private prompt", TranslationContext{}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestOpenAITranslatorSanitizesAPIErrors(t *testing.T) {
	client := openaiRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     http.Header{"X-Request-Id": []string{"request-123"}},
			Body: io.NopCloser(strings.NewReader(
				`{"error":{"type":"invalid_request_error","code":"unsupported_parameter","param":"metadata","message":"SECRET private prompt"}}`,
			)),
		}, nil
	})
	_, err := translateMulti(t, context.Background(), testTranslator(client), []string{"en"}, "private prompt", TranslationContext{}, nil)
	if err == nil || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "private prompt") {
		t.Fatalf("unsafe error = %v", err)
	}
	for _, expected := range []string{"HTTP 400", "type=invalid_request_error", "code=unsupported_parameter", "param=metadata", "request_id=request-123"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("error %q does not contain %q", err, expected)
		}
	}
}

func TestOpenAITranslatorOmitsUnsupportedRequestFields(t *testing.T) {
	bases := []string{testOpenAIBaseURL, "https://openrouter.ai/api/v1"}
	for _, base := range bases {
		t.Run(base, func(t *testing.T) {
			client := openaiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				body, err := io.ReadAll(req.Body)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(body), `"temperature"`) || strings.Contains(string(body), `"reasoning_effort"`) || strings.Contains(string(body), `"provider"`) || strings.Contains(string(body), "guild-1") || strings.Contains(string(body), "message-2") {
					t.Fatalf("request contains unsupported fields: %s", body)
				}
				if !strings.Contains(string(body), `"response_format"`) || !strings.Contains(string(body), `"json_schema"`) {
					t.Fatalf("request missing structured outputs: %s", body)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(successfulOpenAIResponse(`{"en":{"translated_text":"Hello"}}`, 1, 1)))}, nil
			})
			translator := newOpenAITranslator(client, testOpenAIAPIKey, testOpenAIModel, joinOpenAIChatCompletionsURL(base), "")
			_, err := translateMulti(t, context.Background(), translator, []string{"en"}, "hello", TranslationContext{GuildID: "guild-1", MessageID: "message-2"}, nil)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTranslationJSONSchemasAreStrictStructuredOutputs(t *testing.T) {
	langs := []string{"en", "ja"}
	schemas := []json.RawMessage{
		mustMessageSchema(t, langs),
		mustAttachmentAltSchema(t, langs, 2),
		mustPollSchema(t, langs),
		mustThreadCreateSchema(t, langs),
	}
	for _, schema := range schemas {
		assertStrictJSONSchema(t, schema, "root")
		gotLangs := schemaLanguageKeys(t, schema)
		if len(gotLangs) != 2 || gotLangs[0] != "en" || gotLangs[1] != "ja" {
			t.Fatalf("language keys = %#v, want [en ja]", gotLangs)
		}
		for _, lang := range gotLangs {
			if lang == "English" || lang == "Japanese" {
				t.Fatalf("language keys include an English name: %#v", gotLangs)
			}
		}
	}
}

func TestMessageTranslationSchemaOmitsAttachmentDescriptionsWithoutAlts(t *testing.T) {
	required := schemaItemRequired(t, mustMessageSchema(t, []string{"en"}))
	if required["language"] || !required["translated_text"] || required["attachment_descriptions"] {
		t.Fatalf("required = %#v", required)
	}
	if _, _, ok := schemaAttachmentDescriptionBounds(t, mustMessageSchema(t, []string{"en"})); ok {
		t.Fatal("attachment_descriptions must be absent from message translation schema")
	}
}

func TestAttachmentAltTranslationSchemaRequiresBounds(t *testing.T) {
	schema := mustAttachmentAltSchema(t, []string{"en"}, 2)
	required := schemaItemRequired(t, schema)
	if required["language"] || required["translated_text"] || !required["attachment_descriptions"] {
		t.Fatalf("required = %#v", required)
	}
	minItems, maxItems, ok := schemaAttachmentDescriptionBounds(t, schema)
	if !ok || minItems != 2 || maxItems != 2 {
		t.Fatalf("attachment_descriptions bounds = %t %d %d", ok, minItems, maxItems)
	}
}

func TestOpenAITranslatorLanguageKeysMatchTargetLanguages(t *testing.T) {
	client := openaiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		var input openaiChatCompletionRequest
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		requireJSONSchemaResponseFormat(t, input, openaiMessageTranslationSchemaName, mustMessageSchema(t, []string{"en", "ja"}))
		got := schemaLanguageKeys(t, input.ResponseFormat.JSONSchema.Schema)
		if len(got) != 2 || got[0] != "en" || got[1] != "ja" {
			t.Fatalf("language keys = %#v", got)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(successfulOpenAIResponse(`{"en":{"translated_text":"Hello"},"ja":{"translated_text":"こんにちは"}}`, 1, 1)))}, nil
	})
	if _, err := translateMulti(t, context.Background(), testTranslator(client), []string{"en", "ja"}, "hello", TranslationContext{}, nil); err != nil {
		t.Fatal(err)
	}
}

func schemaLanguageKeys(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	required, _ := root["required"].([]any)
	out := make([]string, 0, len(required))
	for _, v := range required {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("language keys contain %#v", v)
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		t.Fatal("language keys are empty")
	}
	return out
}

func schemaItemRequired(t *testing.T, raw json.RawMessage) map[string]bool {
	t.Helper()
	item := schemaTranslationItem(t, raw)
	required, _ := item["required"].([]any)
	out := make(map[string]bool, len(required))
	for _, name := range required {
		s, ok := name.(string)
		if !ok {
			t.Fatalf("required contains %#v", name)
		}
		out[s] = true
	}
	return out
}

func schemaTranslationItem(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	props, _ := root["properties"].(map[string]any)
	required, _ := root["required"].([]any)
	if len(required) == 0 {
		t.Fatalf("missing language keys: %s", raw)
	}
	lang, _ := required[0].(string)
	item, _ := props[lang].(map[string]any)
	if item == nil {
		t.Fatalf("missing language object %q: %s", lang, raw)
	}
	return item
}

func schemaAttachmentDescriptionBounds(t *testing.T, raw json.RawMessage) (minItems, maxItems int, present bool) {
	t.Helper()
	item := schemaTranslationItem(t, raw)
	props, _ := item["properties"].(map[string]any)
	desc, ok := props["attachment_descriptions"].(map[string]any)
	if !ok {
		return 0, 0, false
	}
	min, _ := desc["minItems"].(float64)
	max, _ := desc["maxItems"].(float64)
	return int(min), int(max), true
}

func assertStrictJSONSchema(t *testing.T, raw json.RawMessage, path string) {
	t.Helper()
	var node map[string]any
	if err := json.Unmarshal(raw, &node); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	switch typ, _ := node["type"].(string); typ {
	case "array":
		items, err := json.Marshal(node["items"])
		if err != nil {
			t.Fatalf("%s.items: %v", path, err)
		}
		assertStrictJSONSchema(t, items, path+"[]")
		return
	case "object":
	default:
		if node["properties"] == nil {
			return
		}
	}
	if node["additionalProperties"] != false {
		t.Fatalf("%s: additionalProperties must be false, got %#v", path, node["additionalProperties"])
	}
	props, _ := node["properties"].(map[string]any)
	required, _ := node["required"].([]any)
	req := make(map[string]bool, len(required))
	for _, name := range required {
		s, ok := name.(string)
		if !ok {
			t.Fatalf("%s: required contains %#v", path, name)
		}
		req[s] = true
		if _, ok := props[s]; !ok {
			t.Fatalf("%s: required property %q is missing", path, s)
		}
	}
	if len(props) == 0 {
		t.Fatalf("%s: object schema has no properties", path)
	}
	if len(req) != len(props) {
		t.Fatalf("%s: required must list every property, required=%d properties=%d", path, len(req), len(props))
	}
	for name, prop := range props {
		propJSON, err := json.Marshal(prop)
		if err != nil {
			t.Fatalf("%s.%s: %v", path, name, err)
		}
		assertStrictJSONSchema(t, propJSON, path+"."+name)
	}
}

func TestOpenAITranslatorSendsConfiguredReasoningEffort(t *testing.T) {
	client := openaiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		var input openaiChatCompletionRequest
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if input.ReasoningEffort != "none" {
			t.Fatalf("reasoning_effort = %q, want none", input.ReasoningEffort)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(successfulOpenAIResponse(`{"en":{"translated_text":"Hello"}}`, 1, 1)))}, nil
	})
	translator := newOpenAITranslator(client, testOpenAIAPIKey, testOpenAIModel, joinOpenAIChatCompletionsURL(testOpenAIBaseURL), "none")
	if _, err := translateMulti(t, context.Background(), translator, []string{"en"}, "hello", TranslationContext{}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestNewOpenAITranslatorRejectsInvalidReasoningEffort(t *testing.T) {
	if _, err := NewOpenAITranslator(context.Background(), testOpenAIBaseURL, testOpenAIAPIKey, testOpenAIModel, "off"); err == nil || !strings.Contains(err.Error(), "OPENAI_REASONING_EFFORT") {
		t.Fatalf("error = %v, want OPENAI_REASONING_EFFORT", err)
	}
}

func TestOpenAITranslatorMessageSystemInstructionIgnoresAttachments(t *testing.T) {
	var systems []string
	client := openaiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		var input openaiChatCompletionRequest
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		systems = append(systems, openaiContentText(t, input.Messages[0].Content))
		body := successfulOpenAIResponse(`{"en":{"translated_text":"Hello"}}`, 1, 1)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	translator := testTranslator(client)
	if _, err := translateMulti(t, context.Background(), translator, []string{"en"}, "hello", TranslationContext{}, nil); err != nil {
		t.Fatal(err)
	}
	prepared, err := prepareMultiTranslation([]string{"en"}, "出口", TranslationContext{
		Attachments: []TranslationAttachment{{Index: 1, Filename: "sign.png", Description: "出口"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared.visionImages = []visionImage{{DataURL: jpegDataURL([]byte{0xff, 0xd8, 0xff})}}
	if _, err := translator.TranslateMulti(context.Background(), prepared); err != nil {
		t.Fatal(err)
	}
	if len(systems) != 2 {
		t.Fatalf("calls = %d, want 2", len(systems))
	}
	if systems[0] != systems[1] {
		t.Fatalf("system instruction changed when attachments were present:\n%s\n---\n%s", systems[0], systems[1])
	}
}

func TestOpenAITranslatorSendsExplicitPromptCacheWithoutTTL(t *testing.T) {
	var bodies []string
	client := openaiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, string(body))
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(successfulOpenAIResponse(`{"en":{"translated_text":"Hello"}}`, 1, 1)))}, nil
	})
	translator := testTranslator(client)
	tc := TranslationContext{
		PromptCacheLocation:   "guild:g:group",
		PromptCacheGeneration: "start1",
	}
	if _, err := translateMulti(t, context.Background(), translator, []string{"en"}, "hello", tc, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := translateMulti(t, context.Background(), translator, []string{"en"}, "hello", tc, nil); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 {
		t.Fatalf("calls = %d, want 2", len(bodies))
	}
	for i, body := range bodies {
		if !strings.Contains(body, `"prompt_cache_key":"guild:g:group"`) {
			t.Fatalf("request %d missing cache key: %s", i, body)
		}
		if !strings.Contains(body, `"prompt_cache_breakpoint"`) {
			t.Fatalf("request %d missing breakpoint: %s", i, body)
		}
		if !strings.Contains(body, `"prompt_cache_options"`) || !strings.Contains(body, `"explicit"`) {
			t.Fatalf("request %d must stay in explicit cache mode: %s", i, body)
		}
		if strings.Contains(body, `"ttl"`) {
			t.Fatalf("request %d must not send cache ttl: %s", i, body)
		}
	}
}

func TestOpenAITranslatorMarksBreakpointsWithoutTTLForShortPrefix(t *testing.T) {
	client := openaiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), `"prompt_cache_key":"guild:g:group:poll"`) {
			t.Fatalf("short prompts still send a cache key for sticky routing: %s", body)
		}
		if !strings.Contains(string(body), `"prompt_cache_breakpoint"`) {
			t.Fatalf("short prompts still mark breakpoints: %s", body)
		}
		if !strings.Contains(string(body), `"mode":"explicit"`) {
			t.Fatalf("short prompts must stay in explicit cache mode: %s", body)
		}
		if strings.Contains(string(body), `"ttl"`) {
			t.Fatalf("short prefix must not send cache ttl: %s", body)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(successfulOpenAIResponse(`{"en":{"question":"Q","answers":["A"]}}`, 1, 1)))}, nil
	})
	translator := testTranslator(client)
	tc := TranslationContext{PromptCacheLocation: "guild:g:group", PromptCacheGeneration: "empty"}
	if _, err := translatePollMulti(t, translator, []string{"en"}, "Q", []string{"A"}, tc, nil); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAITranslatorMarksSeparateHistoryBreakpoint(t *testing.T) {
	client := openaiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		var input openaiChatCompletionRequest
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if !contentHasBreakpoint(t, input.Messages[0].Content) {
			t.Fatalf("system missing prompt_cache_breakpoint: %s", input.Messages[0].Content)
		}
		var parts []map[string]any
		if err := json.Unmarshal(input.Messages[1].Content, &parts); err != nil {
			t.Fatalf("user content = %s", input.Messages[1].Content)
		}
		if len(parts) != 3 {
			t.Fatalf("parts = %#v", parts)
		}
		if _, ok := parts[0]["prompt_cache_breakpoint"]; !ok {
			t.Fatalf("stable part missing breakpoint: %#v", parts[0])
		}
		if _, ok := parts[1]["prompt_cache_breakpoint"]; !ok {
			t.Fatalf("history part missing breakpoint: %#v", parts[1])
		}
		if _, ok := parts[2]["prompt_cache_breakpoint"]; ok {
			t.Fatalf("target message must not have a breakpoint: %#v", parts[2])
		}
		stable, _ := parts[0]["text"].(string)
		history, _ := parts[1]["text"].(string)
		variable, _ := parts[2]["text"].(string)
		if strings.Contains(stable, "<recent_context>") || strings.Contains(stable, "<topic_summary>") {
			t.Fatalf("stable includes history: %s", stable)
		}
		if !strings.Contains(history, "<recent_context>") || !strings.Contains(history, "earlier") {
			t.Fatalf("history missing recent_context: %s", history)
		}
		if !strings.Contains(variable, "<final_message") || strings.Contains(variable, "<recent_context>") {
			t.Fatalf("variable should be the target message: %s", variable)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(successfulOpenAIResponse(`{"en":{"translated_text":"Hello"}}`, 1, 1)))}, nil
	})
	tc := TranslationContext{
		History: []ChatContextMessage{{Author: "alice", Content: "earlier"}},
	}
	if _, err := translateMulti(t, context.Background(), testTranslator(client), []string{"en"}, "hello", tc, nil); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAITranslatorSeparatesPollPromptCacheKey(t *testing.T) {
	var keys []string
	client := openaiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		var input openaiChatCompletionRequest
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, input.PromptCacheKey)
		if len(keys) == 2 {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(successfulOpenAIResponse(`{"en":{"question":"Q","answers":["A"]}}`, 1, 1)))}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(successfulOpenAIResponse(`{"en":{"translated_text":"Hello"}}`, 1, 1)))}, nil
	})
	translator := testTranslator(client)
	tc := TranslationContext{PromptCacheLocation: "guild:g:group", PromptCacheGeneration: "empty"}
	if _, err := translateMulti(t, context.Background(), translator, []string{"en"}, "hello", tc, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := translatePollMulti(t, translator, []string{"en"}, "Q", []string{"A"}, tc, nil); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || keys[0] == keys[1] {
		t.Fatalf("keys = %#v", keys)
	}
	if keys[0] != "guild:g:group" || keys[1] != "guild:g:group:poll" {
		t.Fatalf("keys = %#v", keys)
	}
}

func TestOpenAITranslatorDebugLogRecordsRequestAndRawResponse(t *testing.T) {
	response := `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{\"en\":{\"translated_text\":\"Hello [USER:Alice]\"}}"}}],"usage":{"prompt_tokens":1,"completion_tokens":2,"completion_tokens_details":{"reasoning_tokens":7}}}`
	client := openaiRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(response))}, nil
	})
	translator, path := debugLoggingTranslator(t, client)
	if _, err := translateMulti(t, context.Background(), translator, []string{"en"}, "こんにちは <@42>", TranslationContext{
		GuildID: "guild-1", MessageID: "message-2", MentionedUsers: map[string]string{"42": "Alice"},
	}, nil); err != nil {
		t.Fatal(err)
	}

	entry := singleDebugLogEntry(t, path)
	if entry.GuildID != "guild-1" || entry.MessageID != "message-2" {
		t.Fatalf("correlation keys = %#v", entry)
	}
	if len(entry.TargetLanguages) != 1 || entry.TargetLanguages[0] != "en" {
		t.Fatalf("target languages = %#v", entry.TargetLanguages)
	}
	if entry.HTTPStatus != http.StatusOK || entry.Error != "" {
		t.Fatalf("status = %d, error = %q", entry.HTTPStatus, entry.Error)
	}
	var request openaiChatCompletionRequest
	if err := json.Unmarshal(entry.Request, &request); err != nil {
		t.Fatal(err)
	}
	if len(request.Messages) != 2 {
		t.Fatalf("logged prompt shape = %#v", request.Messages)
	}
	requireJSONSchemaResponseFormat(t, request, openaiMessageTranslationSchemaName, mustMessageSchema(t, []string{"en"}))
	if schemaHasJSONSchemaDump(openaiContentText(t, request.Messages[0].Content)) {
		t.Fatalf("logged system instruction still contains schema: %s", request.Messages[0].Content)
	}
	if !strings.Contains(openaiContentText(t, request.Messages[1].Content), "<final_message>こんにちは [USER:Alice]</final_message>") {
		t.Fatalf("logged user prompt = %q", openaiContentText(t, request.Messages[1].Content))
	}
	if string(entry.Response) != response || entry.ResponseText != "" {
		t.Fatalf("logged response = %s", entry.Response)
	}
	if entry.Model != testOpenAIModel || entry.SchemaName != openaiMessageTranslationSchemaName {
		t.Fatalf("model/schema = %q %q", entry.Model, entry.SchemaName)
	}
	if entry.SystemInstruction == "" || !strings.Contains(entry.UserPromptVariable, "<final_message>こんにちは [USER:Alice]</final_message>") {
		t.Fatalf("logged synthesized prompts system=%q stable=%q variable=%q", entry.SystemInstruction, entry.UserPromptStable, entry.UserPromptVariable)
	}
	if entry.Usage == nil || entry.Usage.PromptTokens != 1 || entry.Usage.CompletionTokens != 2 {
		t.Fatalf("logged usage = %#v", entry.Usage)
	}
	if entry.Usage.ReasoningTokens == nil || *entry.Usage.ReasoningTokens != 7 {
		t.Fatalf("logged reasoning tokens = %#v", entry.Usage.ReasoningTokens)
	}
	if entry.PromptCacheHit != nil {
		t.Fatalf("cache hit should be unknown without cached_tokens: %#v", entry.PromptCacheHit)
	}
	if entry.Attempt != 1 || entry.Ended == nil || entry.WaitMS == nil || entry.ReadMS == nil {
		t.Fatalf("timing fields = attempt=%d ended=%v wait=%v read=%v", entry.Attempt, entry.Ended, entry.WaitMS, entry.ReadMS)
	}
}

func TestOpenAITranslatorDebugLogRecordsTimingFromSameRoundTrip(t *testing.T) {
	response := `{"id":"chatcmpl-1","created":1700000000,"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{\"en\":{\"translated_text\":\"Hello\"}}"}}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_time":0.53,"queue_time":0.01}}`
	client := openaiRoundTripFunc(func(*http.Request) (*http.Response, error) {
		time.Sleep(25 * time.Millisecond)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Openai-Processing-Ms": []string{"790"},
				"Server-Timing":        []string{"total;dur=800"},
			},
			Body: io.NopCloser(strings.NewReader(response)),
		}, nil
	})
	translator, path := debugLoggingTranslator(t, client)
	if _, err := translateMulti(t, context.Background(), translator, []string{"en"}, "hello", TranslationContext{}, nil); err != nil {
		t.Fatal(err)
	}

	entry := singleDebugLogEntry(t, path)
	if entry.Attempt != 1 {
		t.Fatalf("attempt = %d", entry.Attempt)
	}
	if entry.WaitMS == nil || *entry.WaitMS < 20 {
		t.Fatalf("wait_ms = %#v, want at least 20ms of HTTP wait", entry.WaitMS)
	}
	if entry.ReadMS == nil {
		t.Fatal("read_ms missing")
	}
	if entry.DurationMS < *entry.WaitMS {
		t.Fatalf("duration_ms=%d wait_ms=%d", entry.DurationMS, *entry.WaitMS)
	}
	if entry.Ended == nil || entry.Ended.Before(entry.Time) {
		t.Fatalf("ended=%v time=%v", entry.Ended, entry.Time)
	}
	if entry.ResponseCreated == nil || *entry.ResponseCreated != 1700000000 {
		t.Fatalf("response_created = %#v", entry.ResponseCreated)
	}
	if entry.ProcessingMS == nil || *entry.ProcessingMS != 790 {
		t.Fatalf("processing_ms = %#v", entry.ProcessingMS)
	}
	if entry.ServerTiming != "total;dur=800" {
		t.Fatalf("server_timing = %q", entry.ServerTiming)
	}
	if entry.Usage == nil || !floatPtrEqual(entry.Usage.TotalTime, floatPtr(0.53)) {
		t.Fatalf("usage total_time = %#v", entry.Usage)
	}
}

func TestOpenAITranslatorDebugLogRecordsCacheHitAndCost(t *testing.T) {
	response := `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{\"en\":{\"translated_text\":\"Hello\"}}"}}],"usage":{"prompt_tokens":1200,"completion_tokens":40,"total_tokens":1240,"cost":0.00014,"prompt_tokens_details":{"cached_tokens":800},"completion_tokens_details":{"reasoning_tokens":5}}}`
	client := openaiRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(response))}, nil
	})
	translator, path := debugLoggingTranslator(t, client)
	if _, err := translateMulti(t, context.Background(), translator, []string{"en"}, "hello", TranslationContext{
		GuildID: "guild-1", MessageID: "message-2", PromptCacheLocation: "guild:g:group", PromptCacheGeneration: "start1",
	}, nil); err != nil {
		t.Fatal(err)
	}

	entry := singleDebugLogEntry(t, path)
	if entry.PromptCacheKey != "guild:g:group" {
		t.Fatalf("cache key = %q", entry.PromptCacheKey)
	}
	if entry.Usage == nil || entry.Usage.CachedTokens == nil || *entry.Usage.CachedTokens != 800 {
		t.Fatalf("cached tokens = %#v", entry.Usage)
	}
	if entry.PromptCacheHit == nil || !*entry.PromptCacheHit {
		t.Fatalf("prompt_cache_hit = %#v", entry.PromptCacheHit)
	}
	if entry.Usage.CostUSD == nil || strconv.FormatFloat(*entry.Usage.CostUSD, 'f', 8, 64) != strconv.FormatFloat(0.00014, 'f', 8, 64) {
		t.Fatalf("cost = %#v", entry.Usage.CostUSD)
	}
}

func TestExtractLoggedUsageFromProviderShapes(t *testing.T) {
	tests := []struct {
		name string
		body string
		want openaiLoggedUsage
	}{
		{
			name: "openai cached_tokens in details",
			body: `{"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":80}}}`,
			want: openaiLoggedUsage{PromptTokens: 100, CompletionTokens: 20, CachedTokens: intPtr(80)},
		},
		{
			name: "openrouter cost and cache miss",
			body: `{"usage":{"prompt_tokens":50,"completion_tokens":10,"total_tokens":60,"cost":0.0002,"prompt_tokens_details":{"cached_tokens":0}}}`,
			want: openaiLoggedUsage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60, CachedTokens: intPtr(0), CostUSD: floatPtr(0.0002)},
		},
		{
			name: "top-level cached_tokens fallback",
			body: `{"usage":{"prompt_tokens":10,"completion_tokens":2,"cached_tokens":4}}`,
			want: openaiLoggedUsage{PromptTokens: 10, CompletionTokens: 2, CachedTokens: intPtr(4)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractLoggedUsage([]byte(tt.body))
			if got == nil {
				t.Fatal("usage is nil")
			}
			if got.PromptTokens != tt.want.PromptTokens || got.CompletionTokens != tt.want.CompletionTokens || got.TotalTokens != tt.want.TotalTokens {
				t.Fatalf("tokens = %#v, want %#v", got, tt.want)
			}
			if !intPtrEqual(got.CachedTokens, tt.want.CachedTokens) {
				t.Fatalf("cached = %#v, want %#v", got.CachedTokens, tt.want.CachedTokens)
			}
			if !floatPtrEqual(got.CostUSD, tt.want.CostUSD) {
				t.Fatalf("cost = %#v, want %#v", got.CostUSD, tt.want.CostUSD)
			}
		})
	}
	if usage := extractLoggedUsage([]byte(`{"choices":[]}`)); usage != nil {
		t.Fatalf("missing usage should be nil, got %#v", usage)
	}
	got := extractLoggedUsage([]byte(`{"usage":{"prompt_tokens":1,"completion_tokens":1,"queue_time":0.01,"prompt_time":0.02,"completion_time":0.5,"total_time":0.53}}`))
	if got == nil || !floatPtrEqual(got.TotalTime, floatPtr(0.53)) || !floatPtrEqual(got.QueueTime, floatPtr(0.01)) {
		t.Fatalf("provider timing = %#v", got)
	}
	if created := extractResponseCreated([]byte(`{"created":1700000000,"choices":[]}`)); created == nil || *created != 1700000000 {
		t.Fatalf("created = %#v", created)
	}
}

func intPtr(v int) *int { return &v }

func floatPtr(v float64) *float64 { return &v }

func intPtrEqual(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func floatPtrEqual(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func TestOpenAITranslatorDebugLogRecordsProviderErrorBody(t *testing.T) {
	client := openaiRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     http.Header{"X-Request-Id": []string{"request-123"}},
			Body: io.NopCloser(strings.NewReader(
				`{"error":{"type":"invalid_request_error","code":"unsupported_parameter","param":"metadata","message":"SECRET private prompt"}}`,
			)),
		}, nil
	})
	translator, path := debugLoggingTranslator(t, client)
	_, err := translateMulti(t, context.Background(), translator, []string{"en"}, "private prompt", TranslationContext{}, nil)
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("unsafe error = %v", err)
	}

	entry := singleDebugLogEntry(t, path)
	if entry.HTTPStatus != http.StatusBadRequest {
		t.Fatalf("status = %d", entry.HTTPStatus)
	}
	if !strings.Contains(string(entry.Response), "SECRET private prompt") {
		t.Fatalf("logged response = %s", entry.Response)
	}
	if !strings.Contains(entry.Error, "HTTP 400") || !strings.Contains(string(entry.Request), "private prompt") {
		t.Fatalf("entry = %#v", entry)
	}
}

func TestOpenAITranslatorDebugLogRecordsTransportFailure(t *testing.T) {
	client := openaiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, req.Context().Err()
	})
	translator, path := debugLoggingTranslator(t, client)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := translateMulti(t, ctx, translator, []string{"en"}, "hello", TranslationContext{}, nil); err == nil {
		t.Fatal("expected error")
	}

	entry := singleDebugLogEntry(t, path)
	if entry.HTTPStatus != 0 || entry.Response != nil || entry.ResponseText != "" {
		t.Fatalf("entry has response data: %#v", entry)
	}
	if !strings.Contains(entry.Error, context.Canceled.Error()) || len(entry.Request) == 0 {
		t.Fatalf("entry = %#v", entry)
	}
}

func TestOpenAITranslatorWithoutDebugLogWritesNoFile(t *testing.T) {
	dir := t.TempDir()
	client := openaiRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(successfulOpenAIResponse(`{"en":{"translated_text":"Hello"}}`, 1, 1)))}, nil
	})
	if _, err := translateMulti(t, context.Background(), testTranslator(client), []string{"en"}, "hello", TranslationContext{GuildID: "guild-1"}, nil); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("unexpected files = %#v", entries)
	}
}

func debugLoggingTranslator(t *testing.T, client openaiHTTPClient) (*OpenAITranslator, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "translation-debug.log")
	debugLog, err := OpenDebugLog(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := debugLog.Close(); err != nil {
			t.Error(err)
		}
	})
	translator := testTranslator(client)
	translator.SetDebugLog(debugLog)
	return translator, path
}

func singleDebugLogEntry(t *testing.T, path string) openaiDebugEntry {
	t.Helper()
	lines := readDebugLogLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("debug log lines = %d, want 1", len(lines))
	}
	var entry openaiDebugEntry
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatal(err)
	}
	return entry
}

func TestOpenAITranslatorRetriesTransientHTTPErrorOnce(t *testing.T) {
	var calls atomic.Int32
	client := openaiRoundTripFunc(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"service_unavailable","code":"unavailable"}}`)),
			}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(successfulOpenAIResponse(`{"en":{"translated_text":"Hello"}}`, 1, 1)))}, nil
	})
	result, err := translateMulti(t, context.Background(), testTranslator(client), []string{"en"}, "hello", TranslationContext{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Translations["en"] != "Hello" {
		t.Fatalf("result = %#v", result)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}

func TestOpenAITranslatorRetriesDeadlineExceededOnce(t *testing.T) {
	var calls atomic.Int32
	client := openaiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return nil, context.DeadlineExceeded
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(successfulOpenAIResponse(`{"en":{"translated_text":"Hello"}}`, 1, 1)))}, nil
	})
	result, err := translateMulti(t, context.Background(), testTranslator(client), []string{"en"}, "hello", TranslationContext{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Translations["en"] != "Hello" || calls.Load() != 2 {
		t.Fatalf("result = %#v, calls = %d", result, calls.Load())
	}
}

func TestOpenAITranslatorDoesNotRetryClientHTTPError(t *testing.T) {
	var calls atomic.Int32
	client := openaiRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","code":"bad_request"}}`)),
		}, nil
	})
	_, err := translateMulti(t, context.Background(), testTranslator(client), []string{"en"}, "hello", TranslationContext{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, errTranslationProvider) {
		t.Fatalf("client HTTP error must not be a provider outage: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestOpenAITranslatorDoesNotRetryContractViolation(t *testing.T) {
	var calls atomic.Int32
	client := openaiRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(
			`{"choices":[{"finish_reason":"length","message":{"role":"assistant","content":"{}"}],"usage":{"prompt_tokens":1,"completion_tokens":4096}}`,
		))}, nil
	})
	_, err := translateMulti(t, context.Background(), testTranslator(client), []string{"en"}, "hello", TranslationContext{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, errTranslationProvider) {
		t.Fatalf("contract violation must not be a provider outage: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestOpenAITranslatorReturnsLastErrorAfterRetryExhausted(t *testing.T) {
	var calls atomic.Int32
	client := openaiRoundTripFunc(func(*http.Request) (*http.Response, error) {
		n := calls.Add(1)
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"service_unavailable","code":"unavailable","message":"attempt-` + fmtInt(int(n)) + `"}}`)),
		}, nil
	})
	_, err := translateMulti(t, context.Background(), testTranslator(client), []string{"en"}, "hello", TranslationContext{}, nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 503") {
		t.Fatalf("error = %v", err)
	}
	if !errors.Is(err, errTranslationProvider) {
		t.Fatalf("exhausted retryable HTTP error must be a provider outage: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}

func TestOpenAIWarmUpUsesAttemptTimeout(t *testing.T) {
	client := openaiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		got, ok := req.Context().Deadline()
		if !ok {
			t.Fatal("missing deadline")
		}
		remaining := time.Until(got)
		if remaining < 59*time.Second || remaining > openaiRequestTimeout {
			t.Fatalf("remaining = %s, want ~%s", remaining, openaiRequestTimeout)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(successfulOpenAIResponse(`{"en":{"translated_text":"warmup"}}`, 1, 1)))}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := testTranslator(client).WarmUp(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAITranslatorTranslatePollMulti(t *testing.T) {
	client := openaiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		var input openaiChatCompletionRequest
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		requireJSONSchemaResponseFormat(t, input, openaiPollTranslationSchemaName, mustPollSchema(t, []string{"en"}))
		if schemaHasJSONSchemaDump(openaiContentText(t, input.Messages[0].Content)) {
			t.Fatalf("poll schema must not be copied into the system instruction: %s", input.Messages[0].Content)
		}
		if !strings.Contains(openaiContentText(t, input.Messages[1].Content), "<poll>") {
			t.Fatalf("unexpected request: %#v", input.Messages)
		}
		body := successfulOpenAIResponse(`{"en":{"question":"Favorite?","answers":["Red","Blue"]}}`, 3, 4)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	result, err := translatePollMulti(t, testTranslator(client), []string{"en"}, "好き？", []string{"赤", "青"}, TranslationContext{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := result.Translations["en"]
	if got.Question != "Favorite?" || len(got.Answers) != 2 || got.Answers[0] != "Red" || got.Answers[1] != "Blue" {
		t.Fatalf("got %#v", got)
	}
	if result.InputTokens != 3 || result.OutputTokens != 4 {
		t.Fatalf("tokens = %d/%d", result.InputTokens, result.OutputTokens)
	}
}

func TestOpenAITranslatorTranslatePollMultiRejectsAnswerCountMismatch(t *testing.T) {
	client := openaiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := successfulOpenAIResponse(`{"en":{"question":"Favorite?","answers":["Red"]}}`, 1, 1)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	_, err := translatePollMulti(t, testTranslator(client), []string{"en"}, "好き？", []string{"赤", "青"}, TranslationContext{}, nil)
	if err == nil {
		t.Fatal("expected answer count mismatch")
	}
}

func TestOpenAITranslatorTranslateThreadCreateMulti(t *testing.T) {
	client := openaiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		var input openaiChatCompletionRequest
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		requireJSONSchemaResponseFormat(t, input, openaiThreadCreateTranslationSchemaName, mustThreadCreateSchema(t, []string{"en"}))
		if schemaHasJSONSchemaDump(openaiContentText(t, input.Messages[0].Content)) {
			t.Fatalf("thread-create schema must not be copied into the system instruction: %s", input.Messages[0].Content)
		}
		if !strings.Contains(openaiContentText(t, input.Messages[1].Content), "<thread_create>") {
			t.Fatalf("unexpected request: %#v", input.Messages)
		}
		userPrompt := openaiContentText(t, input.Messages[1].Content)
		if !strings.Contains(userPrompt, "<name>議題</name>") || !strings.Contains(userPrompt, `<message author="alice">本文</message>`) {
			t.Fatalf("unexpected user prompt: %s", userPrompt)
		}
		body := successfulOpenAIResponse(`{"en":{"name":"Topic","message":"Body"}}`, 3, 4)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	result, err := translateThreadCreateMulti(t, testTranslator(client), []string{"en"}, "議題", "本文", TranslationContext{Author: "alice"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := result.Translations["en"]
	if got.Name != "Topic" || got.Message != "Body" {
		t.Fatalf("got %#v", got)
	}
	if result.InputTokens != 3 || result.OutputTokens != 4 {
		t.Fatalf("tokens = %d/%d", result.InputTokens, result.OutputTokens)
	}
}

func TestOpenAITranslatorTranslateThreadCreateMultiRejectsEmptyMessage(t *testing.T) {
	client := openaiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := successfulOpenAIResponse(`{"en":{"name":"Topic","message":""}}`, 1, 1)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	_, err := translateThreadCreateMulti(t, testTranslator(client), []string{"en"}, "議題", "本文", TranslationContext{}, nil)
	if err == nil {
		t.Fatal("expected empty message rejection")
	}
}

func TestTopicSummarySchemaIsStrictStructuredOutput(t *testing.T) {
	schema := mustTopicSummarySchema(t)
	assertStrictJSONSchema(t, schema, "root")
	var root map[string]any
	if err := json.Unmarshal(schema, &root); err != nil {
		t.Fatal(err)
	}
	required, _ := root["required"].([]any)
	if len(required) != 1 || required[0] != "summary" {
		t.Fatalf("required = %#v", required)
	}
}

func TestOpenAITranslatorSummarizeTopic(t *testing.T) {
	client := openaiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		var input openaiChatCompletionRequest
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		requireJSONSchemaResponseFormat(t, input, openaiTopicSummarySchemaName, mustTopicSummarySchema(t))
		if input.PromptCacheKey != "" {
			t.Fatalf("topic summary should not share translation prompt cache keys: %q", input.PromptCacheKey)
		}
		system := openaiContentText(t, input.Messages[0].Content)
		user := openaiContentText(t, input.Messages[1].Content)
		if !strings.Contains(system, "untrusted") {
			t.Fatalf("system = %s", system)
		}
		if !strings.Contains(user, "<previous_summary>packing</previous_summary>") || !strings.Contains(user, `author="Alice"`) {
			t.Fatalf("user = %s", user)
		}
		body := successfulOpenAIResponse(`{"summary":"they are coordinating a delayed shipment"}`, 11, 6)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	prepared, err := prepareTopicSummary(TopicSummaryRequest{
		PreviousSummary: "packing",
		Discarded:       []ChatContextMessage{{Author: "Alice", Content: "遅れるかも"}},
		GuildID:         "guild",
		MessageID:       "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := testTranslator(client).SummarizeTopic(context.Background(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary != "they are coordinating a delayed shipment" {
		t.Fatalf("summary = %q", result.Summary)
	}
	if result.InputTokens != 11 || result.OutputTokens != 6 {
		t.Fatalf("tokens = %d/%d", result.InputTokens, result.OutputTokens)
	}
}
