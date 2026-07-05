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
	"formal":   `Default to a formal, polite register. In languages with politeness levels (e.g. Japanese keigo, French vous), prefer the polite form.`,
	"casual":   `Default to relaxed, everyday conversational language, like friends chatting.`,
	"business": `Default to professional workplace language: concise, clear, and courteous.`,
	"literal":  `When several renderings are possible, choose the most literal one: keep the original sentence structure and word choice even if slightly unnatural, and do not paraphrase, embellish, or omit.`,
	"gaming":   `Default to the casual voice of online gaming communities, with gaming slang and abbreviations natural to the target language (e.g. gg, nerf, buff). Keep game titles, character names, and technical game terms recognizable to players.`,
	"friendly": `Default to warm, friendly, and approachable language, like talking to a good friend.`,
	"netslang": `Default to the voice of anonymous message boards in the target language (e.g. 2ch/5ch-style for Japanese, imageboard/Reddit-style for English): net slang, abbreviations, and short low-formality sentences; dropping subjects and particles is fine where natural.`,
	"tweet":    `Default to casual social media phrasing (a tweet): short, punchy, and colloquial. Do not add hashtags, emoji, or mentions that are not in the source text.`,
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
