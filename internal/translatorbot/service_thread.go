package translatorbot

import (
	"context"
	"errors"
	"fmt"
)

type threadCreateRequest struct {
	GuildID                 string
	SourceChannelID         string
	SourceThreadID          string
	SourceMessageID         string
	Name                    string
	InitialMessageID        string
	InitialMessageAuthor    string
	InitialMessageUsername  string
	InitialMessageAvatar    string
	InitialMessageRoleColor int
	InitialMessageText      string
	InitialMessageFiles     []DiscordAttachment
	InitialMessageStickers  []DiscordSticker
	InitialMessageTTS       bool
	DeferWithoutSourceMsg   bool
}

func (s *Service) SyncThreadCreate(ctx context.Context, guildID, sourceChannelID, sourceThreadID, name string) error {
	_, err := s.syncThreadCreate(ctx, threadCreateRequest{
		GuildID:         guildID,
		SourceChannelID: sourceChannelID,
		SourceThreadID:  sourceThreadID,
		SourceMessageID: sourceThreadID,
		Name:            name,
	})
	return err
}

func (s *Service) SyncThreadCreateFromGateway(ctx context.Context, guildID, sourceChannelID, sourceThreadID, name string) error {
	_, err := s.syncThreadCreate(ctx, threadCreateRequest{
		GuildID:               guildID,
		SourceChannelID:       sourceChannelID,
		SourceThreadID:        sourceThreadID,
		SourceMessageID:       sourceThreadID,
		Name:                  name,
		DeferWithoutSourceMsg: true,
	})
	return err
}

// syncThreadCreate mirrors a newly created thread to every peer channel,
// translating the thread name and, when present, the initial message.
// Returns whether any target thread was created together with its initial
// message so the caller can skip mirroring that message again.
func (s *Service) syncThreadCreate(ctx context.Context, req threadCreateRequest) (bool, error) {
	s.threadMu.Lock()
	defer s.threadMu.Unlock()

	groups, err := s.store.ChannelsByChannel(ctx, req.GuildID, req.SourceChannelID)
	if err != nil {
		return false, err
	}
	existing, err := s.store.SourceThreadTargets(ctx, req.SourceThreadID)
	if err != nil {
		return false, err
	}
	createdWithInitialMessage := false
	for _, source := range groups {
		channels, err := s.store.ChannelsInGroup(ctx, req.GuildID, source.GroupID)
		if err != nil {
			return false, err
		}
		for _, target := range channels {
			if target.ChannelID == source.ChannelID {
				continue
			}
			if existingThreadTarget(existing, source.GroupID, target.ChannelID) {
				continue
			}
			created, err := s.createThreadForTarget(ctx, req, source, target)
			if err != nil {
				return false, err
			}
			createdWithInitialMessage = createdWithInitialMessage || created
		}
	}
	return createdWithInitialMessage, nil
}

// createThreadForTarget translates the thread name and initial message for
// one target channel, creates the thread there, and records the links.
func (s *Service) createThreadForTarget(ctx context.Context, req threadCreateRequest, source, target GroupChannel) (bool, error) {
	languages := []string{target.Language}
	contextFn := func() TranslationContext {
		messageID := req.InitialMessageID
		if messageID == "" {
			messageID = req.SourceMessageID
		}
		if messageID == "" {
			messageID = req.SourceThreadID
		}
		return s.groupTranslationContext(ctx, req.GuildID, source.GroupID, req.SourceChannelID, req.SourceThreadID, source.Language, messageID, "", "", req.InitialMessageUsername, req.Name)
	}
	nameTranslations, err := s.translateWithLimit(ctx, req.GuildID, req.Name, languages, contextFn)
	if err != nil {
		return false, err
	}
	translatedName := nameTranslations[target.Language]

	initialTranslations, err := s.translateWithLimit(ctx, req.GuildID, req.InitialMessageText, languages, contextFn)
	if err != nil {
		return false, err
	}
	translatedInitial := s.postProcessContent(ctx, req.GuildID, initialTranslations[target.Language], target.Language)

	threadID, initialMessageID, err := s.createTargetThread(ctx, source.GroupID, req, target, translatedName, translatedInitial)
	if err != nil {
		return false, err
	}
	if threadID == "" {
		return false, nil
	}
	err = s.store.SaveThreadLink(ctx, ThreadLink{
		GroupID: source.GroupID, SourceThreadID: req.SourceThreadID, SourceChannelID: req.SourceChannelID,
		TargetThreadID: threadID, TargetChannelID: target.ChannelID, TargetLanguage: target.Language,
	})
	if err != nil {
		return false, err
	}
	if req.InitialMessageID == "" {
		return false, nil
	}

	if initialMessageID == "" && (translatedInitial != "" || len(req.InitialMessageFiles) > 0 || len(req.InitialMessageStickers) > 0) {
		synced, err := s.targetAlreadySynced(ctx, req.SourceThreadID, req.InitialMessageID, threadID)
		if err != nil {
			return false, err
		}
		if !synced {
			content, err := messageContentWithAssetURLs(translatedInitial, req.InitialMessageFiles, req.InitialMessageStickers)
			if err != nil {
				return false, err
			}
			avatar := AvatarWithLanguageBadge(ctx, s.publicBaseURL, req.InitialMessageAvatar, target.Language, req.InitialMessageRoleColor)
			if err := s.sendAndSaveLink(ctx, target, threadID, WebhookSend{
				Content: content, Username: req.InitialMessageUsername, AvatarURL: avatar, TTS: req.InitialMessageTTS, ThreadID: threadID,
			}, MessageLink{
				SourceMessageID: req.InitialMessageID, SourceChannelID: req.SourceThreadID, GroupID: source.GroupID,
				TargetChannelID: threadID, TargetLanguage: target.Language,
				SourceAuthorID: req.InitialMessageAuthor, SourceAuthorDisplayName: req.InitialMessageUsername, SourceContentSnapshot: req.InitialMessageText,
			}); err != nil {
				return false, err
			}
		}
		return true, nil
	}
	if initialMessageID != "" {
		synced, err := s.targetAlreadySynced(ctx, req.SourceThreadID, req.InitialMessageID, threadID)
		if err != nil {
			return false, err
		}
		if !synced {
			if err := s.store.SaveMessageLink(ctx, MessageLink{
				SourceMessageID: req.InitialMessageID, SourceChannelID: req.SourceThreadID, GroupID: source.GroupID,
				TargetChannelID: threadID, TargetMessageID: initialMessageID, TargetLanguage: target.Language,
				SourceAuthorID: req.InitialMessageAuthor, SourceAuthorDisplayName: req.InitialMessageUsername, SourceContentSnapshot: req.InitialMessageText,
			}); err != nil {
				return false, err
			}
			_, _ = s.store.MarkProcessed(ctx, messageLinkProcessedKey(req.SourceThreadID, req.InitialMessageID, threadID))
		}
		return true, nil
	}
	return false, nil
}

func existingThreadTarget(links []ThreadLink, groupID, targetChannelID string) bool {
	for _, link := range links {
		if link.GroupID == groupID && link.TargetChannelID == targetChannelID {
			return true
		}
	}
	return false
}

// ensureThreadSynced creates peer threads for a message that arrives inside
// a not-yet-synced thread. Returns whether the thread was created together
// with this message as its initial message.
func (s *Service) ensureThreadSynced(ctx context.Context, m DiscordMessage) (bool, error) {
	if m.ParentChannelID == "" || m.ThreadName == "" {
		return false, nil
	}
	if existing, err := s.store.SourceThreadTargets(ctx, m.ChannelID); err != nil {
		return false, err
	} else if len(existing) > 0 {
		return false, nil
	}
	req := threadCreateRequest{
		GuildID:         m.GuildID,
		SourceChannelID: m.ParentChannelID,
		SourceThreadID:  m.ChannelID,
		Name:            m.ThreadName,
	}
	if m.ThreadStarterMessage {
		req.SourceMessageID = m.ReferencedMessageID
		req.DeferWithoutSourceMsg = true
	} else if isThreadOnlySourceMessage(ctx, s.store, m.GuildID, m.ParentChannelID, m.ID, m.ChannelID) {
		req.InitialMessageID = m.ID
		req.InitialMessageAuthor = m.AuthorID
		req.InitialMessageUsername = m.AuthorDisplayName
		req.InitialMessageAvatar = m.AuthorAvatarURL
		req.InitialMessageRoleColor = m.AuthorRoleColor
		req.InitialMessageText = m.Content
		req.InitialMessageFiles = m.Attachments
		req.InitialMessageStickers = m.Stickers
		req.InitialMessageTTS = m.TTS
	} else {
		req.SourceMessageID = m.ChannelID
	}
	return s.syncThreadCreate(ctx, req)
}

func (s *Service) handleThreadMessageCreate(ctx context.Context, m DiscordMessage) error {
	threads, err := s.store.ThreadTargets(ctx, m.ChannelID)
	if err != nil {
		return err
	}
	var errs []error
	for _, thread := range threads {
		if err := s.mirrorThreadMessage(ctx, m, thread); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) mirrorThreadMessage(ctx context.Context, m DiscordMessage, thread ThreadLink) error {
	targets, err := s.store.ChannelsInGroup(ctx, m.GuildID, thread.GroupID)
	if err != nil {
		return err
	}
	target := findChannel(targets, thread.TargetChannelID)
	if target == nil {
		return nil
	}
	synced, err := s.targetAlreadySynced(ctx, m.ChannelID, m.ID, thread.TargetThreadID)
	if err != nil {
		return fmt.Errorf("target %s: %w", thread.TargetThreadID, err)
	}
	if synced {
		return nil
	}
	sourceLanguage := languageForChannel(targets, thread.SourceChannelID)
	contextFn := func() TranslationContext {
		replyChannelID := m.ReferencedMessageChannelID
		if replyChannelID == "" {
			replyChannelID = m.ChannelID
		}
		tc := s.groupTranslationContext(ctx, m.GuildID, thread.GroupID, thread.SourceChannelID, m.ChannelID, sourceLanguage, m.ID, replyChannelID, m.ReferencedMessageID, m.AuthorDisplayName, s.resolveThreadName(m))
		tc.MentionedUsers = m.MentionedUsers
		tc.MentionedChannels = m.MentionedChannels
		tc.MentionedRoles = m.MentionedRoles
		return tc
	}
	dests := []mirrorDestination{destinationForThread(*target, thread.TargetThreadID)}
	return s.mirrorMessage(ctx, m, thread.GroupID, sourceLanguage, contextFn, dests)
}

type pendingThreadEdit struct {
	thread ThreadLink
	target GroupChannel
}

func (s *Service) SyncThreadUpdate(ctx context.Context, guildID, sourceThreadID, name string) error {
	threads, err := s.store.SourceThreadTargets(ctx, sourceThreadID)
	if err != nil {
		return err
	}
	byGroup := make(map[string][]ThreadLink)
	for _, thread := range threads {
		byGroup[thread.GroupID] = append(byGroup[thread.GroupID], thread)
	}
	for groupID, groupThreads := range byGroup {
		targets, err := s.store.ChannelsInGroup(ctx, guildID, groupID)
		if err != nil {
			return err
		}
		pending := make([]pendingThreadEdit, 0, len(groupThreads))
		for _, thread := range groupThreads {
			target := findChannel(targets, thread.TargetChannelID)
			if target == nil {
				continue
			}
			pending = append(pending, pendingThreadEdit{thread: thread, target: *target})
		}
		if len(pending) == 0 {
			continue
		}
		contextFn := func() TranslationContext {
			return TranslationContext{GuildID: guildID, MessageID: sourceThreadID, StyleInstructions: s.groupStyleInstructions(ctx, guildID, groupID)}
		}
		languages := make([]string, 0, len(pending))
		for _, p := range pending {
			languages = append(languages, p.target.Language)
		}
		translations, err := s.translateWithLimit(ctx, guildID, name, languages, contextFn)
		if err != nil {
			if errors.Is(err, errTranslationRateLimited) {
				continue
			}
			return err
		}
		for _, p := range pending {
			if err := s.discord.EditThread(p.thread.TargetThreadID, translations[p.target.Language]); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) SyncThreadDelete(ctx context.Context, sourceThreadID string) error {
	threads, err := s.store.SourceThreadTargets(ctx, sourceThreadID)
	if err != nil {
		return err
	}
	for _, thread := range threads {
		if err := s.discord.DeleteThread(thread.TargetThreadID); err != nil {
			return err
		}
	}
	if err := s.store.DeleteMessageLinksByChannel(ctx, sourceThreadID); err != nil {
		return err
	}
	for _, thread := range threads {
		if err := s.store.DeleteMessageLinksByChannel(ctx, thread.TargetThreadID); err != nil {
			return err
		}
	}
	return s.store.DeleteThreadLinks(ctx, sourceThreadID)
}

// createTargetThread creates the mirrored thread in one target channel.
// For forum/media targets the thread starts with the translated initial
// message; for text targets it is attached to the mirrored source message
// when one exists, or deferred when DeferWithoutSourceMsg is set.
func (s *Service) createTargetThread(ctx context.Context, groupID string, req threadCreateRequest, target GroupChannel, name, initialMessage string) (string, string, error) {
	if isThreadOnlyChannelType(target.ChannelType) {
		content, err := messageContentWithAssetURLs(initialMessage, req.InitialMessageFiles, req.InitialMessageStickers)
		if err != nil {
			return "", "", err
		}
		if content == "" {
			if req.DeferWithoutSourceMsg {
				return "", "", nil
			}
			content = name
		}
		return s.discord.CreateThread(target.ChannelID, target.ChannelType, name, content)
	}
	if req.SourceMessageID != "" {
		links, err := s.store.MessagePeers(ctx, req.SourceChannelID, req.SourceMessageID)
		if err != nil {
			return "", "", err
		}
		for _, link := range links {
			if link.GroupID == groupID && link.TargetChannelID == target.ChannelID {
				threadID, err := s.discord.CreateThreadFromMessage(target.ChannelID, link.TargetMessageID, name)
				return threadID, "", err
			}
		}
		if req.DeferWithoutSourceMsg {
			return "", "", nil
		}
	}
	threadID, _, err := s.discord.CreateThread(target.ChannelID, target.ChannelType, name, "")
	return threadID, "", err
}

func isThreadOnlySourceMessage(ctx context.Context, store *Store, guildID, parentChannelID, messageID, threadID string) bool {
	if messageID == "" || messageID != threadID {
		return false
	}
	groups, err := store.ChannelsByChannel(ctx, guildID, parentChannelID)
	if err != nil {
		return false
	}
	for _, group := range groups {
		if isThreadOnlyChannelType(group.ChannelType) {
			return true
		}
	}
	return false
}
