package translatorbot

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
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
		text = strings.ReplaceAll(text, key, p.items[key])
	}
	return text
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
	return strings.TrimSpace(protectedPattern.ReplaceAllString(text, "")) != ""
}

func needsTranslation(content string, imageAttachments []DiscordAttachment) bool {
	if hasTranslatableText(content) {
		return true
	}
	for _, attachment := range imageAttachments {
		if hasTranslatableText(attachment.Description) {
			return true
		}
	}
	return false
}
