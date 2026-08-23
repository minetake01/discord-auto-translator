package main

import (
	"context"
	"encoding/json"
	"log"
	"strings"

	"discord-auto-translator/internal/translatorbot"
	"github.com/bwmarrin/discordgo"
)

func handleGatewayMessageCreate(s *discordgo.Session, service *translatorbot.Service, m *discordgo.MessageCreate, raw json.RawMessage) {
	if m.Author == nil {
		return
	}
	parentChannelID, threadName := threadContext(s, m.ChannelID)
	descriptions, snapshotDescriptions := attachmentDescriptionsFromRaw(raw)
	forwarded, err := forwardedMessageFields(m.MessageReference, m.MessageSnapshots, snapshotDescriptions)
	if err != nil {
		log.Printf("message create forward payload: %v", err)
		return
	}
	refID, refChannelID, refContent := referencedMessageFields(m.MessageReference, m.ReferencedMessage)
	mentionedUsers, mentionedChannels, mentionedRoles := mentionNameMaps(s, m.GuildID, m.Message)
	err = service.HandleMessageCreate(context.Background(), translatorbot.DiscordMessage{
		ID: m.ID, ChannelID: m.ChannelID, GuildID: m.GuildID, AuthorID: m.Author.ID,
		ParentChannelID: parentChannelID, ThreadName: threadName,
		AuthorDisplayName: authorDisplayName(m.Author, m.Member), AuthorAvatarURL: m.Author.AvatarURL("128"), AuthorRoleColor: memberRoleColor(s, m.GuildID, m.Member), Content: m.Content,
		Attachments:                attachmentsFromDiscord(m.Attachments, descriptions),
		Stickers:                   stickersFromDiscord(m.StickerItems),
		ReferencedMessageID:        refID,
		ReferencedMessageChannelID: refChannelID,
		ReferencedMessageContent:   refContent,
		ForwardedMessage:           forwarded,
		Poll:                       pollFromDiscord(m.Poll),
		PollResult:                 pollResultFromDiscord(m.Message),
		TTS:                        m.TTS,
		WebhookID:                  m.WebhookID, Bot: m.Author.Bot, ThreadSystemMessage: isThreadSystemMessage(m.Type), ThreadStarterMessage: isThreadStarterMessage(m.Type),
		MentionedUsers:    mentionedUsers,
		MentionedChannels: mentionedChannels,
		MentionedRoles:    mentionedRoles,
	})
	if err != nil {
		log.Printf("message create sync: %v", err)
	}
}

func handleGatewayMessageUpdate(s *discordgo.Session, service *translatorbot.Service, m *discordgo.MessageUpdate, raw json.RawMessage) {
	if m.Author == nil || isThreadSystemMessage(m.Type) {
		return
	}
	ctx := context.Background()
	if err := service.HandleMessagePinUpdate(ctx, m.ChannelID, m.ID, m.Pinned); err != nil {
		log.Printf("pin sync: %v", err)
	}
	if strings.TrimSpace(m.Content) == "" {
		return
	}
	parentChannelID, threadName := threadContext(s, m.ChannelID)
	descriptions, _ := attachmentDescriptionsFromRaw(raw)
	mentionedUsers, mentionedChannels, mentionedRoles := mentionNameMaps(s, m.GuildID, m.Message)
	err := service.HandleMessageUpdate(ctx, translatorbot.DiscordMessage{
		ID: m.ID, ChannelID: m.ChannelID, GuildID: m.GuildID, AuthorID: m.Author.ID,
		ParentChannelID: parentChannelID, ThreadName: threadName,
		AuthorDisplayName: authorDisplayName(m.Author, m.Member), AuthorAvatarURL: m.Author.AvatarURL("128"), AuthorRoleColor: memberRoleColor(s, m.GuildID, m.Member), Content: m.Content,
		Attachments: attachmentsFromDiscord(m.Attachments, descriptions), Stickers: stickersFromDiscord(m.StickerItems),
		WebhookID: m.WebhookID, Bot: m.Author.Bot, Edited: true,
		MentionedUsers: mentionedUsers, MentionedChannels: mentionedChannels, MentionedRoles: mentionedRoles,
	})
	if err != nil {
		log.Printf("message update sync: %v", err)
	}
}
