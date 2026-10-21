package translatorbot

import "context"

// sendAndSaveLink posts a webhook message and persists its link. A body longer
// than the Discord content limit becomes one primary message followed by
// continuation messages under the same webhook identity. When any part cannot
// be posted or the link cannot be saved, every part already posted is deleted
// as compensation. ref may be zero when the source message is not a reply.
func (s *Service) sendAndSaveLink(ctx context.Context, target GroupChannel, threadID string, send WebhookSend, link MessageLink, ref MessageReference) (WebhookSendResult, error) {
	parts := splitMessageContent(send.Content, discordMessageContentLimit)
	primary := send
	primary.Content = parts[0]
	result, err := s.sendWebhookMessage(target, primary)
	if err != nil {
		return WebhookSendResult{}, err
	}
	sent := []string{result.MessageID}
	for _, part := range parts[1:] {
		continuation := send
		continuation.Content = part
		continuation.Embeds = nil
		continuation.Files = nil
		continuationResult, err := s.sendWebhookMessage(target, continuation)
		if err != nil {
			s.deletePostedMirror(target, threadID, sent)
			return WebhookSendResult{}, err
		}
		sent = append(sent, continuationResult.MessageID)
	}
	link.TargetMessageID = result.MessageID
	link.ContinuationMessageIDs = sent[1:]
	if err := s.store.SaveMessageLinkWithReference(ctx, link, ref); err != nil {
		s.deletePostedMirror(target, threadID, sent)
		return WebhookSendResult{}, err
	}
	return result, nil
}

// deletePostedMirror deletes messages the bot just posted. It is compensation
// for a failed send or link save, so failures are left to the caller's error.
func (s *Service) deletePostedMirror(target GroupChannel, threadID string, messageIDs []string) {
	channelID := target.ChannelID
	if threadID != "" {
		channelID = threadID
	}
	for _, messageID := range messageIDs {
		_ = s.withOpenThread(channelID, restoreThreadFully, func() error {
			return s.discord.DeleteWebhook(target.WebhookID, target.WebhookToken, messageID, threadID)
		})
	}
}

// replaceMirrorMessages rewrites a posted mirror to content, however many
// messages it takes: existing parts are edited in order, new parts are posted
// under the same author identity, and parts the shorter text no longer needs
// are deleted. The link's stored continuation IDs follow the result.
func (s *Service) replaceMirrorMessages(ctx context.Context, target GroupChannel, link *MessageLink, content mirrorText) error {
	parts := splitMessageContent(string(content), discordMessageContentLimit)
	existing := append([]string{link.TargetMessageID}, link.ContinuationMessageIDs...)
	common := min(len(parts), len(existing))
	for i := 0; i < common; i++ {
		if err := s.editWebhookMessageAt(target, *link, existing[i], parts[i]); err != nil {
			return err
		}
	}
	threadID := threadIDForWebhook(*link, &target)
	addedIDs := make([]string, 0, len(parts)-common)
	for _, part := range parts[common:] {
		messageID, err := s.sendMirrorContinuation(target, *link, part)
		if err != nil {
			s.deletePostedMirror(target, threadID, addedIDs)
			return err
		}
		existing = append(existing, messageID)
		addedIDs = append(addedIDs, messageID)
	}
	for _, messageID := range existing[len(parts):] {
		if err := s.deleteWebhookMessageAt(target, *link, messageID); err != nil && !isUnknownMessageError(err) {
			return err
		}
	}
	existing = existing[:len(parts)]
	link.ContinuationMessageIDs = existing[1:]
	if err := s.store.UpdateMessageLinkContinuations(ctx, link.SourceChannelID, link.SourceMessageID, link.TargetChannelID, link.ContinuationMessageIDs); err != nil {
		s.deletePostedMirror(target, threadID, addedIDs)
		return err
	}
	return nil
}

// sendMirrorContinuation posts one further message of a mirror under the
// author identity the mirror was created with.
func (s *Service) sendMirrorContinuation(target GroupChannel, link MessageLink, content string) (string, error) {
	result, err := s.sendWebhookMessage(target, WebhookSend{
		Content:   content,
		Username:  link.SourceAuthorDisplayName,
		AvatarURL: avatarProxyURL(s.publicBaseURL, link.SourceAuthorAvatarURL, link.SourceAuthorRoleColor),
		ThreadID:  threadIDForWebhook(link, &target),
	})
	if err != nil {
		return "", err
	}
	return result.MessageID, nil
}

// targetAlreadySynced reports whether a source message already has a mirror
// in the target channel.
func (s *Service) targetAlreadySynced(ctx context.Context, sourceChannelID, sourceMessageID, targetChannelID string) (bool, error) {
	links, err := s.store.MessageTargets(ctx, sourceChannelID, sourceMessageID)
	if err != nil {
		return false, err
	}
	for _, link := range links {
		if link.TargetChannelID == targetChannelID {
			return true, nil
		}
	}
	return false, nil
}

func findChannel(channels []GroupChannel, id string) *GroupChannel {
	for i := range channels {
		if channels[i].ChannelID == id {
			return &channels[i]
		}
	}
	return nil
}

func languageForChannel(channels []GroupChannel, id string) string {
	if channel := findChannel(channels, id); channel != nil {
		return channel.Language
	}
	return ""
}

func threadIDForWebhook(link MessageLink, target *GroupChannel) string {
	if target == nil || link.TargetChannelID == target.ChannelID {
		return ""
	}
	return link.TargetChannelID
}

// editPair pairs one mirror link with the target channel that now hosts the
// mirror. The link type is MessageLink for mirrored messages and ThreadLink
// for synced threads.
type editPair[L any] struct {
	link   L
	target GroupChannel
}

// forEachGroupEdit walks the groups a set of mirror links belongs to. For
// every group it fetches the group's channels, resolves each link's target
// channel, and hands the resolved pairs to act once per group. Links whose
// target is gone are skipped, and processing stops at the first error.
func forEachGroupEdit[L any](ctx context.Context, store *Store, guildID string, links []L, groupOf func(L) string, resolve func(ctx context.Context, targets []GroupChannel, link L) (*GroupChannel, error), act func(ctx context.Context, groupID string, pending []editPair[L]) error) error {
	byGroup := make(map[string][]L)
	for _, link := range links {
		groupID := groupOf(link)
		byGroup[groupID] = append(byGroup[groupID], link)
	}
	for groupID, groupLinks := range byGroup {
		targets, err := store.ChannelsInGroup(ctx, guildID, groupID)
		if err != nil {
			return err
		}
		pending := make([]editPair[L], 0, len(groupLinks))
		for _, link := range groupLinks {
			target, err := resolve(ctx, targets, link)
			if err != nil {
				return err
			}
			if target == nil {
				continue
			}
			pending = append(pending, editPair[L]{link: link, target: *target})
		}
		if len(pending) == 0 {
			continue
		}
		if err := act(ctx, groupID, pending); err != nil {
			return err
		}
	}
	return nil
}
