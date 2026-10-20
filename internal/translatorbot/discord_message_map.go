package translatorbot

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"
)

// MessageTypePollResult is Discord's POLL_RESULT system message type.
// discordgo v0.29.0 does not define this constant yet.
const MessageTypePollResult discordgo.MessageType = 46

const embedTypePollResult discordgo.EmbedType = "poll_result"

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

// memberCache remembers guild members fetched over REST while mapping
// history pages, whose messages carry no member object.
type memberCache struct {
	mu      sync.Mutex
	members map[string]*discordgo.Member
}

// ForgetMemberCache drops the members cached while mapping history pages.
func (d DiscordGoAPI) ForgetMemberCache() {
	d.members.forget()
}

// forget drops every cached member; the next lookup fetches again.
func (c *memberCache) forget() {
	c.mu.Lock()
	c.members = nil
	c.mu.Unlock()
}

func (c *memberCache) lookup(session *discordgo.Session, guildID, userID string) *discordgo.Member {
	if session == nil || guildID == "" || userID == "" {
		return nil
	}
	if member, err := session.State.Member(guildID, userID); err == nil && member != nil {
		return member
	}
	key := guildID + "\x00" + userID
	c.mu.Lock()
	member, ok := c.members[key]
	c.mu.Unlock()
	if ok {
		return member
	}
	member, err := session.GuildMember(guildID, userID)
	if err != nil {
		member = nil
	}
	c.mu.Lock()
	if c.members == nil {
		c.members = make(map[string]*discordgo.Member)
	}
	c.members[key] = member
	c.mu.Unlock()
	return member
}

// messageMapOptions selects how much of a Discord message is mapped.
// Edits never carry reply, forward, or poll semantics.
type messageMapOptions struct {
	guildID      string
	forUpdate    bool
	fetchMembers bool
	// knownThread supplies the thread context of a history page so it is
	// not looked up once per message.
	knownThread     bool
	parentChannelID string
	threadName      string
}

// MapGatewayMessage converts a gateway MESSAGE_CREATE/MESSAGE_UPDATE payload.
func (d DiscordGoAPI) MapGatewayMessage(m *discordgo.Message, raw json.RawMessage, update bool) (DiscordMessage, error) {
	return d.mapMessage(m, raw, messageMapOptions{guildID: m.GuildID, forUpdate: update})
}

func (d DiscordGoAPI) mapMessage(m *discordgo.Message, raw json.RawMessage, opts messageMapOptions) (DiscordMessage, error) {
	if m == nil || m.Author == nil {
		return DiscordMessage{}, fmt.Errorf("message has no author")
	}
	guildID := opts.guildID
	member := m.Member
	if member == nil && opts.fetchMembers {
		member = d.members.lookup(d.session, guildID, m.Author.ID)
	}
	if member != nil && member.User == nil {
		copied := *member
		copied.User = m.Author
		member = &copied
	}
	parentChannelID, threadName := opts.parentChannelID, opts.threadName
	if !opts.knownThread {
		parentChannelID, threadName = d.threadContext(m.ChannelID)
	}
	descriptions, snapshotDescriptions := attachmentDescriptionsFromRaw(raw)
	users, channels, roles := d.mentionNameMaps(guildID, m)
	out := DiscordMessage{
		ID: m.ID, ChannelID: m.ChannelID, GuildID: guildID, AuthorID: m.Author.ID,
		ParentChannelID: parentChannelID, ThreadName: threadName,
		AuthorDisplayName: userDisplayName(member, m.Author),
		AuthorAvatarURL:   m.Author.AvatarURL("128"),
		AuthorRoleColor:   d.memberRoleColor(guildID, member),
		Content:           m.Content,
		Attachments:       attachmentsFromDiscord(m.Attachments, descriptions),
		Stickers:          stickersFromDiscord(m.StickerItems),
		TTS:               m.TTS,
		WebhookID:         m.WebhookID,
		Bot:               m.Author.Bot,
		Edited:            opts.forUpdate,
		MentionedUsers:    users,
		MentionedChannels: channels,
		MentionedRoles:    roles,
		Pinned:            m.Pinned,
		Reactions:         reactionsFromDiscord(m.Reactions),
	}
	if opts.forUpdate {
		return out, nil
	}
	out.ThreadSystemMessage = isThreadSystemMessage(m.Type)
	out.ThreadStarterMessage = m.Type == discordgo.MessageTypeThreadStarterMessage
	out.StartsThread = m.Thread != nil
	forwarded, err := forwardedMessageFields(m.MessageReference, m.MessageSnapshots, snapshotDescriptions)
	if err != nil {
		return DiscordMessage{}, err
	}
	out.ForwardedMessage = forwarded
	out.ReferencedMessageID, out.ReferencedMessageChannelID, out.ReferencedMessageContent = referencedMessageFields(m.MessageReference, m.ReferencedMessage)
	out.Poll = pollFromDiscord(m.Poll)
	out.PollResult = pollResultFromDiscord(m)
	return out, nil
}

func reactionsFromDiscord(reactions []*discordgo.MessageReactions) []string {
	out := make([]string, 0, len(reactions))
	for _, reaction := range reactions {
		if reaction == nil || reaction.Emoji == nil || reaction.Count <= 0 {
			continue
		}
		if name := reaction.Emoji.APIName(); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// attachmentDescriptionsFromRaw reads attachments[].description from the raw
// payload. discordgo v0.29.0 MessageAttachment has no Description field.
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

func attachmentsFromDiscord(attachments []*discordgo.MessageAttachment, descriptions []string) []DiscordAttachment {
	out := make([]DiscordAttachment, 0, len(attachments))
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
		out = append(out, DiscordAttachment{
			URL:         attachment.URL,
			Filename:    attachment.Filename,
			ContentType: attachment.ContentType,
			Description: description,
		})
	}
	return out
}

func stickersFromDiscord(stickers []*discordgo.StickerItem) []DiscordSticker {
	out := make([]DiscordSticker, 0, len(stickers))
	for _, sticker := range stickers {
		if sticker == nil {
			continue
		}
		out = append(out, DiscordSticker{ID: sticker.ID, Name: sticker.Name, FormatType: int(sticker.FormatType)})
	}
	return out
}

func pollFromDiscord(poll *discordgo.Poll) *DiscordPoll {
	if poll == nil {
		return nil
	}
	out := &DiscordPoll{Question: poll.Question.Text, Expiry: poll.Expiry}
	out.Answers = make([]DiscordPollAnswer, 0, len(poll.Answers))
	for _, answer := range poll.Answers {
		item := DiscordPollAnswer{}
		if answer.Media != nil {
			item.Text = answer.Media.Text
			if answer.Media.Emoji != nil {
				item.Emoji = &DiscordPollEmoji{
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

func pollResultFromDiscord(m *discordgo.Message) *DiscordPollResult {
	if m == nil || m.Type != MessageTypePollResult {
		return nil
	}
	out := &DiscordPollResult{}
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
					out.VictorEmoji = &DiscordPollEmoji{}
				}
				out.VictorEmoji.Name = field.Value
			case "victor_answer_emoji_id":
				if out.VictorEmoji == nil {
					out.VictorEmoji = &DiscordPollEmoji{}
				}
				out.VictorEmoji.ID = field.Value
			case "victor_answer_emoji_animated":
				if out.VictorEmoji == nil {
					out.VictorEmoji = &DiscordPollEmoji{}
				}
				out.VictorEmoji.Animated = strings.EqualFold(strings.TrimSpace(field.Value), "true")
			}
		}
		break
	}
	return out
}

func (d DiscordGoAPI) mentionNameMaps(guildID string, m *discordgo.Message) (users, channels, roles map[string]string) {
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
		users[user.ID] = d.userMentionName(guildID, user)
	}
	for _, ch := range m.MentionChannels {
		if ch == nil {
			continue
		}
		channels[ch.ID] = strings.TrimSpace(ch.Name)
	}
	for _, roleID := range m.MentionRoles {
		// Roles resolve through the session State cache only: the mapping
		// hot path never issues a REST round trip per mentioned role.
		if role, err := d.session.State.Role(guildID, roleID); err == nil && role != nil {
			roles[roleID] = strings.TrimSpace(role.Name)
		}
	}
	return users, channels, roles
}

// userDisplayName resolves a user's display name through the single policy:
// the server's member view first (member display name, then nickname), then
// the user's own profile (global name, then username). It never issues REST
// calls; the mapping hot path relies on the gateway member payload, the
// member cache, or the mention's State lookup.
func userDisplayName(member *discordgo.Member, user *discordgo.User) string {
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
	if user != nil {
		if name := strings.TrimSpace(user.DisplayName()); name != "" {
			return name
		}
		return strings.TrimSpace(user.Username)
	}
	return ""
}

// userMentionName names a mentioned user through the State cache; a mention
// whose member is unknown to State falls back to the user's own profile.
func (d DiscordGoAPI) userMentionName(guildID string, user *discordgo.User) string {
	if user == nil {
		return ""
	}
	if guildID == "" {
		return userDisplayName(nil, user)
	}
	if member, err := d.session.State.Member(guildID, user.ID); err == nil && member != nil {
		return userDisplayName(member, user)
	}
	return userDisplayName(nil, user)
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

func forwardedMessageFields(ref *discordgo.MessageReference, snapshots []discordgo.MessageSnapshot, snapshotDescriptions []string) (*DiscordForwardedMessage, error) {
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
	return &DiscordForwardedMessage{
		MessageID: ref.MessageID, ChannelID: ref.ChannelID, GuildID: ref.GuildID, Content: snapshot.Content,
		Attachments: attachmentsFromDiscord(snapshot.Attachments, snapshotDescriptions),
		Stickers:    stickersFromDiscord(snapshot.StickerItems),
	}, nil
}

func (d DiscordGoAPI) threadContext(channelID string) (string, string) {
	ch, err := d.session.State.Channel(channelID)
	if err != nil || ch == nil {
		ch, err = d.session.Channel(channelID)
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

func (d DiscordGoAPI) memberRoleColor(guildID string, member *discordgo.Member) int {
	if guildID == "" || member == nil || len(member.Roles) == 0 {
		return 0
	}
	guild, err := d.session.State.Guild(guildID)
	if err != nil {
		guild, err = d.session.Guild(guildID)
		if err != nil {
			return 0
		}
	}
	if guild == nil || len(guild.Roles) == 0 {
		return 0
	}
	roles := make([]GuildRole, 0, len(guild.Roles))
	for _, role := range guild.Roles {
		if role == nil {
			continue
		}
		roles = append(roles, GuildRole{ID: role.ID, Color: role.Color, Position: role.Position})
	}
	return HighestRoleColor(member.Roles, roles)
}
