package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestPrintDetailReadsChatCompletionsRoundTrip(t *testing.T) {
	entry := logEntry{
		Request: []byte(`{
			"messages":[
				{"role":"system","content":"Return JSON"},
				{"role":"user","content":[
					{"type":"text","text":"<translation_request><recent_context>"},
					{"type":"text","text":"</recent_context><final_message>こんにちは</final_message></translation_request>"}
				]}
			]
		}`),
		Response: []byte(`{
			"choices":[{
				"finish_reason":"stop",
				"message":{
					"role":"assistant",
					"content":"{\"translations\":[{\"language\":\"en\",\"translated_text\":\"Hello\"}]}",
					"reasoning":"keep [USER:Alice] verbatim"
				}
			}],
			"usage":{"prompt_tokens":11,"completion_tokens":22,"completion_tokens_details":{"reasoning_tokens":7}}
		}`),
	}

	output := captureStdout(t, func() { printDetail(entry) })
	for _, want := range []string{
		"source: こんにちは",
		"finish_reason: stop",
		"tokens: in=11 out=22 reasoning=7",
		"reasoning: keep [USER:Alice] verbatim",
		"[en] Hello",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("detail output missing %q\n%s", want, output)
		}
	}
}

func TestPrintDetailOmitsReasoningWhenAbsent(t *testing.T) {
	entry := logEntry{
		Response: []byte(`{
			"choices":[{
				"finish_reason":"stop",
				"message":{"role":"assistant","content":"{\"translations\":[{\"language\":\"en\",\"translated_text\":\"Hi\"}]}"}
			}],
			"usage":{"prompt_tokens":1,"completion_tokens":2}
		}`),
	}

	output := captureStdout(t, func() { printDetail(entry) })
	if !strings.Contains(output, "tokens: in=1 out=2\n") {
		t.Fatalf("unexpected tokens line\n%s", output)
	}
	if strings.Contains(output, "reasoning") {
		t.Fatalf("unexpected reasoning output\n%s", output)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()

	fn()
	w.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}
