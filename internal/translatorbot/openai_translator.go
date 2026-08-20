package translatorbot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	openaiMaxTokens      = 4096
	openaiRequestTimeout = 60 * time.Second
	openaiRetryAttempts  = 2 // initial attempt + one retry
	openaiRetryBackoff   = 1 * time.Second

	promptCacheTTL     = time.Hour
	promptCacheTTLText = "1h"

	// Keep TCP probes active and drop idle pooled connections before common
	// middlebox idle timeouts silently invalidate them. Per-attempt deadlines
	// still come from context; the client itself has no overall Timeout.
	openaiHTTPDialTimeout     = 30 * time.Second
	openaiHTTPKeepAlive       = 30 * time.Second
	openaiHTTPIdleConnTimeout = 45 * time.Second

	openaiMessageTranslationSchemaName      = "message_translations"
	openaiPollTranslationSchemaName         = "poll_translations"
	openaiThreadCreateTranslationSchemaName = "thread_create_translations"
	openaiTopicSummarySchemaName            = "topic_summary"
)

type openaiHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type OpenAITranslator struct {
	client            openaiHTTPClient
	apiKey            string
	model             string
	completionsURL    string
	reasoningEffort   string
	requireParameters bool
	now               func() time.Time
	debugLog          *DebugLog
	promptCacheMu     sync.Mutex
	promptCacheExpiry map[string]time.Time
}

// openaiDebugEntry records one Chat Completions round trip. The raw request
// and response keep unknown fields intact. First-class prompt, cache, and
// usage fields make accuracy and cache-efficiency measurement possible without
// re-parsing the provider payload. GuildID and MessageID are local
// correlation keys; they are not sent to the provider.
type openaiDebugEntry struct {
	Time               time.Time          `json:"time"`
	GuildID            string             `json:"guild_id,omitempty"`
	MessageID          string             `json:"message_id,omitempty"`
	Model              string             `json:"model,omitempty"`
	SchemaName         string             `json:"schema_name,omitempty"`
	TargetLanguages    []string           `json:"target_languages"`
	DurationMS         int64              `json:"duration_ms"`
	PromptCacheKey     string             `json:"prompt_cache_key,omitempty"`
	PromptCacheTTLSent bool               `json:"prompt_cache_ttl_sent"`
	PromptCacheHit     *bool              `json:"prompt_cache_hit,omitempty"`
	SystemInstruction  string             `json:"system_instruction,omitempty"`
	UserPromptFrozen   string             `json:"user_prompt_frozen,omitempty"`
	UserPromptVariable string             `json:"user_prompt_variable,omitempty"`
	VisionImageCount   int                `json:"vision_image_count,omitempty"`
	Usage              *openaiLoggedUsage `json:"usage,omitempty"`
	Request            json.RawMessage    `json:"request,omitempty"`
	HTTPStatus         int                `json:"http_status,omitempty"`
	Response           json.RawMessage    `json:"response,omitempty"`
	ResponseText       string             `json:"response_text,omitempty"`
	Error              string             `json:"error,omitempty"`
}

// openaiLoggedUsage is the subset of Chat Completions usage used for
// measurement. CachedTokens and CostUSD are pointers so a reported zero stays
// distinct from a provider that omitted the field.
type openaiLoggedUsage struct {
	PromptTokens     int      `json:"prompt_tokens,omitempty"`
	CompletionTokens int      `json:"completion_tokens,omitempty"`
	TotalTokens      int      `json:"total_tokens,omitempty"`
	CachedTokens     *int     `json:"cached_tokens,omitempty"`
	CacheWriteTokens *int     `json:"cache_write_tokens,omitempty"`
	ReasoningTokens  *int     `json:"reasoning_tokens,omitempty"`
	CostUSD          *float64 `json:"cost_usd,omitempty"`
}

type openaiChatCompletionRequest struct {
	Model              string                     `json:"model"`
	Messages           []openaiChatMessage        `json:"messages"`
	MaxTokens          int                        `json:"max_tokens"`
	ReasoningEffort    string                     `json:"reasoning_effort,omitempty"`
	PromptCacheKey     string                     `json:"prompt_cache_key,omitempty"`
	PromptCacheOptions *openaiPromptCacheOptions  `json:"prompt_cache_options,omitempty"`
	ResponseFormat     openaiResponseFormat       `json:"response_format"`
	Provider           *openaiProviderPreferences `json:"provider,omitempty"`
}

type openaiResponseFormat struct {
	Type       string           `json:"type"`
	JSONSchema openaiJSONSchema `json:"json_schema"`
}

type openaiJSONSchema struct {
	Name   string          `json:"name"`
	Strict bool            `json:"strict"`
	Schema json.RawMessage `json:"schema"`
}

type openaiProviderPreferences struct {
	RequireParameters bool `json:"require_parameters"`
}

type openaiChatMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type openaiImageURL struct {
	URL string `json:"url"`
}

type openaiImagePart struct {
	Type     string         `json:"type"`
	ImageURL openaiImageURL `json:"image_url"`
}

type openaiTextPart struct {
	Type                  string                       `json:"type"`
	Text                  string                       `json:"text"`
	PromptCacheBreakpoint *openaiPromptCacheBreakpoint `json:"prompt_cache_breakpoint,omitempty"`
}

type openaiPromptCacheBreakpoint struct {
	Mode string `json:"mode"`
}

type openaiPromptCacheOptions struct {
	Mode string `json:"mode"`
	TTL  string `json:"ttl"`
}

type openaiChatCompletionResponse struct {
	Choices []openaiChatChoice    `json:"choices"`
	Usage   *openaiChatTokenUsage `json:"usage"`
}

type openaiChatChoice struct {
	FinishReason string            `json:"finish_reason"`
	Message      openaiChatMessage `json:"message"`
}

type openaiChatTokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type openaiErrorEnvelope struct {
	Type      string `json:"type"`
	Code      string `json:"code"`
	Param     string `json:"param"`
	RequestID string `json:"request_id"`
	Error     struct {
		Type  string `json:"type"`
		Code  string `json:"code"`
		Param string `json:"param"`
	} `json:"error"`
}

// openaiHTTPStatusError is a sanitized provider HTTP failure. Status is kept
// so transient codes (429 / 5xx) can be retried without logging response bodies.
type openaiHTTPStatusError struct {
	status  int
	message string
}

func (e *openaiHTTPStatusError) Error() string { return e.message }

func NewOpenAITranslator(_ context.Context, baseURL, apiKey, model, reasoningEffort string) (*OpenAITranslator, error) {
	normalizedBase, err := normalizeOpenAIBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, errors.New("OPENAI_API_KEY is required")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, errors.New("OPENAI_MODEL is required")
	}
	effort, err := normalizeOpenAIReasoningEffort(reasoningEffort)
	if err != nil {
		return nil, err
	}
	return newOpenAITranslator(newOpenAIHTTPClient(), apiKey, model, joinOpenAIChatCompletionsURL(normalizedBase), effort), nil
}

func normalizeOpenAIBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("OPENAI_BASE_URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("OPENAI_BASE_URL is invalid")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("OPENAI_BASE_URL must use http or https")
	}
	if u.Host == "" {
		return "", errors.New("OPENAI_BASE_URL must include a host")
	}
	u.Fragment = ""
	normalized := strings.TrimRight(u.String(), "/")
	return normalized, nil
}

func joinOpenAIChatCompletionsURL(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/chat/completions"
}

func normalizeOpenAIReasoningEffort(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	switch value {
	case "none", "minimal", "low", "medium", "high", "xhigh", "max":
		return value, nil
	default:
		return "", errors.New("OPENAI_REASONING_EFFORT must be none, minimal, low, medium, high, xhigh, or max")
	}
}

// newOpenAIHTTPClient builds a dedicated client with explicit TCP keepalive
// and a short idle-pool lifetime so long-quiet bots do not reuse half-open
// connections through NAT/firewalls.
func newOpenAIHTTPClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   openaiHTTPDialTimeout,
		KeepAlive: openaiHTTPKeepAlive,
	}
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       openaiHTTPIdleConnTimeout,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

func newOpenAITranslator(client openaiHTTPClient, apiKey, model, completionsURL, reasoningEffort string) *OpenAITranslator {
	return &OpenAITranslator{
		client:            client,
		apiKey:            apiKey,
		model:             model,
		completionsURL:    completionsURL,
		reasoningEffort:   reasoningEffort,
		requireParameters: openaiHostRequiresParameters(completionsURL),
		now:               time.Now,
	}
}

func openaiHostRequiresParameters(completionsURL string) bool {
	u, err := url.Parse(completionsURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "openrouter.ai" || strings.HasSuffix(host, ".openrouter.ai")
}

// SetDebugLog records every Chat Completions round trip for diagnosis and
// measurement (synthesized prompts, cache hit, token usage, and provider cost).
// It is off unless configured.
func (t *OpenAITranslator) SetDebugLog(debugLog *DebugLog) {
	t.debugLog = debugLog
}

func openaiLanguageSchema(targetLanguages []string) map[string]any {
	langs := make([]string, len(targetLanguages))
	copy(langs, targetLanguages)
	return map[string]any{
		"type":        "string",
		"enum":        langs,
		"description": "BCP-47 language tag from <target_languages>, copied character-for-character.",
	}
}

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

func openaiMessageTranslationSchema(targetLanguages []string) (json.RawMessage, error) {
	if err := requireSchemaLanguages(targetLanguages); err != nil {
		return nil, err
	}
	item := openaiObjectSchema([]string{"language", "translated_text", "attachment_descriptions"}, map[string]any{
		"language": openaiLanguageSchema(targetLanguages),
		"translated_text": map[string]any{
			"type":        "string",
			"description": "The <final_message> translated into this item's language. Empty when <final_message> is empty.",
		},
		"attachment_descriptions": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Exactly as many strings as <attachment> elements, in source order. Translate existing non-empty alt text. If an attachment has no alt, return an empty string. Never invent alt text. Empty array when <attachments> is absent. Do not describe background images from history, replies, or linked pages.",
		},
	})
	return marshalOpenAIJSONSchema(openaiObjectSchema([]string{"translations"}, map[string]any{
		"translations": map[string]any{"type": "array", "items": item},
	}))
}

func openaiPollTranslationSchema(targetLanguages []string) (json.RawMessage, error) {
	if err := requireSchemaLanguages(targetLanguages); err != nil {
		return nil, err
	}
	item := openaiObjectSchema([]string{"language", "question", "answers"}, map[string]any{
		"language": openaiLanguageSchema(targetLanguages),
		"question": map[string]any{
			"type":        "string",
			"description": "The poll question translated into this item's language.",
		},
		"answers": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "The poll answers translated into this item's language, in source order.",
		},
	})
	return marshalOpenAIJSONSchema(openaiObjectSchema([]string{"translations"}, map[string]any{
		"translations": map[string]any{"type": "array", "items": item},
	}))
}

func openaiThreadCreateTranslationSchema(targetLanguages []string) (json.RawMessage, error) {
	if err := requireSchemaLanguages(targetLanguages); err != nil {
		return nil, err
	}
	item := openaiObjectSchema([]string{"language", "name", "message"}, map[string]any{
		"language": openaiLanguageSchema(targetLanguages),
		"name": map[string]any{
			"type":        "string",
			"description": "The thread <name> translated into this item's language.",
		},
		"message": map[string]any{
			"type":        "string",
			"description": "The initial thread <message> translated into this item's language. Empty when <message> was omitted.",
		},
	})
	return marshalOpenAIJSONSchema(openaiObjectSchema([]string{"translations"}, map[string]any{
		"translations": map[string]any{"type": "array", "items": item},
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

func (t *OpenAITranslator) TranslateMulti(ctx context.Context, prepared preparedTranslation) (MultiTranslationResult, error) {
	if len(prepared.targetLanguages) == 0 {
		return MultiTranslationResult{Translations: map[string]string{}}, nil
	}
	schema, err := openaiMessageTranslationSchema(prepared.targetLanguages)
	if err != nil {
		return MultiTranslationResult{}, err
	}
	text, inputTokens, outputTokens, err := t.invokePreparedWithRetry(ctx, prepared, openaiMessageTranslationSchemaName, schema)
	if err != nil {
		return MultiTranslationResult{}, err
	}
	translations, descriptions, err := parseMultiTranslationResponse(text, prepared.targetLanguages, prepared.protector, prepared.content, prepared.attachmentCount)
	if err != nil {
		return MultiTranslationResult{}, err
	}
	return MultiTranslationResult{Translations: translations, AttachmentDescriptions: descriptions, InputTokens: inputTokens, OutputTokens: outputTokens}, nil
}

func (t *OpenAITranslator) TranslatePollMulti(ctx context.Context, prepared preparedTranslation) (PollMultiTranslationResult, error) {
	if len(prepared.targetLanguages) == 0 {
		return PollMultiTranslationResult{Translations: map[string]PollTranslation{}}, nil
	}
	schema, err := openaiPollTranslationSchema(prepared.targetLanguages)
	if err != nil {
		return PollMultiTranslationResult{}, err
	}
	text, inputTokens, outputTokens, err := t.invokePreparedWithRetry(ctx, prepared, openaiPollTranslationSchemaName, schema)
	if err != nil {
		return PollMultiTranslationResult{}, err
	}
	translations, err := parsePollTranslationResponse(text, prepared.targetLanguages, prepared.answerCount, prepared.protector)
	if err != nil {
		return PollMultiTranslationResult{}, err
	}
	return PollMultiTranslationResult{Translations: translations, InputTokens: inputTokens, OutputTokens: outputTokens}, nil
}

func (t *OpenAITranslator) TranslateThreadCreateMulti(ctx context.Context, prepared preparedTranslation) (ThreadCreateMultiTranslationResult, error) {
	if len(prepared.targetLanguages) == 0 {
		return ThreadCreateMultiTranslationResult{Translations: map[string]ThreadCreateTranslation{}}, nil
	}
	schema, err := openaiThreadCreateTranslationSchema(prepared.targetLanguages)
	if err != nil {
		return ThreadCreateMultiTranslationResult{}, err
	}
	text, inputTokens, outputTokens, err := t.invokePreparedWithRetry(ctx, prepared, openaiThreadCreateTranslationSchemaName, schema)
	if err != nil {
		return ThreadCreateMultiTranslationResult{}, err
	}
	translations, err := parseThreadCreateTranslationResponse(text, prepared.targetLanguages, prepared.messageRequired, prepared.protector)
	if err != nil {
		return ThreadCreateMultiTranslationResult{}, err
	}
	return ThreadCreateMultiTranslationResult{Translations: translations, InputTokens: inputTokens, OutputTokens: outputTokens}, nil
}

func (t *OpenAITranslator) SummarizeTopic(ctx context.Context, req TopicSummaryRequest) (TopicSummaryResult, error) {
	prepared, err := prepareTopicSummary(req)
	if err != nil {
		return TopicSummaryResult{}, err
	}
	schema, err := openaiTopicSummarySchema()
	if err != nil {
		return TopicSummaryResult{}, err
	}
	text, inputTokens, outputTokens, err := t.invokePreparedWithRetry(ctx, prepared, openaiTopicSummarySchemaName, schema)
	if err != nil {
		return TopicSummaryResult{}, err
	}
	summary, err := parseTopicSummaryResponse(text)
	if err != nil {
		return TopicSummaryResult{}, err
	}
	return TopicSummaryResult{Summary: summary, InputTokens: inputTokens, OutputTokens: outputTokens}, nil
}

// WarmUp verifies credentials, model access, and the fixed response contract
// without starting Discord, SQLite, or the HTTP server. Uses the same 60s
// per-attempt timeout and single retry as TranslateMulti; the caller still
// owns the overall deadline.
func (t *OpenAITranslator) WarmUp(ctx context.Context) error {
	prepared, err := prepareMultiTranslation([]string{"en"}, "warmup", TranslationContext{}, nil)
	if err != nil {
		return err
	}
	if _, err := t.TranslateMulti(ctx, prepared); err != nil {
		return fmt.Errorf("prewarm OpenAI-compatible model: %w", err)
	}
	return nil
}

func (t *OpenAITranslator) invokePreparedWithRetry(ctx context.Context, prepared preparedTranslation, schemaName string, jsonSchema json.RawMessage) (text string, inputTokens, outputTokens int, err error) {
	var lastErr error
	for attempt := 0; attempt < openaiRetryAttempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, openaiRequestTimeout)
		text, inputTokens, outputTokens, err = t.invokePrepared(attemptCtx, prepared, schemaName, jsonSchema)
		cancel()
		if err == nil {
			return text, inputTokens, outputTokens, nil
		}
		lastErr = err
		if attempt == openaiRetryAttempts-1 || !isOpenAIRetryable(err) || ctx.Err() != nil {
			return "", 0, 0, wrapProviderIssue(lastErr)
		}
		timer := time.NewTimer(openaiRetryBackoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", 0, 0, ctx.Err()
		case <-timer.C:
		}
	}
	return "", 0, 0, wrapProviderIssue(lastErr)
}

// errTranslationProvider marks a translation API outage (timeout, transport, or
// HTTP 429/5xx after retry). Channel notices are shown once per outage.
var errTranslationProvider = errors.New("translation provider unavailable")

func wrapProviderIssue(err error) error {
	if err == nil || errors.Is(err, errTranslationProvider) || errors.Is(err, context.Canceled) {
		return err
	}
	if isOpenAIRetryable(err) {
		return fmt.Errorf("%w: %w", errTranslationProvider, err)
	}
	return err
}

func isOpenAIRetryable(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var httpErr *openaiHTTPStatusError
	if errors.As(err, &httpErr) {
		return httpErr.status == http.StatusTooManyRequests || httpErr.status >= http.StatusInternalServerError
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

func openaiTextContent(text string) json.RawMessage {
	encoded, err := json.Marshal(text)
	if err != nil {
		return json.RawMessage(`""`)
	}
	return encoded
}

func openaiUserContent(images []visionImage, frozen, variable string) json.RawMessage {
	if frozen == "" && variable == "" && len(images) == 0 {
		return openaiTextContent("")
	}
	if frozen == "" {
		frozen, variable = variable, ""
	}
	parts := make([]any, 0, 2+len(images))
	frozenPart := openaiTextPart{Type: "text", Text: frozen, PromptCacheBreakpoint: &openaiPromptCacheBreakpoint{Mode: "explicit"}}
	parts = append(parts, frozenPart)
	if variable != "" {
		parts = append(parts, openaiTextPart{Type: "text", Text: variable})
	}
	for _, img := range images {
		parts = append(parts, openaiImagePart{Type: "image_url", ImageURL: openaiImageURL{URL: img.DataURL}})
	}
	encoded, err := json.Marshal(parts)
	if err != nil {
		return openaiTextContent(frozen + variable)
	}
	return encoded
}

func (t *OpenAITranslator) invokePrepared(ctx context.Context, prepared preparedTranslation, schemaName string, jsonSchema json.RawMessage) (text string, inputTokens, outputTokens int, err error) {
	payload := openaiChatCompletionRequest{
		Model: t.model,
		Messages: []openaiChatMessage{
			{Role: "system", Content: openaiTextContent(prepared.systemInstruction)},
			{Role: "user", Content: openaiUserContent(prepared.visionImages, prepared.userPromptFrozen, prepared.userPromptVariable)},
		},
		MaxTokens:       openaiMaxTokens,
		ReasoningEffort: t.reasoningEffort,
		PromptCacheKey:  prepared.promptCacheKey,
		ResponseFormat: openaiResponseFormat{
			Type: "json_schema",
			JSONSchema: openaiJSONSchema{
				Name:   schemaName,
				Strict: true,
				Schema: jsonSchema,
			},
		},
	}
	if t.requireParameters {
		payload.Provider = &openaiProviderPreferences{RequireParameters: true}
	}
	wroteTTL := false
	if t.promptCacheNeedsTTL(prepared.promptCacheKey) {
		payload.PromptCacheOptions = &openaiPromptCacheOptions{Mode: "explicit", TTL: promptCacheTTLText}
		wroteTTL = true
	}
	start := t.now()
	entry := openaiDebugEntry{
		Time:               start,
		GuildID:            prepared.guildID,
		MessageID:          prepared.messageID,
		Model:              t.model,
		SchemaName:         schemaName,
		TargetLanguages:    prepared.targetLanguages,
		PromptCacheKey:     prepared.promptCacheKey,
		PromptCacheTTLSent: wroteTTL,
		SystemInstruction:  prepared.systemInstruction,
		UserPromptFrozen:   prepared.userPromptFrozen,
		UserPromptVariable: prepared.userPromptVariable,
		VisionImageCount:   len(prepared.visionImages),
	}
	defer func() {
		entry.DurationMS = t.now().Sub(start).Milliseconds()
		if err != nil {
			entry.Error = err.Error()
		}
		entry.PromptCacheHit = promptCacheHit(entry.Usage)
		t.debugLog.writeEntry(entry)
	}()
	body, err := json.Marshal(payload)
	if err != nil {
		return "", 0, 0, errors.New("encode OpenAI translation request")
	}
	entry.Request = body
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.completionsURL, bytes.NewReader(body))
	if err != nil {
		return "", 0, 0, errors.New("create OpenAI translation request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.apiKey)
	response, err := t.client.Do(req)
	if err != nil {
		return "", 0, 0, fmt.Errorf("OpenAI translation request: %w", err)
	}
	if response == nil {
		return "", 0, 0, errors.New("OpenAI response is nil")
	}
	defer response.Body.Close()
	entry.HTTPStatus = response.StatusCode
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return "", 0, 0, errors.New("read OpenAI translation response")
	}
	entry.recordResponse(responseBody)
	entry.Usage = extractLoggedUsage(responseBody)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", 0, 0, openaiHTTPError(response, responseBody)
	}
	if wroteTTL {
		t.rememberPromptCache(prepared.promptCacheKey)
	}
	var output openaiChatCompletionResponse
	decoder := json.NewDecoder(bytes.NewReader(responseBody))
	if err := decoder.Decode(&output); err != nil {
		return "", 0, 0, errors.New("decode OpenAI translation response")
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return "", 0, 0, errors.New("decode OpenAI translation response")
	}
	return extractOpenAIChatText(output)
}

func (t *OpenAITranslator) promptCacheNeedsTTL(key string) bool {
	if key == "" {
		return false
	}
	now := t.now()
	t.promptCacheMu.Lock()
	defer t.promptCacheMu.Unlock()
	if t.promptCacheExpiry == nil {
		return true
	}
	expiry, ok := t.promptCacheExpiry[key]
	return !ok || !now.Before(expiry)
}

func (t *OpenAITranslator) rememberPromptCache(key string) {
	if key == "" {
		return
	}
	now := t.now()
	t.promptCacheMu.Lock()
	defer t.promptCacheMu.Unlock()
	if t.promptCacheExpiry == nil {
		t.promptCacheExpiry = make(map[string]time.Time)
	}
	t.promptCacheExpiry[key] = now.Add(promptCacheTTL)
}

func (e *openaiDebugEntry) recordResponse(body []byte) {
	if json.Valid(body) {
		e.Response = body
		return
	}
	e.ResponseText = string(body)
}

func extractLoggedUsage(body []byte) *openaiLoggedUsage {
	if !json.Valid(body) {
		return nil
	}
	var payload struct {
		Usage *struct {
			PromptTokens        int      `json:"prompt_tokens"`
			CompletionTokens    int      `json:"completion_tokens"`
			TotalTokens         int      `json:"total_tokens"`
			CachedTokens        *int     `json:"cached_tokens"`
			CacheWriteTokens    *int     `json:"cache_write_tokens"`
			Cost                *float64 `json:"cost"`
			PromptTokensDetails *struct {
				CachedTokens     *int `json:"cached_tokens"`
				CacheWriteTokens *int `json:"cache_write_tokens"`
			} `json:"prompt_tokens_details"`
			CompletionTokensDetails *struct {
				ReasoningTokens *int `json:"reasoning_tokens"`
			} `json:"completion_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Usage == nil {
		return nil
	}
	u := payload.Usage
	out := &openaiLoggedUsage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.TotalTokens,
		CostUSD:          u.Cost,
		CachedTokens:     u.CachedTokens,
		CacheWriteTokens: u.CacheWriteTokens,
	}
	if details := u.PromptTokensDetails; details != nil {
		if details.CachedTokens != nil {
			out.CachedTokens = details.CachedTokens
		}
		if details.CacheWriteTokens != nil {
			out.CacheWriteTokens = details.CacheWriteTokens
		}
	}
	if u.CompletionTokensDetails != nil {
		out.ReasoningTokens = u.CompletionTokensDetails.ReasoningTokens
	}
	return out
}

func promptCacheHit(usage *openaiLoggedUsage) *bool {
	if usage == nil || usage.CachedTokens == nil {
		return nil
	}
	hit := *usage.CachedTokens > 0
	return &hit
}

func openaiHTTPError(response *http.Response, body []byte) error {
	var envelope openaiErrorEnvelope
	_ = json.Unmarshal(body, &envelope)

	errorType := firstSafeErrorField(
		envelope.Error.Type,
		envelope.Type,
	)
	code := firstSafeErrorField(envelope.Error.Code, envelope.Code)
	param := firstSafeErrorField(envelope.Error.Param, envelope.Param)
	requestID := firstSafeErrorField(
		response.Header.Get("x-request-id"),
		envelope.RequestID,
	)

	details := make([]string, 0, 4)
	if errorType != "" {
		details = append(details, "type="+errorType)
	}
	if code != "" {
		details = append(details, "code="+code)
	}
	if param != "" {
		details = append(details, "param="+param)
	}
	if requestID != "" {
		details = append(details, "request_id="+requestID)
	}
	var message string
	if len(details) == 0 {
		message = fmt.Sprintf("OpenAI translation request returned HTTP %d", response.StatusCode)
	} else {
		message = fmt.Sprintf("OpenAI translation request returned HTTP %d (%s)", response.StatusCode, strings.Join(details, ", "))
	}
	return &openaiHTTPStatusError{status: response.StatusCode, message: message}
}

func firstSafeErrorField(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 128 {
			continue
		}
		safe := true
		for _, r := range value {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._:/-", r) {
				continue
			}
			safe = false
			break
		}
		if safe {
			return value
		}
	}
	return ""
}

func extractOpenAIChatText(output openaiChatCompletionResponse) (text string, inputTokens, outputTokens int, err error) {
	if len(output.Choices) != 1 {
		return "", 0, 0, fmt.Errorf("OpenAI response has %d choices, want 1", len(output.Choices))
	}
	choice := output.Choices[0]
	if strings.EqualFold(choice.FinishReason, "length") {
		return "", 0, 0, errors.New("OpenAI response was truncated by max_tokens")
	}
	if choice.Message.Role != "" && choice.Message.Role != "assistant" {
		return "", 0, 0, fmt.Errorf("OpenAI response has unexpected message role %q", choice.Message.Role)
	}
	text, err = decodeOpenAIMessageContent(choice.Message.Content)
	if err != nil {
		return "", 0, 0, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", 0, 0, errors.New("OpenAI response has empty assistant content")
	}
	if output.Usage == nil || output.Usage.PromptTokens < 0 || output.Usage.CompletionTokens < 0 {
		return "", 0, 0, errors.New("OpenAI response has no valid token usage")
	}
	return text, output.Usage.PromptTokens, output.Usage.CompletionTokens, nil
}

func decodeOpenAIMessageContent(raw json.RawMessage) (string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return "", errors.New("OpenAI response has empty assistant content")
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}
	var parts []openaiTextPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", errors.New("OpenAI response has unsupported assistant content")
	}
	var b strings.Builder
	for _, part := range parts {
		if part.Type != "" && part.Type != "text" {
			return "", fmt.Errorf("OpenAI response has unsupported content part type %q", part.Type)
		}
		b.WriteString(part.Text)
	}
	return b.String(), nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values")
	}
	return nil
}
