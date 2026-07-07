package translatorbot

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var protectedPattern = regexp.MustCompile("<https?://[^\\s<>()]+>|https?://[^\\s<>()]+|<@!?\\d+>|<#\\d+>|<@&\\d+>|<a?:[A-Za-z0-9_]+:\\d+>|</[A-Za-z0-9_\\- ]+:\\d+>|<t:\\d+(?::[tTdDfFR])?>|```[\\s\\S]*?```|`[^`]*`")

func hasTranslatableText(text string) bool {
	return strings.TrimSpace(protectedPattern.ReplaceAllString(text, "")) != ""
}

type NameMaps struct {
	Users    map[string]string // userID → display name
	Channels map[string]string // channelID → channel name (source)
	Roles    map[string]string // roleID → role name
}

type Protector struct {
	names  NameMaps
	items  map[string]string
	counts map[string]int
}

func NewProtector(names NameMaps) *Protector {
	return &Protector{
		names:  names,
		items:  map[string]string{},
		counts: map[string]int{},
	}
}

func (p *Protector) Protect(text string) string {
	return protectedPattern.ReplaceAllStringFunc(text, func(match string) string {
		key := p.tokenFor(match)
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

func (p *Protector) tokenFor(match string) string {
	switch {
	case strings.HasPrefix(match, "<t:"):
		return p.nextToken("TIME", "")

	case strings.HasPrefix(match, "</"):
		rest := match[2 : len(match)-1]
		if idx := strings.Index(rest, ":"); idx >= 0 {
			return p.nextToken("CMD", sanitizeLabel(rest[:idx]))
		}
		return p.nextToken("CMD", "")

	case strings.HasPrefix(match, "<@&"):
		id := match[3 : len(match)-1]
		return p.nextToken("ROLE", sanitizeLabel(p.names.Roles[id]))

	case strings.HasPrefix(match, "<@"):
		id := strings.TrimPrefix(match[2:len(match)-1], "!")
		return p.nextToken("USER", sanitizeLabel(p.names.Users[id]))

	case strings.HasPrefix(match, "<#"):
		id := match[2 : len(match)-1]
		return p.nextToken("CHANNEL", sanitizeLabel(p.names.Channels[id]))

	case strings.HasPrefix(match, "<a:"):
		if name := emojiName(match); name != "" {
			return p.nextToken("EMOJI", sanitizeLabel(name))
		}
		return p.nextToken("EMOJI", "")

	case strings.HasPrefix(match, "<:"):
		if name := emojiName(match); name != "" {
			return p.nextToken("EMOJI", sanitizeLabel(name))
		}
		return p.nextToken("EMOJI", "")

	case strings.HasPrefix(match, "http") || strings.HasPrefix(match, "<http"):
		rawURL := strings.Trim(match, "<>")
		if u, err := url.Parse(rawURL); err == nil && u.Host != "" {
			return p.nextToken("URL", sanitizeLabel(u.Host))
		}
		return p.nextToken("URL", "")

	default:
		return p.nextToken("CODE", "")
	}
}

func emojiName(match string) string {
	var inner string
	switch {
	case strings.HasPrefix(match, "<a:"):
		inner = match[3 : len(match)-1]
	case strings.HasPrefix(match, "<:"):
		inner = match[2 : len(match)-1]
	default:
		return ""
	}
	if idx := strings.Index(inner, ":"); idx >= 0 {
		return inner[:idx]
	}
	return ""
}

func (p *Protector) nextToken(kind, label string) string {
	label = sanitizeLabel(label)
	key := kind
	if label != "" {
		key = kind + ":" + label
	}
	p.counts[key]++
	n := p.counts[key]
	if n == 1 {
		return "[" + key + "]"
	}
	return "[" + key + ":" + strconv.Itoa(n) + "]"
}

func sanitizeLabel(s string) string {
	s = strings.ReplaceAll(s, ":", "_")
	s = strings.ReplaceAll(s, "]", "_")
	return strings.TrimSpace(s)
}
