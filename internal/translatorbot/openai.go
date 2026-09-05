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
	"time"
)

const (
	openaiMaxTokens      = 4096
	openaiRequestTimeout = 60 * time.Second
	openaiRetryAttempts  = 2 // initial attempt + one retry
	openaiRetryBackoff   = 1 * time.Second

	// Keep TCP probes active and drop idle pooled connections before common
	// middlebox idle timeouts silently invalidate them. Per-attempt deadlines
	// still come from context; the client itself has no overall Timeout.
	openaiHTTPDialTimeout     = 30 * time.Second
	openaiHTTPKeepAlive       = 30 * time.Second
	openaiHTTPIdleConnTimeout = 45 * time.Second

	openaiMessageTranslationSchemaName       = "message_translations"
	openaiAttachmentAltTranslationSchemaName = "attachment_alt_translations"
	openaiPollTranslationSchemaName          = "poll_translations"
	openaiThreadCreateTranslationSchemaName  = "thread_create_translations"
	openaiTopicSummarySchemaName             = "topic_summary"
)

// errTranslationProvider marks a translation API outage (timeout, transport, or
// HTTP 429/5xx after retry). Channel notices are shown once per outage.
var errTranslationProvider = errors.New("translation provider unavailable")

// openaiHTTPStatusError is a sanitized provider HTTP failure. Status is kept
// so transient codes (429 / 5xx) can be retried without logging response bodies.
type openaiHTTPStatusError struct {
	status  int
	message string
}

func (e *openaiHTTPStatusError) Error() string { return e.message }

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

type openaiHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type OpenAITranslator struct {
	client          openaiHTTPClient
	apiKey          string
	model           string
	completionsURL  string
	reasoningEffort string
	debugLog        *DebugLog
}

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
		client:          client,
		apiKey:          apiKey,
		model:           model,
		completionsURL:  completionsURL,
		reasoningEffort: reasoningEffort,
	}
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
	translations, err := parseMultiTranslationResponse(text, prepared.targetLanguages, prepared.protector, prepared.content)
	if err != nil {
		return MultiTranslationResult{}, err
	}
	return MultiTranslationResult{Translations: translations, InputTokens: inputTokens, OutputTokens: outputTokens}, nil
}

func (t *OpenAITranslator) TranslateAttachmentAlts(ctx context.Context, prepared preparedTranslation) (AttachmentAltTranslationResult, error) {
	if len(prepared.targetLanguages) == 0 || prepared.altCount == 0 {
		return AttachmentAltTranslationResult{}, nil
	}
	schema, err := openaiAttachmentAltTranslationSchema(prepared.targetLanguages, prepared.altCount)
	if err != nil {
		return AttachmentAltTranslationResult{}, err
	}
	text, inputTokens, outputTokens, err := t.invokePreparedWithRetry(ctx, prepared, openaiAttachmentAltTranslationSchemaName, schema)
	if err != nil {
		return AttachmentAltTranslationResult{}, err
	}
	descriptions, err := parseAttachmentAltTranslationResponse(text, prepared.targetLanguages, prepared.protector, prepared.translationContext.Attachments)
	if err != nil {
		return AttachmentAltTranslationResult{}, err
	}
	return AttachmentAltTranslationResult{Descriptions: descriptions, InputTokens: inputTokens, OutputTokens: outputTokens}, nil
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

func (t *OpenAITranslator) SummarizeTopic(ctx context.Context, prepared preparedTranslation) (TopicSummaryResult, error) {
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
	for attempt := range openaiRetryAttempts {
		attemptCtx, cancel := context.WithTimeout(ctx, openaiRequestTimeout)
		text, inputTokens, outputTokens, err = t.invokePrepared(attemptCtx, prepared, schemaName, jsonSchema, attempt+1)
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

type openaiChatCompletionRequest struct {
	Model              string                    `json:"model"`
	Messages           []openaiChatMessage       `json:"messages"`
	MaxTokens          int                       `json:"max_tokens"`
	ReasoningEffort    string                    `json:"reasoning_effort,omitempty"`
	PromptCacheKey     string                    `json:"prompt_cache_key,omitempty"`
	PromptCacheOptions *openaiPromptCacheOptions `json:"prompt_cache_options,omitempty"`
	ResponseFormat     openaiResponseFormat      `json:"response_format"`
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

func openaiTextContent(text string) json.RawMessage {
	encoded, err := json.Marshal(text)
	if err != nil {
		return json.RawMessage(`""`)
	}
	return encoded
}

func openaiSystemContent(text string) json.RawMessage {
	return openaiContentParts([]any{openaiCachedTextPart(text)})
}

func openaiCachedTextPart(text string) openaiTextPart {
	return openaiTextPart{
		Type:                  "text",
		Text:                  text,
		PromptCacheBreakpoint: &openaiPromptCacheBreakpoint{Mode: "explicit"},
	}
}

func openaiUserContent(images []visionImage, stable, history, variable string) json.RawMessage {
	parts := make([]any, 0, 3+len(images))
	if stable != "" {
		parts = append(parts, openaiCachedTextPart(stable))
	}
	if history != "" {
		parts = append(parts, openaiCachedTextPart(history))
	}
	if variable != "" {
		parts = append(parts, openaiTextPart{Type: "text", Text: variable})
	}
	for _, img := range images {
		parts = append(parts, openaiImagePart{Type: "image_url", ImageURL: openaiImageURL{URL: img.DataURL}})
	}
	if len(parts) == 0 {
		return openaiTextContent("")
	}
	return openaiContentParts(parts)
}

func openaiContentParts(parts []any) json.RawMessage {
	encoded, err := json.Marshal(parts)
	if err != nil {
		return json.RawMessage(`[]`)
	}
	return encoded
}

func (t *OpenAITranslator) invokePrepared(ctx context.Context, prepared preparedTranslation, schemaName string, jsonSchema json.RawMessage, attempt int) (text string, inputTokens, outputTokens int, err error) {
	payload := openaiChatCompletionRequest{
		Model: t.model,
		Messages: []openaiChatMessage{
			{Role: "system", Content: openaiSystemContent(prepared.systemInstruction)},
			{Role: "user", Content: openaiUserContent(prepared.visionImages, prepared.userPromptStable, prepared.userPromptHistory, prepared.userPromptVariable)},
		},
		MaxTokens:          openaiMaxTokens,
		ReasoningEffort:    t.reasoningEffort,
		PromptCacheKey:     prepared.promptCacheKey,
		PromptCacheOptions: &openaiPromptCacheOptions{Mode: "explicit"},
		ResponseFormat: openaiResponseFormat{
			Type: "json_schema",
			JSONSchema: openaiJSONSchema{
				Name:   schemaName,
				Strict: true,
				Schema: jsonSchema,
			},
		},
	}
	start := time.Now()
	entry := openaiDebugEntry{
		Time:               start,
		GuildID:            prepared.guildID,
		MessageID:          prepared.messageID,
		Model:              t.model,
		SchemaName:         schemaName,
		TargetLanguages:    prepared.targetLanguages,
		Attempt:            attempt,
		PromptCacheKey:     prepared.promptCacheKey,
		SystemInstruction:  prepared.systemInstruction,
		UserPromptStable:   prepared.userPromptStable,
		UserPromptHistory:  prepared.userPromptHistory,
		UserPromptVariable: prepared.userPromptVariable,
		VisionImageCount:   len(prepared.visionImages),
	}
	defer func() {
		end := time.Now()
		entry.Ended = &end
		entry.DurationMS = elapsedMS(start)
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
	waitStart := time.Now()
	response, err := t.client.Do(req)
	waitMS := elapsedMS(waitStart)
	entry.WaitMS = &waitMS
	if err != nil {
		return "", 0, 0, fmt.Errorf("OpenAI translation request: %w", err)
	}
	if response == nil {
		return "", 0, 0, errors.New("OpenAI response is nil")
	}
	defer response.Body.Close()
	entry.HTTPStatus = response.StatusCode
	entry.ProcessingMS = headerInt64(response.Header, "openai-processing-ms", "x-openai-processing-ms")
	entry.ServerTiming = strings.TrimSpace(response.Header.Get("Server-Timing"))
	readStart := time.Now()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	readMS := elapsedMS(readStart)
	entry.ReadMS = &readMS
	if err != nil {
		return "", 0, 0, errors.New("read OpenAI translation response")
	}
	entry.recordResponse(responseBody)
	entry.Usage = extractLoggedUsage(responseBody)
	entry.ResponseCreated = extractResponseCreated(responseBody)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", 0, 0, openaiHTTPError(response, responseBody)
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
