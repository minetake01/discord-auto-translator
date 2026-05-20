package translatorbot

import (
	"strings"
	"testing"
)

func TestBuildTranslationPromptIncludesHistory(t *testing.T) {
	prompt := BuildTranslationPrompt("en", "こんにちは", []ChatContextMessage{
		{Author: "a", Language: "ja", Content: "前の発言"},
	})
	if !strings.Contains(prompt, "Translate the final message into en") {
		t.Fatal(prompt)
	}
	if !strings.Contains(prompt, "a [ja]: 前の発言") {
		t.Fatal(prompt)
	}
	if !strings.Contains(prompt, "こんにちは") {
		t.Fatal(prompt)
	}
}

func TestProtectorRestoresURLsAndMarkdown(t *testing.T) {
	p := NewProtector()
	in := "see https://example.com/a?x=1 and `code` ||secret||"
	protected := p.Protect(in)
	if strings.Contains(protected, "https://example.com") || strings.Contains(protected, "`code`") {
		t.Fatalf("not protected: %s", protected)
	}
	if got := p.Restore(protected); got != in {
		t.Fatalf("got %q want %q", got, in)
	}
}

func TestLanguageSuggestionsAllowRepresentativeCodes(t *testing.T) {
	got := LanguageSuggestions("zh", 25)
	if len(got) != 2 || got[0] != "zh-CN" || got[1] != "zh-TW" {
		t.Fatalf("unexpected suggestions: %#v", got)
	}
}
