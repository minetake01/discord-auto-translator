package translatorbot

import (
	"regexp"
	"sort"
	"testing"
)

var formatVerbPattern = regexp.MustCompile(`%\[\d+\][a-z]|%[a-z]`)

// TestUIStringCatalogIsComplete verifies that every supported language defines
// every key with the same format verbs as the English reference catalog.
func TestUIStringCatalogIsComplete(t *testing.T) {
	reference := uiStrings["en"]
	if len(reference) == 0 {
		t.Fatal("English catalog is empty")
	}
	for lang, catalog := range uiStrings {
		if len(catalog) != len(reference) {
			t.Errorf("lang %s has %d keys, want %d", lang, len(catalog), len(reference))
		}
		for key, refText := range reference {
			text, ok := catalog[key]
			if !ok {
				t.Errorf("lang %s is missing key %s", lang, key)
				continue
			}
			if text == "" {
				t.Errorf("lang %s has empty string for key %s", lang, key)
				continue
			}
			wantVerbs := formatVerbPattern.FindAllString(refText, -1)
			gotVerbs := formatVerbPattern.FindAllString(text, -1)
			sort.Strings(wantVerbs)
			sort.Strings(gotVerbs)
			if len(gotVerbs) != len(wantVerbs) {
				t.Errorf("lang %s key %s format verbs = %v, want %v", lang, key, gotVerbs, wantVerbs)
				continue
			}
			for n := range wantVerbs {
				if gotVerbs[n] != wantVerbs[n] {
					t.Errorf("lang %s key %s format verbs = %v, want %v", lang, key, gotVerbs, wantVerbs)
					break
				}
			}
		}
	}
}

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
