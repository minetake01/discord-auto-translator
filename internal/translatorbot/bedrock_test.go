// SPEC 3.13: Amazon Bedrock translation API client.
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

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

const (
	testBedrockRegion    = "test-region-1"
	testBedrockProjectID = "proj_testproject123"
)

type bedrockRoundTripFunc func(*http.Request) (*http.Response, error)

func (f bedrockRoundTripFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

func bedrockContentText(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		t.Fatalf("content %s: %v", raw, err)
	}
	return text
}

func translateMulti(t testing.TB, ctx context.Context, translator *BedrockTranslator, targetLanguages []string, content string, translationContext TranslationContext, glossary []GlossaryEntry) (MultiTranslationResult, error) {
	t.Helper()
	prepared, err := prepareMultiTranslation(targetLanguages, content, translationContext, glossary)
	if err != nil {
		return MultiTranslationResult{}, err
	}
	return translator.TranslateMulti(ctx, prepared)
}

func translatePollMulti(t testing.TB, translator *BedrockTranslator, targetLanguages []string, question string, answers []string, translationContext TranslationContext, glossary []GlossaryEntry) (PollMultiTranslationResult, error) {
	t.Helper()
	prepared, err := preparePollTranslation(targetLanguages, question, answers, translationContext, glossary)
	if err != nil {
		return PollMultiTranslationResult{}, err
	}
	return translator.TranslatePollMulti(context.Background(), prepared)
}

func translateThreadCreateMulti(t testing.TB, translator *BedrockTranslator, targetLanguages []string, name, message string, translationContext TranslationContext, glossary []GlossaryEntry) (ThreadCreateMultiTranslationResult, error) {
	t.Helper()
	prepared, err := prepareThreadCreateTranslation(targetLanguages, name, message, translationContext, glossary)
	if err != nil {
		return ThreadCreateMultiTranslationResult{}, err
	}
	return translator.TranslateThreadCreateMulti(context.Background(), prepared)
}

type recordingSigner struct {
	calls       atomic.Int32
	service     string
	region      string
	projectID   string
	payloadHash string
}

func (s *recordingSigner) SignHTTP(_ context.Context, _ aws.Credentials, req *http.Request, payloadHash, service, region string, _ time.Time, _ ...func(*v4.SignerOptions)) error {
	s.calls.Add(1)
	s.service, s.region, s.projectID, s.payloadHash = service, region, req.Header.Get("OpenAI-Project"), payloadHash
	return nil
}

func successfulBedrockResponse(raw string, inputTokens, outputTokens int) string {
	encoded, _ := json.Marshal(raw)
	return `{"status":"completed","error":null,"incomplete_details":null,"output":[{"type":"reasoning","status":"completed","role":"","content":[]},{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":` + string(encoded) + `}]}],"usage":{"input_tokens":` + fmtInt(inputTokens) + `,"output_tokens":` + fmtInt(outputTokens) + `}}`
}

func fmtInt(value int) string { return strconv.Itoa(value) }

func testTranslator(client bedrockHTTPClient, signer bedrockRequestSigner) *BedrockTranslator {
	translator := newBedrockTranslator(client, signer, credentials.NewStaticCredentialsProvider("AKID", "SECRET", ""), testBedrockRegion, testBedrockProjectID)
	translator.now = func() time.Time { return time.Unix(123, 0) }
	return translator
}

func TestNewBedrockTranslatorRejectsInvalidLocation(t *testing.T) {
	for _, tt := range []struct {
		name      string
		region    string
		projectID string
	}{
		{name: "empty region", projectID: testBedrockProjectID},
		{name: "invalid region", region: "test-region-1/path", projectID: testBedrockProjectID},
		{name: "empty project ID", region: testBedrockRegion},
		{name: "invalid project ID", region: testBedrockRegion, projectID: "project-name"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewBedrockTranslator(context.Background(), "AKID", "SECRET", tt.region, tt.projectID); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestNewBedrockHTTPClientUsesKeepAliveTransport(t *testing.T) {
	client := newBedrockHTTPClient()
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
	if transport.IdleConnTimeout != bedrockHTTPIdleConnTimeout {
		t.Fatalf("IdleConnTimeout = %s, want %s", transport.IdleConnTimeout, bedrockHTTPIdleConnTimeout)
	}
	if transport.DialContext == nil {
		t.Fatal("DialContext is nil")
	}
	translator, err := NewBedrockTranslator(context.Background(), "AKID", "SECRET", "us-west-2", testBedrockProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if translator.client == http.DefaultClient {
		t.Fatal("NewBedrockTranslator still uses http.DefaultClient")
	}
}

func TestBedrockTranslatorSendsVisionImagesBeforeText(t *testing.T) {
	signer := &recordingSigner{}
	client := bedrockRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		var input bedrockResponsesRequest
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if len(input.Input) != 2 {
			t.Fatalf("prompt shape = %#v", input.Input)
		}
		var parts []map[string]any
		if err := json.Unmarshal(input.Input[1].Content, &parts); err != nil {
			t.Fatalf("user content = %s", input.Input[1].Content)
		}
		if len(parts) != 2 || parts[0]["type"] != "input_image" || parts[1]["type"] != "input_text" {
			t.Fatalf("parts = %#v", parts)
		}
		imageURL, _ := parts[0]["image_url"].(string)
		if !strings.HasPrefix(imageURL, "data:image/jpeg;base64,") {
			t.Fatalf("image_url = %#v", parts[0]["image_url"])
		}
		text, _ := parts[1]["text"].(string)
		if !strings.Contains(text, "<attachments>") {
			t.Fatalf("text part = %#v", parts[1]["text"])
		}
		if !strings.Contains(bedrockContentText(t, input.Input[0].Content), `"attachment_descriptions"`) {
			t.Fatalf("schema missing attachment_descriptions: %s", input.Input[0].Content)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(successfulBedrockResponse(`{"translations":[{"language":"en","translated_text":"Hello","attachment_descriptions":["Exit"]}]}`, 1, 2)))}, nil
	})
	prepared, err := prepareMultiTranslation([]string{"en"}, "出口", TranslationContext{
		Attachments: []TranslationAttachment{{Index: 1, Filename: "sign.png", Description: "出口"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared.visionImages = []visionImage{{DataURL: jpegDataURL([]byte{0xff, 0xd8, 0xff})}}
	result, err := testTranslator(client, signer).TranslateMulti(context.Background(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	if result.Translations["en"] != "Hello" || result.AttachmentDescriptions["en"][0] != "Exit" {
		t.Fatalf("result = %#v", result)
	}
}

func TestBedrockTranslatorRequestContractAndResponseUsage(t *testing.T) {
	signer := &recordingSigner{}
	client := bedrockRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.String() != "https://bedrock-mantle.test-region-1.api.aws/openai/v1/responses" {
			t.Fatalf("request = %s %s", req.Method, req.URL)
		}
		if req.Header.Get("Content-Type") != "application/json" || req.Header.Get("Accept") != "application/json" {
			t.Fatalf("headers = %#v", req.Header)
		}
		if req.Header.Get("OpenAI-Project") != testBedrockProjectID {
			t.Fatalf("OpenAI-Project = %q, want %q", req.Header.Get("OpenAI-Project"), testBedrockProjectID)
		}
		var input bedrockResponsesRequest
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if input.Model != "google.gemma-4-26b-a4b" || input.MaxOutputTokens != 4096 || input.Store || input.ServiceTier != "priority" {
			t.Fatalf("request config = %#v", input)
		}
		if len(input.Input) != 2 || input.Input[0].Role != "system" || input.Input[1].Role != "user" {
			t.Fatalf("prompt shape = %#v", input.Input)
		}
		if !strings.Contains(bedrockContentText(t, input.Input[0].Content), bedrockTranslationJSONSchema) || !strings.Contains(bedrockContentText(t, input.Input[1].Content), "<target_languages>en</target_languages>") {
			t.Fatalf("prompt contract = %#v", input.Input)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(successfulBedrockResponse(`{"translations":[{"language":"en","translated_text":"Hello [USER:Alice]"}]}`, 1, 2)))}, nil
	})
	result, err := translateMulti(t, context.Background(), testTranslator(client, signer), []string{"en"}, "こんにちは <@42>", TranslationContext{
		GuildID: "guild-1", MessageID: "message-2", MentionedUsers: map[string]string{"42": "Alice"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Translations["en"] != "Hello <@42>" || result.InputTokens != 1 || result.OutputTokens != 2 {
		t.Fatalf("result = %#v", result)
	}
	if signer.calls.Load() != 1 || signer.service != "bedrock-mantle" || signer.region != testBedrockRegion || signer.projectID != testBedrockProjectID || len(signer.payloadHash) != 64 {
		t.Fatalf("signature = %#v", signer)
	}
}

func TestBedrockTranslatorRejectsInvalidResponses(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "incomplete", body: `{"status":"incomplete","error":null,"incomplete_details":{"reason":"max_output_tokens"},"output":[],"usage":{"input_tokens":1,"output_tokens":4096}}`},
		{name: "api error", body: `{"status":"failed","error":{"message":"private"},"incomplete_details":null,"output":[],"usage":null}`},
		{name: "no message", body: `{"status":"completed","error":null,"incomplete_details":null,"output":[],"usage":{"input_tokens":1,"output_tokens":1}}`},
		{name: "two messages", body: `{"status":"completed","error":null,"incomplete_details":null,"output":[{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"x"}]},{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"y"}]}],"usage":{"input_tokens":1,"output_tokens":1}}`},
		{name: "non-text", body: `{"status":"completed","error":null,"incomplete_details":null,"output":[{"type":"message","status":"completed","role":"assistant","content":[{"type":"refusal","text":"no"}]}],"usage":{"input_tokens":1,"output_tokens":1}}`},
		{name: "tool output", body: `{"status":"completed","error":null,"incomplete_details":null,"output":[{"type":"function_call","status":"completed"},{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"x"}]}],"usage":{"input_tokens":1,"output_tokens":1}}`},
		{name: "missing usage", body: `{"status":"completed","error":null,"incomplete_details":null,"output":[{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"x"}]}],"usage":null}`},
		{name: "unknown response field", body: `{"status":"completed","unexpected":true}`},
		{name: "malformed JSON", body: successfulBedrockResponse("not-json", 1, 1)},
		{name: "missing language", body: successfulBedrockResponse(`{"translations":[{"language":"en","translated_text":"Hello"}]}`, 1, 1)},
		{name: "duplicate language", body: successfulBedrockResponse(`{"translations":[{"language":"en","translated_text":"Hello"},{"language":"en","translated_text":"Hi"}]}`, 1, 1)},
		{name: "wrong order", body: successfulBedrockResponse(`{"translations":[{"language":"ja","translated_text":"こんにちは"},{"language":"en","translated_text":"Hello"}]}`, 1, 1)},
		{name: "empty translation", body: successfulBedrockResponse(`{"translations":[{"language":"en","translated_text":"Hello"},{"language":"ja","translated_text":" "}]}`, 1, 1)},
		{name: "unknown translation field", body: successfulBedrockResponse(`{"translations":[{"language":"en","translated_text":"Hello"},{"language":"ja","translated_text":"こんにちは","extra":true}]}`, 1, 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := bedrockRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(tt.body))}, nil
			})
			_, err := translateMulti(t, context.Background(), testTranslator(client, &recordingSigner{}), []string{"en", "ja"}, "hello", TranslationContext{}, nil)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestBedrockTranslatorCallsOnceAndHonorsCancellation(t *testing.T) {
	var calls atomic.Int32
	client := bedrockRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := translateMulti(t, ctx, testTranslator(client, &recordingSigner{}), []string{"en"}, "private prompt", TranslationContext{}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestBedrockTranslatorSanitizesAPIErrors(t *testing.T) {
	client := bedrockRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     http.Header{"X-Amzn-Requestid": []string{"request-123"}},
			Body: io.NopCloser(strings.NewReader(
				`{"error":{"type":"invalid_request_error","code":"unsupported_parameter","param":"metadata","message":"SECRET private prompt"}}`,
			)),
		}, nil
	})
	_, err := translateMulti(t, context.Background(), testTranslator(client, &recordingSigner{}), []string{"en"}, "private prompt", TranslationContext{}, nil)
	if err == nil || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "private prompt") {
		t.Fatalf("unsafe error = %v", err)
	}
	for _, expected := range []string{"HTTP 400", "type=invalid_request_error", "code=unsupported_parameter", "param=metadata", "request_id=request-123"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("error %q does not contain %q", err, expected)
		}
	}
}

func TestBedrockTranslatorOmitsMantleRequestMetadata(t *testing.T) {
	client := bedrockRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), `"metadata"`) || strings.Contains(string(body), `"temperature"`) || strings.Contains(string(body), "guild-1") || strings.Contains(string(body), "message-2") {
			t.Fatalf("Mantle request contains unsupported fields: %s", body)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(successfulBedrockResponse(`{"translations":[{"language":"en","translated_text":"Hello"}]}`, 1, 1)))}, nil
	})
	_, err := translateMulti(t, context.Background(), testTranslator(client, &recordingSigner{}), []string{"en"}, "hello", TranslationContext{GuildID: "guild-1", MessageID: "message-2"}, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestBedrockTranslatorDebugLogRecordsRequestAndRawResponse(t *testing.T) {
	response := `{"status":"completed","error":null,"incomplete_details":null,"output":[` +
		`{"type":"reasoning","status":"completed","role":"","content":[{"type":"reasoning_text","text":"keep [USER:Alice] verbatim"}]},` +
		`{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"{\"translations\":[{\"language\":\"en\",\"translated_text\":\"Hello [USER:Alice]\"}]}"}]}],` +
		`"usage":{"input_tokens":1,"output_tokens":2,"output_tokens_details":{"reasoning_tokens":7}}}`
	client := bedrockRoundTripFunc(func(*http.Request) (*http.Response, error) {
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
	var request bedrockResponsesRequest
	if err := json.Unmarshal(entry.Request, &request); err != nil {
		t.Fatal(err)
	}
	if len(request.Input) != 2 || !strings.Contains(bedrockContentText(t, request.Input[0].Content), bedrockTranslationJSONSchema) {
		t.Fatalf("logged system instruction = %#v", request.Input)
	}
	if !strings.Contains(bedrockContentText(t, request.Input[1].Content), "<final_message>こんにちは [USER:Alice]</final_message>") {
		t.Fatalf("logged user prompt = %q", bedrockContentText(t, request.Input[1].Content))
	}
	if string(entry.Response) != response || entry.ResponseText != "" {
		t.Fatalf("logged response = %s", entry.Response)
	}
}

func TestBedrockTranslatorDebugLogRecordsProviderErrorBody(t *testing.T) {
	client := bedrockRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     http.Header{"X-Amzn-Requestid": []string{"request-123"}},
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

func TestBedrockTranslatorDebugLogRecordsTransportFailure(t *testing.T) {
	client := bedrockRoundTripFunc(func(req *http.Request) (*http.Response, error) {
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

func TestBedrockTranslatorWithoutDebugLogWritesNoFile(t *testing.T) {
	dir := t.TempDir()
	client := bedrockRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(successfulBedrockResponse(`{"translations":[{"language":"en","translated_text":"Hello"}]}`, 1, 1)))}, nil
	})
	if _, err := translateMulti(t, context.Background(), testTranslator(client, &recordingSigner{}), []string{"en"}, "hello", TranslationContext{GuildID: "guild-1"}, nil); err != nil {
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

func debugLoggingTranslator(t *testing.T, client bedrockHTTPClient) (*BedrockTranslator, string) {
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
	translator := testTranslator(client, &recordingSigner{})
	translator.SetDebugLog(debugLog)
	return translator, path
}

func singleDebugLogEntry(t *testing.T, path string) bedrockDebugEntry {
	t.Helper()
	lines := readDebugLogLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("debug log lines = %d, want 1", len(lines))
	}
	var entry bedrockDebugEntry
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatal(err)
	}
	return entry
}

func TestBedrockTranslatorRetriesTransientHTTPErrorOnce(t *testing.T) {
	var calls atomic.Int32
	client := bedrockRoundTripFunc(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"service_unavailable","code":"unavailable"}}`)),
			}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(successfulBedrockResponse(`{"translations":[{"language":"en","translated_text":"Hello"}]}`, 1, 1)))}, nil
	})
	result, err := translateMulti(t, context.Background(), testTranslator(client, &recordingSigner{}), []string{"en"}, "hello", TranslationContext{}, nil)
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

func TestBedrockTranslatorRetriesDeadlineExceededOnce(t *testing.T) {
	var calls atomic.Int32
	client := bedrockRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return nil, context.DeadlineExceeded
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(successfulBedrockResponse(`{"translations":[{"language":"en","translated_text":"Hello"}]}`, 1, 1)))}, nil
	})
	result, err := translateMulti(t, context.Background(), testTranslator(client, &recordingSigner{}), []string{"en"}, "hello", TranslationContext{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Translations["en"] != "Hello" || calls.Load() != 2 {
		t.Fatalf("result = %#v, calls = %d", result, calls.Load())
	}
}

func TestBedrockTranslatorDoesNotRetryClientHTTPError(t *testing.T) {
	var calls atomic.Int32
	client := bedrockRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","code":"bad_request"}}`)),
		}, nil
	})
	_, err := translateMulti(t, context.Background(), testTranslator(client, &recordingSigner{}), []string{"en"}, "hello", TranslationContext{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestBedrockTranslatorDoesNotRetryContractViolation(t *testing.T) {
	var calls atomic.Int32
	client := bedrockRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(
			`{"status":"incomplete","error":null,"incomplete_details":{"reason":"max_output_tokens"},"output":[],"usage":{"input_tokens":1,"output_tokens":4096}}`,
		))}, nil
	})
	_, err := translateMulti(t, context.Background(), testTranslator(client, &recordingSigner{}), []string{"en"}, "hello", TranslationContext{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestBedrockTranslatorReturnsLastErrorAfterRetryExhausted(t *testing.T) {
	var calls atomic.Int32
	client := bedrockRoundTripFunc(func(*http.Request) (*http.Response, error) {
		n := calls.Add(1)
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"service_unavailable","code":"unavailable","message":"attempt-` + fmtInt(int(n)) + `"}}`)),
		}, nil
	})
	_, err := translateMulti(t, context.Background(), testTranslator(client, &recordingSigner{}), []string{"en"}, "hello", TranslationContext{}, nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 503") {
		t.Fatalf("error = %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}

func TestBedrockWarmUpUsesAttemptTimeout(t *testing.T) {
	client := bedrockRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		got, ok := req.Context().Deadline()
		if !ok {
			t.Fatal("missing deadline")
		}
		remaining := time.Until(got)
		if remaining < 59*time.Second || remaining > bedrockRequestTimeout {
			t.Fatalf("remaining = %s, want ~%s", remaining, bedrockRequestTimeout)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(successfulBedrockResponse(`{"translations":[{"language":"en","translated_text":"warmup"}]}`, 1, 1)))}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := testTranslator(client, &recordingSigner{}).WarmUp(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestBedrockTranslatorTranslatePollMulti(t *testing.T) {
	client := bedrockRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		var input bedrockResponsesRequest
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(bedrockContentText(t, input.Input[0].Content), bedrockPollTranslationJSONSchema) || !strings.Contains(bedrockContentText(t, input.Input[1].Content), "<poll>") {
			t.Fatalf("unexpected request: %#v", input.Input)
		}
		body := successfulBedrockResponse(`{"translations":[{"language":"en","question":"Favorite?","answers":["Red","Blue"]}]}`, 3, 4)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	result, err := translatePollMulti(t, testTranslator(client, &recordingSigner{}), []string{"en"}, "好き？", []string{"赤", "青"}, TranslationContext{}, nil)
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

func TestBedrockTranslatorTranslatePollMultiRejectsAnswerCountMismatch(t *testing.T) {
	client := bedrockRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := successfulBedrockResponse(`{"translations":[{"language":"en","question":"Favorite?","answers":["Red"]}]}`, 1, 1)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	_, err := translatePollMulti(t, testTranslator(client, &recordingSigner{}), []string{"en"}, "好き？", []string{"赤", "青"}, TranslationContext{}, nil)
	if err == nil {
		t.Fatal("expected answer count mismatch")
	}
}

func TestBedrockTranslatorTranslateThreadCreateMulti(t *testing.T) {
	client := bedrockRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		var input bedrockResponsesRequest
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(bedrockContentText(t, input.Input[0].Content), bedrockThreadCreateTranslationJSONSchema) || !strings.Contains(bedrockContentText(t, input.Input[1].Content), "<thread_create>") {
			t.Fatalf("unexpected request: %#v", input.Input)
		}
		userPrompt := bedrockContentText(t, input.Input[1].Content)
		if !strings.Contains(userPrompt, "<name>議題</name>") || !strings.Contains(userPrompt, `<message author="alice">本文</message>`) {
			t.Fatalf("unexpected user prompt: %s", userPrompt)
		}
		body := successfulBedrockResponse(`{"translations":[{"language":"en","name":"Topic","message":"Body"}]}`, 3, 4)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	result, err := translateThreadCreateMulti(t, testTranslator(client, &recordingSigner{}), []string{"en"}, "議題", "本文", TranslationContext{Author: "alice"}, nil)
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

func TestBedrockTranslatorTranslateThreadCreateMultiRejectsEmptyMessage(t *testing.T) {
	client := bedrockRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := successfulBedrockResponse(`{"translations":[{"language":"en","name":"Topic","message":""}]}`, 1, 1)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	_, err := translateThreadCreateMulti(t, testTranslator(client, &recordingSigner{}), []string{"en"}, "議題", "本文", TranslationContext{}, nil)
	if err == nil {
		t.Fatal("expected empty message rejection")
	}
}
