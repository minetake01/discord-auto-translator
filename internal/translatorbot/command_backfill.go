package translatorbot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	backfillCustomStart  = "bf:start:"
	backfillCustomCancel = "bf:cancel:"
	// backfillScanTimeout keeps the scan inside the 15 minute window in
	// which the deferred interaction response can still be edited.
	backfillScanTimeout = 14 * time.Minute
	backfillPendingTTL  = time.Hour
)

var errWebhookCreate = errors.New("create webhook")

// BackfillService is the part of Service the backfill command uses.
type BackfillService interface {
	EstimateBackfill(ctx context.Context, guildID, groupID string) (BackfillEstimate, error)
	StartBackfill(guildID string)
}

// pendingBackfill is an estimate waiting for the invoker to start or cancel.
// The channel joins the group only when the backfill is started.
type pendingBackfill struct {
	channel   GroupChannel
	requester string
}

type pendingBackfills struct {
	sessions *sessionStore[pendingBackfill]
}

func newPendingBackfills() pendingBackfills {
	return pendingBackfills{sessions: newSessionStore[pendingBackfill](backfillPendingTTL)}
}

// put stores a pending backfill under a fresh random id and returns it.
func (p *pendingBackfills) put(item pendingBackfill) string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(err)
	}
	id := hex.EncodeToString(raw[:])
	p.sessions.put(id, item)
	return id
}

func (p *pendingBackfills) take(id string) (pendingBackfill, bool) {
	return p.sessions.take(id)
}

func (h *CommandHandler) SetBackfill(backfill BackfillService) {
	h.backfill = backfill
}

// handleJoinWithBackfill scans the group's history, shows the estimate, and
// waits for the invoker to start or cancel. The response is deferred because
// the scan can take minutes.
func (h *CommandHandler) handleJoinWithBackfill(s *discordgo.Session, i *discordgo.InteractionCreate, channel GroupChannel, peers []GroupChannel) {
	locale := commandLocale(i)
	for _, peer := range peers {
		switch {
		case peer.ChannelID == channel.ChannelID:
			h.reply(s, i, uiKeyDuplicateChannel, channel.GroupID)
			return
		case peer.Language == channel.Language:
			h.reply(s, i, uiKeyDuplicateLanguage, channel.GroupID)
			return
		}
	}
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	}); err != nil {
		log.Printf("join-channel backfill defer: %v", err)
		return
	}
	edit := func(content string, components []discordgo.MessageComponent) {
		if components == nil {
			components = []discordgo.MessageComponent{}
		}
		if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &content, Components: &components}); err != nil {
			log.Printf("join-channel backfill edit response: %v", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), backfillScanTimeout)
	defer cancel()
	estimate, err := h.backfill.EstimateBackfill(ctx, channel.GuildID, channel.GroupID)
	if err != nil {
		log.Printf("join-channel backfill scan: %v", err)
		key := uiKeyBackfillScanFailed
		if errors.Is(err, context.DeadlineExceeded) {
			key = uiKeyBackfillScanTimeout
		}
		edit(localizedUIStringf(locale, key, channel.ChannelID), nil)
		return
	}
	if estimate.Messages == 0 {
		if err := h.joinChannel(context.Background(), channel, "", false); err != nil {
			edit(h.groupErrorText(locale, "join-channel", channel.GroupID, err), nil)
			return
		}
		edit(localizedUIStringf(locale, uiKeyChannelJoined, channel.GroupID, channel.ChannelID, channel.Language), nil)
		return
	}
	id := h.pendingBackfills.put(pendingBackfill{channel: channel, requester: interactionUserID(i)})
	content := localizedUIStringf(locale, uiKeyBackfillEstimate, channel.GroupID, channel.ChannelID,
		estimateCount(estimate), estimate.Images, estimate.InputTokens, estimate.OutputTokens, formatEstimateDuration(estimate.Duration))
	edit(content, []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{Label: localizedUIString(locale, uiKeyBackfillStart), Style: discordgo.PrimaryButton, CustomID: backfillCustomStart + id},
		discordgo.Button{Label: localizedUIString(locale, uiKeyBackfillCancel), Style: discordgo.SecondaryButton, CustomID: backfillCustomCancel + id},
	}}})
}

// handleBackfillComponent starts or cancels a pending backfill. The estimate
// is ephemeral to the invoker, so the click needs no permission check.
func (h *CommandHandler) handleBackfillComponent(s *discordgo.Session, i *discordgo.InteractionCreate, customID string) {
	locale := commandLocale(i)
	start := strings.HasPrefix(customID, backfillCustomStart)
	id := strings.TrimPrefix(strings.TrimPrefix(customID, backfillCustomStart), backfillCustomCancel)
	pending, ok := h.pendingBackfills.take(id)
	if !ok {
		h.updateInteraction(s, i, localizedUIString(locale, uiKeyUnexpectedError), nil)
		return
	}
	channel := pending.channel
	if !start {
		h.updateInteraction(s, i, localizedUIStringf(locale, uiKeyBackfillCanceled, channel.ChannelID), nil)
		return
	}
	if err := h.joinChannel(context.Background(), channel, pending.requester, true); err != nil {
		h.updateInteraction(s, i, h.groupErrorText(locale, "join-channel", channel.GroupID, err), nil)
		return
	}
	h.backfill.StartBackfill(channel.GuildID)
	h.updateInteraction(s, i, localizedUIStringf(locale, uiKeyBackfillJoined, channel.GroupID, channel.ChannelID, channel.Language), nil)
}

// handleBackfill dispatches the /backfill subcommands.
func (h *CommandHandler) handleBackfill(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	if len(data.Options) != 1 || data.Options[0].Type != discordgo.ApplicationCommandOptionSubCommand {
		log.Printf("backfill invalid subcommand payload")
		h.reply(s, i, uiKeyUnexpectedError)
		return
	}
	subcommand := data.Options[0]
	switch subcommand.Name {
	case "stop":
		h.handleBackfillStop(s, i, subcommand.Options)
	default:
		log.Printf("backfill unknown subcommand %q", subcommand.Name)
		h.reply(s, i, uiKeyUnexpectedError)
	}
}

// handleBackfillStop removes every job that fills the channel. The worker
// watches the job row, so deleting it stops the run before the next message is
// posted and live mirroring into the channel resumes immediately.
func (h *CommandHandler) handleBackfillStop(s *discordgo.Session, i *discordgo.InteractionCreate, options []*discordgo.ApplicationCommandInteractionDataOption) {
	ctx := context.Background()
	channelID := optionChannel(options, "channel", i.ChannelID)
	stopped, err := h.store.CancelBackfillJobs(ctx, i.GuildID, channelID)
	if err != nil {
		log.Printf("backfill stop store: %v", err)
		h.reply(s, i, uiKeyUnexpectedError)
		return
	}
	if stopped == 0 {
		h.reply(s, i, uiKeyBackfillNotRunning, channelID)
		return
	}
	h.reply(s, i, uiKeyBackfillStopped, channelID)
}

// joinChannel creates the channel's webhook and registers it, together with
// a backfill job when requested.
func (h *CommandHandler) joinChannel(ctx context.Context, channel GroupChannel, requester string, backfill bool) error {
	webhookID, token, err := h.api.CreateWebhook(channel.ChannelID, defaultWebhookName)
	if err != nil {
		return fmt.Errorf("%w: %v", errWebhookCreate, err)
	}
	channel.WebhookID, channel.WebhookToken = webhookID, token
	if backfill {
		_, err = h.store.JoinChannelWithBackfill(ctx, channel, requester)
		return err
	}
	return h.store.JoinChannel(ctx, channel)
}

// estimateCount formats the message count of an estimate. A truncated scan
// only knows a lower bound, which the "+" marks.
func estimateCount(estimate BackfillEstimate) string {
	if estimate.Truncated {
		return fmt.Sprintf("%d+", estimate.Messages)
	}
	return fmt.Sprintf("%d", estimate.Messages)
}

func formatEstimateDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	if d < time.Minute {
		return "1m"
	}
	hours := int(d / time.Hour)
	minutes := int((d % time.Hour) / time.Minute)
	switch {
	case hours == 0:
		return fmt.Sprintf("%dm", minutes)
	case minutes == 0:
		return fmt.Sprintf("%dh", hours)
	default:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
}
