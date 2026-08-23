package translatorbot

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

var errTranslationRateLimited = errors.New("translation rate limit exceeded")

// Service implements the mirroring pipeline: it receives normalized Discord
// events, translates content through the Translator, and fans the result out
// to every peer channel of a translation group via webhooks.
type Service struct {
	store                *Store
	discord              DiscordAPI
	translator           Translator
	rateLimiter          *TokenRateLimiter
	urlPages             *urlPageCache
	httpClient           *http.Client
	publicBaseURL        string
	selfBotUserID        string
	threadMu             sync.Mutex
	messageLocks         sync.Map
	topicSummaryAttempts sync.Map
	runTopicSummary      func(func())
	issueNotices         issueNoticeState
}

func NewService(store *Store, discord DiscordAPI, translator Translator) *Service {
	return &Service{
		store:       store,
		discord:     discord,
		translator:  translator,
		rateLimiter: NewTokenRateLimiter(defaultRateLimitTokensPerMinute),
		urlPages:    newURLPageCache(http.DefaultClient, urlPageCacheTTL, time.Now),
	}
}

func (s *Service) SetRateLimiter(limiter *TokenRateLimiter) {
	s.rateLimiter = limiter
}

func (s *Service) SetPublicBaseURL(publicBaseURL string) {
	s.publicBaseURL = strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
}

func (s *Service) SetSelfBotUserID(selfBotUserID string) {
	s.selfBotUserID = selfBotUserID
}

// shouldProcessMessage is the single source policy for create and update.
// Human messages do not depend on SQLite. Automated sources fail closed when
// their guild-scoped allowlist lookup cannot be completed.
func (s *Service) shouldProcessMessage(ctx context.Context, m DiscordMessage) (bool, error) {
	if s.selfBotUserID != "" && m.AuthorID == s.selfBotUserID {
		return false, nil
	}
	if m.WebhookID != "" {
		return s.store.IsMessageSourceAllowed(ctx, m.GuildID, SourceTypeWebhook, m.WebhookID)
	}
	if !m.Bot {
		return true, nil
	}
	return s.store.IsMessageSourceAllowed(ctx, m.GuildID, SourceTypeBot, m.AuthorID)
}

// postProcessContent applies target-language link rewriting to translated
// content: hreflang URLs from the page cache first, then managed Discord references.
func (s *Service) postProcessContent(ctx context.Context, guildID, text, targetLanguage string) string {
	text = s.urlPages.Replace(ctx, text, targetLanguage)
	return ReplaceDiscordRefs(ctx, s.store, guildID, text, targetLanguage)
}

// lockMessage serializes concurrent handling of the same (channel, message).
func (s *Service) lockMessage(channelID, messageID string) func() {
	key := channelID + "\x00" + messageID
	mu := &sync.Mutex{}
	actual, _ := s.messageLocks.LoadOrStore(key, mu)
	m := actual.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}
