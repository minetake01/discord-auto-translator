package translatorbot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// translatedPollEmbeds composes the poll embed for one destination language.
func (s *Service) translatedPollEmbeds(ctx context.Context, guildID string, poll *DiscordPoll, translated PollTranslation, language string, roleColor int) []*discordgo.MessageEmbed {
	question := s.postProcessContent(ctx, guildID, translated.Question, language)
	answers := make([]string, len(translated.Answers))
	for i, answer := range translated.Answers {
		answers[i] = s.postProcessContent(ctx, guildID, answer, language)
	}
	embed := buildPollEmbed(question, formatTranslatedPollAnswers(poll, answers), roleColor)
	if embed == nil {
		return nil
	}
	return []*discordgo.MessageEmbed{embed}
}

// saveTranslatedPolls records the translated answers per language so that a
// later poll-result message can name the victor in the target language. Polls
// without an expiry can never be resolved, so nothing is stored.
func (s *Service) saveTranslatedPolls(ctx context.Context, pollChannelID, pollMessageID string, expiry *time.Time, translations map[string]PollTranslation) error {
	if expiry == nil || expiry.IsZero() {
		return nil
	}
	var errs []error
	for language, poll := range translations {
		answers := append([]string(nil), poll.Answers...)
		if err := s.store.SavePollTranslationCache(ctx, pollChannelID, pollMessageID, language, answers, *expiry); err != nil {
			errs = append(errs, fmt.Errorf("poll translation cache %s: %w", language, err))
		}
	}
	return errors.Join(errs...)
}

// pollMessageSnapshot is the stored source snapshot of a message that carries a
// poll: the message text followed by the poll question and answers.
func pollMessageSnapshot(text string, poll *DiscordPoll) string {
	text = strings.TrimSpace(text)
	pollText := formatPollSnapshot(poll)
	switch {
	case text == "":
		return pollText
	case pollText == "":
		return text
	default:
		return text + "\n\n" + pollText
	}
}
