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
	InitialMessageAvatarURL string
	InitialMessageRoleColor int
	InitialMessageText      string
	InitialMessagePoll      *DiscordPoll
	InitialMessageFiles     []DiscordAttachment
	InitialMessageStickers  []DiscordSticker
	DeferWithoutSourceMsg   bool
	// OnlyTargetChannelID limits creation to one target channel (backfill).
	OnlyTargetChannelID string
	// Timestamp puts the source posting time on the initial message.
	Timestamp bool
}

func (s *Service) SyncThreadCreate(ctx context.Context, guildID, sourceChannelID, sourceThreadID, name string, appliedTags []string) error {
	_, err := s.syncThreadCreate(ctx, threadCreateFromEvent(guildID, sourceChannelID, sourceThreadID, name, appliedTags, false), liveTranslation)
	return err
}

func (s *Service) SyncThreadCreateFromGateway(ctx context.Context, guildID, sourceChannelID, sourceThreadID, name string, appliedTags []string) error {
	_, err := s.syncThreadCreate(ctx, threadCreateFromEvent(guildID, sourceChannelID, sourceThreadID, name, appliedTags, true), liveTranslation)
	return err
}

// threadCreateFromEvent builds the request for a thread created in Discord.
// A gateway event can arrive before the parent message is linked and before a
// forum post has its initial message, so that path defers instead.
func threadCreateFromEvent(guildID, sourceChannelID, sourceThreadID, name string, appliedTags []string, fromGateway bool) threadCreateRequest {
	return threadCreateRequest{
		GuildID:               guildID,
		SourceChannelID:       sourceChannelID,
		SourceThreadID:        sourceThreadID,
		SourceMessageID:       sourceThreadID,
		Name:                  name,
		AppliedTags:           appliedTags,
		DeferWithoutSourceMsg: fromGateway,
	}
}

// syncThreadCreate mirrors a newly created thread to every peer channel,
// translating the thread name and initial message together when present.
// Returns whether any target thread was created together with its initial
// message so the caller can skip mirroring that message again.
func (s *Service) syncThreadCreate(ctx context.Context, req threadCreateRequest, scope translationScope) (bool, error) {
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
			if req.OnlyTargetChannelID != "" && target.ChannelID != req.OnlyTargetChannelID {
				continue
			}
			if existingThreadTarget(existing, source.GroupID, target.ChannelID) {
				continue
			}
			held, err := s.liveTargetHeld(ctx, source.GroupID, target.ChannelID, scope)
			if err != nil {
				return false, err
			}
			if held {
				continue
			}
			created, err := s.createThreadForTarget(ctx, req, source, target, scope)
			if err != nil {
				return false, err
			}
			createdWithInitialMessage = createdWithInitialMessage || created
		}
	}
	return createdWithInitialMessage, nil
}

type targetThreadAction int

const (
	targetThreadDefer targetThreadAction = iota
	targetThreadFromMessage
	targetThreadStandalone
	targetThreadForum
)

type targetThreadPlan struct {
	action        targetThreadAction
	fromMessageID string
}

// createThreadForTarget translates the thread name and initial message in one
// call for one target channel, creates the thread there, and records the links.
func (s *Service) createThreadForTarget(ctx context.Context, req threadCreateRequest, source, target GroupChannel, scope translationScope) (bool, error) {
	plan, err := s.resolveTargetThreadPlan(ctx, source.GroupID, req, target)
	if err != nil {
		return false, err
	}
	if plan.action == targetThreadDefer {
		return false, nil
	}

	languages := []string{target.Language}
	contextFn := func() TranslationContext {
		messageID := req.InitialMessageID
		if messageID == "" {
			messageID = req.SourceMessageID
		}
		if messageID == "" {
			messageID = req.SourceThreadID
		}
		return s.groupTranslationContext(ctx, translationContextRequest{
			guildID:          req.GuildID,
			groupID:          source.GroupID,
			contextChannelID: req.SourceChannelID,
			historyChannelID: req.SourceThreadID,
			excludeMessageID: messageID,
			author:           req.InitialMessageUsername,
			threadName:       req.Name,
		}, scope)
	}
	threadTranslations, err := s.translateThreadCreateWithLimit(ctx, req.GuildID, req.Name, req.InitialMessageText, languages, contextFn, scope)
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
		pollTranslations, err := s.translatePollWithLimit(ctx, req.GuildID, question, answers, languages, contextFn, scope)
		if err != nil {
			return false, err
		}
		embeds = s.translatedPollEmbeds(ctx, req.GuildID, req.InitialMessagePoll, pollTranslations[target.Language], target.Language, req.InitialMessageRoleColor)
		snapshot = pollMessageSnapshot(snapshot, req.InitialMessagePoll)
		if err := s.saveTranslatedPolls(ctx, req.SourceThreadID, req.InitialMessageID, req.InitialMessagePoll.Expiry, pollTranslations); err != nil {
			return false, err
		}
		translatedInitial = withPollStartedHeader(translatedInitial, target.Language, req.GuildID, req.SourceThreadID, req.InitialMessageID, true)
	}

	threadID, initialMessageID, mirrorBody, continuationIDs, err := s.createTargetThread(ctx, source.GroupID, req, target, translatedName, translatedInitial, embeds, plan, scope)
	if err != nil {
		return false, err
	}
	if threadID == "" {
		return false, fmt.Errorf("create target thread in %s: empty thread id", target.ChannelID)
	}
	err = s.store.SaveThreadLink(ctx, ThreadLink{
		GroupID: source.GroupID, SourceThreadID: req.SourceThreadID, SourceChannelID: req.SourceChannelID,
		TargetThreadID: threadID, TargetChannelID: target.ChannelID, TargetLanguage: target.Language,
	})
	if err != nil {
		// The peer thread exists on Discord but was never linked, so nothing
		// would ever sync or clean it up; delete it the way sendAndSaveLink
		// deletes a webhook message whose link could not be saved.
		_ = s.discord.DeleteThread(threadID)
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
				TargetChannelID: threadID, TargetMessageID: initialMessageID, ContinuationMessageIDs: continuationIDs, TargetLanguage: target.Language,
				SourceAuthorID: req.InitialMessageAuthor, SourceAuthorDisplayName: req.InitialMessageUsername,
				SourceAuthorAvatarURL: req.InitialMessageAvatarURL, SourceAuthorRoleColor: req.InitialMessageRoleColor,
				SourceContentSnapshot:  snapshot,
				SourceImageAttachments: imageAttachmentsOnly(req.InitialMessageFiles), TimestampLine: req.Timestamp,
				MirrorBody: mirrorBody,
			}); err != nil {
				return false, err
			}
		}
		return true, nil
	}
	return false, nil
}

func threadCreateHasInitialPayload(req threadCreateRequest) bool {
	return !DiscordMessage{
		Content:     req.InitialMessageText,
		Attachments: req.InitialMessageFiles,
		Stickers:    req.InitialMessageStickers,
		Poll:        req.InitialMessagePoll,
	}.hasNoPayload()
}

// resolveTargetThreadPlan decides how to create the mirrored thread before any
// translation. Gateway THREAD_CREATE defers when the parent message link is
// missing or a forum/media post has no initial payload yet.
func (s *Service) resolveTargetThreadPlan(ctx context.Context, groupID string, req threadCreateRequest, target GroupChannel) (targetThreadPlan, error) {
	if isThreadOnlyChannelType(target.ChannelType) {
		if req.DeferWithoutSourceMsg && !threadCreateHasInitialPayload(req) {
			return targetThreadPlan{action: targetThreadDefer}, nil
		}
		return targetThreadPlan{action: targetThreadForum}, nil
	}
	if req.SourceMessageID != "" {
		links, err := s.store.MessagePeers(ctx, req.SourceChannelID, req.SourceMessageID)
		if err != nil {
			return targetThreadPlan{}, err
		}
		for _, link := range links {
			if link.GroupID == groupID && link.TargetChannelID == target.ChannelID {
				return targetThreadPlan{action: targetThreadFromMessage, fromMessageID: link.TargetMessageID}, nil
			}
		}
		if req.DeferWithoutSourceMsg {
			return targetThreadPlan{action: targetThreadDefer}, nil
		}
	}
	return targetThreadPlan{action: targetThreadStandalone}, nil
}

// createTargetThread creates the mirrored thread in one target channel.
// For forum/media targets the thread starts with the translated initial
// message. For text/news targets it is attached to the mirrored source
// message when one exists. The returned mirror body is the body the initial
// message records in its link, and the continuation IDs are the messages
// that carry it beyond the Discord content limit.
func (s *Service) createTargetThread(ctx context.Context, groupID string, req threadCreateRequest, target GroupChannel, name, initialMessage string, embeds []*discordgo.MessageEmbed, plan targetThreadPlan, scope translationScope) (string, string, string, []string, error) {
	switch plan.action {
	case targetThreadForum:
		content, err := messageContentWithAssetURLs(initialMessage, req.InitialMessageFiles, req.InitialMessageStickers)
		if err != nil {
			return "", "", "", nil, err
		}
		loaded := s.loadImageAttachments(ctx, imageAttachmentsOnly(req.InitialMessageFiles))
		content, err = messageContentWithAllAssetURLs(content, imagesNotReuploaded(req.InitialMessageFiles, loaded), nil)
		if err != nil {
			return "", "", "", nil, err
		}
		alts, altErr := s.translateAttachmentAltsWithLimit(ctx, req.GuildID, loaded, []string{target.Language}, func() TranslationContext {
			return TranslationContext{GuildID: req.GuildID, MessageID: req.InitialMessageID}
		}, scope)
		descriptions := usableAttachmentAlts(alts, altErr, target.Language, len(loaded))
		files := webhookFilesForImages(loaded, descriptions)
		if content == "" && len(embeds) == 0 && len(files) == 0 {
			content = name
		}
		final, err := finishMirrorContent(req.InitialMessageID, content, req.Timestamp && req.InitialMessageID != "")
		if err != nil {
			return "", "", "", nil, err
		}
		body := mirroredMessageBody(string(final))
		parts := splitMessageContent(string(final), discordMessageContentLimit)
		appliedTags, err := s.mappedAppliedTagsForTarget(ctx, req.GuildID, groupID, req.SourceChannelID, target, req.AppliedTags)
		if err != nil {
			return "", "", "", nil, err
		}
		threadID, initialMessageID, err := s.discord.CreateThread(target.ChannelID, target.ChannelType, name, parts[0], embeds, appliedTags, files)
		if err != nil {
			return "", "", "", nil, err
		}
		if len(parts) == 1 {
			return threadID, initialMessageID, body, nil, nil
		}
		if initialMessageID == "" {
			_ = s.discord.DeleteThread(threadID)
			return "", "", "", nil, fmt.Errorf("create target thread in %s: initial message id is empty, so continuation parts cannot be linked", target.ChannelID)
		}
		continuationIDs, err := s.postMirrorContinuations(target, threadID, WebhookSend{
			Username:  req.InitialMessageUsername,
			AvatarURL: avatarProxyURL(s.publicBaseURL, req.InitialMessageAvatarURL, req.InitialMessageRoleColor),
		}, parts[1:])
		if err != nil {
			_ = s.discord.DeleteThread(threadID)
			return "", "", "", nil, err
		}
		return threadID, initialMessageID, body, continuationIDs, nil
	case targetThreadFromMessage:
		threadID, err := s.discord.CreateThreadFromMessage(target.ChannelID, plan.fromMessageID, name)
		return threadID, "", "", nil, err
	case targetThreadStandalone:
		threadID, _, err := s.discord.CreateThread(target.ChannelID, target.ChannelType, name, "", nil, nil, nil)
		return threadID, "", "", nil, err
	default:
		return "", "", "", nil, fmt.Errorf("unhandled target thread action %d", plan.action)
	}
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

func existingThreadTarget(links []ThreadLink, groupID, targetChannelID string) bool {
	for _, link := range links {
		if link.GroupID == groupID && link.TargetChannelID == targetChannelID {
			return true
		}
	}
	return false
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

// ensureThreadSynced creates peer threads for a message that arrives inside
// a not-yet-synced thread. ThreadTargets includes reverse links, so a thread
// already stored as a source or a target is left to message mirroring and
// must not spawn a second family. Returns whether the thread was created
// together with this message as its initial message.
func (s *Service) ensureThreadSynced(ctx context.Context, m DiscordMessage) (bool, error) {
	if m.ParentChannelID == "" || m.ThreadName == "" {
		return false, nil
	}
	if existing, err := s.store.ThreadTargets(ctx, m.ChannelID); err != nil {
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
		req.InitialMessageAvatarURL = m.AuthorAvatarURL
		req.InitialMessageRoleColor = m.AuthorRoleColor
		req.InitialMessageText = m.Content
		req.InitialMessagePoll = m.Poll
		req.InitialMessageFiles = m.Attachments
		req.InitialMessageStickers = m.Stickers
	} else {
		req.SourceMessageID = m.ChannelID
	}
	return s.syncThreadCreate(ctx, req, liveTranslation)
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
	held, err := s.liveTargetHeld(ctx, thread.GroupID, target.ChannelID, liveTranslation)
	if err != nil || held {
		return err
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
		return s.translationContextForMessage(ctx, m, thread.GroupID, thread.SourceChannelID, m.ChannelID, s.resolveThreadName(m), liveTranslation)
	}
	dests := []mirrorDestination{destinationForThread(*target, thread.TargetThreadID)}
	return s.mirrorMessage(ctx, m, thread.GroupID, sourceLanguage, contextFn, dests, liveTranslation)
}

func (s *Service) SyncThreadUpdate(ctx context.Context, guildID, sourceThreadID, name string, appliedTags []string, nameChanged, tagsChanged bool) error {
	if !nameChanged && !tagsChanged {
		return nil
	}
	threads, err := s.store.SourceThreadTargets(ctx, sourceThreadID)
	if err != nil {
		return err
	}
	return forEachGroupEdit(ctx, s.store, guildID, threads,
		func(thread ThreadLink) string { return thread.GroupID },
		func(ctx context.Context, targets []GroupChannel, thread ThreadLink) (*GroupChannel, error) {
			target := findChannel(targets, thread.TargetChannelID)
			if target == nil {
				return nil, nil
			}
			return target, nil
		},
		func(ctx context.Context, groupID string, pending []editPair[ThreadLink]) error {
			return s.editThreadMirrors(ctx, guildID, groupID, sourceThreadID, name, appliedTags, nameChanged, tagsChanged, pending)
		},
	)
}

// editThreadMirrors translates the new thread name once for the group's
// target languages and applies it, together with the mapped tags, to every
// peer thread. A rate-limited name translation leaves the renames out and
// still syncs the tags.
func (s *Service) editThreadMirrors(ctx context.Context, guildID, groupID, sourceThreadID, name string, appliedTags []string, nameChanged, tagsChanged bool, pending []editPair[ThreadLink]) error {
	var translations MultiTranslationResult
	if nameChanged && strings.TrimSpace(name) != "" {
		contextFn := func() TranslationContext {
			// The rename speaks in the thread's own conversation, the same
			// context the thread's creation used.
			return s.groupTranslationContext(ctx, translationContextRequest{
				guildID:          guildID,
				groupID:          groupID,
				contextChannelID: sourceThreadID,
				historyChannelID: sourceThreadID,
				excludeMessageID: sourceThreadID,
			}, liveTranslation)
		}
		languages := make([]string, 0, len(pending))
		for _, p := range pending {
			languages = append(languages, p.target.Language)
		}
		translated, err := s.translateWithLimit(ctx, guildID, name, nil, languages, contextFn, liveTranslation)
		if err != nil && !errors.Is(err, errTranslationRateLimited) {
			return err
		}
		if err == nil {
			translations = translated
		}
	}
	for _, p := range pending {
		editName := ""
		if translations.Translations != nil {
			editName = translations.Translations[p.target.Language]
		}
		var editTags *[]string
		if tagsChanged && isThreadOnlyChannelType(p.target.ChannelType) {
			mapping, err := s.store.ForumTagMapsBetween(ctx, guildID, groupID, p.link.SourceChannelID, p.target.ChannelID)
			if err != nil {
				return err
			}
			mapped := MapAppliedForumTags(mapping, appliedTags)
			targetThread, err := s.discord.Channel(p.link.TargetThreadID)
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
		if err := s.editThread(p.link.TargetThreadID, editName, editTags); err != nil {
			return err
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
