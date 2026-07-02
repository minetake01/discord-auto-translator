package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

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
	service.SetPublicBaseURL(cfg.PublicBaseURL)
	service.SetRateLimiter(translatorbot.NewTokenRateLimiter(cfg.GeminiRateLimitTokensPerMin))
	commands := translatorbot.NewCommandHandler(store, api)
	httpMux := http.NewServeMux()
	httpMux.Handle("/avatar", translatorbot.NewAvatarHandler(http.DefaultClient))
	httpServer := &http.Server{Addr: cfg.HTTPAddr, Handler: httpMux}
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("avatar http server: %v", err)
		}
	}()
	dg.AddHandler(func(s *discordgo.Session, g *discordgo.GuildCreate) {
		if g == nil || g.Unavailable || g.ID == "" {
			return
		}
		cmdID, err := translatorbot.RegisterGuildCommandsForGuild(s, s.State.User.ID, g.ID)
		if err != nil {
			log.Printf("register commands in new guild %s: %v", g.ID, err)
			return
		}
		if cmdID != "" {
			service.SetAddGlossaryCommandID(g.ID, cmdID)
		}
	})
	dg.AddHandler(commands.Handle)
	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if m.Author == nil {
			return
		}
		parentChannelID, threadName := threadContext(s, m.ChannelID)
		err := service.HandleMessageCreate(context.Background(), translatorbot.DiscordMessage{
			ID: m.ID, ChannelID: m.ChannelID, GuildID: m.GuildID, AuthorID: m.Author.ID,
			ParentChannelID: parentChannelID, ThreadName: threadName,
			AuthorDisplayName: authorDisplayName(m.Author, m.Member), AuthorAvatarURL: m.Author.AvatarURL("128"), Content: m.Content,
			Attachments:         attachmentsFromDiscord(m.Attachments),
			ReferencedMessageID: referencedMessageID(m.MessageReference), MentionAuthor: mentionsReferencedAuthor(m.Message, m.ReferencedMessage),
			WebhookID: m.WebhookID, Bot: m.Author.Bot, ThreadSystemMessage: isThreadSystemMessage(m.Type), ThreadStarterMessage: isThreadStarterMessage(m.Type),
		})
		if err != nil {
			log.Printf("message create sync: %v", err)
		}
	})
	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageUpdate) {
		if m.Author == nil || isThreadSystemMessage(m.Type) {
			return
		}
		if m.Author.Bot || m.WebhookID != "" {
			return
		}
		ctx := context.Background()
		if pinStateChanged(m) {
			if err := service.SyncPin(ctx, m.ChannelID, m.ID, m.Pinned); err != nil {
				log.Printf("pin sync: %v", err)
			}
		}
		if strings.TrimSpace(m.Content) == "" {
			return
		}
		err := service.HandleMessageUpdate(ctx, translatorbot.DiscordMessage{
			ID: m.ID, ChannelID: m.ChannelID, GuildID: m.GuildID, AuthorID: m.Author.ID,
			AuthorDisplayName: authorDisplayName(m.Author, m.Member), AuthorAvatarURL: m.Author.AvatarURL("128"), Content: m.Content,
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
		if err := service.SyncThreadCreateFromGateway(context.Background(), t.GuildID, t.ParentID, t.ID, t.Name); err != nil {
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
	addGlossaryCommandIDs := translatorbot.RegisterGuildCommands(dg, dg.State.User.ID)
	service.SetAddGlossaryCommandIDs(addGlossaryCommandIDs)
	log.Println("Discord Gemini Auto Translator is running")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("avatar http server shutdown: %v", err)
	}
	if err := dg.Close(); err != nil {
		log.Printf("discord session close: %v", err)
	}
	if err := store.Close(); err != nil {
		log.Printf("store close: %v", err)
	}
}

func attachmentsFromDiscord(attachments []*discordgo.MessageAttachment) []translatorbot.DiscordAttachment {
	out := make([]translatorbot.DiscordAttachment, 0, len(attachments))
	for _, attachment := range attachments {
		if attachment == nil {
			continue
		}
		out = append(out, translatorbot.DiscordAttachment{
			URL:         attachment.URL,
			Filename:    attachment.Filename,
			ContentType: attachment.ContentType,
		})
	}
	return out
}

func authorDisplayName(author *discordgo.User, member *discordgo.Member) string {
	if member != nil {
		if member.User != nil {
			if name := strings.TrimSpace(member.DisplayName()); name != "" {
				return name
			}
		}
		if name := strings.TrimSpace(member.Nick); name != "" {
			return name
		}
	}
	if author != nil {
		if name := strings.TrimSpace(author.DisplayName()); name != "" {
			return name
		}
	}
	return ""
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

func threadContext(s *discordgo.Session, channelID string) (string, string) {
	ch, err := s.State.Channel(channelID)
	if err != nil || ch == nil {
		ch, err = s.Channel(channelID)
		if err != nil || ch == nil {
			return "", ""
		}
	}
	if !ch.IsThread() {
		return "", ""
	}
	return ch.ParentID, ch.Name
}

func isThreadSystemMessage(t discordgo.MessageType) bool {
	return t == discordgo.MessageTypeThreadCreated || t == discordgo.MessageTypeThreadStarterMessage
}

func isThreadStarterMessage(t discordgo.MessageType) bool {
	return t == discordgo.MessageTypeThreadStarterMessage
}

func pinStateChanged(m *discordgo.MessageUpdate) bool {
	if m == nil || m.BeforeUpdate == nil {
		return false
	}
	return m.Pinned != m.BeforeUpdate.Pinned
}
