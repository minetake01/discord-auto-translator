package main

import (
	"context"
	"encoding/json"
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

type startupOptions struct {
	envFile      string
	modelPrewarm bool
}

type modelWarmer interface {
	WarmUp(context.Context) error
}

func parseStartupOptions(args []string) (startupOptions, error) {
	fs := flag.NewFlagSet("discord-auto-translator", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var options startupOptions
	fs.StringVar(&options.envFile, "env-file", ".env", "path to the environment file")
	fs.BoolVar(&options.modelPrewarm, "model-prewarm", false, "validate OpenAI-compatible model access and the response contract, then exit")
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

func prewarmModel(ctx context.Context, warmer modelWarmer) error {
	prewarmCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	return warmer.WarmUp(prewarmCtx)
}

func main() {
	startup, err := parseStartupOptions(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}
	cfg, err := translatorbot.LoadConfig(startup.envFile)
	if err != nil {
		log.Fatal(err)
	}
	translator, err := translatorbot.NewOpenAITranslator(context.Background(), cfg.OpenAIBaseURL, cfg.OpenAIAPIKey, cfg.OpenAIModel, cfg.OpenAIReasoningEffort)
	if err != nil {
		log.Fatal(err)
	}
	if cfg.TranslationDebugLogPath != "" {
		debugLog, err := translatorbot.OpenDebugLog(cfg.TranslationDebugLogPath)
		if err != nil {
			log.Fatal(err)
		}
		defer func() {
			if err := debugLog.Close(); err != nil {
				log.Printf("translation debug log close: %v", err)
			}
		}()
		translator.SetDebugLog(debugLog)
	}
	if startup.modelPrewarm {
		if err := prewarmModel(context.Background(), translator); err != nil {
			log.Fatal(err)
		}
		log.Println("OpenAI-compatible model access and response contract are ready")
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
	dg.AddHandler(func(s *discordgo.Session, e *discordgo.Event) {
		switch e.Type {
		case "MESSAGE_CREATE":
			var m discordgo.MessageCreate
			if err := json.Unmarshal(e.RawData, &m); err != nil {
				log.Printf("message create unmarshal: %v", err)
				return
			}
			handleGatewayMessageCreate(s, service, &m, e.RawData)
		case "MESSAGE_UPDATE":
			var m discordgo.MessageUpdate
			if err := json.Unmarshal(e.RawData, &m); err != nil {
				log.Printf("message update unmarshal: %v", err)
				return
			}
			handleGatewayMessageUpdate(s, service, &m, e.RawData)
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
		if err := service.SyncThreadCreateFromGateway(context.Background(), t.GuildID, t.ParentID, t.ID, t.Name, t.AppliedTags); err != nil {
			log.Printf("thread create sync: %v", err)
		}
	})
	dg.AddHandler(func(s *discordgo.Session, t *discordgo.ThreadUpdate) {
		if t.Channel == nil {
			return
		}
		nameChanged := t.Name != "" && (t.BeforeUpdate == nil || t.BeforeUpdate.Name != t.Name)
		tagsChanged := t.BeforeUpdate == nil || !translatorbot.ForumTagSetsEqual(t.BeforeUpdate.AppliedTags, t.AppliedTags)
		if !nameChanged && !tagsChanged {
			return
		}
		if err := service.SyncThreadUpdate(context.Background(), t.GuildID, t.ID, t.Name, t.AppliedTags, nameChanged, tagsChanged); err != nil {
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
