package translatorbot

import (
	"context"
	"fmt"
)

// sendAndSaveLink posts a webhook message and persists its link. A body longer
// than the Discord content limit becomes one primary message followed by
// continuation messages under the same webhook identity. The link records the
// body the mirror renders, without the generated headers on its first message.
// When any part cannot be posted or the link cannot be saved, every part
// already posted is deleted as compensation. ref may be zero when the source
// message is not a reply.
func (s *Service) sendAndSaveLink(ctx context.Context, target GroupChannel, threadID string, send WebhookSend, link MessageLink, ref MessageReference) (WebhookSendResult, error) {
	parts := splitMessageContent(send.Content, discordMessageContentLimit)
	primary := send
	primary.Content = parts[0]
	result, err := s.sendWebhookMessage(target, primary)
	if err != nil {
		return WebhookSendResult{}, err
	}
	sent := []string{result.MessageID}
	continuationIDs, err := s.postMirrorContinuations(target, threadID, send, parts[1:])
	if err != nil {
		s.deletePostedMirror(target, threadID, append(sent, continuationIDs...))
		return WebhookSendResult{}, err
	}
	sent = append(sent, continuationIDs...)
	link.TargetMessageID = result.MessageID
	link.ContinuationMessageIDs = sent[1:]
	link.MirrorBody = mirroredMessageBody(send.Content)
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
// messages it takes: parts whose text is unchanged in previous stay untouched,
// differing parts are edited in order, new parts are posted under the same
// author identity, and parts the shorter text no longer needs are deleted.
// previous holds the current contents of the mirror's messages in order,
// starting with the primary; nil means every part is edited without
// comparison. The link's stored body and continuation IDs follow the result.
func (s *Service) replaceMirrorMessages(ctx context.Context, target GroupChannel, link *MessageLink, content mirrorText, previous []string) error {
	parts := splitMessageContent(string(content), discordMessageContentLimit)
	existing := append([]string{link.TargetMessageID}, link.ContinuationMessageIDs...)
	if previous != nil && len(previous) != len(existing) {
		return fmt.Errorf("replace mirror %s/%s: %d stored contents for %d messages", link.TargetChannelID, link.TargetMessageID, len(previous), len(existing))
	}
	common := min(len(parts), len(existing))
	threadID := threadIDForWebhook(*link, &target)
	for i := 0; i < common; i++ {
		if previous != nil && previous[i] == parts[i] {
			continue
		}
		if err := s.editWebhookMessageAt(target, *link, existing[i], parts[i]); err != nil {
			return err
		}
	}
	addedIDs := make([]string, 0, len(parts)-common)
	if len(parts) > common {
		identity := WebhookSend{
			Username:  link.SourceAuthorDisplayName,
			AvatarURL: avatarProxyURL(s.publicBaseURL, link.SourceAuthorAvatarURL, link.SourceAuthorRoleColor),
		}
		added, err := s.postMirrorContinuations(target, threadID, identity, parts[common:])
		addedIDs = append(addedIDs, added...)
		if len(addedIDs) > 0 {
			existing = append(existing, addedIDs...)
		}
		if err != nil {
			s.deletePostedMirror(target, threadID, addedIDs)
			return err
		}
	}
	for _, messageID := range existing[len(parts):] {
		if err := s.deleteWebhookMessageAt(target, *link, messageID); err != nil && !isUnknownMessageError(err) {
			return err
		}
	}
	existing = existing[:len(parts)]
	link.ContinuationMessageIDs = existing[1:]
	if err := s.store.UpdateMessageLinkContent(ctx, link.SourceChannelID, link.SourceMessageID, link.TargetChannelID, mirroredMessageBody(string(content)), link.ContinuationMessageIDs); err != nil {
		s.deletePostedMirror(target, threadID, addedIDs)
		return err
	}
	return nil
}

// postMirrorContinuations posts the messages after a mirror's first one under
// the author identity the mirror was posted with: the identity's name and
// avatar only, so a continuation carries text rather than TTS, embeds, or
// files. When any part cannot be posted, the IDs posted so far come back with
// the error for the caller's compensation.
func (s *Service) postMirrorContinuations(target GroupChannel, threadID string, identity WebhookSend, parts []string) ([]string, error) {
	ids := make([]string, 0, len(parts))
	for _, part := range parts {
		result, err := s.sendWebhookMessage(target, WebhookSend{
			Content:   part,
			Username:  identity.Username,
			AvatarURL: identity.AvatarURL,
			ThreadID:  threadID,
		})
		if err != nil {
			return ids, err
		}
		ids = append(ids, result.MessageID)
	}
	return ids, nil
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
