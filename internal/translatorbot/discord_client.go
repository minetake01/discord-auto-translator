package translatorbot

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/bwmarrin/discordgo"
)

const defaultWebhookName = "Discord Auto Translator"

var reservedWebhookNamePattern = regexp.MustCompile(`(?i)discord`)

type DiscordAPI interface {
	GuildName(guildID string) (string, error)
	GuildDescription(guildID string) (string, error)
	ChannelName(channelID string) (string, error)
	ChannelTopic(channelID string) (string, error)
	Message(channelID, messageID string) (DiscordFetchedMessage, error)
	CreateWebhook(channelID, name string) (id, token string, err error)
	SendChannelMessage(channelID, content string) error
	SendWebhook(webhookID, token string, msg WebhookSend) (messageID string, err error)
	EditWebhook(webhookID, token, messageID, threadID, content string) error
	DeleteWebhook(webhookID, token, messageID, threadID string) error
	AddReaction(channelID, messageID, emoji string) error
	RemoveOwnReaction(channelID, messageID, emoji string) error
	PinMessage(channelID, messageID string) error
	UnpinMessage(channelID, messageID string) error
	CreateThread(channelID string, channelType int, name, initialMessage string) (threadID, initialMessageID string, err error)
	CreateThreadFromMessage(channelID, messageID, name string) (threadID string, err error)
	EditThread(threadID, name string) error
	DeleteThread(threadID string) error
}

type WebhookSend struct {
	Content   string
	Username  string
	AvatarURL string
	ThreadID  string
	TTS       bool
}

type DiscordGoAPI struct {
	session *discordgo.Session
}

func NewDiscordGoAPI(session *discordgo.Session) DiscordGoAPI {
	return DiscordGoAPI{session: session}
}

func (d DiscordGoAPI) CurrentUserID() (string, error) {
	user, err := d.session.User("@me")
	if err != nil {
		return "", fmt.Errorf("fetch current Discord user: %w", err)
	}
	if user == nil || user.ID == "" {
		return "", errors.New("fetch current Discord user: response did not include a user ID")
	}
	return user.ID, nil
}

func (d DiscordGoAPI) GuildName(guildID string) (string, error) {
	g, err := d.session.Guild(guildID)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(g.Name), nil
}

func (d DiscordGoAPI) GuildDescription(guildID string) (string, error) {
	g, err := d.session.Guild(guildID)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(g.Description), nil
}

func (d DiscordGoAPI) ChannelName(channelID string) (string, error) {
	ch, err := d.session.Channel(channelID)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(ch.Name), nil
}

func (d DiscordGoAPI) ChannelTopic(channelID string) (string, error) {
	ch, err := d.session.Channel(channelID)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(ch.Topic), nil
}

func (d DiscordGoAPI) Message(channelID, messageID string) (DiscordFetchedMessage, error) {
	message, err := withDiscordRetryValue(func() (*discordgo.Message, error) {
		return d.session.ChannelMessage(channelID, messageID)
	})
	if err != nil {
		return DiscordFetchedMessage{}, err
	}
	return fetchedMessageFromDiscord(message), nil
}

func fetchedMessageFromDiscord(message *discordgo.Message) DiscordFetchedMessage {
	result := DiscordFetchedMessage{Content: message.Content}
	if message.Author != nil {
		result.AuthorDisplayName = strings.TrimSpace(message.Author.Username)
	}
	if message.MessageReference != nil && message.MessageReference.Type != discordgo.MessageReferenceTypeForward {
		result.ReferencedMessageID = message.MessageReference.MessageID
		result.ReferencedChannelID = message.MessageReference.ChannelID
	}
	return result
}

func (d DiscordGoAPI) CreateWebhook(channelID, name string) (string, string, error) {
	name = sanitizeWebhookName(name)
	w, err := d.session.WebhookCreate(channelID, name, "")
	if err != nil {
		return "", "", err
	}
	return w.ID, w.Token, nil
}

func (d DiscordGoAPI) SendChannelMessage(channelID, content string) error {
	_, err := d.session.ChannelMessageSend(channelID, content)
	return err
}

func (d DiscordGoAPI) SendWebhook(webhookID, token string, msg WebhookSend) (string, error) {
	params := &discordgo.WebhookParams{
		Content:   msg.Content,
		Username:  sanitizeWebhookName(msg.Username),
		AvatarURL: sanitizeWebhookAvatarURL(msg.AvatarURL),
		TTS:       msg.TTS,
	}
	m, err := withDiscordRetryValue(func() (*discordgo.Message, error) {
		if msg.ThreadID != "" {
			return d.session.WebhookThreadExecute(webhookID, token, true, msg.ThreadID, params)
		}
		return d.session.WebhookExecute(webhookID, token, true, params)
	})
	if err != nil {
		return "", err
	}
	return m.ID, nil
}

func sanitizeWebhookAvatarURL(avatarURL string) string {
	avatarURL = strings.TrimSpace(avatarURL)
	if avatarURL == "" || len(avatarURL) > 2048 {
		return ""
	}
	u, err := url.Parse(avatarURL)
	if err != nil || u.Host == "" {
		return ""
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return avatarURL
}

func sanitizeWebhookName(name string) string {
	name = strings.TrimSpace(name)
	name = reservedWebhookNamePattern.ReplaceAllString(name, "D-scord")
	if name == "" {
		return defaultWebhookName
	}
	const maxRunes = 80
	runes := []rune(name)
	if len(runes) > maxRunes {
		name = strings.TrimSpace(string(runes[:maxRunes]))
	}
	if name == "" {
		return defaultWebhookName
	}
	return name
}

func (d DiscordGoAPI) EditWebhook(webhookID, token, messageID, threadID, content string) error {
	edit := &discordgo.WebhookEdit{Content: &content}
	if threadID == "" {
		_, err := withDiscordRetryValue(func() (*discordgo.Message, error) {
			return d.session.WebhookMessageEdit(webhookID, token, messageID, edit)
		})
		return err
	}
	_, err := withDiscordRetryValue(func() (*discordgo.Message, error) {
		return d.webhookMessageEditInThread(webhookID, token, messageID, threadID, edit)
	})
	return err
}

func (d DiscordGoAPI) DeleteWebhook(webhookID, token, messageID, threadID string) error {
	if threadID == "" {
		return withDiscordRetry(func() error {
			return d.session.WebhookMessageDelete(webhookID, token, messageID)
		})
	}
	return withDiscordRetry(func() error {
		_, err := d.session.RequestWithBucketID("DELETE", webhookMessageURL(webhookID, token, messageID, threadID), nil, discordgo.EndpointWebhookToken("", ""))
		return err
	})
}

func (d DiscordGoAPI) webhookMessageEditInThread(webhookID, token, messageID, threadID string, edit *discordgo.WebhookEdit) (*discordgo.Message, error) {
	response, err := d.session.RequestWithBucketID("PATCH", webhookMessageURL(webhookID, token, messageID, threadID), edit, discordgo.EndpointWebhookToken("", ""))
	if err != nil {
		return nil, err
	}
	var msg discordgo.Message
	if err := discordgo.Unmarshal(response, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func webhookMessageURL(webhookID, token, messageID, threadID string) string {
	uri := discordgo.EndpointWebhookMessage(webhookID, token, messageID)
	if threadID == "" {
		return uri
	}
	v := url.Values{}
	v.Set("thread_id", threadID)
	return uri + "?" + v.Encode()
}

func (d DiscordGoAPI) AddReaction(channelID, messageID, emoji string) error {
	return d.session.MessageReactionAdd(channelID, messageID, emoji)
}

func (d DiscordGoAPI) RemoveOwnReaction(channelID, messageID, emoji string) error {
	return d.session.MessageReactionRemove(channelID, messageID, emoji, "@me")
}

func (d DiscordGoAPI) PinMessage(channelID, messageID string) error {
	return d.session.ChannelMessagePin(channelID, messageID)
}

func (d DiscordGoAPI) UnpinMessage(channelID, messageID string) error {
	return d.session.ChannelMessageUnpin(channelID, messageID)
}

func (d DiscordGoAPI) CreateThread(channelID string, channelType int, name, initialMessage string) (string, string, error) {
	if isThreadOnlyChannelType(channelType) {
		if strings.TrimSpace(initialMessage) == "" {
			initialMessage = name
		}
		message := &discordgo.MessageSend{Content: initialMessage}
		t, err := d.session.ForumThreadStartComplex(channelID, &discordgo.ThreadStart{
			Name:                name,
			AutoArchiveDuration: 1440,
		}, message)
		if err != nil {
			return "", "", err
		}
		messageID := t.ID
		if t.LastMessageID != "" {
			messageID = t.LastMessageID
		}
		return t.ID, messageID, nil
	}
	t, err := d.session.ThreadStart(channelID, name, discordgo.ChannelTypeGuildPublicThread, 1440)
	if err != nil {
		return "", "", err
	}
	return t.ID, "", nil
}

func isThreadOnlyChannelType(channelType int) bool {
	return channelType == int(discordgo.ChannelTypeGuildForum) || channelType == int(discordgo.ChannelTypeGuildMedia)
}

func (d DiscordGoAPI) CreateThreadFromMessage(channelID, messageID, name string) (string, error) {
	t, err := d.session.MessageThreadStart(channelID, messageID, name, 1440)
	if err != nil {
		return "", err
	}
	return t.ID, nil
}

func (d DiscordGoAPI) EditThread(threadID, name string) error {
	_, err := d.session.ChannelEdit(threadID, &discordgo.ChannelEdit{Name: name})
	return err
}

func (d DiscordGoAPI) DeleteThread(threadID string) error {
	_, err := d.session.ChannelDelete(threadID)
	return err
}
