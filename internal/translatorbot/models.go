package translatorbot

import (
	"time"
)

type TranslationGroup struct {
	ID          string
	GuildID     string
	DisplayName string
	CreatedBy   string
	CreatedAt   time.Time
	StylePreset string
	StyleCustom string
}

type GroupChannel struct {
	GroupID      string
	GuildID      string
	ChannelID    string
	ChannelType  int
	Language     string
	WebhookID    string
	WebhookToken string
}

// MessageLink is one mirrored copy of a source message (message_links row).
type MessageLink struct {
	SourceMessageID         string
	SourceChannelID         string
	GroupID                 string
	TargetChannelID         string
	TargetMessageID         string
	TargetLanguage          string
	SourceAuthorID          string
	SourceAuthorDisplayName string
	SourceContentSnapshot   string
	SourceImageAttachments  []DiscordAttachment
}

// MessageReference is the reply target of a source message (message_references row).
// It is keyed by the source message, not by each mirror copy.
type MessageReference struct {
	MessageID string
	ChannelID string
}

type ThreadLink struct {
	GroupID         string
	SourceThreadID  string
	SourceChannelID string
	TargetThreadID  string
	TargetChannelID string
	TargetLanguage  string
}

type DiscordMessage struct {
	ID                         string
	ChannelID                  string
	GuildID                    string
	ParentChannelID            string
	ThreadName                 string
	AuthorID                   string
	AuthorDisplayName          string
	AuthorAvatarURL            string
	AuthorRoleColor            int
	Content                    string
	Attachments                []DiscordAttachment
	Stickers                   []DiscordSticker
	ReferencedMessageID        string
	ReferencedMessageChannelID string
	ReferencedMessageContent   string
	ForwardedMessage           *DiscordForwardedMessage
	Poll                       *DiscordPoll
	PollResult                 *DiscordPollResult
	TTS                        bool
	WebhookID                  string
	Bot                        bool
	Edited                     bool
	ThreadSystemMessage        bool
	ThreadStarterMessage       bool
	MentionedUsers             map[string]string // userID → display name
	MentionedChannels          map[string]string // channelID → channel name (source)
	MentionedRoles             map[string]string // roleID → role name
}

type DiscordPoll struct {
	Question string
	Answers  []DiscordPollAnswer
	Expiry   *time.Time
}

// DiscordPollResult is the payload of a Discord POLL_RESULT system message
// (type 46), parsed from the poll_result embed fields.
type DiscordPollResult struct {
	HasEmbed          bool
	QuestionText      string
	VictorAnswerID    int // 0 when absent; Discord answer ids are 1-based
	VictorAnswerText  string
	VictorEmoji       *DiscordPollEmoji
	VictorAnswerVotes int
	TotalVotes        int
}

type DiscordPollAnswer struct {
	Text  string
	Emoji *DiscordPollEmoji
}

type DiscordPollEmoji struct {
	Name     string
	ID       string
	Animated bool
}

type DiscordForwardedMessage struct {
	MessageID   string
	ChannelID   string
	GuildID     string
	Content     string
	Attachments []DiscordAttachment
	Stickers    []DiscordSticker
}

type DiscordAttachment struct {
	URL         string
	Filename    string
	ContentType string
	Description string
}

type DiscordSticker struct {
	ID         string
	Name       string
	FormatType int
}

type DiscordFetchedMessage struct {
	Content             string
	AuthorDisplayName   string
	ReferencedChannelID string
	ReferencedMessageID string
	Attachments         []DiscordAttachment
}
