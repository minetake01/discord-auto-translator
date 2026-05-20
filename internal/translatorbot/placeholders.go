package translatorbot

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"
)

var protectedPattern = regexp.MustCompile("https?://[^\\s<>()]+|<@!?\\d+>|<#\\d+>|<@&\\d+>|```[\\s\\S]*?```|`[^`]*`|\\|\\|[\\s\\S]*?\\|\\|")

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
		return "__DAT_KEEP_" + strings.NewReplacer("-", "_").Replace(randomFallback()) + "__"
	}
	return "__DAT_KEEP_" + strings.ToUpper(hex.EncodeToString(b[:])) + "__"
}

func randomFallback() string { return "fallback" }
