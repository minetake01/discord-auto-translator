package translatorbot

import (
	"strings"
	"time"
)

type GlossaryEntry struct {
	SourceTerm           string
	PreferredTranslation string
	Attribute            string
	AlwaysInclude        bool
}

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
// TargetMessageID is the first message of the copy; ContinuationMessageIDs
// holds the messages that carry the rest of a copy too long for one Discord
// message, in order.
type MessageLink struct {
	SourceMessageID         string
	SourceChannelID         string
	GroupID                 string
	TargetChannelID         string
	TargetMessageID         string
	ContinuationMessageIDs  []string
	TargetLanguage          string
	SourceAuthorID          string
	SourceAuthorDisplayName string
	// SourceAuthorAvatarURL and SourceAuthorRoleColor keep the webhook identity
	// a mirror was posted under, so growing it on an edit can post the new
	// parts under the same name and avatar.
	SourceAuthorAvatarURL  string
	SourceAuthorRoleColor  int
	SourceContentSnapshot  string
	SourceImageAttachments []DiscordAttachment
	// TimestampLine marks a backfilled mirror whose first line shows the
	// source posting time; edits rebuild it.
	TimestampLine bool
	// MirrorBody is the mirror text without the generated headers on its first
	// message: the reference for re-quoting on an edit and for reusing the
	// mirror through a forwarded message without retranslating.
	MirrorBody string
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

type DiscordPollEmoji struct {
	Name     string
	ID       string
	Animated bool
}

type DiscordPollAnswer struct {
	Text  string
	Emoji *DiscordPollEmoji
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

type DiscordForwardedMessage struct {
	MessageID   string
	ChannelID   string
	GuildID     string
	Content     string
	Attachments []DiscordAttachment
	Stickers    []DiscordSticker
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
	// ReferencedMessageAttachments carries the referenced message's
	// attachments so a reply quote can describe an attachment-only target.
	ReferencedMessageAttachments []DiscordAttachment
	ForwardedMessage           *DiscordForwardedMessage
	Poll                       *DiscordPoll
	PollResult                 *DiscordPollResult
	TTS                        bool
	WebhookID                  string
	Bot                        bool
	Edited                     bool
	ThreadSystemMessage        bool
	ThreadStarterMessage       bool
	StartsThread               bool              // a thread was started from this message
	MentionedUsers             map[string]string // userID → display name
	MentionedChannels          map[string]string // channelID → channel name (source)
	MentionedRoles             map[string]string // roleID → role name
	// Pinned and Reactions describe the message state when it was read from
	// history; backfill applies them to the new mirror.
	Pinned    bool
	Reactions []string // emoji API names
}

// hasNoPayload reports whether the message carries nothing worth mirroring.
func (m DiscordMessage) hasNoPayload() bool {
	return strings.TrimSpace(m.Content) == "" && len(m.Attachments) == 0 && len(m.Stickers) == 0 &&
		m.ReferencedMessageID == "" && m.ForwardedMessage == nil && m.Poll == nil && m.PollResult == nil
}

type DiscordFetchedMessage struct {
	Content             string
	AuthorDisplayName   string
	ReferencedChannelID string
	ReferencedMessageID string
	Attachments         []DiscordAttachment
	Pinned              bool
	Reactions           []string // emoji API names
}
