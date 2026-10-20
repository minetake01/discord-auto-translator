package translatorbot

import (
	"regexp"
	"strings"
)

// languageChoices lists the languages the autocomplete offers. Aliases are only
// matched against what the user types; the codes are what gets displayed and
// stored, so a user never sees this table's English or native names.
var languageChoices = []struct {
	Code    string
	Aliases []string
}{
	{"ja", []string{"Japanese", "日本語"}},
	{"en", []string{"English", "英語"}},
	{"zh-CN", []string{"Chinese Simplified", "简体中文"}},
	{"zh-TW", []string{"Chinese Traditional", "繁體中文"}},
	{"ko", []string{"Korean", "한국어"}},
	{"fr", []string{"French", "Français"}},
	{"de", []string{"German", "Deutsch"}},
	{"es", []string{"Spanish", "Español"}},
	{"pt-BR", []string{"Portuguese Brazil", "Português (Brasil)"}},
	{"it", []string{"Italian", "Italiano"}},
	{"id", []string{"Indonesian", "Bahasa Indonesia"}},
	{"th", []string{"Thai", "ไทย"}},
	{"vi", []string{"Vietnamese", "Tiếng Việt"}},
}

var languageCodePattern = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8}){0,3}$`)

func IsValidLanguageCode(language string) bool {
	language = normalizeLanguage(language)
	if language == "" || len(language) > 35 {
		return false
	}
	for _, l := range languageChoices {
		if strings.EqualFold(language, l.Code) {
			return true
		}
	}
	return languageCodePattern.MatchString(language)
}

func LanguageSuggestions(query string, limit int) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	var out []string
	for _, l := range languageChoices {
		haystack := strings.ToLower(l.Code + " " + strings.Join(l.Aliases, " "))
		if query != "" && !strings.Contains(haystack, query) {
			continue
		}
		out = append(out, l.Code)
		if len(out) >= limit {
			break
		}
	}
	return out
}
