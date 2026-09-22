package translatorbot

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

var protectedPattern = regexp.MustCompile("<https?://[^\\s<>()]+>|https?://[^\\s<>()]+|<@!?\\d+>|<#\\d+>|<@&\\d+>|<a?:[A-Za-z0-9_]+:\\d+>|</[A-Za-z0-9_\\- ]+:\\d+>|<t:\\d+(?::[tTdDfFR])?>|```[\\s\\S]*?```|`[^`]*`")

type NameMaps struct {
	Users    map[string]string // userID → display name
	Channels map[string]string // channelID → channel name (source)
	Roles    map[string]string // roleID → role name
	Sites    map[string]string // rawURL → page title
}

type SiteContextEntry struct {
	ID             string // matches N in [SITE:N]
	Title          string // page title for model background only
	Description    string
	ImageURL       string
	HasVisionImage bool // set after a linked-page image is actually loaded for vision
}

type Protector struct {
	names            NameMaps
	siteDescriptions map[string]string
	siteImages       map[string]string
	items            map[string]string
	counts           map[string]int
	sites            []SiteContextEntry
	siteSeq          int
}

func NewProtector(names NameMaps) *Protector {
	return &Protector{
		names:  names,
		items:  map[string]string{},
		counts: map[string]int{},
	}
}

func (p *Protector) SetSiteDescriptions(descriptions map[string]string) {
	p.siteDescriptions = descriptions
}

func (p *Protector) SetSiteImages(images map[string]string) {
	p.siteImages = images
}

func (p *Protector) SiteContext() []SiteContextEntry {
	if len(p.sites) == 0 {
		return nil
	}
	out := make([]SiteContextEntry, len(p.sites))
	copy(out, p.sites)
	return out
}

func (p *Protector) Protect(text string) string {
	return protectedPattern.ReplaceAllStringFunc(text, func(match string) string {
		key := p.tokenFor(match)
		p.items[key] = match
		return key
	})
}

func (p *Protector) Restore(text string) string {
	keys := make([]string, 0, len(p.items))
	for key := range p.items {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return len(keys[i]) > len(keys[j])
	})
	for _, key := range keys {
		value := p.items[key]
		if bareHTTPURL(value) {
			text = replaceBareURL(text, key, value)
			continue
		}
		text = strings.ReplaceAll(text, key, value)
	}
	return text
}

func bareHTTPURL(value string) bool {
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}

func replaceBareURL(text, key, url string) string {
	var b strings.Builder
	for {
		i := strings.Index(text, key)
		if i < 0 {
			b.WriteString(text)
			return b.String()
		}
		b.WriteString(text[:i])
		b.WriteString(url)
		rest := text[i+len(key):]
		if bareURLNeedsFollowingSpace(rest) {
			b.WriteByte(' ')
		}
		text = rest
	}
}

// Discord's autolink absorbs the following run of non-whitespace into a bare
// URL. A trailing run of <.,:;"')] is left outside the link, so a separator is
// needed only when something else follows.
func bareURLNeedsFollowingSpace(rest string) bool {
	for _, r := range rest {
		if unicode.IsSpace(r) {
			return false
		}
		if !discordAutolinkTrailingPunct(r) {
			return true
		}
	}
	return false
}

func discordAutolinkTrailingPunct(r rune) bool {
	switch r {
	case '<', '.', ',', ':', ';', '"', '\'', ')', ']':
		return true
	default:
		return false
	}
}

func (p *Protector) tokenFor(match string) string {
	switch {
	case strings.HasPrefix(match, "<t:"):
		return p.nextToken("TIME", "")

	case strings.HasPrefix(match, "</"):
		rest := match[2 : len(match)-1]
		if name, _, ok := strings.Cut(rest, ":"); ok {
			return p.nextToken("CMD", sanitizeLabel(name))
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
		p.siteSeq++
		id := strconv.Itoa(p.siteSeq)
		title := ""
		if p.names.Sites != nil {
			title = p.names.Sites[rawURL]
		}
		p.recordSiteContext(rawURL, id, title)
		return "[SITE:" + id + "]"

	default:
		return p.nextToken("CODE", "")
	}
}

func (p *Protector) recordSiteContext(rawURL, id, title string) {
	if strings.TrimSpace(title) == "" {
		return
	}
	desc := ""
	if p.siteDescriptions != nil {
		desc = p.siteDescriptions[rawURL]
	}
	imageURL := ""
	if p.siteImages != nil {
		imageURL = p.siteImages[rawURL]
	}
	p.sites = append(p.sites, SiteContextEntry{ID: id, Title: title, Description: desc, ImageURL: imageURL})
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
	name, _, ok := strings.Cut(inner, ":")
	if !ok {
		return ""
	}
	return name
}

func sanitizeLabel(s string) string {
	s = strings.ReplaceAll(s, ":", "_")
	s = strings.ReplaceAll(s, "]", "_")
	return strings.TrimSpace(s)
}

func hasTranslatableText(text string) bool {
	remaining := protectedPattern.ReplaceAllString(text, "")
	remaining = stripUnicodeEmojiSequences(remaining)
	for _, r := range remaining {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func needsTranslation(content string) bool {
	return hasTranslatableText(content)
}
