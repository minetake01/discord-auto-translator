package translatorbot

import "strings"

var languageChoices = []struct {
	Code string
	Name string
}{
	{"ja", "Japanese"}, {"en", "English"}, {"zh-CN", "Chinese Simplified"},
	{"zh-TW", "Chinese Traditional"}, {"ko", "Korean"}, {"fr", "French"},
	{"de", "German"}, {"es", "Spanish"}, {"pt-BR", "Portuguese Brazil"},
	{"it", "Italian"}, {"id", "Indonesian"}, {"th", "Thai"}, {"vi", "Vietnamese"},
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
