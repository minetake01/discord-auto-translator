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
		"cache: ?",
		"prompt: system=11B frozen=",
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

func TestPrintDetailShowsCacheHitCostAndPromptSizes(t *testing.T) {
	hit := true
	ttl := false
	cached := 800
	cost := 0.00014
	entry := logEntry{
		Model:              "qwen/qwen3.6-35b-a3b",
		SchemaName:         "message_translations",
		PromptCacheKey:     "guild:g:group:start1",
		PromptCacheTTLSent: &ttl,
		PromptCacheHit:     &hit,
		DurationMS:         812,
		WaitMS:             int64Ptr(800),
		ReadMS:             int64Ptr(5),
		Attempt:            1,
		ProcessingMS:       int64Ptr(790),
		SystemInstruction:  "Translate faithfully.",
		UserPromptFrozen:   "<translation_request><target_languages>en</target_languages>",
		UserPromptVariable: "<final_message>こんにちは</final_message></translation_request>",
		Usage: &loggedUsage{
			PromptTokens:     1200,
			CompletionTokens: 40,
			CachedTokens:     &cached,
			CostUSD:          &cost,
		},
	}

	output := captureStdout(t, func() { printDetail(entry) })
	for _, want := range []string{
		"schema: message_translations",
		"model: qwen/qwen3.6-35b-a3b",
		"cache_key: guild:g:group:start1",
		"cache: hit  cached=800/1200  ttl_sent=false",
		"timing: duration=812ms  wait=800ms  read=5ms  attempt=1  processing=790ms",
		"cost_usd: $0.00014",
		"tokens: in=1200 cached=800 out=40",
		"source: こんにちは",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("detail output missing %q\n%s", want, output)
		}
	}
}

func TestPrintPromptDumpsSynthesizedParts(t *testing.T) {
	entry := logEntry{
		SystemInstruction:  "System line",
		UserPromptFrozen:   "Frozen part",
		UserPromptVariable: "Variable part",
	}
	output := captureStdout(t, func() { printPrompt(entry) })
	for _, want := range []string{
		"--- system ---",
		"System line",
		"--- user frozen ---",
		"Frozen part",
		"--- user variable ---",
		"Variable part",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("prompt output missing %q\n%s", want, output)
		}
	}
}

func TestPrintStatsAggregatesCacheAndCost(t *testing.T) {
	hit, miss := true, false
	cachedHit, cachedMiss := 800, 0
	cost1, cost2 := 0.0002, 0.0001
	ttlTrue, ttlFalse := true, false
	entries := []logEntry{
		{DurationMS: 100, WaitMS: int64Ptr(90), PromptCacheHit: &hit, PromptCacheTTLSent: &ttlTrue, Usage: &loggedUsage{PromptTokens: 1000, CachedTokens: &cachedHit, CompletionTokens: 10, CostUSD: &cost1}},
		{DurationMS: 300, WaitMS: int64Ptr(280), PromptCacheHit: &miss, PromptCacheTTLSent: &ttlFalse, Usage: &loggedUsage{PromptTokens: 200, CachedTokens: &cachedMiss, CompletionTokens: 5, CostUSD: &cost2}},
		{DurationMS: 200, Usage: &loggedUsage{PromptTokens: 50, CompletionTokens: 2}},
	}

	output := captureStdout(t, func() { printStats(entries) })
	for _, want := range []string{
		"stats (filtered n=3):",
		"cost_usd=$0.0003 (2/3 reported)",
		"tokens: prompt=1250 cached=800 (64.0%) completion=17",
		"cache: hit=1 miss=1 unknown=1 hit_rate=50.0% ttl_writes=1",
		"duration_ms: avg=200 min=100 max=300 n=3",
		"wait_ms: avg=185 min=90 max=280 n=2",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stats output missing %q\n%s", want, output)
		}
	}
}

func TestParseEntryReadsMeasurementFields(t *testing.T) {
	line := []byte(`{"time":"2026-08-20T05:00:00Z","schema_name":"message_translations","prompt_cache_key":"k","prompt_cache_ttl_sent":false,"prompt_cache_hit":true,"system_instruction":"sys","user_prompt_frozen":"frozen","user_prompt_variable":"var","usage":{"prompt_tokens":9,"cached_tokens":3,"cost_usd":0.001}}`)
	entry, err := parseEntry(line)
	if err != nil {
		t.Fatal(err)
	}
	if entry.SchemaName != "message_translations" || entry.PromptCacheKey != "k" || entry.SystemInstruction != "sys" {
		t.Fatalf("entry = %#v", entry)
	}
	if entry.PromptCacheTTLSent == nil || *entry.PromptCacheTTLSent || entry.PromptCacheHit == nil || !*entry.PromptCacheHit {
		t.Fatalf("cache flags = ttl=%v hit=%v", entry.PromptCacheTTLSent, entry.PromptCacheHit)
	}
	if entry.Usage == nil || entry.Usage.PromptTokens != 9 || entry.Usage.CachedTokens == nil || *entry.Usage.CachedTokens != 3 || entry.Usage.CostUSD == nil || *entry.Usage.CostUSD != 0.001 {
		t.Fatalf("usage = %#v", entry.Usage)
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

func int64Ptr(v int64) *int64 { return &v }
