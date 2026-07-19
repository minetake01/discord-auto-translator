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
	bedrockRequestTimeout = 30 * time.Second

	bedrockTranslationJSONSchema = `{"type":"object","additionalProperties":false,"required":["translations"],"properties":{"translations":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["language","translated_text"],"properties":{"language":{"type":"string"},"translated_text":{"type":"string","description":"The <final_message> translated into this item's language."}}}}}}`
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
}

type bedrockResponsesRequest struct {
	Model           string                `json:"model"`
	Input           []bedrockInputMessage `json:"input"`
	MaxOutputTokens int                   `json:"max_output_tokens"`
	Store           bool                  `json:"store"`
}

type bedrockInputMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
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
		http.DefaultClient,
		v4.NewSigner(),
		credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, ""),
		region,
		projectID,
	), nil
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

func (t *BedrockTranslator) TranslateMulti(ctx context.Context, targetLanguages []string, content string, translationContext TranslationContext, glossary []GlossaryEntry) (MultiTranslationResult, error) {
	prepared, err := prepareMultiTranslation(targetLanguages, content, translationContext, glossary)
	if err != nil {
		return MultiTranslationResult{}, err
	}
	if len(prepared.targetLanguages) == 0 {
		return MultiTranslationResult{Translations: map[string]string{}}, nil
	}
	runtimeCtx, cancel := context.WithTimeout(ctx, bedrockRequestTimeout)
	defer cancel()
	return t.translatePrepared(runtimeCtx, prepared)
}

// WarmUp verifies credentials, model access, and the fixed response contract
// without starting Discord, SQLite, or the HTTP server. The caller owns the deadline.
func (t *BedrockTranslator) WarmUp(ctx context.Context) error {
	prepared, err := prepareMultiTranslation([]string{"en"}, "warmup", TranslationContext{}, nil)
	if err != nil {
		return err
	}
	_, err = t.translatePrepared(ctx, prepared)
	if err != nil {
		return fmt.Errorf("prewarm Amazon Bedrock model: %w", err)
	}
	return nil
}

func (t *BedrockTranslator) translatePrepared(ctx context.Context, prepared preparedTranslation) (MultiTranslationResult, error) {
	systemInstruction := prepared.systemInstruction + "\nReturn only JSON matching this exact schema, without markdown fences: " + bedrockTranslationJSONSchema
	payload := bedrockResponsesRequest{
		Model: bedrockModel,
		Input: []bedrockInputMessage{
			{Role: "system", Content: systemInstruction},
			{Role: "user", Content: prepared.userPrompt},
		},
		MaxOutputTokens: bedrockMaxTokens,
		Store:           false,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return MultiTranslationResult{}, errors.New("encode Amazon Bedrock translation request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.responsesURL, bytes.NewReader(body))
	if err != nil {
		return MultiTranslationResult{}, errors.New("create Amazon Bedrock translation request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("OpenAI-Project", t.projectID)
	creds, err := t.credentials.Retrieve(ctx)
	if err != nil {
		return MultiTranslationResult{}, errors.New("retrieve AWS credentials")
	}
	sum := sha256.Sum256(body)
	if err := t.signer.SignHTTP(ctx, creds, req, hex.EncodeToString(sum[:]), bedrockService, t.region, t.now()); err != nil {
		return MultiTranslationResult{}, errors.New("sign Amazon Bedrock translation request")
	}
	response, err := t.client.Do(req)
	if err != nil {
		return MultiTranslationResult{}, fmt.Errorf("Amazon Bedrock translation request: %w", err)
	}
	if response == nil {
		return MultiTranslationResult{}, errors.New("Amazon Bedrock response is nil")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return MultiTranslationResult{}, bedrockHTTPError(response)
	}
	var output bedrockResponsesResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, 8<<20))
	if err := decoder.Decode(&output); err != nil {
		return MultiTranslationResult{}, errors.New("decode Amazon Bedrock translation response")
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return MultiTranslationResult{}, errors.New("decode Amazon Bedrock translation response")
	}
	return parseBedrockResponse(output, prepared)
}

func bedrockHTTPError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
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
	if len(details) == 0 {
		return fmt.Errorf("Amazon Bedrock translation request returned HTTP %d", response.StatusCode)
	}
	return fmt.Errorf("Amazon Bedrock translation request returned HTTP %d (%s)", response.StatusCode, strings.Join(details, ", "))
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

func parseBedrockResponse(output bedrockResponsesResponse, prepared preparedTranslation) (MultiTranslationResult, error) {
	if output.Status != "completed" {
		return MultiTranslationResult{}, fmt.Errorf("Amazon Bedrock response status is %q", output.Status)
	}
	if !isNullJSON(output.Error) || !isNullJSON(output.IncompleteDetails) {
		return MultiTranslationResult{}, errors.New("Amazon Bedrock response reports an error or incomplete output")
	}
	var text string
	messages := 0
	for _, item := range output.Output {
		if item.Type == "reasoning" {
			if item.Status != "completed" {
				return MultiTranslationResult{}, errors.New("Amazon Bedrock response has incomplete reasoning")
			}
			continue // Gemma reasoning items are separate from the final message.
		}
		if item.Type != "message" {
			return MultiTranslationResult{}, fmt.Errorf("Amazon Bedrock response has unsupported output type %q", item.Type)
		}
		messages++
		if item.Status != "completed" || item.Role != "assistant" || len(item.Content) != 1 || item.Content[0].Type != "output_text" {
			return MultiTranslationResult{}, errors.New("Amazon Bedrock response has an invalid message")
		}
		text = strings.TrimSpace(item.Content[0].Text)
	}
	if messages != 1 || text == "" {
		return MultiTranslationResult{}, fmt.Errorf("Amazon Bedrock response has %d final messages, want 1", messages)
	}
	if output.Usage == nil || output.Usage.InputTokens < 0 || output.Usage.OutputTokens < 0 {
		return MultiTranslationResult{}, errors.New("Amazon Bedrock response has no valid token usage")
	}
	translations, err := parseMultiTranslationResponse(text, prepared.targetLanguages, prepared.protector)
	if err != nil {
		return MultiTranslationResult{}, err
	}
	return MultiTranslationResult{Translations: translations, InputTokens: output.Usage.InputTokens, OutputTokens: output.Usage.OutputTokens}, nil
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
