package translatorbot

import (
	"strings"
	"testing"
)

func TestResolveStyleInstructions(t *testing.T) {
	custom := "短くカジュアルに"
	if got := ResolveStyleInstructions("", custom); got != custom {
		t.Fatalf("custom = %q", got)
	}
	if got := ResolveStyleInstructions("formal", ""); got != stylePresetInstructions["formal"] {
		t.Fatalf("formal = %q", got)
	}
	if got := ResolveStyleInstructions(StylePresetDefault, ""); got != nativePhrasingInstruction {
		t.Fatalf("default = %q", got)
	}
	if got := ResolveStyleInstructions("", ""); got != nativePhrasingInstruction {
		t.Fatalf("empty = %q", got)
	}
	if got := ResolveStyleInstructions(StylePresetDefault, ""); !strings.Contains(got, "casual Japanese: そう, not そうだ") {
		t.Fatalf("default = %q", got)
	}
	if got := ResolveStyleInstructions("netslang", ""); !strings.HasPrefix(got, nativePhrasingInstruction) || !strings.Contains(got, "2ch/5ch-style") {
		t.Fatalf("netslang = %q", got)
	}
	if got := ResolveStyleInstructions("casual", ""); !strings.HasPrefix(got, nativePhrasingInstruction) || !strings.Contains(got, "friends chatting") {
		t.Fatalf("casual = %q", got)
	}
	if got := ResolveStyleInstructions("formal", ""); strings.Contains(got, nativePhrasingInstruction) {
		t.Fatalf("formal should not include native phrasing: %q", got)
	}
	if got := ResolveStyleInstructions("literal", ""); strings.Contains(got, nativePhrasingInstruction) {
		t.Fatalf("literal should not include native phrasing: %q", got)
	}
	if got := ResolveStyleInstructions("tweet", ""); !strings.Contains(got, "social media phrasing") {
		t.Fatalf("tweet = %q", got)
	}
	if IsValidStylePreset("natural") {
		t.Fatal("natural preset should no longer exist")
	}
}

func TestValidateStyleCustom(t *testing.T) {
	if err := ValidateStyleCustom(""); err == nil {
		t.Fatal("expected error for empty custom")
	}
	if err := ValidateStyleCustom(strings.Repeat("あ", styleCustomMaxRunes+1)); err == nil {
		t.Fatal("expected error for too long custom")
	}
	if err := ValidateStyleCustom("短く"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFormatGroupStyle(t *testing.T) {
	if got := FormatGroupStyle(TranslationGroup{}); got != StylePresetDefault {
		t.Fatalf("default = %q", got)
	}
	if got := FormatGroupStyle(TranslationGroup{StylePreset: "gaming"}); got != "gaming" {
		t.Fatalf("preset = %q", got)
	}
	if got := FormatGroupStyle(TranslationGroup{StyleCustom: "敬語を使わない"}); got != "custom: 敬語を使わない" {
		t.Fatalf("custom = %q", got)
	}
}
