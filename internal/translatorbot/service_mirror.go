package translatorbot

import "context"

// sendAndSaveLink posts a webhook message and persists its link. When the
// link cannot be saved, the just-posted message is deleted as compensation.
// ref may be zero when the source message is not a reply.
func (s *Service) sendAndSaveLink(ctx context.Context, target GroupChannel, threadID string, send WebhookSend, link MessageLink, ref MessageReference) error {
	msgID, err := s.discord.SendWebhook(target.WebhookID, target.WebhookToken, send)
	if err != nil {
		return err
	}
	link.TargetMessageID = msgID
	if err := s.store.SaveMessageLinkWithReference(ctx, link, ref); err != nil {
		_ = s.discord.DeleteWebhook(target.WebhookID, target.WebhookToken, msgID, threadID)
		return err
	}
	_, _ = s.store.MarkProcessed(ctx, messageLinkProcessedKey(link.SourceChannelID, link.SourceMessageID, link.TargetChannelID))
	return nil
}

// targetAlreadySynced reports whether a source message already has a mirror
// in the target channel, checking both processed-event markers and links.
func (s *Service) targetAlreadySynced(ctx context.Context, sourceChannelID, sourceMessageID, targetChannelID string) (bool, error) {
	key := messageLinkProcessedKey(sourceChannelID, sourceMessageID, targetChannelID)
	if processed, err := s.store.IsEventProcessed(ctx, key); err != nil {
		return false, err
	} else if processed {
		return true, nil
	}
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

func messageLinkProcessedKey(sourceChannelID, sourceMessageID, targetChannelID string) string {
	return "msglink:" + sourceChannelID + ":" + sourceMessageID + ":" + targetChannelID
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
