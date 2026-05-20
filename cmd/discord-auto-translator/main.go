package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"discord-auto-translator/internal/translatorbot"
	"github.com/bwmarrin/discordgo"
)

func main() {
	ctx := context.Background()
	cfg, err := translatorbot.LoadConfig(".env")
	if err != nil {
		log.Fatal(err)
	}
	store, err := translatorbot.OpenStore(cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	dg, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		log.Fatal(err)
	}
	dg.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMessages | discordgo.IntentsGuildMessageReactions | discordgo.IntentsMessageContent
	api := translatorbot.NewDiscordGoAPI(dg)
	translator, err := translatorbot.NewGeminiTranslator(ctx, cfg.GeminiAPIKey)
	if err != nil {
		log.Fatal(err)
	}
	service := translatorbot.NewService(store, api, translator)
	commands := translatorbot.NewCommandHandler(store, api)
	dg.AddHandler(commands.Handle)
	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if m.Author == nil {
			return
		}
		name := m.Author.Username
		if m.Member != nil && m.Member.Nick != "" {
			name = m.Member.Nick
		}
		err := service.HandleMessageCreate(context.Background(), translatorbot.DiscordMessage{
			ID: m.ID, ChannelID: m.ChannelID, GuildID: m.GuildID, AuthorID: m.Author.ID,
			AuthorDisplayName: name, AuthorAvatarURL: m.Author.AvatarURL("128"), Content: m.Content,
			ReferencedMessageID: referencedMessageID(m.MessageReference), MentionAuthor: mentionsReferencedAuthor(m.Message, m.ReferencedMessage),
			WebhookID: m.WebhookID, Bot: m.Author.Bot, ThreadSystemMessage: isThreadSystemMessage(m.Type), ThreadStarterMessage: isThreadStarterMessage(m.Type),
		})
		if err != nil {
			log.Printf("message create sync: %v", err)
		}
	})
	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageUpdate) {
		if m.Author == nil || m.Content == "" || isThreadSystemMessage(m.Type) {
			return
		}
		err := service.HandleMessageUpdate(context.Background(), translatorbot.DiscordMessage{
			ID: m.ID, ChannelID: m.ChannelID, GuildID: m.GuildID, AuthorID: m.Author.ID,
			AuthorDisplayName: m.Author.Username, AuthorAvatarURL: m.Author.AvatarURL("128"), Content: m.Content,
			WebhookID: m.WebhookID, Bot: m.Author.Bot, Edited: true,
		})
		if err != nil {
			log.Printf("message update sync: %v", err)
		}
	})
	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageDelete) {
		if err := service.HandleMessageDelete(context.Background(), m.GuildID, m.ChannelID, m.ID); err != nil {
			log.Printf("message delete sync: %v", err)
		}
	})
	dg.AddHandler(func(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
		if r.UserID == s.State.User.ID {
			return
		}
		if err := service.SyncReaction(context.Background(), r.GuildID, r.ChannelID, r.MessageID, r.Emoji.APIName(), s.State.User.ID, true); err != nil {
			log.Printf("reaction add sync: %v", err)
		}
	})
	dg.AddHandler(func(s *discordgo.Session, r *discordgo.MessageReactionRemove) {
		if r.UserID == s.State.User.ID {
			return
		}
		if err := service.SyncReaction(context.Background(), r.GuildID, r.ChannelID, r.MessageID, r.Emoji.APIName(), s.State.User.ID, false); err != nil {
			log.Printf("reaction remove sync: %v", err)
		}
	})
	dg.AddHandler(func(s *discordgo.Session, t *discordgo.ThreadCreate) {
		if t.Channel == nil || !t.NewlyCreated || t.OwnerID == s.State.User.ID || t.ParentID == "" {
			return
		}
		if err := service.SyncThreadCreate(context.Background(), t.GuildID, t.ParentID, t.ID, t.Name); err != nil {
			log.Printf("thread create sync: %v", err)
		}
	})
	dg.AddHandler(func(s *discordgo.Session, t *discordgo.ThreadUpdate) {
		if t.Channel == nil || t.Name == "" {
			return
		}
		if t.BeforeUpdate != nil && t.BeforeUpdate.Name == t.Name {
			return
		}
		if err := service.SyncThreadUpdate(context.Background(), t.GuildID, t.ID, t.Name); err != nil {
			log.Printf("thread update sync: %v", err)
		}
	})
	dg.AddHandler(func(s *discordgo.Session, t *discordgo.ThreadDelete) {
		if t.Channel == nil {
			return
		}
		if err := service.SyncThreadDelete(context.Background(), t.ID); err != nil {
			log.Printf("thread delete sync: %v", err)
		}
	})
	if err := dg.Open(); err != nil {
		log.Fatal(err)
	}
	defer dg.Close()
	translatorbot.RegisterGuildCommands(dg, dg.State.User.ID)
	log.Println("Discord Gemini Auto Translator is running")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
}

func referencedMessageID(ref *discordgo.MessageReference) string {
	if ref == nil {
		return ""
	}
	return ref.MessageID
}

func mentionsReferencedAuthor(m *discordgo.Message, referenced *discordgo.Message) bool {
	if m == nil || referenced == nil || referenced.Author == nil {
		return false
	}
	for _, mention := range m.Mentions {
		if mention.ID == referenced.Author.ID {
			return true
		}
	}
	return false
}

func isThreadSystemMessage(t discordgo.MessageType) bool {
	return t == discordgo.MessageTypeThreadCreated || t == discordgo.MessageTypeThreadStarterMessage
}

func isThreadStarterMessage(t discordgo.MessageType) bool {
	return t == discordgo.MessageTypeThreadStarterMessage
}
