package translatorbot

import "context"

// sendAndSaveLink posts a webhook message and persists its link. When the
// link cannot be saved, the just-posted message is deleted as compensation.
// ref may be zero when the source message is not a reply.
func (s *Service) sendAndSaveLink(ctx context.Context, target GroupChannel, threadID string, send WebhookSend, link MessageLink, ref MessageReference) (WebhookSendResult, error) {
	result, err := s.sendWebhookMessage(target, send)
	if err != nil {
		return WebhookSendResult{}, err
	}
	link.TargetMessageID = result.MessageID
	if err := s.store.SaveMessageLinkWithReference(ctx, link, ref); err != nil {
		_ = s.discord.DeleteWebhook(target.WebhookID, target.WebhookToken, result.MessageID, threadID)
		return WebhookSendResult{}, err
	}
	return result, nil
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
