package translatorbot

import (
	"strings"
	"testing"
)

func TestGlossaryFeedbackHintUsesLocalizedSuffix(t *testing.T) {
	got := GlossaryFeedbackHint("ja", "123")
	if !strings.Contains(got, "で用語を登録できます") {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, "</add-glossary:123>") {
		t.Fatalf("got %q", got)
	}
}

func TestGlossaryFeedbackHintFallsBackToEnglish(t *testing.T) {
	got := GlossaryFeedbackHint("nl", "456")
	if !strings.Contains(got, "to register preferred terms") {
		t.Fatalf("got %q", got)
	}
}

func TestGlossaryFeedbackHintWithoutCommandID(t *testing.T) {
	got := GlossaryFeedbackHint("en", "")
	if got != "\n-# /add-glossary to register preferred terms" {
		t.Fatalf("got %q", got)
	}
}
