package translatorbot

import (
	"regexp"
	"strings"
)

var languageChoices = []struct {
	Code string
	Name string
}{
	{"ja", "Japanese"}, {"en", "English"}, {"zh-CN", "Chinese Simplified"},
	{"zh-TW", "Chinese Traditional"}, {"ko", "Korean"}, {"fr", "French"},
	{"de", "German"}, {"es", "Spanish"}, {"pt-BR", "Portuguese Brazil"},
	{"it", "Italian"}, {"id", "Indonesian"}, {"th", "Thai"}, {"vi", "Vietnamese"},
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
		hay := strings.ToLower(l.Code + " " + l.Name)
		if query == "" || strings.Contains(hay, query) {
			out = append(out, l.Code)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

func FlagForLanguage(language string) string {
	switch strings.ToLower(language) {
	case "ja":
		return "🇯🇵"
	case "en":
		return "🇺🇸"
	case "zh-cn":
		return "🇨🇳"
	case "zh-tw":
		return "🇹🇼"
	case "ko":
		return "🇰🇷"
	case "fr":
		return "🇫🇷"
	case "de":
		return "🇩🇪"
	case "es":
		return "🇪🇸"
	case "pt-br":
		return "🇧🇷"
	default:
		return ""
	}
}
