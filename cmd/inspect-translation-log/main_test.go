package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestExtractReasoningText(t *testing.T) {
	resp := parseResponse([]byte(`{
		"output":[
			{"type":"reasoning","content":[{"type":"reasoning_text","text":"keep mention"}]},
			{"type":"message","content":[{"type":"output_text","text":"{\"translations\":[]}"}]}
		]
	}`))
	got := extractReasoningText(resp)
	if got != "keep mention" {
		t.Fatalf("extractReasoningText = %q", got)
	}
}

func TestPrintDetailIncludesReasoningTokensAndText(t *testing.T) {
	entry := logEntry{
		Request: []byte(`{"input":[{"role":"user","content":"<final_message>こんにちは</final_message>"}]}`),
		Response: []byte(`{
			"status":"completed",
			"usage":{"input_tokens":11,"output_tokens":22,"output_tokens_details":{"reasoning_tokens":7}},
			"output":[
				{"type":"reasoning","content":[{"type":"reasoning_text","text":"keep [USER:Alice] verbatim"}]},
				{"type":"message","content":[{"type":"output_text","text":"{\"translations\":[{\"language\":\"en\",\"translated_text\":\"Hello\"}]}"}]}
			]
		}`),
	}

	output := captureStdout(t, func() { printDetail(entry) })
	for _, want := range []string{
		"source: こんにちは",
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
			"status":"completed",
			"usage":{"input_tokens":1,"output_tokens":2},
			"output":[{"type":"message","content":[{"type":"output_text","text":"{\"translations\":[{\"language\":\"en\",\"translated_text\":\"Hi\"}]}"}]}]
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
