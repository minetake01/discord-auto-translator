package translatorbot

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
)

const StylePresetDefault = "default"

const styleCustomMaxRunes = 200

var (
	ErrStyleCustomEmpty   = errors.New("custom style instruction is empty")
	ErrStyleCustomTooLong = errors.New("custom style instruction is too long")
)

var stylePresetInstructions = map[string]string{
	"formal":   `Use formal, polite, and respectful language. In languages with politeness levels (e.g. Japanese keigo, French vous), use the polite register consistently. Avoid slang, contractions, and casual interjections.`,
	"casual":   `Use relaxed, conversational, everyday language, like friends chatting. Prefer contractions and common colloquial expressions. Avoid stiff or bookish phrasing.`,
	"business": `Use professional business language suitable for workplace communication. Be concise, clear, and courteous. Prefer standard business terminology and avoid slang or overly emotional expressions.`,
	"literal":  `Translate as literally as possible while staying grammatical. Preserve the original sentence structure, word choice, and nuances even if the result sounds slightly unnatural. Do not paraphrase, embellish, or omit anything.`,
	"gaming":   `Use the casual voice of online gaming communities. Prefer gaming slang and abbreviations natural to the target language (e.g. gg, nerf, buff, grind) and keep hype or banter energetic. Keep game titles, character names, and technical game terms recognizable to players.`,
	"friendly": `Use warm, friendly, and approachable language, like talking to a good friend. Keep a positive and welcoming tone, and soften harsh-sounding phrasing where the meaning allows.`,
	"netslang": `Use the voice of anonymous internet forums and message boards in the target language (e.g. 2ch/5ch-style for Japanese, imageboard/Reddit-style for English). Prefer net slang, abbreviations, and meme-ish phrasing natural to that community. Short, punchy, low-formality sentences are fine; dropping subjects and particles is fine where natural.`,
	"tweet":    `Write like a casual social media post (a tweet). Keep it short, punchy, and colloquial, with the loose grammar and rhythm typical of the target language's social media. Do not add hashtags, emoji, or mentions that are not in the source text.`,
}

func StylePresetChoices() []*discordgo.ApplicationCommandOptionChoice {
	return []*discordgo.ApplicationCommandOptionChoice{
		{Name: "default (スタイルなし)", Value: StylePresetDefault},
		{Name: "formal (丁寧・格式)", Value: "formal"},
		{Name: "casual (カジュアル)", Value: "casual"},
		{Name: "business (ビジネス)", Value: "business"},
		{Name: "literal (直訳)", Value: "literal"},
		{Name: "gaming (ゲーム)", Value: "gaming"},
		{Name: "friendly (親しみやすい)", Value: "friendly"},
		{Name: "netslang (ネットスレ風)", Value: "netslang"},
		{Name: "tweet (つぶやき風)", Value: "tweet"},
	}
}

func IsValidStylePreset(preset string) bool {
	preset = strings.TrimSpace(preset)
	if preset == "" || preset == StylePresetDefault {
		return true
	}
	_, ok := stylePresetInstructions[preset]
	return ok
}

func ValidateStyleCustom(custom string) error {
	custom = strings.TrimSpace(custom)
	if custom == "" {
		return ErrStyleCustomEmpty
	}
	if utf8.RuneCountInString(custom) > styleCustomMaxRunes {
		return ErrStyleCustomTooLong
	}
	return nil
}

func ResolveStyleInstructions(preset, custom string) string {
	custom = strings.TrimSpace(custom)
	if custom != "" {
		return custom
	}
	preset = strings.TrimSpace(preset)
	if preset == "" || preset == StylePresetDefault {
		return ""
	}
	return stylePresetInstructions[preset]
}

func FormatGroupStyle(g TranslationGroup) string {
	if custom := strings.TrimSpace(g.StyleCustom); custom != "" {
		return formatCustomStylePreview(custom)
	}
	if preset := strings.TrimSpace(g.StylePreset); preset != "" && preset != StylePresetDefault {
		return preset
	}
	return StylePresetDefault
}

func formatCustomStylePreview(custom string) string {
	const maxRunes = 40
	runes := []rune(custom)
	if len(runes) <= maxRunes {
		return "custom: " + custom
	}
	return "custom: " + string(runes[:maxRunes]) + "…"
}
