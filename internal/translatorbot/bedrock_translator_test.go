package translatorbot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

type bedrockRoundTripFunc func(*http.Request) (*http.Response, error)

func (f bedrockRoundTripFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

type recordingSigner struct {
	calls       atomic.Int32
	service     string
	region      string
	payloadHash string
}

func (s *recordingSigner) SignHTTP(_ context.Context, _ aws.Credentials, _ *http.Request, payloadHash, service, region string, _ time.Time, _ ...func(*v4.SignerOptions)) error {
	s.calls.Add(1)
	s.service, s.region, s.payloadHash = service, region, payloadHash
	return nil
}

func successfulBedrockResponse(raw string, inputTokens, outputTokens int) string {
	encoded, _ := json.Marshal(raw)
	return `{"status":"completed","error":null,"incomplete_details":null,"output":[{"type":"reasoning","status":"completed","role":"","content":[]},{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":` + string(encoded) + `}]}],"usage":{"input_tokens":` + fmtInt(inputTokens) + `,"output_tokens":` + fmtInt(outputTokens) + `}}`
}

func fmtInt(value int) string { return strconv.Itoa(value) }

func testTranslator(client bedrockHTTPClient, signer bedrockRequestSigner) *BedrockTranslator {
	translator := newBedrockTranslator(client, signer, credentials.NewStaticCredentialsProvider("AKID", "SECRET", ""))
	translator.now = func() time.Time { return time.Unix(123, 0) }
	return translator
}

func TestBedrockTranslatorRequestContractAndResponseUsage(t *testing.T) {
	signer := &recordingSigner{}
	client := bedrockRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.String() != bedrockResponsesURL {
			t.Fatalf("request = %s %s", req.Method, req.URL)
		}
		if req.Header.Get("Content-Type") != "application/json" || req.Header.Get("Accept") != "application/json" {
			t.Fatalf("headers = %#v", req.Header)
		}
		var input bedrockResponsesRequest
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if input.Model != bedrockModel || input.MaxOutputTokens != bedrockMaxTokens || input.Store {
			t.Fatalf("request config = %#v", input)
		}
		if len(input.Input) != 2 || input.Input[0].Role != "system" || input.Input[1].Role != "user" {
			t.Fatalf("prompt shape = %#v", input.Input)
		}
		if !strings.Contains(input.Input[0].Content, bedrockTranslationJSONSchema) || !strings.Contains(input.Input[1].Content, "<target_languages>en</target_languages>") {
			t.Fatalf("prompt contract = %#v", input.Input)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(successfulBedrockResponse(`{"translations":[{"language":"en","translated_text":"Hello [USER:Alice]"}]}`, 1, 2)))}, nil
	})
	result, err := testTranslator(client, signer).TranslateMulti(context.Background(), []string{"en"}, "こんにちは <@42>", TranslationContext{
		GuildID: "guild-1", MessageID: "message-2", MentionedUsers: map[string]string{"42": "Alice"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Translations["en"] != "Hello <@42>" || result.InputTokens != 1 || result.OutputTokens != 2 {
		t.Fatalf("result = %#v", result)
	}
	if signer.calls.Load() != 1 || signer.service != bedrockService || signer.region != bedrockRegion || len(signer.payloadHash) != 64 {
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
			_, err := testTranslator(client, &recordingSigner{}).TranslateMulti(context.Background(), []string{"en", "ja"}, "hello", TranslationContext{}, nil)
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
	_, err := testTranslator(client, &recordingSigner{}).TranslateMulti(ctx, []string{"en"}, "private prompt", TranslationContext{}, nil)
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
	_, err := testTranslator(client, &recordingSigner{}).TranslateMulti(context.Background(), []string{"en"}, "private prompt", TranslationContext{}, nil)
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
	_, err := testTranslator(client, &recordingSigner{}).TranslateMulti(context.Background(), []string{"en"}, "hello", TranslationContext{GuildID: "guild-1", MessageID: "message-2"}, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestBedrockTranslatorRuntimeTimeoutIsThirtySeconds(t *testing.T) {
	if bedrockRequestTimeout != 30*time.Second {
		t.Fatalf("timeout = %s", bedrockRequestTimeout)
	}
}

func TestBedrockWarmUpUsesCallerDeadline(t *testing.T) {
	deadline := time.Now().Add(2 * time.Minute)
	client := bedrockRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		got, ok := req.Context().Deadline()
		if !ok || got.Before(deadline.Add(-time.Second)) {
			t.Fatalf("deadline = %v, want caller deadline near %v", got, deadline)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(successfulBedrockResponse(`{"translations":[{"language":"en","translated_text":"warmup"}]}`, 1, 1)))}, nil
	})
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	if err := testTranslator(client, &recordingSigner{}).WarmUp(ctx); err != nil {
		t.Fatal(err)
	}
}
