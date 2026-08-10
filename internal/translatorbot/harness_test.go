package translatorbot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

var png1x1 = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
	0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

func stubImageHTTP(service *Service) {
	service.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"image/png"}},
			Body:       io.NopCloser(bytes.NewReader(png1x1)),
			Request:    req,
		}, nil
	})}
}

type fakeDiscordAPI struct {
	sent              []WebhookSend
	channelMessages   []channelMessageCall
	reactions         []reactionCall
	removedReactions  []reactionCall
	threads           []threadCall
	webhookEdits      []webhookEditCall
	webhookDeletes    []webhookDeleteCall
	pinCalls          []pinCall
	edits             []threadEditCall
	deletes           []string
	channels          map[string]*discordgo.Channel
	guildNames        map[string]string
	guildDescriptions map[string]string
	channelNames      map[string]string
	channelTopics     map[string]string
	messageContents   map[string]string
	messages          map[string]DiscordFetchedMessage
	messageErrors     map[string]error
	nextID            int
}
type channelMessageCall struct {
	channelID        string
	replyToMessageID string
	content          string
}
type reactionCall struct {
	channelID string
	messageID string
	emoji     string
}
type threadCall struct {
	channelID   string
	channelType int
	messageID   string
	name        string
	content     string
	embeds      []*discordgo.MessageEmbed
	appliedTags []string
	files       []WebhookFile
}
type threadEditCall struct {
	threadID    string
	name        string
	appliedTags *[]string
}
type webhookEditCall struct {
	messageID string
	threadID  string
	content   string
}
type webhookDeleteCall struct {
	messageID string
	threadID  string
}
type pinCall struct {
	channelID string
	messageID string
	pinned    bool
}

func (f *fakeDiscordAPI) GuildName(guildID string) (string, error) {
	return f.guildNames[guildID], nil
}
func (f *fakeDiscordAPI) GuildDescription(guildID string) (string, error) {
	return f.guildDescriptions[guildID], nil
}
func (f *fakeDiscordAPI) ChannelName(channelID string) (string, error) {
	return f.channelNames[channelID], nil
}
func (f *fakeDiscordAPI) ChannelTopic(channelID string) (string, error) {
	return f.channelTopics[channelID], nil
}
func (f *fakeDiscordAPI) Message(channelID, messageID string) (DiscordFetchedMessage, error) {
	key := channelID + "\x00" + messageID
	if err := f.messageErrors[key]; err != nil {
		return DiscordFetchedMessage{}, err
	}
	if msg, ok := f.messages[key]; ok {
		return msg, nil
	}
	content, ok := f.messageContents[key]
	if !ok {
		return DiscordFetchedMessage{}, errors.New("message not found")
	}
	return DiscordFetchedMessage{Content: content}, nil
}
func (f *fakeDiscordAPI) CreateWebhook(channelID, name string) (id, token string, err error) {
	return "webhook-" + channelID, "token-" + channelID, nil
}
func (f *fakeDiscordAPI) SendChannelMessage(channelID, replyToMessageID, content string) error {
	if replyToMessageID == "" {
		return errors.New("send channel message: replyToMessageID is required")
	}
	f.channelMessages = append(f.channelMessages, channelMessageCall{
		channelID:        channelID,
		replyToMessageID: replyToMessageID,
		content:          content,
	})
	return nil
}
func (f *fakeDiscordAPI) SendWebhook(webhookID, token string, msg WebhookSend) (messageID string, err error) {
	f.nextID++
	f.sent = append(f.sent, msg)
	return fmt.Sprintf("sent-%d", f.nextID), nil
}
func (f *fakeDiscordAPI) EditWebhook(webhookID, token, messageID, threadID, content string) error {
	f.webhookEdits = append(f.webhookEdits, webhookEditCall{messageID: messageID, threadID: threadID, content: content})
	return nil
}
func (f *fakeDiscordAPI) DeleteWebhook(webhookID, token, messageID, threadID string) error {
	f.webhookDeletes = append(f.webhookDeletes, webhookDeleteCall{messageID: messageID, threadID: threadID})
	return nil
}
func (f *fakeDiscordAPI) AddReaction(channelID, messageID, emoji string) error {
	f.reactions = append(f.reactions, reactionCall{channelID: channelID, messageID: messageID, emoji: emoji})
	return nil
}
func (f *fakeDiscordAPI) RemoveOwnReaction(channelID, messageID, emoji string) error {
	f.removedReactions = append(f.removedReactions, reactionCall{channelID: channelID, messageID: messageID, emoji: emoji})
	return nil
}
func (f *fakeDiscordAPI) PinMessage(channelID, messageID string) error {
	f.pinCalls = append(f.pinCalls, pinCall{channelID: channelID, messageID: messageID, pinned: true})
	return nil
}
func (f *fakeDiscordAPI) UnpinMessage(channelID, messageID string) error {
	f.pinCalls = append(f.pinCalls, pinCall{channelID: channelID, messageID: messageID, pinned: false})
	return nil
}
func (f *fakeDiscordAPI) CreateThread(channelID string, channelType int, name, initialMessage string, embeds []*discordgo.MessageEmbed, appliedTags []string, files []WebhookFile) (threadID, initialMessageID string, err error) {
	f.nextID++
	threadID = fmt.Sprintf("thread-%d", f.nextID)
	if isThreadOnlyChannelType(channelType) {
		initialMessageID = threadID
	}
	f.threads = append(f.threads, threadCall{channelID: channelID, channelType: channelType, name: name, content: initialMessage, embeds: embeds, appliedTags: append([]string(nil), appliedTags...), files: append([]WebhookFile(nil), files...)})
	return threadID, initialMessageID, nil
}
func (f *fakeDiscordAPI) CreateThreadFromMessage(channelID, messageID, name string) (threadID string, err error) {
	f.nextID++
	f.threads = append(f.threads, threadCall{channelID: channelID, messageID: messageID, name: name})
	return fmt.Sprintf("thread-%d", f.nextID), nil
}
func (f *fakeDiscordAPI) EditThread(threadID, name string, appliedTags *[]string) error {
	call := threadEditCall{threadID: threadID, name: name}
	if appliedTags != nil {
		tags := append([]string(nil), (*appliedTags)...)
		call.appliedTags = &tags
	}
	f.edits = append(f.edits, call)
	return nil
}
func (f *fakeDiscordAPI) DeleteThread(threadID string) error {
	f.deletes = append(f.deletes, threadID)
	return nil
}
func (f *fakeDiscordAPI) Channel(channelID string) (*discordgo.Channel, error) {
	if f.channels != nil {
		if ch, ok := f.channels[channelID]; ok {
			return ch, nil
		}
	}
	return &discordgo.Channel{ID: channelID}, nil
}

type echoTranslator struct {
	contexts     []TranslationContext
	pollContexts []TranslationContext
}

func (e *echoTranslator) TranslateMulti(ctx context.Context, prepared preparedTranslation) (MultiTranslationResult, error) {
	e.contexts = append(e.contexts, prepared.translationContext)
	out := make(map[string]string, len(prepared.targetLanguages))
	alts := make(map[string][]string, len(prepared.targetLanguages))
	for _, lang := range prepared.targetLanguages {
		text := prepared.content
		if hasTranslatableText(text) || strings.TrimSpace(text) != "" {
			out[lang] = "[" + lang + "] " + text
		} else {
			out[lang] = text
		}
		if prepared.attachmentCount > 0 {
			descriptions := make([]string, prepared.attachmentCount)
			for i, attachment := range prepared.translationContext.Attachments {
				if strings.TrimSpace(attachment.Description) != "" {
					descriptions[i] = "[" + lang + "] " + attachment.Description
				}
			}
			alts[lang] = descriptions
		}
	}
	return MultiTranslationResult{Translations: out, AttachmentDescriptions: alts}, nil
}
func (e *echoTranslator) TranslatePollMulti(ctx context.Context, prepared preparedTranslation) (PollMultiTranslationResult, error) {
	e.pollContexts = append(e.pollContexts, prepared.translationContext)
	out := make(map[string]PollTranslation, len(prepared.targetLanguages))
	for _, lang := range prepared.targetLanguages {
		translatedAnswers := make([]string, len(prepared.answers))
		for i, answer := range prepared.answers {
			translatedAnswers[i] = "[" + lang + "] " + answer
		}
		out[lang] = PollTranslation{
			Question: "[" + lang + "] " + prepared.question,
			Answers:  translatedAnswers,
		}
	}
	return PollMultiTranslationResult{Translations: out}, nil
}
func (e *echoTranslator) TranslateThreadCreateMulti(ctx context.Context, prepared preparedTranslation) (ThreadCreateMultiTranslationResult, error) {
	e.contexts = append(e.contexts, prepared.translationContext)
	out := make(map[string]ThreadCreateTranslation, len(prepared.targetLanguages))
	for _, lang := range prepared.targetLanguages {
		name := prepared.threadName
		if hasTranslatableText(name) {
			name = "[" + lang + "] " + name
		}
		message := prepared.threadMessage
		if hasTranslatableText(message) {
			message = "[" + lang + "] " + message
		}
		out[lang] = ThreadCreateTranslation{Name: name, Message: message}
	}
	return ThreadCreateMultiTranslationResult{Translations: out}, nil
}
func seedGroup(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	if err := s.CreateGroupWithChannel(ctx, TranslationGroup{ID: "g", GuildID: "guild", DisplayName: "g", CreatedBy: "u"}, GroupChannel{
		GroupID: "g", GuildID: "guild", ChannelID: "ja", Language: "ja", WebhookID: "w-ja", WebhookToken: "t-ja",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.JoinChannel(ctx, GroupChannel{
		GroupID: "g", GuildID: "guild", ChannelID: "en", Language: "en", WebhookID: "w-en", WebhookToken: "t-en",
	}); err != nil {
		t.Fatal(err)
	}
}
func seedMultiLangGroup(t *testing.T, s *Store) {
	t.Helper()
	seedGroup(t, s)
	ctx := context.Background()
	if err := s.JoinChannel(ctx, GroupChannel{
		GroupID: "g", GuildID: "guild", ChannelID: "fr", Language: "fr", WebhookID: "w-fr", WebhookToken: "t-fr",
	}); err != nil {
		t.Fatal(err)
	}
}

type selectiveFailTranslator struct {
	failLanguage string
}

func (s *selectiveFailTranslator) TranslateMulti(ctx context.Context, prepared preparedTranslation) (MultiTranslationResult, error) {
	for _, lang := range prepared.targetLanguages {
		if lang == s.failLanguage {
			return MultiTranslationResult{}, errors.New("translation failed")
		}
	}
	out := make(map[string]string, len(prepared.targetLanguages))
	for _, lang := range prepared.targetLanguages {
		out[lang] = "[" + lang + "] " + prepared.content
	}
	return MultiTranslationResult{Translations: out}, nil
}
func (s *selectiveFailTranslator) TranslatePollMulti(ctx context.Context, prepared preparedTranslation) (PollMultiTranslationResult, error) {
	for _, lang := range prepared.targetLanguages {
		if lang == s.failLanguage {
			return PollMultiTranslationResult{}, errors.New("translation failed")
		}
	}
	out := make(map[string]PollTranslation, len(prepared.targetLanguages))
	for _, lang := range prepared.targetLanguages {
		translatedAnswers := make([]string, len(prepared.answers))
		for i, answer := range prepared.answers {
			translatedAnswers[i] = "[" + lang + "] " + answer
		}
		out[lang] = PollTranslation{Question: "[" + lang + "] " + prepared.question, Answers: translatedAnswers}
	}
	return PollMultiTranslationResult{Translations: out}, nil
}
func (s *selectiveFailTranslator) TranslateThreadCreateMulti(ctx context.Context, prepared preparedTranslation) (ThreadCreateMultiTranslationResult, error) {
	for _, lang := range prepared.targetLanguages {
		if lang == s.failLanguage {
			return ThreadCreateMultiTranslationResult{}, errors.New("translation failed")
		}
	}
	out := make(map[string]ThreadCreateTranslation, len(prepared.targetLanguages))
	for _, lang := range prepared.targetLanguages {
		name := prepared.threadName
		if hasTranslatableText(name) {
			name = "[" + lang + "] " + name
		}
		message := prepared.threadMessage
		if hasTranslatableText(message) {
			message = "[" + lang + "] " + message
		}
		out[lang] = ThreadCreateTranslation{Name: name, Message: message}
	}
	return ThreadCreateMultiTranslationResult{Translations: out}, nil
}
func seedThreeChannelGroup(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	if err := s.CreateGroupWithChannel(ctx, TranslationGroup{ID: "g", GuildID: "guild", DisplayName: "g", CreatedBy: "u"}, GroupChannel{
		GroupID: "g", GuildID: "guild", ChannelID: "ja", Language: "ja", WebhookID: "w-ja", WebhookToken: "t-ja",
	}); err != nil {
		t.Fatal(err)
	}
	for _, ch := range []GroupChannel{
		{GroupID: "g", GuildID: "guild", ChannelID: "en", Language: "en", WebhookID: "w-en", WebhookToken: "t-en"},
		{GroupID: "g", GuildID: "guild", ChannelID: "fr", Language: "fr", WebhookID: "w-fr", WebhookToken: "t-fr"},
	} {
		if err := s.JoinChannel(ctx, ch); err != nil {
			t.Fatal(err)
		}
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
