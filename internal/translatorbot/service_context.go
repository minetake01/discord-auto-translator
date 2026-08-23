package translatorbot

import (
	"context"
	"errors"
	"strings"
	"time"
)

const (
	topicSummaryTimeout          = 2 * time.Minute
	topicSummarySourceTokenLimit = 1200
	translationReplyChainLimit   = 3
)

type translationContextRequest struct {
	guildID          string
	groupID          string
	contextChannelID string
	historyChannelID string
	excludeMessageID string
	replyChannelID   string
	replyMessageID   string
	author           string
	threadName       string
}

func (s *Service) translationContextForMessage(ctx context.Context, m DiscordMessage, groupID, contextChannelID, historyChannelID, threadName string) TranslationContext {
	replyChannelID := m.ReferencedMessageChannelID
	if replyChannelID == "" {
		replyChannelID = m.ChannelID
	}
	tc := s.groupTranslationContext(ctx, translationContextRequest{
		guildID:          m.GuildID,
		groupID:          groupID,
		contextChannelID: contextChannelID,
		historyChannelID: historyChannelID,
		excludeMessageID: m.ID,
		replyChannelID:   replyChannelID,
		replyMessageID:   m.ReferencedMessageID,
		author:           m.AuthorDisplayName,
		threadName:       threadName,
	})
	tc.MentionedUsers = m.MentionedUsers
	tc.MentionedChannels = m.MentionedChannels
	tc.MentionedRoles = m.MentionedRoles
	return tc
}

func (s *Service) groupTranslationContext(ctx context.Context, req translationContextRequest) TranslationContext {
	channelIDs, locationKey := s.conversationScope(ctx, req.guildID, req.groupID, req.historyChannelID)
	replyChain, replyKeys := s.replyChainContext(ctx, req.replyChannelID, req.replyMessageID)
	translationContext := s.loadConversationContext(ctx, req.guildID, req.contextChannelID, channelIDs, locationKey, req.historyChannelID, req.excludeMessageID, replyKeys)
	translationContext.ReplyChain = replyChain
	translationContext.StyleInstructions = s.groupStyleInstructions(ctx, req.guildID, req.groupID)
	translationContext.Author = strings.TrimSpace(req.author)
	translationContext.ThreadName = strings.TrimSpace(req.threadName)
	translationContext.PromptCacheLocation = locationKey
	return translationContext
}

func (s *Service) groupStyleInstructions(ctx context.Context, guildID, groupID string) string {
	preset, custom, err := s.store.GroupStyle(ctx, guildID, groupID)
	if err != nil {
		return ""
	}
	return ResolveStyleInstructions(preset, custom)
}

func (s *Service) resolveThreadName(m DiscordMessage) string {
	if name := strings.TrimSpace(m.ThreadName); name != "" {
		return name
	}
	return bestEffortString(func() (string, error) {
		return s.discord.ChannelName(m.ChannelID)
	})
}

func (s *Service) conversationScope(ctx context.Context, guildID, groupID, historyChannelID string) (channelIDs []string, locationKey string) {
	groupLocation := guildID + ":" + groupID + ":group"
	channels, err := s.store.ChannelsInGroup(ctx, guildID, groupID)
	if err != nil {
		return nil, groupLocation
	}
	if findChannel(channels, historyChannelID) != nil {
		channelIDs := make([]string, len(channels))
		for i, ch := range channels {
			channelIDs[i] = ch.ChannelID
		}
		return channelIDs, groupLocation
	}
	if historyChannelID == "" {
		return nil, groupLocation
	}
	channelIDs = []string{historyChannelID}
	threads, err := s.store.ThreadTargets(ctx, historyChannelID)
	if err != nil {
		return channelIDs, guildID + ":" + groupID + ":thread:" + historyChannelID
	}
	seen := map[string]bool{historyChannelID: true}
	for _, thread := range threads {
		if thread.SourceThreadID != "" && !seen[thread.SourceThreadID] {
			seen[thread.SourceThreadID] = true
			channelIDs = append(channelIDs, thread.SourceThreadID)
		}
		if thread.TargetThreadID != "" && !seen[thread.TargetThreadID] {
			seen[thread.TargetThreadID] = true
			channelIDs = append(channelIDs, thread.TargetThreadID)
		}
	}
	return channelIDs, guildID + ":" + groupID + ":thread:" + minStableID(channelIDs)
}

func minStableID(ids []string) string {
	min := ""
	for _, id := range ids {
		if id == "" {
			continue
		}
		if min == "" || snowflakeIDLess(id, min) {
			min = id
		}
	}
	return min
}

func snowflakeIDLess(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

func (s *Service) loadConversationContext(ctx context.Context, guildID, channelID string, historyChannelIDs []string, locationKey, sourceChannelID, excludeMessageID string, excludeReplyKeys map[string]bool) TranslationContext {
	translationContext := TranslationContext{
		GuildID:   guildID,
		MessageID: excludeMessageID,
		ServerName: bestEffortString(func() (string, error) {
			return s.discord.GuildName(guildID)
		}),
		ServerDescription: bestEffortString(func() (string, error) {
			return s.discord.GuildDescription(guildID)
		}),
		ChannelName: bestEffortString(func() (string, error) {
			return s.discord.ChannelName(channelID)
		}),
		ChannelTopic: bestEffortString(func() (string, error) {
			return s.discord.ChannelTopic(channelID)
		}),
	}
	if len(historyChannelIDs) == 0 {
		translationContext.PromptCacheGeneration = historyGenerationID(nil, sourceChannelID, excludeMessageID)
		return translationContext
	}
	links, err := s.store.RecentMessageHistory(ctx, historyChannelIDs, excludeMessageID, historyFetchLimit)
	if err != nil {
		translationContext.PromptCacheGeneration = historyGenerationID(nil, sourceChannelID, excludeMessageID)
		return translationContext
	}
	history, _, discarded := selectRecentHistory(links, excludeMessageID, excludeReplyKeys)
	translationContext.History = history
	translationContext.PromptCacheGeneration = historyGenerationID(history, sourceChannelID, excludeMessageID)
	if summary, err := s.store.TopicSummary(ctx, locationKey, translationContext.PromptCacheGeneration); err == nil {
		translationContext.TopicSummary = summary
	}
	if len(discarded) > 0 && translationContext.TopicSummary == "" {
		s.scheduleTopicSummary(guildID, locationKey, translationContext.PromptCacheGeneration, excludeMessageID, discarded)
	}
	return translationContext
}

func bestEffortString(fn func() (string, error)) string {
	value, err := fn()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

type messageRef struct {
	channelID string
	messageID string
}

func messageRefKey(channelID, messageID string) string {
	return channelID + "\x00" + messageID
}

func (s *Service) replyChainContext(ctx context.Context, refChannelID, refMessageID string) ([]ChatContextMessage, map[string]bool) {
	sourceKeys := make(map[string]bool)
	if refMessageID == "" || refChannelID == "" {
		return nil, sourceKeys
	}
	collected := make([]ChatContextMessage, 0, translationReplyChainLimit)
	currentChannelID := refChannelID
	currentMessageID := refMessageID
	for len(collected) < translationReplyChainLimit {
		entry, sourceChannelID, sourceMessageID, nextRef, ok := s.resolveReplyChainEntry(ctx, currentChannelID, currentMessageID)
		if !ok {
			break
		}
		collected = append(collected, entry)
		sourceKeys[messageRefKey(sourceChannelID, sourceMessageID)] = true
		if nextRef.messageID == "" {
			break
		}
		currentChannelID = nextRef.channelID
		currentMessageID = nextRef.messageID
		if currentChannelID == "" {
			currentChannelID = sourceChannelID
		}
	}
	for i, j := 0, len(collected)-1; i < j; i, j = i+1, j-1 {
		collected[i], collected[j] = collected[j], collected[i]
	}
	return collected, sourceKeys
}

func (s *Service) resolveReplyChainEntry(ctx context.Context, channelID, messageID string) (entry ChatContextMessage, sourceChannelID, sourceMessageID string, nextRef messageRef, ok bool) {
	original, tracked, err := s.store.MessageOriginal(ctx, channelID, messageID)
	if err != nil {
		return entry, "", "", nextRef, false
	}
	fetchChannelID := channelID
	fetchMessageID := messageID
	if tracked {
		sourceChannelID = original.SourceChannelID
		sourceMessageID = original.SourceMessageID
		fetchChannelID = sourceChannelID
		fetchMessageID = sourceMessageID
		entry.Content = original.Snapshot
		entry.Author = strings.TrimSpace(original.SourceAuthorDisplayName)
		entry.Attachments = append([]DiscordAttachment(nil), original.ImageAttachments...)
		entry.SourceChannelID = sourceChannelID
		entry.SourceMessageID = sourceMessageID
	}
	fetched, fetchErr := s.discord.Message(fetchChannelID, fetchMessageID)
	if fetchErr != nil {
		if !tracked {
			return entry, "", "", nextRef, false
		}
		return entry, sourceChannelID, sourceMessageID, nextRef, contextMessageHasContent(entry)
	}
	if !tracked {
		entry.Content = fetched.Content
		entry.Author = strings.TrimSpace(fetched.AuthorDisplayName)
		entry.Attachments = append([]DiscordAttachment(nil), fetched.Attachments...)
		sourceChannelID = channelID
		sourceMessageID = messageID
	} else if entry.Author == "" {
		entry.Author = strings.TrimSpace(fetched.AuthorDisplayName)
	}
	if len(fetched.Attachments) > 0 {
		entry.Attachments = append([]DiscordAttachment(nil), fetched.Attachments...)
	}
	entry.SourceChannelID = sourceChannelID
	entry.SourceMessageID = sourceMessageID
	nextRef = messageRef{
		channelID: fetched.ReferencedChannelID,
		messageID: fetched.ReferencedMessageID,
	}
	if nextRef.channelID == "" && nextRef.messageID != "" {
		nextRef.channelID = fetchChannelID
	}
	return entry, sourceChannelID, sourceMessageID, nextRef, contextMessageHasContent(entry)
}

func contextMessageHasContent(entry ChatContextMessage) bool {
	return strings.TrimSpace(entry.Content) != "" || len(imageAttachmentsOnly(entry.Attachments)) > 0
}

func (s *Service) scheduleTopicSummary(guildID, locationKey, generationID, messageID string, discarded []ChatContextMessage) {
	if _, ok := s.translator.(TopicSummarizer); !ok {
		return
	}
	if strings.TrimSpace(locationKey) == "" || strings.TrimSpace(generationID) == "" || generationID == "empty" || len(discarded) == 0 {
		return
	}
	attemptKey := locationKey + "\x00" + generationID
	if _, loaded := s.topicSummaryAttempts.LoadOrStore(attemptKey, struct{}{}); loaded {
		return
	}
	copied := append([]ChatContextMessage(nil), discarded...)
	run := func() {
		ctx, cancel := context.WithTimeout(context.Background(), topicSummaryTimeout)
		defer cancel()
		if err := s.generateAndStoreTopicSummary(ctx, guildID, locationKey, generationID, messageID, copied); err != nil && errors.Is(err, errTranslationRateLimited) {
			s.topicSummaryAttempts.Delete(attemptKey)
		}
	}
	if s.runTopicSummary != nil {
		s.runTopicSummary(run)
		return
	}
	go run()
}

func (s *Service) generateAndStoreTopicSummary(ctx context.Context, guildID, locationKey, generationID, messageID string, discarded []ChatContextMessage) error {
	summarizer, ok := s.translator.(TopicSummarizer)
	if !ok {
		return nil
	}
	if existing, err := s.store.TopicSummary(ctx, locationKey, generationID); err == nil && existing != "" {
		return nil
	}
	previous := ""
	if prevGeneration, prevSummary, err := s.store.TopicSummaryForLocation(ctx, locationKey); err == nil && prevGeneration != generationID {
		previous = prevSummary
	}
	req := TopicSummaryRequest{
		PreviousSummary: previous,
		Discarded:       capDiscardedForSummary(discarded),
		GuildID:         guildID,
		MessageID:       messageID,
	}
	prepared, err := prepareTopicSummary(req)
	if err != nil {
		return err
	}
	if err := s.checkPreparedTranslationRateLimit(guildID, prepared); err != nil {
		return err
	}
	result, err := summarizer.SummarizeTopic(ctx, prepared)
	if err != nil {
		return err
	}
	s.recordSuccessfulTranslation(guildID, result.InputTokens, result.OutputTokens)
	if strings.TrimSpace(result.Summary) == "" {
		return errors.New("empty topic summary")
	}
	return s.store.UpsertTopicSummary(ctx, guildID, locationKey, generationID, result.Summary)
}

func capDiscardedForSummary(messages []ChatContextMessage) []ChatContextMessage {
	if len(messages) == 0 {
		return nil
	}
	total := 0
	start := len(messages)
	for i := len(messages) - 1; i >= 0; i-- {
		n := EstimateTranslationTokens(messages[i].Content, "")
		if total+n > topicSummarySourceTokenLimit && start < len(messages) {
			break
		}
		total += n
		start = i
	}
	return messages[start:]
}
