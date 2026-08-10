package translatorbot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

const (
	bedrockModel          = "google.gemma-4-26b-a4b"
	bedrockService        = "bedrock-mantle"
	bedrockMaxTokens      = 4096
	bedrockRequestTimeout = 60 * time.Second
	bedrockRetryAttempts  = 2 // initial attempt + one retry
	bedrockRetryBackoff   = 1 * time.Second

	// Keep TCP probes active and drop idle pooled connections before common
	// middlebox idle timeouts silently invalidate them. Per-attempt deadlines
	// still come from context; the client itself has no overall Timeout.
	bedrockHTTPDialTimeout     = 30 * time.Second
	bedrockHTTPKeepAlive       = 30 * time.Second
	bedrockHTTPIdleConnTimeout = 45 * time.Second
	bedrockServiceTier         = "priority"

	bedrockTranslationJSONSchema = `{"type":"object","additionalProperties":false,"required":["translations"],"properties":{"translations":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["language","translated_text"],"properties":{"language":{"type":"string"},"translated_text":{"type":"string","description":"The <final_message> translated into this item's language."}}}}}}`
	bedrockPollTranslationJSONSchema        = `{"type":"object","additionalProperties":false,"required":["translations"],"properties":{"translations":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["language","question","answers"],"properties":{"language":{"type":"string"},"question":{"type":"string","description":"The poll question translated into this item's language."},"answers":{"type":"array","items":{"type":"string"},"description":"The poll answers translated into this item's language, in source order."}}}}}}`
	bedrockThreadCreateTranslationJSONSchema = `{"type":"object","additionalProperties":false,"required":["translations"],"properties":{"translations":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["language","name","message"],"properties":{"language":{"type":"string"},"name":{"type":"string","description":"The thread <name> translated into this item's language."},"message":{"type":"string","description":"The initial thread <message> translated into this item's language. Empty when <message> was omitted."}}}}}}`
)

var (
	validBedrockRegion    = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)+-[0-9]+$`)
	validBedrockProjectID = regexp.MustCompile(`^(?:default|proj_[a-z0-9]+)$`)
)

type bedrockHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type bedrockRequestSigner interface {
	SignHTTP(context.Context, aws.Credentials, *http.Request, string, string, string, time.Time, ...func(*v4.SignerOptions)) error
}

type BedrockTranslator struct {
	client       bedrockHTTPClient
	signer       bedrockRequestSigner
	credentials  aws.CredentialsProvider
	region       string
	projectID    string
	responsesURL string
	now          func() time.Time
	debugLog     *DebugLog
}

// bedrockDebugEntry records one Mantle round trip verbatim. The raw response
// body keeps reasoning items, token usage details, and unknown fields intact,
// which the response parser discards. GuildID and MessageID are local
// correlation keys only; Mantle still receives no Discord IDs.
type bedrockDebugEntry struct {
	Time            time.Time       `json:"time"`
	GuildID         string          `json:"guild_id,omitempty"`
	MessageID       string          `json:"message_id,omitempty"`
	TargetLanguages []string        `json:"target_languages"`
	DurationMS      int64           `json:"duration_ms"`
	Request         json.RawMessage `json:"request,omitempty"`
	HTTPStatus      int             `json:"http_status,omitempty"`
	Response        json.RawMessage `json:"response,omitempty"`
	ResponseText    string          `json:"response_text,omitempty"`
	Error           string          `json:"error,omitempty"`
}

type bedrockResponsesRequest struct {
	Model           string                `json:"model"`
	Input           []bedrockInputMessage `json:"input"`
	MaxOutputTokens int                   `json:"max_output_tokens"`
	Store           bool                  `json:"store"`
	ServiceTier     string                `json:"service_tier"`
}

type bedrockInputMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type bedrockInputImagePart struct {
	Type     string `json:"type"`
	ImageURL string `json:"image_url"`
}

type bedrockInputTextPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type bedrockResponsesResponse struct {
	Status            string                     `json:"status"`
	Error             json.RawMessage            `json:"error"`
	IncompleteDetails json.RawMessage            `json:"incomplete_details"`
	Output            []bedrockResponseOutput    `json:"output"`
	Usage             *bedrockResponseTokenUsage `json:"usage"`
}

type bedrockResponseOutput struct {
	Type    string                   `json:"type"`
	Status  string                   `json:"status"`
	Role    string                   `json:"role"`
	Content []bedrockResponseContent `json:"content"`
}

type bedrockResponseContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type bedrockResponseTokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type bedrockErrorEnvelope struct {
	Type      string `json:"type"`
	Code      string `json:"code"`
	Param     string `json:"param"`
	AWSType   string `json:"__type"`
	RequestID string `json:"request_id"`
	Error     struct {
		Type  string `json:"type"`
		Code  string `json:"code"`
		Param string `json:"param"`
	} `json:"error"`
}

// bedrockHTTPStatusError is a sanitized provider HTTP failure. Status is kept
// so transient codes (429 / 5xx) can be retried without logging response bodies.
type bedrockHTTPStatusError struct {
	status  int
	message string
}

func (e *bedrockHTTPStatusError) Error() string { return e.message }

func NewBedrockTranslator(_ context.Context, accessKeyID, secretAccessKey, region, projectID string) (*BedrockTranslator, error) {
	if strings.TrimSpace(accessKeyID) == "" || strings.TrimSpace(secretAccessKey) == "" {
		return nil, errors.New("AWS credentials are required")
	}
	region = strings.TrimSpace(region)
	if !validBedrockRegion.MatchString(region) {
		return nil, errors.New("AWS Bedrock region is invalid")
	}
	projectID = strings.TrimSpace(projectID)
	if !validBedrockProjectID.MatchString(projectID) {
		return nil, errors.New("AWS Bedrock project ID is invalid")
	}
	return newBedrockTranslator(
		newBedrockHTTPClient(),
		v4.NewSigner(),
		credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, ""),
		region,
		projectID,
	), nil
}

// newBedrockHTTPClient builds a Mantle-only client with explicit TCP keepalive
// and a short idle-pool lifetime so long-quiet bots do not reuse half-open
// connections through NAT/firewalls.
func newBedrockHTTPClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   bedrockHTTPDialTimeout,
		KeepAlive: bedrockHTTPKeepAlive,
	}
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       bedrockHTTPIdleConnTimeout,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

func newBedrockTranslator(client bedrockHTTPClient, signer bedrockRequestSigner, provider aws.CredentialsProvider, region, projectID string) *BedrockTranslator {
	return &BedrockTranslator{
		client:       client,
		signer:       signer,
		credentials:  provider,
		region:       region,
		projectID:    projectID,
		responsesURL: fmt.Sprintf("https://bedrock-mantle.%s.api.aws/openai/v1/responses", region),
		now:          time.Now,
	}
}

// SetDebugLog enables verbose diagnosis of translation failures by recording
// every request payload and raw response body. It is off unless configured.
func (t *BedrockTranslator) SetDebugLog(debugLog *DebugLog) {
	t.debugLog = debugLog
}

func (t *BedrockTranslator) TranslateMulti(ctx context.Context, prepared preparedTranslation) (MultiTranslationResult, error) {
	if len(prepared.targetLanguages) == 0 {
		return MultiTranslationResult{Translations: map[string]string{}}, nil
	}
	text, inputTokens, outputTokens, err := t.invokePreparedWithRetry(ctx, prepared, translationJSONSchema(prepared.attachmentCount))
	if err != nil {
		return MultiTranslationResult{}, err
	}
	translations, descriptions, err := parseMultiTranslationResponse(text, prepared.targetLanguages, prepared.protector, prepared.content, prepared.attachmentCount)
	if err != nil {
		return MultiTranslationResult{}, err
	}
	return MultiTranslationResult{Translations: translations, AttachmentDescriptions: descriptions, InputTokens: inputTokens, OutputTokens: outputTokens}, nil
}

func (t *BedrockTranslator) TranslatePollMulti(ctx context.Context, prepared preparedTranslation) (PollMultiTranslationResult, error) {
	if len(prepared.targetLanguages) == 0 {
		return PollMultiTranslationResult{Translations: map[string]PollTranslation{}}, nil
	}
	text, inputTokens, outputTokens, err := t.invokePreparedWithRetry(ctx, prepared, bedrockPollTranslationJSONSchema)
	if err != nil {
		return PollMultiTranslationResult{}, err
	}
	translations, err := parsePollTranslationResponse(text, prepared.targetLanguages, prepared.answerCount, prepared.protector)
	if err != nil {
		return PollMultiTranslationResult{}, err
	}
	return PollMultiTranslationResult{Translations: translations, InputTokens: inputTokens, OutputTokens: outputTokens}, nil
}

func (t *BedrockTranslator) TranslateThreadCreateMulti(ctx context.Context, prepared preparedTranslation) (ThreadCreateMultiTranslationResult, error) {
	if len(prepared.targetLanguages) == 0 {
		return ThreadCreateMultiTranslationResult{Translations: map[string]ThreadCreateTranslation{}}, nil
	}
	text, inputTokens, outputTokens, err := t.invokePreparedWithRetry(ctx, prepared, bedrockThreadCreateTranslationJSONSchema)
	if err != nil {
		return ThreadCreateMultiTranslationResult{}, err
	}
	translations, err := parseThreadCreateTranslationResponse(text, prepared.targetLanguages, prepared.messageRequired, prepared.protector)
	if err != nil {
		return ThreadCreateMultiTranslationResult{}, err
	}
	return ThreadCreateMultiTranslationResult{Translations: translations, InputTokens: inputTokens, OutputTokens: outputTokens}, nil
}

// WarmUp verifies credentials, model access, and the fixed response contract
// without starting Discord, SQLite, or the HTTP server. Uses the same 60s
// per-attempt timeout and single retry as TranslateMulti; the caller still
// owns the overall deadline.
func (t *BedrockTranslator) WarmUp(ctx context.Context) error {
	prepared, err := prepareMultiTranslation([]string{"en"}, "warmup", TranslationContext{}, nil)
	if err != nil {
		return err
	}
	_, _, _, err = t.invokePreparedWithRetry(ctx, prepared, bedrockTranslationJSONSchema)
	if err != nil {
		return fmt.Errorf("prewarm Amazon Bedrock model: %w", err)
	}
	return nil
}

func (t *BedrockTranslator) invokePreparedWithRetry(ctx context.Context, prepared preparedTranslation, jsonSchema string) (text string, inputTokens, outputTokens int, err error) {
	var lastErr error
	for attempt := 0; attempt < bedrockRetryAttempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, bedrockRequestTimeout)
		text, inputTokens, outputTokens, err = t.invokePrepared(attemptCtx, prepared, jsonSchema)
		cancel()
		if err == nil {
			return text, inputTokens, outputTokens, nil
		}
		lastErr = err
		if attempt == bedrockRetryAttempts-1 || !isBedrockRetryable(err) || ctx.Err() != nil {
			return "", 0, 0, lastErr
		}
		timer := time.NewTimer(bedrockRetryBackoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", 0, 0, ctx.Err()
		case <-timer.C:
		}
	}
	return "", 0, 0, lastErr
}

func isBedrockRetryable(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var httpErr *bedrockHTTPStatusError
	if errors.As(err, &httpErr) {
		return httpErr.status == http.StatusTooManyRequests || httpErr.status >= http.StatusInternalServerError
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

func translationJSONSchema(attachmentCount int) string {
	if attachmentCount <= 0 {
		return bedrockTranslationJSONSchema
	}
	return fmt.Sprintf(
		`{"type":"object","additionalProperties":false,"required":["translations"],"properties":{"translations":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["language","translated_text","attachment_descriptions"],"properties":{"language":{"type":"string"},"translated_text":{"type":"string","description":"The <final_message> translated into this item's language. Empty when <final_message> is empty."},"attachment_descriptions":{"type":"array","items":{"type":"string"},"description":"Exactly %d attachment descriptions in source order. Translate existing alt text. If an image has no alt and is not primarily text, use an empty string."}}}}}}`,
		attachmentCount,
	)
}

func bedrockTextContent(text string) json.RawMessage {
	encoded, err := json.Marshal(text)
	if err != nil {
		return json.RawMessage(`""`)
	}
	return encoded
}

func bedrockUserContent(images []visionImage, text string) json.RawMessage {
	if len(images) == 0 {
		return bedrockTextContent(text)
	}
	parts := make([]any, 0, len(images)+1)
	for _, img := range images {
		parts = append(parts, bedrockInputImagePart{Type: "input_image", ImageURL: img.DataURL})
	}
	parts = append(parts, bedrockInputTextPart{Type: "input_text", Text: text})
	encoded, err := json.Marshal(parts)
	if err != nil {
		return bedrockTextContent(text)
	}
	return encoded
}

func (t *BedrockTranslator) invokePrepared(ctx context.Context, prepared preparedTranslation, jsonSchema string) (text string, inputTokens, outputTokens int, err error) {
	systemInstruction := prepared.systemInstruction + "\nReturn only JSON matching this exact schema, without markdown fences: " + jsonSchema
	payload := bedrockResponsesRequest{
		Model: bedrockModel,
		Input: []bedrockInputMessage{
			{Role: "system", Content: bedrockTextContent(systemInstruction)},
			{Role: "user", Content: bedrockUserContent(prepared.visionImages, prepared.userPrompt)},
		},
		MaxOutputTokens: bedrockMaxTokens,
		Store:           false,
		ServiceTier:     bedrockServiceTier,
	}
	start := t.now()
	entry := bedrockDebugEntry{
		Time:            start,
		GuildID:         prepared.guildID,
		MessageID:       prepared.messageID,
		TargetLanguages: prepared.targetLanguages,
	}
	defer func() {
		entry.DurationMS = t.now().Sub(start).Milliseconds()
		if err != nil {
			entry.Error = err.Error()
		}
		t.debugLog.writeEntry(entry)
	}()
	body, err := json.Marshal(payload)
	if err != nil {
		return "", 0, 0, errors.New("encode Amazon Bedrock translation request")
	}
	entry.Request = body
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.responsesURL, bytes.NewReader(body))
	if err != nil {
		return "", 0, 0, errors.New("create Amazon Bedrock translation request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("OpenAI-Project", t.projectID)
	creds, err := t.credentials.Retrieve(ctx)
	if err != nil {
		return "", 0, 0, errors.New("retrieve AWS credentials")
	}
	sum := sha256.Sum256(body)
	if err := t.signer.SignHTTP(ctx, creds, req, hex.EncodeToString(sum[:]), bedrockService, t.region, t.now()); err != nil {
		return "", 0, 0, errors.New("sign Amazon Bedrock translation request")
	}
	response, err := t.client.Do(req)
	if err != nil {
		return "", 0, 0, fmt.Errorf("Amazon Bedrock translation request: %w", err)
	}
	if response == nil {
		return "", 0, 0, errors.New("Amazon Bedrock response is nil")
	}
	defer response.Body.Close()
	entry.HTTPStatus = response.StatusCode
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return "", 0, 0, errors.New("read Amazon Bedrock translation response")
	}
	entry.recordResponse(responseBody)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", 0, 0, bedrockHTTPError(response, responseBody)
	}
	var output bedrockResponsesResponse
	decoder := json.NewDecoder(bytes.NewReader(responseBody))
	if err := decoder.Decode(&output); err != nil {
		return "", 0, 0, errors.New("decode Amazon Bedrock translation response")
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return "", 0, 0, errors.New("decode Amazon Bedrock translation response")
	}
	return extractBedrockOutputText(output)
}

func (e *bedrockDebugEntry) recordResponse(body []byte) {
	if json.Valid(body) {
		e.Response = body
		return
	}
	e.ResponseText = string(body)
}

func bedrockHTTPError(response *http.Response, body []byte) error {
	var envelope bedrockErrorEnvelope
	_ = json.Unmarshal(body, &envelope)

	errorType := firstSafeErrorField(
		response.Header.Get("x-amzn-errortype"),
		envelope.Error.Type,
		envelope.AWSType,
		envelope.Type,
	)
	code := firstSafeErrorField(envelope.Error.Code, envelope.Code)
	param := firstSafeErrorField(envelope.Error.Param, envelope.Param)
	requestID := firstSafeErrorField(
		response.Header.Get("x-amzn-requestid"),
		response.Header.Get("x-amz-request-id"),
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
		message = fmt.Sprintf("Amazon Bedrock translation request returned HTTP %d", response.StatusCode)
	} else {
		message = fmt.Sprintf("Amazon Bedrock translation request returned HTTP %d (%s)", response.StatusCode, strings.Join(details, ", "))
	}
	return &bedrockHTTPStatusError{status: response.StatusCode, message: message}
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

func extractBedrockOutputText(output bedrockResponsesResponse) (text string, inputTokens, outputTokens int, err error) {
	if output.Status != "completed" {
		return "", 0, 0, fmt.Errorf("Amazon Bedrock response status is %q", output.Status)
	}
	if !isNullJSON(output.Error) || !isNullJSON(output.IncompleteDetails) {
		return "", 0, 0, errors.New("Amazon Bedrock response reports an error or incomplete output")
	}
	messages := 0
	for _, item := range output.Output {
		if item.Type == "reasoning" {
			if item.Status != "completed" {
				return "", 0, 0, errors.New("Amazon Bedrock response has incomplete reasoning")
			}
			continue // Gemma reasoning items are separate from the final message.
		}
		if item.Type != "message" {
			return "", 0, 0, fmt.Errorf("Amazon Bedrock response has unsupported output type %q", item.Type)
		}
		messages++
		if item.Status != "completed" || item.Role != "assistant" || len(item.Content) != 1 || item.Content[0].Type != "output_text" {
			return "", 0, 0, errors.New("Amazon Bedrock response has an invalid message")
		}
		text = strings.TrimSpace(item.Content[0].Text)
	}
	if messages != 1 || text == "" {
		return "", 0, 0, fmt.Errorf("Amazon Bedrock response has %d final messages, want 1", messages)
	}
	if output.Usage == nil || output.Usage.InputTokens < 0 || output.Usage.OutputTokens < 0 {
		return "", 0, 0, errors.New("Amazon Bedrock response has no valid token usage")
	}
	return text, output.Usage.InputTokens, output.Usage.OutputTokens, nil
}

func isNullJSON(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed == "" || trimmed == "null"
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values")
	}
	return nil
}
