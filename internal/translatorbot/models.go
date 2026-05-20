package translatorbot

import "time"

type TranslationGroup struct {
	ID          string
	GuildID     string
	DisplayName string
	CreatedBy   string
	CreatedAt   time.Time
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
	SourceMessageID       string
	SourceChannelID       string
	GroupID               string
	TargetChannelID       string
	TargetMessageID       string
	TargetLanguage        string
	SourceAuthorID        string
	SourceContentSnapshot string
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
	ID                  string
	ChannelID           string
	GuildID             string
	AuthorID            string
	AuthorDisplayName   string
	AuthorAvatarURL     string
	Content             string
	ReferencedMessageID string
	MentionAuthor       bool
	WebhookID           string
	Bot                 bool
	Edited              bool
	ThreadSystemMessage bool
}
