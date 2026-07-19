package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
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
	startup, err := parseStartupOptions(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}
	cfg, err := translatorbot.LoadConfig(startup.envFile)
	if err != nil {
		log.Fatal(err)
	}
	translator, err := translatorbot.NewBedrockTranslator(context.Background(), cfg.AWSAccessKeyID, cfg.AWSSecretAccessKey, cfg.AWSBedrockRegion, cfg.AWSBedrockProjectID)
	if err != nil {
		log.Fatal(err)
	}
	if startup.bedrockPrewarm {
		if err := prewarmBedrock(context.Background(), translator); err != nil {
			log.Fatal(err)
		}
		log.Println("Amazon Bedrock Gemma model access and response contract are ready")
		return
	}
	dg, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		log.Fatal(err)
	}
	dg.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMessages | discordgo.IntentsGuildMessageReactions | discordgo.IntentsMessageContent
	api := translatorbot.NewDiscordGoAPI(dg)
	selfBotUserID, err := api.CurrentUserID()
	if err != nil {
		log.Fatalf("Discord startup configuration: %v", err)
	}
	store, err := translatorbot.OpenStore(cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	lifecycle := newGuildLifecycleHandler(store, dg.State)
	service := translatorbot.NewService(store, api, translator)
	service.SetSelfBotUserID(selfBotUserID)
	service.SetPublicBaseURL(cfg.PublicBaseURL)
	service.SetRateLimiter(translatorbot.NewTokenRateLimiter(cfg.TranslationRateLimitTokensPerMin))
	commands := translatorbot.NewCommandHandler(store, api)
	httpMux := http.NewServeMux()
	httpMux.Handle("/avatar", translatorbot.NewAvatarHandler(http.DefaultClient, translatorbot.NewRequestRateLimiter(cfg.AvatarRateLimitRequestsPerMin)))
	httpServer := &http.Server{Addr: cfg.HTTPAddr, Handler: httpMux}
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("avatar http server: %v", err)
		}
	}()
	dg.AddHandler(func(s *discordgo.Session, g *discordgo.GuildCreate) {
		register := func(guildID string) error {
			return translatorbot.RegisterGuildCommandsForGuild(s, s.State.User.ID, guildID)
		}
		if err := lifecycle.handleCreate(context.Background(), time.Now, register, g); err != nil {
			var persistenceErr *guildLifecyclePersistenceError
			if errors.As(err, &persistenceErr) {
				failGuildLifecycle("guild create", err, log.Fatalf)
				return
			}
			log.Printf("guild create commands: %v", err)
		}
	})
	dg.AddHandler(func(_ *discordgo.Session, g *discordgo.GuildDelete) {
		failGuildLifecycle("guild delete", lifecycle.handleDelete(context.Background(), time.Now, g), log.Fatalf)
	})
	dg.AddHandler(func(_ *discordgo.Session, ready *discordgo.Ready) {
		failGuildLifecycle("ready", lifecycle.handleReady(context.Background(), time.Now, ready), log.Fatalf)
	})
	dg.AddHandler(commands.Handle)
	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if m.Author == nil {
			return
		}
		parentChannelID, threadName := threadContext(s, m.ChannelID)
		forwarded, err := forwardedMessageFields(m.MessageReference, m.MessageSnapshots)
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
			Attachments:                attachmentsFromDiscord(m.Attachments),
			Stickers:                   stickersFromDiscord(m.StickerItems),
			ReferencedMessageID:        refID,
			ReferencedMessageChannelID: refChannelID,
			ReferencedMessageContent:   refContent,
			ForwardedMessage:           forwarded,
			TTS:                        m.TTS,
			WebhookID:                  m.WebhookID, Bot: m.Author.Bot, ThreadSystemMessage: isThreadSystemMessage(m.Type), ThreadStarterMessage: isThreadStarterMessage(m.Type),
			MentionedUsers:    mentionedUsers,
			MentionedChannels: mentionedChannels,
			MentionedRoles:    mentionedRoles,
		})
		if err != nil {
			log.Printf("message create sync: %v", err)
		}
	})
	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageUpdate) {
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
		mentionedUsers, mentionedChannels, mentionedRoles := mentionNameMaps(s, m.GuildID, m.Message)
		err := service.HandleMessageUpdate(ctx, translatorbot.DiscordMessage{
			ID: m.ID, ChannelID: m.ChannelID, GuildID: m.GuildID, AuthorID: m.Author.ID,
			ParentChannelID: parentChannelID, ThreadName: threadName,
			AuthorDisplayName: authorDisplayName(m.Author, m.Member), AuthorAvatarURL: m.Author.AvatarURL("128"), AuthorRoleColor: memberRoleColor(s, m.GuildID, m.Member), Content: m.Content,
			Attachments: attachmentsFromDiscord(m.Attachments), Stickers: stickersFromDiscord(m.StickerItems),
			WebhookID: m.WebhookID, Bot: m.Author.Bot, Edited: true,
			MentionedUsers: mentionedUsers, MentionedChannels: mentionedChannels, MentionedRoles: mentionedRoles,
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
		if err := service.SyncReaction(context.Background(), r.GuildID, r.ChannelID, r.MessageID, r.Emoji.APIName(), true); err != nil {
			log.Printf("reaction add sync: %v", err)
		}
	})
	dg.AddHandler(func(s *discordgo.Session, r *discordgo.MessageReactionRemove) {
		if r.UserID == s.State.User.ID {
			return
		}
		if err := service.SyncReaction(context.Background(), r.GuildID, r.ChannelID, r.MessageID, r.Emoji.APIName(), false); err != nil {
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
	translatorbot.RegisterGuildCommands(dg, dg.State.User.ID)
	log.Println("Discord Auto Translator is running")
	var retention *retentionWorker
	if cfg.MessageLinkRetentionDays > 0 || cfg.GuildDataRetentionDays > 0 {
		retention = startRetentionWorker(
			store,
			cfg.MessageLinkRetentionDays,
			cfg.GuildDataRetentionDays,
			time.Now,
			dg.State,
			log.Printf,
		)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	if retention != nil {
		retention.Stop()
	}
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

type startupOptions struct {
	envFile        string
	bedrockPrewarm bool
}

type bedrockWarmer interface {
	WarmUp(context.Context) error
}

func prewarmBedrock(ctx context.Context, warmer bedrockWarmer) error {
	prewarmCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	return warmer.WarmUp(prewarmCtx)
}

func parseStartupOptions(args []string) (startupOptions, error) {
	fs := flag.NewFlagSet("discord-auto-translator", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var options startupOptions
	fs.StringVar(&options.envFile, "env-file", ".env", "path to the environment file")
	fs.BoolVar(&options.bedrockPrewarm, "bedrock-prewarm", false, "validate Bedrock model access and the response contract, then exit")
	if err := fs.Parse(args); err != nil {
		return startupOptions{}, err
	}
	if fs.NArg() != 0 {
		return startupOptions{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(options.envFile) == "" {
		return startupOptions{}, errors.New("--env-file must not be empty")
	}
	return options, nil
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

func stickersFromDiscord(stickers []*discordgo.StickerItem) []translatorbot.DiscordSticker {
	out := make([]translatorbot.DiscordSticker, 0, len(stickers))
	for _, sticker := range stickers {
		if sticker == nil {
			continue
		}
		out = append(out, translatorbot.DiscordSticker{
			ID:         sticker.ID,
			Name:       sticker.Name,
			FormatType: int(sticker.FormatType),
		})
	}
	return out
}

func mentionNameMaps(s *discordgo.Session, guildID string, m *discordgo.Message) (users, channels, roles map[string]string) {
	users = map[string]string{}
	channels = map[string]string{}
	roles = map[string]string{}
	if m == nil {
		return users, channels, roles
	}
	for _, user := range m.Mentions {
		if user == nil {
			continue
		}
		users[user.ID] = userMentionName(s, guildID, user)
	}
	for _, ch := range m.MentionChannels {
		if ch == nil {
			continue
		}
		channels[ch.ID] = strings.TrimSpace(ch.Name)
	}
	for _, roleID := range m.MentionRoles {
		if role, err := s.State.Role(guildID, roleID); err == nil && role != nil {
			roles[roleID] = strings.TrimSpace(role.Name)
		}
	}
	return users, channels, roles
}

func userMentionName(s *discordgo.Session, guildID string, user *discordgo.User) string {
	if user == nil {
		return ""
	}
	if guildID != "" {
		if member, err := s.State.Member(guildID, user.ID); err == nil && member != nil {
			if name := strings.TrimSpace(member.DisplayName()); name != "" {
				return name
			}
			if name := strings.TrimSpace(member.Nick); name != "" {
				return name
			}
		}
	}
	if name := strings.TrimSpace(user.DisplayName()); name != "" {
		return name
	}
	return strings.TrimSpace(user.Username)
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

func referencedMessageFields(ref *discordgo.MessageReference, referenced *discordgo.Message) (id, channelID, content string) {
	if ref != nil && ref.Type == discordgo.MessageReferenceTypeForward {
		return "", "", ""
	}
	if ref != nil {
		id = ref.MessageID
		channelID = ref.ChannelID
	}
	if referenced != nil {
		if id == "" {
			id = referenced.ID
		}
		if channelID == "" {
			channelID = referenced.ChannelID
		}
		content = referenced.Content
	}
	return id, channelID, content
}

func forwardedMessageFields(ref *discordgo.MessageReference, snapshots []discordgo.MessageSnapshot) (*translatorbot.DiscordForwardedMessage, error) {
	if ref == nil || ref.Type != discordgo.MessageReferenceTypeForward {
		return nil, nil
	}
	if ref.MessageID == "" || ref.ChannelID == "" {
		return nil, fmt.Errorf("forward reference requires message_id and channel_id")
	}
	if len(snapshots) != 1 || snapshots[0].Message == nil {
		return nil, fmt.Errorf("forward reference requires exactly one non-nil snapshot, got %d", len(snapshots))
	}
	snapshot := snapshots[0].Message
	return &translatorbot.DiscordForwardedMessage{
		MessageID: ref.MessageID, ChannelID: ref.ChannelID, GuildID: ref.GuildID, Content: snapshot.Content,
		Attachments: attachmentsFromDiscord(snapshot.Attachments),
		Stickers:    stickersFromDiscord(snapshot.StickerItems),
	}, nil
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
