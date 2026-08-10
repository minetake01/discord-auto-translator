package translatorbot

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
)

type threadCreateRequest struct {
	GuildID                 string
	SourceChannelID         string
	SourceThreadID          string
	SourceMessageID         string
	Name                    string
	AppliedTags             []string
	InitialMessageID        string
	InitialMessageAuthor    string
	InitialMessageUsername  string
	InitialMessageRoleColor int
	InitialMessageText      string
	InitialMessagePoll      *DiscordPoll
	InitialMessageFiles     []DiscordAttachment
	InitialMessageStickers  []DiscordSticker
	DeferWithoutSourceMsg   bool
}

func (s *Service) SyncThreadCreate(ctx context.Context, guildID, sourceChannelID, sourceThreadID, name string, appliedTags []string) error {
	_, err := s.syncThreadCreate(ctx, threadCreateRequest{
		GuildID:         guildID,
		SourceChannelID: sourceChannelID,
		SourceThreadID:  sourceThreadID,
		SourceMessageID: sourceThreadID,
		Name:            name,
		AppliedTags:     appliedTags,
	})
	return err
}

func (s *Service) SyncThreadCreateFromGateway(ctx context.Context, guildID, sourceChannelID, sourceThreadID, name string, appliedTags []string) error {
	_, err := s.syncThreadCreate(ctx, threadCreateRequest{
		GuildID:               guildID,
		SourceChannelID:       sourceChannelID,
		SourceThreadID:        sourceThreadID,
		SourceMessageID:       sourceThreadID,
		Name:                  name,
		AppliedTags:           appliedTags,
		DeferWithoutSourceMsg: true,
	})
	return err
}

// syncThreadCreate mirrors a newly created thread to every peer channel,
// translating the thread name and initial message together when present.
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

// createThreadForTarget translates the thread name and initial message in one
// call for one target channel, creates the thread there, and records the links.
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
	threadTranslations, err := s.translateThreadCreateWithLimit(ctx, req.GuildID, req.Name, req.InitialMessageText, languages, contextFn)
	if err != nil {
		return false, err
	}
	translated := threadTranslations[target.Language]
	translatedName := translated.Name
	translatedInitial := s.postProcessContent(ctx, req.GuildID, translated.Message, target.Language)

	var embeds []*discordgo.MessageEmbed
	snapshot := req.InitialMessageText
	if req.InitialMessagePoll != nil {
		question := strings.TrimSpace(req.InitialMessagePoll.Question)
		answers := pollAnswerTexts(req.InitialMessagePoll)
		pollTranslations, err := s.translatePollWithLimit(ctx, req.GuildID, question, answers, languages, contextFn)
		if err != nil {
			return false, err
		}
		poll := pollTranslations[target.Language]
		translatedQuestion := s.postProcessContent(ctx, req.GuildID, poll.Question, target.Language)
		translatedAnswers := make([]string, len(poll.Answers))
		for i, answer := range poll.Answers {
			translatedAnswers[i] = s.postProcessContent(ctx, req.GuildID, answer, target.Language)
		}
		if embed := buildPollEmbed(translatedQuestion, formatTranslatedPollAnswers(req.InitialMessagePoll, translatedAnswers), req.InitialMessageRoleColor); embed != nil {
			embeds = []*discordgo.MessageEmbed{embed}
		}
		pollSnapshot := formatPollSnapshot(req.InitialMessagePoll)
		if strings.TrimSpace(snapshot) != "" && pollSnapshot != "" {
			snapshot = strings.TrimSpace(snapshot) + "\n\n" + pollSnapshot
		} else if pollSnapshot != "" {
			snapshot = pollSnapshot
		}
		translatedInitial = withPollStartedHeader(translatedInitial, target.Language, req.GuildID, req.SourceThreadID, req.InitialMessageID, true)
	}

	threadID, initialMessageID, err := s.createTargetThread(ctx, source.GroupID, req, target, translatedName, translatedInitial, embeds)
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

	if initialMessageID != "" {
		synced, err := s.targetAlreadySynced(ctx, req.SourceThreadID, req.InitialMessageID, threadID)
		if err != nil {
			return false, err
		}
		if !synced {
			if err := s.store.SaveMessageLink(ctx, MessageLink{
				SourceMessageID: req.InitialMessageID, SourceChannelID: req.SourceThreadID, GroupID: source.GroupID,
				TargetChannelID: threadID, TargetMessageID: initialMessageID, TargetLanguage: target.Language,
				SourceAuthorID: req.InitialMessageAuthor, SourceAuthorDisplayName: req.InitialMessageUsername, SourceContentSnapshot: snapshot,
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
	if ch, err := s.discord.Channel(m.ChannelID); err == nil && ch != nil {
		req.AppliedTags = append([]string(nil), ch.AppliedTags...)
	}
	if m.ThreadStarterMessage {
		req.SourceMessageID = m.ReferencedMessageID
		req.DeferWithoutSourceMsg = true
	} else if isThreadOnlySourceMessage(ctx, s.store, m.GuildID, m.ParentChannelID, m.ID, m.ChannelID) {
		req.InitialMessageID = m.ID
		req.InitialMessageAuthor = m.AuthorID
		req.InitialMessageUsername = m.AuthorDisplayName
		req.InitialMessageRoleColor = m.AuthorRoleColor
		req.InitialMessageText = m.Content
		req.InitialMessagePoll = m.Poll
		req.InitialMessageFiles = m.Attachments
		req.InitialMessageStickers = m.Stickers
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

func (s *Service) SyncThreadUpdate(ctx context.Context, guildID, sourceThreadID, name string, appliedTags []string, nameChanged, tagsChanged bool) error {
	if !nameChanged && !tagsChanged {
		return nil
	}
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
		var translations MultiTranslationResult
		if nameChanged && strings.TrimSpace(name) != "" {
			contextFn := func() TranslationContext {
				return TranslationContext{GuildID: guildID, MessageID: sourceThreadID, StyleInstructions: s.groupStyleInstructions(ctx, guildID, groupID)}
			}
			languages := make([]string, 0, len(pending))
			for _, p := range pending {
				languages = append(languages, p.target.Language)
			}
			translations, err = s.translateWithLimit(ctx, guildID, name, nil, languages, contextFn)
			if err != nil {
				if errors.Is(err, errTranslationRateLimited) {
					translations = MultiTranslationResult{}
				} else {
					return err
				}
			}
		}
		for _, p := range pending {
			editName := ""
			if translations.Translations != nil {
				editName = translations.Translations[p.target.Language]
			}
			var editTags *[]string
			if tagsChanged && isThreadOnlyChannelType(p.target.ChannelType) {
				mapping, err := s.store.ForumTagMapsBetween(ctx, guildID, groupID, p.thread.SourceChannelID, p.target.ChannelID)
				if err != nil {
					return err
				}
				mapped := MapAppliedForumTags(mapping, appliedTags)
				targetThread, err := s.discord.Channel(p.thread.TargetThreadID)
				if err != nil {
					return err
				}
				if ForumTagSetsEqual(targetThread.AppliedTags, mapped) {
					mapped = nil
					editTags = nil
				} else {
					tags := mapped
					if tags == nil {
						tags = []string{}
					}
					editTags = &tags
				}
			}
			if editName == "" && editTags == nil {
				continue
			}
			if err := s.discord.EditThread(p.thread.TargetThreadID, editName, editTags); err != nil {
				return err
			}
		}
	}
	return nil
}

func ForumTagSetsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, id := range a {
		counts[id]++
	}
	for _, id := range b {
		counts[id]--
		if counts[id] < 0 {
			return false
		}
	}
	return true
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
// message. For text/news targets it is attached to the mirrored source
// message when one exists, or deferred when DeferWithoutSourceMsg is set.
func (s *Service) createTargetThread(ctx context.Context, groupID string, req threadCreateRequest, target GroupChannel, name, initialMessage string, embeds []*discordgo.MessageEmbed) (string, string, error) {
	if isThreadOnlyChannelType(target.ChannelType) {
		content, err := messageContentWithAssetURLs(initialMessage, req.InitialMessageFiles, req.InitialMessageStickers)
		if err != nil {
			return "", "", err
		}
		loaded, err := s.downloadImageOriginals(ctx, imageAttachmentsOnly(req.InitialMessageFiles))
		if err != nil {
			return "", "", err
		}
		files := webhookFilesForImages(loaded, sourceImageDescriptions(req.InitialMessageFiles))
		if content == "" && len(embeds) == 0 && len(files) == 0 {
			if req.DeferWithoutSourceMsg {
				return "", "", nil
			}
			content = name
		}
		appliedTags, err := s.mappedAppliedTagsForTarget(ctx, req.GuildID, groupID, req.SourceChannelID, target, req.AppliedTags)
		if err != nil {
			return "", "", err
		}
		return s.discord.CreateThread(target.ChannelID, target.ChannelType, name, content, embeds, appliedTags, files)
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
		threadID, _, err := s.discord.CreateThread(target.ChannelID, target.ChannelType, name, "", nil, nil, nil)
	return threadID, "", err
}

func (s *Service) mappedAppliedTagsForTarget(ctx context.Context, guildID, groupID, sourceChannelID string, target GroupChannel, sourceApplied []string) ([]string, error) {
	mapping, err := s.store.ForumTagMapsBetween(ctx, guildID, groupID, sourceChannelID, target.ChannelID)
	if err != nil {
		return nil, err
	}
	mapped := MapAppliedForumTags(mapping, sourceApplied)
	parent, err := s.discord.Channel(target.ChannelID)
	if err != nil {
		return nil, fmt.Errorf("fetch target forum channel %s: %w", target.ChannelID, err)
	}
	if parent.Flags&discordgo.ChannelFlagRequireTag != 0 && len(mapped) == 0 {
		return nil, fmt.Errorf("forum channel %s requires tags but none are mapped from source tags %v", target.ChannelID, sourceApplied)
	}
	return mapped, nil
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
