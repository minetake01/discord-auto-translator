package translatorbot

import "testing"

func TestResolveUILanguage(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"ja", "ja"},
		{"en", "en"},
		{"zh-CN", "zh-CN"},
		{"zh-cn", "zh-CN"},
		{"pt-BR", "pt-BR"},
		{"", "en"},
		{"xx", "en"},
		{"de-AT", "de"},
	}
	for _, tc := range cases {
		if got := resolveUILanguage(tc.in); got != tc.want {
			t.Fatalf("resolveUILanguage(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLocalizedUIStringKnownLanguages(t *testing.T) {
	for lang := range uiStrings {
		for key := range uiStrings["en"] {
			got := localizedUIString(lang, key)
			if got == "" {
				t.Fatalf("empty string for lang=%s key=%s", lang, key)
			}
			if lang == "en" && got != uiStrings["en"][key] {
				t.Fatalf("en mismatch for key %s: %q", key, got)
			}
		}
	}
}

func TestLocalizedUIStringFallbackToEnglish(t *testing.T) {
	got := localizedUIString("xx-unknown", uiKeyAlreadyOriginal)
	want := uiStrings["en"][uiKeyAlreadyOriginal]
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
