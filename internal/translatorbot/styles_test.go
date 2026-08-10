package translatorbot

import (
	"strings"
	"testing"
)

// SPEC 3.1: custom overrides preset; default/casual-family presets use native chat
// phrasing; formal/literal do not; unknown presets are rejected.
func TestResolveStyleInstructions(t *testing.T) {
	custom := "短くカジュアルに"
	if got := ResolveStyleInstructions("", custom); got != custom {
		t.Fatalf("custom should win over empty preset: %q", got)
	}
	if got := ResolveStyleInstructions("formal", custom); got != custom {
		t.Fatalf("custom should win over preset: %q", got)
	}

	defaultInstr := ResolveStyleInstructions(StylePresetDefault, "")
	if defaultInstr == "" {
		t.Fatal("default preset must produce instructions")
	}
	emptyInstr := ResolveStyleInstructions("", "")
	if emptyInstr != defaultInstr {
		t.Fatalf("empty preset should match default: %q vs %q", emptyInstr, defaultInstr)
	}

	for _, preset := range []string{"casual", "gaming", "friendly", "netslang", "tweet"} {
		got := ResolveStyleInstructions(preset, "")
		if !strings.HasPrefix(got, defaultInstr) {
			t.Fatalf("%s must include native phrasing as prefix: %q", preset, got)
		}
		if got == defaultInstr {
			t.Fatalf("%s must add preset-specific guidance beyond default", preset)
		}
	}

	for _, preset := range []string{"formal", "literal", "business"} {
		got := ResolveStyleInstructions(preset, "")
		if got == "" {
			t.Fatalf("%s must produce instructions", preset)
		}
		if strings.Contains(got, defaultInstr) {
			t.Fatalf("%s must not include native phrasing: %q", preset, got)
		}
	}

	if IsValidStylePreset("natural") {
		t.Fatal("unknown preset natural must be rejected")
	}
}

// SPEC 3.1: custom style instructions are bounded (non-empty, max 200 runes).
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
