package translatorbot

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type forwardedTargetContent struct {
	body        string
	jumpURL     string
	needsAssets bool
}

// mirrorForwardedMessage mirrors a forwarded message to every destination.
// When the forwarded source already has a mirror in the destination, its
// translated body and jump URL are reused without calling the translator.
func (s *Service) mirrorForwardedMessage(ctx context.Context, m DiscordMessage, groupID, sourceLanguage string, contextFn func() TranslationContext, dests []mirrorDestination) error {
	contents, err := s.forwardedContents(ctx, m, contextFn, dests)
	if err != nil {
		s.notifyTranslationIssue(m.ChannelID, sourceLanguage, err)
		if errors.Is(err, errTranslationRateLimited) {
			return nil
		}
		return err
	}
	var errs []error
	for _, dest := range dests {
		if err := s.sendMirror(ctx, m, groupID, dest, contents[dest.targetID], m.ForwardedMessage.Content); err != nil {
			errs = append(errs, fmt.Errorf("target %s: %w", dest.targetID, err))
		}
	}
	return errors.Join(errs...)
}

// forwardedContents prepares the outgoing content per destination: a
// localized forwarded header plus either a reused mirror body or a fresh
// translation of the forwarded snapshot.
func (s *Service) forwardedContents(ctx context.Context, m DiscordMessage, contextFn func() TranslationContext, dests []mirrorDestination) (map[string]string, error) {
	forwarded := m.ForwardedMessage

	prepared := make(map[string]forwardedTargetContent, len(dests))
	translateDests := make([]mirrorDestination, 0, len(dests))
	for _, dest := range dests {
		_, mirrorChannelID, mirrorMessageID, ok, err := s.store.MessageQuoteTarget(ctx, forwarded.ChannelID, forwarded.MessageID, dest.targetID)
		if err != nil {
			return nil, err
		}
		if ok && mirrorChannelID == dest.targetID && mirrorMessageID != "" {
			body := forwarded.Content
			needsAssets := mirrorChannelID == forwarded.ChannelID && mirrorMessageID == forwarded.MessageID
			if mirrorChannelID != forwarded.ChannelID || mirrorMessageID != forwarded.MessageID {
				body, err = s.discord.MessageContent(mirrorChannelID, mirrorMessageID)
				if err != nil {
					return nil, fmt.Errorf("fetch forwarded mirror %s/%s: %w", mirrorChannelID, mirrorMessageID, err)
				}
			}
			prepared[dest.targetID] = forwardedTargetContent{
				body: mirroredMessageBody(body), jumpURL: MessageJumpURL(m.GuildID, mirrorChannelID, mirrorMessageID),
				needsAssets: needsAssets,
			}
			continue
		}
		translateDests = append(translateDests, dest)
	}

	if len(translateDests) > 0 {
		languages := make([]string, 0, len(translateDests))
		for _, dest := range translateDests {
			languages = append(languages, dest.channel.Language)
		}
		translations, err := s.translateWithLimit(ctx, m.GuildID, forwarded.Content, languages, contextFn)
		if err != nil {
			return nil, err
		}
		jumpGuildID := forwarded.GuildID
		if jumpGuildID == "" {
			jumpGuildID = m.GuildID
		}
		for _, dest := range translateDests {
			prepared[dest.targetID] = forwardedTargetContent{
				body:        s.postProcessContent(ctx, m.GuildID, translations[dest.channel.Language], dest.channel.Language),
				jumpURL:     MessageJumpURL(jumpGuildID, forwarded.ChannelID, forwarded.MessageID),
				needsAssets: true,
			}
		}
	}

	contents := make(map[string]string, len(dests))
	for _, dest := range dests {
		item := prepared[dest.targetID]
		body := item.body
		var err error
		if item.needsAssets {
			body, err = messageContentWithAssetURLs(body, forwarded.Attachments, forwarded.Stickers)
			if err != nil {
				return nil, err
			}
		}
		header := fmt.Sprintf("-# %s · %s", localizedUIString(dest.channel.Language, uiKeyForwarded), item.jumpURL)
		if strings.TrimSpace(body) == "" {
			contents[dest.targetID] = header
		} else {
			contents[dest.targetID] = header + "\n" + body
		}
	}
	return contents, nil
}
