package translatorbot

import (
	"context"
	"fmt"
	"regexp"
)

var discordChannelURLPattern = regexp.MustCompile(`https?://(?:(?:www|ptb|canary)\.)?discord(?:app)?\.com/channels/([^/\s<>()]+)/([^/\s<>()]+)(?:/([^/\s<>()]+))?`)
var discordChannelMentionPattern = regexp.MustCompile(`<#([^>]+)>`)

func MessageJumpURL(guildID, channelID, messageID string) string {
	return fmt.Sprintf("https://discord.com/channels/%s/%s/%s", guildID, channelID, messageID)
}

func discordChannelURL(guildID, channelID, messageID string) string {
	if messageID == "" {
		return fmt.Sprintf("https://discord.com/channels/%s/%s", guildID, channelID)
	}
	return MessageJumpURL(guildID, channelID, messageID)
}

func ReplaceDiscordRefs(ctx context.Context, store *Store, guildID, text, targetLanguage string) string {
	resolver := &discordLinkResolver{
		store:          store,
		guildID:        guildID,
		targetLanguage: targetLanguage,
	}
	text = discordChannelURLPattern.ReplaceAllStringFunc(text, func(match string) string {
		linkGuild, channelID, messageID, ok := parseDiscordChannelURL(match)
		if !ok || linkGuild != guildID {
			return match
		}
		newChannelID, newMessageID, resolved, err := resolver.resolve(ctx, channelID, messageID)
		if err != nil || !resolved {
			return match
		}
		return discordChannelURL(guildID, newChannelID, newMessageID)
	})
	text = discordChannelMentionPattern.ReplaceAllStringFunc(text, func(match string) string {
		submatch := discordChannelMentionPattern.FindStringSubmatch(match)
		if len(submatch) < 2 {
			return match
		}
		newChannelID, _, resolved, err := resolver.resolve(ctx, submatch[1], "")
		if err != nil || !resolved || newChannelID == submatch[1] {
			return match
		}
		return "<#" + newChannelID + ">"
	})
	return text
}

func parseDiscordChannelURL(u string) (guildID, channelID, messageID string, ok bool) {
	m := discordChannelURLPattern.FindStringSubmatch(u)
	if m == nil {
		return "", "", "", false
	}
	return m[1], m[2], m[3], true
}

type discordLinkResolver struct {
	store          *Store
	guildID        string
	targetLanguage string
}

func (r *discordLinkResolver) resolve(ctx context.Context, channelID, messageID string) (newChannelID, newMessageID string, ok bool, err error) {
	newChannelID, newMessageID, ok, err = r.resolveRegisteredChannel(ctx, channelID, messageID)
	if err != nil || ok {
		return newChannelID, newMessageID, ok, err
	}
	return r.resolveThreadChannel(ctx, channelID, messageID)
}

func (r *discordLinkResolver) resolveRegisteredChannel(ctx context.Context, channelID, messageID string) (string, string, bool, error) {
	groups, err := r.store.ChannelsByChannel(ctx, r.guildID, channelID)
	if err != nil {
		return "", "", false, err
	}
	for _, group := range groups {
		targets, err := r.store.ChannelsInGroup(ctx, r.guildID, group.GroupID)
		if err != nil {
			return "", "", false, err
		}
		target := findChannelByLanguage(targets, r.targetLanguage)
		if target == nil {
			continue
		}
		if target.ChannelID == channelID && messageID == "" {
			return channelID, "", true, nil
		}
		if messageID == "" {
			return target.ChannelID, "", true, nil
		}
		targetMessageID, found, err := resolveMessageTarget(ctx, r.store, channelID, messageID, target.ChannelID)
		if err != nil {
			return "", "", false, err
		}
		if !found {
			return "", "", false, nil
		}
		return target.ChannelID, targetMessageID, true, nil
	}
	return "", "", false, nil
}

func (r *discordLinkResolver) resolveThreadChannel(ctx context.Context, threadID, messageID string) (string, string, bool, error) {
	links, err := r.store.ThreadTargets(ctx, threadID)
	if err != nil {
		return "", "", false, err
	}
	for _, link := range links {
		peerThreadID, matched := threadPeerForLanguage(link, threadID, r.guildID, r.targetLanguage, r.store, ctx)
		if !matched {
			continue
		}
		if peerThreadID == threadID && messageID == "" {
			return threadID, "", true, nil
		}
		if messageID == "" {
			return peerThreadID, "", true, nil
		}
		targetMessageID, found, err := resolveMessageTarget(ctx, r.store, threadID, messageID, peerThreadID)
		if err != nil {
			return "", "", false, err
		}
		if !found {
			return "", "", false, nil
		}
		return peerThreadID, targetMessageID, true, nil
	}
	return "", "", false, nil
}

func threadPeerForLanguage(link ThreadLink, currentThreadID, guildID, targetLanguage string, store *Store, ctx context.Context) (peerThreadID string, ok bool) {
	if link.SourceThreadID == currentThreadID {
		if link.TargetLanguage == targetLanguage {
			return link.TargetThreadID, true
		}
		return "", false
	}
	if link.TargetThreadID == currentThreadID {
		targets, err := store.ChannelsInGroup(ctx, guildID, link.GroupID)
		if err != nil {
			return "", false
		}
		sourceLang := languageForChannel(targets, link.SourceChannelID)
		if sourceLang == targetLanguage {
			return link.SourceThreadID, true
		}
	}
	return "", false
}

func findChannelByLanguage(channels []GroupChannel, lang string) *GroupChannel {
	for i := range channels {
		if channels[i].Language == lang {
			return &channels[i]
		}
	}
	return nil
}

func resolveMessageTarget(ctx context.Context, store *Store, channelID, messageID, targetChannelID string) (string, bool, error) {
	links, err := store.MessageTargets(ctx, channelID, messageID)
	if err != nil {
		return "", false, err
	}
	for _, link := range links {
		if link.TargetChannelID == targetChannelID {
			return link.TargetMessageID, true, nil
		}
	}
	if len(links) > 0 && links[0].SourceChannelID == targetChannelID {
		return links[0].SourceMessageID, true, nil
	}

	peers, err := store.MessagePeers(ctx, channelID, messageID)
	if err != nil {
		return "", false, err
	}
	for _, peer := range peers {
		if peer.TargetChannelID == targetChannelID {
			return peer.TargetMessageID, true, nil
		}
	}
	return "", false, nil
}
