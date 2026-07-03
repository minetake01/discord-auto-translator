package translatorbot

import "time"

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
	Content                    string
	Attachments                []DiscordAttachment
	Stickers                   []DiscordSticker
	ReferencedMessageID        string
	ReferencedMessageChannelID string
	ReferencedMessageContent   string
	TTS                        bool
	WebhookID                  string
	Bot                        bool
	Edited                     bool
	ThreadSystemMessage        bool
	ThreadStarterMessage       bool
}

type DiscordAttachment struct {
	URL         string
	Filename    string
	ContentType string
}

type DiscordSticker struct {
	ID         string
	Name       string
	FormatType int
}
