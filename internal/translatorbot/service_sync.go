package translatorbot

import (
	"context"
	"errors"
)

func (s *Service) SyncReaction(ctx context.Context, guildID, sourceChannelID, sourceMessageID, emoji string, add bool) error {
	links, err := s.store.MessagePeers(ctx, sourceChannelID, sourceMessageID)
	if err != nil {
		return err
	}
	for _, link := range links {
		if add {
			err = s.discord.AddReaction(link.TargetChannelID, link.TargetMessageID, emoji)
		} else {
			err = s.discord.RemoveOwnReaction(link.TargetChannelID, link.TargetMessageID, emoji)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// HandleMessagePinUpdate syncs a pin state change to all peer messages.
// Comparing against the stored pin state suppresses echo loops caused by the
// bot's own pin operations.
func (s *Service) HandleMessagePinUpdate(ctx context.Context, channelID, messageID string, pinned bool) error {
	prevPinned, known, err := s.store.GetPinState(ctx, channelID, messageID)
	if err != nil {
		return err
	}
	if known && prevPinned == pinned {
		return nil
	}
	if err := s.SyncPin(ctx, channelID, messageID, pinned); err != nil {
		return err
	}
	return s.savePinStatesForPeers(ctx, channelID, messageID, pinned)
}

func (s *Service) SyncPin(ctx context.Context, sourceChannelID, sourceMessageID string, pinned bool) error {
	links, err := s.store.MessagePeers(ctx, sourceChannelID, sourceMessageID)
	if err != nil {
		return err
	}
	var errs []error
	for _, link := range links {
		if pinned {
			err = s.discord.PinMessage(link.TargetChannelID, link.TargetMessageID)
		} else {
			err = s.discord.UnpinMessage(link.TargetChannelID, link.TargetMessageID)
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) savePinStatesForPeers(ctx context.Context, channelID, messageID string, pinned bool) error {
	if err := s.store.SavePinState(ctx, channelID, messageID, pinned); err != nil {
		return err
	}
	peers, err := s.store.MessagePeers(ctx, channelID, messageID)
	if err != nil {
		return err
	}
	for _, link := range peers {
		if link.TargetChannelID == channelID && link.TargetMessageID == messageID {
			if err := s.store.SavePinState(ctx, link.SourceChannelID, link.SourceMessageID, pinned); err != nil {
				return err
			}
			continue
		}
		if err := s.store.SavePinState(ctx, link.TargetChannelID, link.TargetMessageID, pinned); err != nil {
			return err
		}
	}
	return nil
}
