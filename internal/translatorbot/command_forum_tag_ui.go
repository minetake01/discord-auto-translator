package translatorbot

import (
	"context"
	"log"
	"sort"
	"strings"

	"github.com/bwmarrin/discordgo"
)

const (
	forumTagUICustomPeer = "ftm:peer"
	forumTagUICustomSrc  = "ftm:src"
	forumTagUICustomDst  = "ftm:dst"
	forumTagUICustomSave = "ftm:save"
	forumTagUICustomDone = "ftm:done"
	forumTagUINoneValue  = "__none__"
	editForumTagsCommand = "edit-forum-tags"
)

type forumTagUISession struct {
	GuildID        string
	GroupID        string
	FocusChannelID string
	PeerChannelID  string
	FocusTagID     string
	PeerTagID      string
	UserID         string
	Locale         string
	Intro          string
	FocusTags      []discordgo.ForumTag
	PeerTagsByCh   map[string][]discordgo.ForumTag
	PeerChannelIDs []string
}

func (h *CommandHandler) putTagUI(messageID string, session *forumTagUISession) {
	h.tagUIMu.Lock()
	defer h.tagUIMu.Unlock()
	if h.tagUI == nil {
		h.tagUI = make(map[string]*forumTagUISession)
	}
	h.tagUI[messageID] = session
}

func (h *CommandHandler) getTagUI(messageID string) *forumTagUISession {
	h.tagUIMu.Lock()
	defer h.tagUIMu.Unlock()
	if h.tagUI == nil {
		return nil
	}
	return h.tagUI[messageID]
}

func (h *CommandHandler) deleteTagUI(messageID string) {
	h.tagUIMu.Lock()
	defer h.tagUIMu.Unlock()
	if h.tagUI == nil {
		return
	}
	delete(h.tagUI, messageID)
}

func (h *CommandHandler) buildForumTagUISession(ctx context.Context, guildID, groupID string, focus *discordgo.Channel, userID, locale, intro string) (*forumTagUISession, error) {
	if focus == nil || !isThreadOnlyChannelType(int(focus.Type)) || len(focus.AvailableTags) == 0 {
		return nil, nil
	}
	channels, err := h.store.ChannelsInGroup(ctx, guildID, groupID)
	if err != nil {
		return nil, err
	}
	peerIDs := make([]string, 0)
	peerTags := make(map[string][]discordgo.ForumTag)
	for _, c := range channels {
		if c.ChannelID == focus.ID {
			continue
		}
		ch, err := h.api.Channel(c.ChannelID)
		if err != nil {
			return nil, err
		}
		if ch == nil || len(ch.AvailableTags) == 0 {
			continue
		}
		peerIDs = append(peerIDs, c.ChannelID)
		peerTags[c.ChannelID] = append([]discordgo.ForumTag(nil), ch.AvailableTags...)
	}
	if len(peerIDs) == 0 {
		return nil, nil
	}
	session := &forumTagUISession{
		GuildID:        guildID,
		GroupID:        groupID,
		FocusChannelID: focus.ID,
		PeerChannelID:  peerIDs[0],
		UserID:         userID,
		Locale:         locale,
		Intro:          intro,
		FocusTags:      append([]discordgo.ForumTag(nil), focus.AvailableTags...),
		PeerTagsByCh:   peerTags,
		PeerChannelIDs: peerIDs,
	}
	return session, nil
}

func (h *CommandHandler) renderForumTagUI(session *forumTagUISession) (string, []discordgo.MessageComponent) {
	ctx := context.Background()
	pairs, err := h.store.ListForumTagMapsBetween(ctx, session.GuildID, session.GroupID, session.FocusChannelID, session.PeerChannelID)
	if err != nil {
		log.Printf("list forum tag maps: %v", err)
		pairs = nil
	}
	focusNames := forumTagNameByID(session.FocusTags)
	peerNames := forumTagNameByID(session.PeerTagsByCh[session.PeerChannelID])

	var b strings.Builder
	if session.Intro != "" {
		b.WriteString(session.Intro)
		b.WriteString("\n\n")
	}
	b.WriteString(localizedUIStringf(session.Locale, uiKeyForumTagMapHeader, session.FocusChannelID, session.PeerChannelID))
	b.WriteByte('\n')
	if len(pairs) == 0 {
		b.WriteString(localizedUIString(session.Locale, uiKeyForumTagMapNone))
	} else {
		sort.Slice(pairs, func(i, j int) bool {
			if pairs[i][0] == pairs[j][0] {
				return pairs[i][1] < pairs[j][1]
			}
			return pairs[i][0] < pairs[j][0]
		})
		for _, pair := range pairs {
			focusLabel := focusNames[pair[0]]
			if focusLabel == "" {
				focusLabel = pair[0]
			}
			peerLabel := peerNames[pair[1]]
			if peerLabel == "" {
				peerLabel = pair[1]
			}
			b.WriteString(localizedUIStringf(session.Locale, uiKeyForumTagMapLine, focusLabel, peerLabel))
			b.WriteByte('\n')
		}
	}
	components := make([]discordgo.MessageComponent, 0, 5)
	if len(session.PeerChannelIDs) > 1 {
		opts := make([]discordgo.SelectMenuOption, 0, len(session.PeerChannelIDs))
		for _, peerID := range session.PeerChannelIDs {
			opts = append(opts, discordgo.SelectMenuOption{
				Label:   truncateSelectLabel("#" + peerID),
				Value:   peerID,
				Default: peerID == session.PeerChannelID,
			})
		}
		components = append(components, discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.SelectMenu{
				CustomID:    forumTagUICustomPeer,
				Placeholder: localizedUIString(session.Locale, uiKeyForumTagMapSelectPeer),
				Options:     opts,
			},
		}})
	}
	focusOpts := make([]discordgo.SelectMenuOption, 0, len(session.FocusTags))
	for _, tag := range session.FocusTags {
		focusOpts = append(focusOpts, discordgo.SelectMenuOption{
			Label:   truncateSelectLabel(tag.Name),
			Value:   tag.ID,
			Default: tag.ID == session.FocusTagID,
		})
	}
	components = append(components, discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.SelectMenu{
			CustomID:    forumTagUICustomSrc,
			Placeholder: localizedUIString(session.Locale, uiKeyForumTagMapSelectFocusTag),
			Options:     focusOpts,
		},
	}})
	peerTags := session.PeerTagsByCh[session.PeerChannelID]
	peerOpts := make([]discordgo.SelectMenuOption, 0, len(peerTags)+1)
	peerOpts = append(peerOpts, discordgo.SelectMenuOption{
		Label:   truncateSelectLabel(localizedUIString(session.Locale, uiKeyForumTagMapNoMapping)),
		Value:   forumTagUINoneValue,
		Default: session.PeerTagID == forumTagUINoneValue,
	})
	for _, tag := range peerTags {
		peerOpts = append(peerOpts, discordgo.SelectMenuOption{
			Label:   truncateSelectLabel(tag.Name),
			Value:   tag.ID,
			Default: tag.ID == session.PeerTagID,
		})
	}
	components = append(components, discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.SelectMenu{
			CustomID:    forumTagUICustomDst,
			Placeholder: localizedUIString(session.Locale, uiKeyForumTagMapSelectPeerTag),
			Options:     peerOpts,
		},
	}})
	components = append(components, discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{
			CustomID: forumTagUICustomSave,
			Label:    localizedUIString(session.Locale, uiKeyForumTagMapSave),
			Style:    discordgo.PrimaryButton,
		},
		discordgo.Button{
			CustomID: forumTagUICustomDone,
			Label:    localizedUIString(session.Locale, uiKeyForumTagMapDone),
			Style:    discordgo.SecondaryButton,
		},
	}})
	return strings.TrimSpace(b.String()), components
}

func forumTagNameByID(tags []discordgo.ForumTag) map[string]string {
	out := make(map[string]string, len(tags))
	for _, tag := range tags {
		out[tag.ID] = tag.Name
	}
	return out
}

func truncateSelectLabel(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "-"
	}
	runes := []rune(s)
	if len(runes) <= 100 {
		return s
	}
	return string(runes[:97]) + "..."
}

func (h *CommandHandler) respondComponents(s *discordgo.Session, i *discordgo.InteractionCreate, content string, components []discordgo.MessageComponent) (*discordgo.Message, error) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content:    content,
			Flags:      discordgo.MessageFlagsEphemeral,
			Components: components,
		},
	})
	if err != nil {
		return nil, err
	}
	return s.InteractionResponse(i.Interaction)
}

func (h *CommandHandler) updateForumTagUIMessage(s *discordgo.Session, i *discordgo.InteractionCreate, session *forumTagUISession) {
	content, components := h.renderForumTagUI(session)
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    content,
			Components: components,
		},
	})
}

func (h *CommandHandler) openForumTagUI(s *discordgo.Session, i *discordgo.InteractionCreate, groupID string, focus *discordgo.Channel, intro string, respondFresh bool) {
	ctx := context.Background()
	locale := commandLocale(i)
	session, err := h.buildForumTagUISession(ctx, i.GuildID, groupID, focus, interactionUserID(i), locale, intro)
	if err != nil {
		log.Printf("forum tag ui build: %v", err)
		if respondFresh {
			h.reply(s, i, uiKeyUnexpectedError)
		}
		return
	}
	if session == nil {
		if respondFresh {
			if intro != "" {
				h.respond(s, i, intro, true)
				return
			}
			h.reply(s, i, uiKeyForumTagMapNoPeers)
		}
		return
	}
	content, components := h.renderForumTagUI(session)
	if respondFresh {
		msg, err := h.respondComponents(s, i, content, components)
		if err != nil {
			log.Printf("forum tag ui respond: %v", err)
			h.reply(s, i, uiKeyUnexpectedError)
			return
		}
		if msg != nil {
			h.putTagUI(msg.ID, session)
		}
		return
	}
}

func interactionUserID(i *discordgo.InteractionCreate) string {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID
	}
	if i.User != nil {
		return i.User.ID
	}
	return ""
}

func (h *CommandHandler) handleEditForumTags(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	ctx := context.Background()
	groupID := strings.TrimSpace(optionString(data.Options, "group"))
	if groupID == "" {
		h.reply(s, i, uiKeyGroupRequired)
		return
	}
	exists, err := h.store.GroupExists(ctx, i.GuildID, groupID)
	if err != nil {
		log.Printf("edit-forum-tags group exists: %v", err)
		h.reply(s, i, uiKeyUnexpectedError)
		return
	}
	if !exists {
		h.reply(s, i, uiKeyGroupNotFound, groupID)
		return
	}
	ch, ok := h.resolveCommandChannel(s, i, data)
	if !ok {
		return
	}
	if !isThreadOnlyChannelType(int(ch.Type)) {
		h.reply(s, i, uiKeyForumTagMapNeedForum)
		return
	}
	channels, err := h.store.ChannelsInGroup(ctx, i.GuildID, groupID)
	if err != nil {
		log.Printf("edit-forum-tags channels: %v", err)
		h.reply(s, i, uiKeyUnexpectedError)
		return
	}
	joined := false
	for _, c := range channels {
		if c.ChannelID == ch.ID {
			joined = true
			break
		}
	}
	if !joined {
		h.reply(s, i, uiKeyChannelNotJoined, groupID)
		return
	}
	h.openForumTagUI(s, i, groupID, ch, "", true)
}

func (h *CommandHandler) handleForumTagComponent(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Message == nil {
		return
	}
	data := i.MessageComponentData()
	session := h.getTagUI(i.Message.ID)
	if session == nil {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    localizedUIString(commandLocale(i), uiKeyForumTagMapFinished),
				Components: []discordgo.MessageComponent{},
			},
		})
		return
	}
	if session.UserID != "" && interactionUserID(i) != session.UserID {
		h.reply(s, i, uiKeyUnexpectedError)
		return
	}
	ctx := context.Background()
	switch data.CustomID {
	case forumTagUICustomPeer:
		if len(data.Values) != 1 {
			h.updateForumTagUIMessage(s, i, session)
			return
		}
		if _, ok := session.PeerTagsByCh[data.Values[0]]; !ok {
			h.updateForumTagUIMessage(s, i, session)
			return
		}
		session.PeerChannelID = data.Values[0]
		session.FocusTagID = ""
		session.PeerTagID = ""
		h.updateForumTagUIMessage(s, i, session)
	case forumTagUICustomSrc:
		if len(data.Values) == 1 {
			session.FocusTagID = data.Values[0]
		}
		h.updateForumTagUIMessage(s, i, session)
	case forumTagUICustomDst:
		if len(data.Values) == 1 {
			session.PeerTagID = data.Values[0]
		}
		h.updateForumTagUIMessage(s, i, session)
	case forumTagUICustomSave:
		if session.FocusTagID == "" || session.PeerTagID == "" {
			h.updateForumTagUIMessage(s, i, session)
			return
		}
		if session.PeerTagID == forumTagUINoneValue {
			if err := h.store.DeleteForumTagMap(ctx, session.GuildID, session.GroupID, session.FocusChannelID, session.FocusTagID, session.PeerChannelID); err != nil {
				log.Printf("delete forum tag map: %v", err)
				_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseUpdateMessage,
					Data: &discordgo.InteractionResponseData{
						Content:    localizedUIString(session.Locale, uiKeyUnexpectedError),
						Components: []discordgo.MessageComponent{},
					},
				})
				return
			}
		} else {
			if err := h.store.UpsertForumTagMap(ctx, session.GuildID, session.GroupID, session.FocusChannelID, session.FocusTagID, session.PeerChannelID, session.PeerTagID); err != nil {
				log.Printf("upsert forum tag map: %v", err)
				_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseUpdateMessage,
					Data: &discordgo.InteractionResponseData{
						Content:    localizedUIString(session.Locale, uiKeyUnexpectedError),
						Components: []discordgo.MessageComponent{},
					},
				})
				return
			}
		}
		h.updateForumTagUIMessage(s, i, session)
	case forumTagUICustomDone:
		h.deleteTagUI(i.Message.ID)
		content := session.Intro
		if content != "" {
			content += "\n\n"
		}
		content += localizedUIString(session.Locale, uiKeyForumTagMapFinished)
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    content,
				Components: []discordgo.MessageComponent{},
			},
		})
	default:
		log.Printf("unknown forum tag ui custom_id: %s", data.CustomID)
	}
}

func (h *CommandHandler) maybeOpenForumTagUIAfterJoin(s *discordgo.Session, i *discordgo.InteractionCreate, groupID string, ch *discordgo.Channel, language string) {
	intro := localizedUIStringf(commandLocale(i), uiKeyChannelJoined, groupID, ch.ID, language)
	if !isThreadOnlyChannelType(int(ch.Type)) {
		h.respond(s, i, intro, true)
		return
	}
	h.openForumTagUI(s, i, groupID, ch, intro, true)
}
