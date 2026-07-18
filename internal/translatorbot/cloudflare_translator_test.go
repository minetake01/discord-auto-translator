package translatorbot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCloudflareTranslatorRequestContractAndResponseUsage(t *testing.T) {
	var gotRequest map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/client/v4/accounts/account-123/ai/v1/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		wantHeaders := map[string]string{
			"Authorization":              "Bearer secret-token",
			"Content-Type":               "application/json",
			"cf-aig-gateway-id":          "gateway-prod",
			"cf-aig-collect-log-payload": "false",
			"cf-aig-metadata":            `{"guild_id":"guild-1","message_id":"message-2"}`,
		}
		for name, want := range wantHeaders {
			if got := r.Header.Get(name); got != want {
				t.Errorf("%s = %q, want %q", name, got, want)
			}
		}
		if got := r.Header.Get("cf-aig-skip-cache"); got != "" {
			t.Errorf("cf-aig-skip-cache must be absent, got %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"{\"translations\":[{\"language\":\"en\",\"translated_text\":\"Hello [USER:Alice]\"}]}"}}],"usage":{"prompt_tokens":111,"completion_tokens":22}}`)
	}))
	defer server.Close()

	translator := newCloudflareTranslator(server.URL+"/client/v4", "account-123", "secret-token", "gateway-prod", server.Client())
	result, err := translator.TranslateMulti(context.Background(), []string{"en"}, "こんにちは <@42>", TranslationContext{
		GuildID: "guild-1", MessageID: "message-2", MentionedUsers: map[string]string{"42": "Alice"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Translations["en"] != "Hello <@42>" || result.InputTokens != 111 || result.OutputTokens != 22 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if gotRequest["model"] != cloudflareModel || gotRequest["temperature"] != 0.2 || gotRequest["stream"] != false {
		t.Fatalf("unexpected fixed request fields: %#v", gotRequest)
	}
	if _, ok := gotRequest["reasoning_effort"]; ok {
		t.Fatal("reasoning_effort must be omitted for Gemma 4")
	}
	chatTemplateKwargs, ok := gotRequest["chat_template_kwargs"].(map[string]any)
	if !ok || chatTemplateKwargs["enable_thinking"] != false {
		t.Fatalf("chat_template_kwargs = %#v, want enable_thinking=false", gotRequest["chat_template_kwargs"])
	}
	if gotRequest["max_completion_tokens"] != float64(maxTranslationCompletionTokens(1)) {
		t.Fatalf("max_completion_tokens = %#v, want %d", gotRequest["max_completion_tokens"], maxTranslationCompletionTokens(1))
	}
	if _, ok := gotRequest["max_tokens"]; ok {
		t.Fatal("deprecated max_tokens must be omitted")
	}
	if _, ok := gotRequest["cache_key"]; ok {
		t.Fatal("custom cache key must be omitted")
	}
	messages, ok := gotRequest["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("messages = %#v", gotRequest["messages"])
	}
	for _, message := range messages {
		content := message.(map[string]any)["content"].(string)
		if strings.Contains(content, "guild-1") || strings.Contains(content, "message-2") {
			t.Fatalf("gateway metadata leaked into prompt: %q", content)
		}
	}
	format := gotRequest["response_format"].(map[string]any)
	if format["type"] != "json_schema" {
		t.Fatalf("response_format = %#v", format)
	}
	jsonSchema := format["json_schema"].(map[string]any)
	if jsonSchema["strict"] != true {
		t.Fatalf("json_schema = %#v", jsonSchema)
	}
	schema := jsonSchema["schema"].(map[string]any)
	if schema["additionalProperties"] != false {
		t.Fatalf("schema must reject unknown fields: %#v", schema)
	}
}

func TestMaxTranslationCompletionTokensScalesWithLanguageCount(t *testing.T) {
	tests := []struct {
		languages int
		want      int
	}{
		{languages: 1, want: 6024},
		{languages: 2, want: 11024},
		{languages: 3, want: 16024},
		{languages: 13, want: 66024},
	}
	for _, tt := range tests {
		if got := maxTranslationCompletionTokens(tt.languages); got != tt.want {
			t.Errorf("maxTranslationCompletionTokens(%d) = %d, want %d", tt.languages, got, tt.want)
		}
	}
}

func TestNewCloudflareTranslatorUsesTenSecondTimeout(t *testing.T) {
	translator := NewCloudflareTranslator("account", "token", "gateway")
	if translator.client.Timeout != 10*time.Second {
		t.Fatalf("timeout = %s, want 10s", translator.client.Timeout)
	}
}

func TestCloudflareTranslatorRejectsMalformedOversizedAndEmptyResponses(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed", body: `{"choices":`},
		{name: "empty choices", body: `{"choices":[]}`},
		{name: "empty content", body: `{"choices":[{"message":{"content":""}}]}`},
		{name: "malformed content", body: `{"choices":[{"message":{"content":"not-json"}}]}`},
		{name: "oversized", body: strings.Repeat("x", maxCloudflareResponseBytes+1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, tt.body) }))
			defer server.Close()
			translator := newCloudflareTranslator(server.URL, "account", "token", "gateway", server.Client())
			if _, err := translator.TranslateMulti(context.Background(), []string{"en"}, "hello", TranslationContext{GuildID: "g", MessageID: "m"}, nil); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestCloudflareTranslatorNon2xxDoesNotRetryOrLeakSecrets(t *testing.T) {
	const secret = "super-secret-token"
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"request exposed super-secret-token and private prompt"}}`)
	}))
	defer server.Close()
	translator := newCloudflareTranslator(server.URL, "account", secret, "gateway", server.Client())
	_, err := translator.TranslateMulti(context.Background(), []string{"en"}, "private prompt", TranslationContext{GuildID: "g", MessageID: "m"}, nil)
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "private prompt") {
		t.Fatalf("secret data leaked in error: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestCloudflareTranslatorHonorsCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	translator := newCloudflareTranslator(server.URL, "account", "token", "gateway", server.Client())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := translator.TranslateMulti(ctx, []string{"en"}, "hello", TranslationContext{GuildID: "g", MessageID: "m"}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}
