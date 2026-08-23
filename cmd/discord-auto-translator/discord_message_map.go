package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"discord-auto-translator/internal/translatorbot"
	"github.com/bwmarrin/discordgo"
)

type gatewayAttachmentDescription struct {
	Description string `json:"description"`
}

type gatewayMessageAttachments struct {
	Attachments      []gatewayAttachmentDescription `json:"attachments"`
	MessageSnapshots []struct {
		Message struct {
			Attachments []gatewayAttachmentDescription `json:"attachments"`
		} `json:"message"`
	} `json:"message_snapshots"`
}

func attachmentDescriptionsFromRaw(raw json.RawMessage) (descriptions []string, snapshotDescriptions []string) {
	var parsed gatewayMessageAttachments
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, nil
	}
	descriptions = make([]string, len(parsed.Attachments))
	for i, attachment := range parsed.Attachments {
		descriptions[i] = strings.TrimSpace(attachment.Description)
	}
	if len(parsed.MessageSnapshots) == 1 {
		snapshot := parsed.MessageSnapshots[0].Message.Attachments
		snapshotDescriptions = make([]string, len(snapshot))
		for i, attachment := range snapshot {
			snapshotDescriptions[i] = strings.TrimSpace(attachment.Description)
		}
	}
	return descriptions, snapshotDescriptions
}

func attachmentsFromDiscord(attachments []*discordgo.MessageAttachment, descriptions []string) []translatorbot.DiscordAttachment {
	out := make([]translatorbot.DiscordAttachment, 0, len(attachments))
	descIndex := 0
	for _, attachment := range attachments {
		if attachment == nil {
			continue
		}
		description := ""
		if descIndex < len(descriptions) {
			description = descriptions[descIndex]
		}
		descIndex++
		out = append(out, translatorbot.DiscordAttachment{
			URL:         attachment.URL,
			Filename:    attachment.Filename,
			ContentType: attachment.ContentType,
			Description: description,
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

// MessageTypePollResult is Discord's POLL_RESULT system message type.
// discordgo v0.29.0 does not define this constant yet.
const MessageTypePollResult discordgo.MessageType = 46

const embedTypePollResult discordgo.EmbedType = "poll_result"

func pollFromDiscord(poll *discordgo.Poll) *translatorbot.DiscordPoll {
	if poll == nil {
		return nil
	}
	out := &translatorbot.DiscordPoll{Question: poll.Question.Text, Expiry: poll.Expiry}
	out.Answers = make([]translatorbot.DiscordPollAnswer, 0, len(poll.Answers))
	for _, answer := range poll.Answers {
		item := translatorbot.DiscordPollAnswer{}
		if answer.Media != nil {
			item.Text = answer.Media.Text
			if answer.Media.Emoji != nil {
				item.Emoji = &translatorbot.DiscordPollEmoji{
					Name:     answer.Media.Emoji.Name,
					ID:       answer.Media.Emoji.ID,
					Animated: answer.Media.Emoji.Animated,
				}
			}
		}
		out.Answers = append(out.Answers, item)
	}
	return out
}

func pollResultFromDiscord(m *discordgo.Message) *translatorbot.DiscordPollResult {
	if m == nil || m.Type != MessageTypePollResult {
		return nil
	}
	out := &translatorbot.DiscordPollResult{}
	for _, embed := range m.Embeds {
		if embed == nil || embed.Type != embedTypePollResult {
			continue
		}
		out.HasEmbed = true
		for _, field := range embed.Fields {
			if field == nil {
				continue
			}
			switch field.Name {
			case "poll_question_text":
				out.QuestionText = field.Value
			case "victor_answer_text":
				out.VictorAnswerText = field.Value
			case "victor_answer_id":
				if id, err := strconv.Atoi(strings.TrimSpace(field.Value)); err == nil {
					out.VictorAnswerID = id
				}
			case "victor_answer_votes":
				if n, err := strconv.Atoi(strings.TrimSpace(field.Value)); err == nil {
					out.VictorAnswerVotes = n
				}
			case "total_votes":
				if n, err := strconv.Atoi(strings.TrimSpace(field.Value)); err == nil {
					out.TotalVotes = n
				}
			case "victor_answer_emoji_name":
				if out.VictorEmoji == nil {
					out.VictorEmoji = &translatorbot.DiscordPollEmoji{}
				}
				out.VictorEmoji.Name = field.Value
			case "victor_answer_emoji_id":
				if out.VictorEmoji == nil {
					out.VictorEmoji = &translatorbot.DiscordPollEmoji{}
				}
				out.VictorEmoji.ID = field.Value
			case "victor_answer_emoji_animated":
				if out.VictorEmoji == nil {
					out.VictorEmoji = &translatorbot.DiscordPollEmoji{}
				}
				out.VictorEmoji.Animated = strings.EqualFold(strings.TrimSpace(field.Value), "true")
			}
		}
		break
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

func forwardedMessageFields(ref *discordgo.MessageReference, snapshots []discordgo.MessageSnapshot, snapshotDescriptions []string) (*translatorbot.DiscordForwardedMessage, error) {
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
		Attachments: attachmentsFromDiscord(snapshot.Attachments, snapshotDescriptions),
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
