// Localized UI strings for slash commands and notifications.
package translatorbot

import (
	"regexp"
	"sort"
	"testing"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
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

func TestLocalizedUIStringFallbackToEnglish(t *testing.T) {
	got := localizedUIString("xx-unknown", uiKeyAlreadyOriginal)
	want := uiStrings["en"][uiKeyAlreadyOriginal]
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestDiscordLocalesCoverCatalogLanguages verifies that every catalog language
// declares the Discord client locales it serves, so a new language cannot ship
// without localized command metadata.
func TestDiscordLocalesCoverCatalogLanguages(t *testing.T) {
	for lang := range uiStrings {
		if len(discordLocalesByLanguage[lang]) == 0 {
			t.Errorf("lang %s has no Discord locales", lang)
		}
	}
	for lang := range discordLocalesByLanguage {
		if _, ok := uiStrings[lang]; !ok {
			t.Errorf("Discord locales declared for unknown lang %s", lang)
		}
	}
}

// TestCommandMetadataCoversCatalogLanguages verifies that a client in any
// non-canonical catalog language receives localized command text instead of the
// canonical description.
func TestCommandMetadataCoversCatalogLanguages(t *testing.T) {
	for _, command := range Commands() {
		if command.Type != discordgo.ChatApplicationCommand {
			continue
		}
		if command.DescriptionLocalizations == nil {
			t.Fatalf("command %s has no localized description", command.Name)
		}
		for lang, locales := range discordLocalesByLanguage {
			if lang == canonicalUILanguage {
				continue
			}
			for _, locale := range locales {
				if (*command.DescriptionLocalizations)[locale] == "" {
					t.Errorf("command %s has no %s description", command.Name, locale)
				}
			}
		}
	}
}

// TestCommandMetadataFitsDiscordLimits verifies that every registered
// description and choice label is non-empty and within the 100 character limit
// Discord enforces for each locale.
func TestCommandMetadataFitsDiscordLimits(t *testing.T) {
	check := func(where, language, text string) {
		if text == "" {
			t.Errorf("%s in %s is empty", where, language)
			return
		}
		if n := utf8.RuneCountInString(text); n > 100 {
			t.Errorf("%s in %s is %d characters, want at most 100", where, language, n)
		}
	}
	var walk func(where string, option *discordgo.ApplicationCommandOption)
	walk = func(where string, option *discordgo.ApplicationCommandOption) {
		check(where, canonicalUILanguage, option.Description)
		for locale, text := range option.DescriptionLocalizations {
			check(where, string(locale), text)
		}
		for _, choice := range option.Choices {
			check(where+" choice", canonicalUILanguage, choice.Name)
			for locale, text := range choice.NameLocalizations {
				check(where+" choice", string(locale), text)
			}
		}
		for _, child := range option.Options {
			walk(where+" "+child.Name, child)
		}
	}
	for _, command := range Commands() {
		if command.Type != discordgo.ChatApplicationCommand {
			continue
		}
		check(command.Name, canonicalUILanguage, command.Description)
		if command.DescriptionLocalizations != nil {
			for locale, text := range *command.DescriptionLocalizations {
				check(command.Name, string(locale), text)
			}
		}
		for _, option := range command.Options {
			walk(command.Name+" "+option.Name, option)
		}
	}
}
