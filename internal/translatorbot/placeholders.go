package translatorbot

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"
)

var protectedPattern = regexp.MustCompile("<https?://[^\\s<>()]+>|https?://[^\\s<>()]+|<@!?\\d+>|<#\\d+>|<@&\\d+>|<a?:[A-Za-z0-9_]+:\\d+>|```[\\s\\S]*?```|`[^`]*`")

func hasTranslatableText(text string) bool {
	return strings.TrimSpace(protectedPattern.ReplaceAllString(text, "")) != ""
}

type Protector struct {
	items map[string]string
}

func NewProtector() *Protector {
	return &Protector{items: map[string]string{}}
}

func (p *Protector) Protect(text string) string {
	return protectedPattern.ReplaceAllStringFunc(text, func(match string) string {
		key := p.token()
		p.items[key] = match
		return key
	})
}

func (p *Protector) Restore(text string) string {
	for key, value := range p.items {
		text = strings.ReplaceAll(text, key, value)
	}
	return text
}

func (p *Protector) token() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "__DAT_KEEP_FALLBACK__"
	}
	return "__DAT_KEEP_" + strings.ToUpper(hex.EncodeToString(b[:])) + "__"
}
