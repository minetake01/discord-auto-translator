package main

import (
	"context"
	"encoding/json"
	"log"
	"strings"

	"discord-auto-translator/internal/translatorbot"
	"github.com/bwmarrin/discordgo"
)

func handleGatewayMessageCreate(api translatorbot.DiscordGoAPI, service *translatorbot.Service, m *discordgo.MessageCreate, raw json.RawMessage) {
	if m.Author == nil {
		return
	}
	message, err := api.MapGatewayMessage(m.Message, raw, false)
	if err != nil {
		log.Printf("message create payload: %v", err)
		return
	}
	if err := service.HandleMessageCreate(context.Background(), message); err != nil {
		log.Printf("message create sync: %v", err)
	}
}

func handleGatewayMessageUpdate(api translatorbot.DiscordGoAPI, service *translatorbot.Service, m *discordgo.MessageUpdate, raw json.RawMessage) {
	if m.Author == nil || m.Type == discordgo.MessageTypeThreadCreated || m.Type == discordgo.MessageTypeThreadStarterMessage {
		return
	}
	ctx := context.Background()
	if err := service.HandleMessagePinUpdate(ctx, m.ChannelID, m.ID, m.Pinned); err != nil {
		log.Printf("pin sync: %v", err)
	}
	if strings.TrimSpace(m.Content) == "" {
		return
	}
	message, err := api.MapGatewayMessage(m.Message, raw, true)
	if err != nil {
		log.Printf("message update payload: %v", err)
		return
	}
	if err := service.ScheduleMessageUpdate(ctx, message); err != nil {
		log.Printf("message update sync: %v", err)
	}
}
