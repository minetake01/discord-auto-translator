package translatorbot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
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
	mu                     sync.Mutex
	sent                   []WebhookSend
	sentContents           map[string]string // webhook message ID → posted content
	channelMessages        []channelMessageCall
	reactions              []reactionCall
	removedReactions       []reactionCall
	threads                []threadCall
	webhookEdits           []webhookEditCall
	webhookAttachmentEdits []webhookAttachmentEditCall
	webhookDeletes         []webhookDeleteCall
	deletedWebhooks        []string
	pinCalls               []pinCall
	edits                  []threadEditCall
	deletes                []string
	channels               map[string]*discordgo.Channel
	guildNames             map[string]string
	guildDescriptions      map[string]string
	channelNames           map[string]string
	channelTopics          map[string]string
	messageContents        map[string]string
	messages               map[string]DiscordFetchedMessage
	messageErrors          map[string]error
	nextID                 int
	threadStates           []threadStateCall
	history                map[string][]DiscordMessage // channel → messages, any order
	publicThreads          []DiscordThread
	// events records what reached Discord, in order, as "message" or "thread"
	// so a test can pin where in a channel a thread was created.
	events                 []string
	sendErr                func(WebhookSend) error
	editErr                func(webhookEditCall) error
	forgetMemberCacheCalls int
}
type threadStateCall struct {
	threadID string
	archived bool
	locked   bool
}

// fakeSentID and fakeThreadID are the snowflakes the fake assigns to posted
// webhook messages and created threads.
func fakeSentID(n int) string   { return fmt.Sprintf("%d", 900000000000000000+n) }
func fakeThreadID(n int) string { return fmt.Sprintf("%d", 800000000000000000+n) }

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
type webhookAttachmentEditCall struct {
	messageID   string
	threadID    string
	attachments []WebhookAttachmentEdit
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

func (f *fakeDiscordAPI) webhookSends() []WebhookSend {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]WebhookSend(nil), f.sent...)
}

// eventLog returns the posts and thread creations of a run in order.
func (f *fakeDiscordAPI) eventLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.events...)
}

func (f *fakeDiscordAPI) attachmentEdits() []webhookAttachmentEditCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]webhookAttachmentEditCall(nil), f.webhookAttachmentEdits...)
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
	if content, ok := f.messageContents[key]; ok {
		return DiscordFetchedMessage{Content: content}, nil
	}
	// A webhook message the fake posted is fetchable like any Discord
	// message, so edit paths can read back what was rendered.
	if content, ok := f.sentContents[messageID]; ok {
		return DiscordFetchedMessage{Content: content}, nil
	}
	return DiscordFetchedMessage{}, errors.New("message not found")
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
func (f *fakeDiscordAPI) SendWebhook(webhookID, token string, msg WebhookSend) (WebhookSendResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		if err := f.sendErr(msg); err != nil {
			return WebhookSendResult{}, err
		}
	}
	f.nextID++
	messageID := fakeSentID(f.nextID)
	if f.sentContents == nil {
		f.sentContents = map[string]string{}
	}
	f.sentContents[messageID] = msg.Content
	f.sent = append(f.sent, msg)
	f.events = append(f.events, "message")
	ids := make([]string, len(msg.Files))
	for i := range msg.Files {
		ids[i] = fmt.Sprintf("att-%d-%d", f.nextID, i)
	}
	return WebhookSendResult{MessageID: messageID, AttachmentIDs: ids}, nil
}
func (f *fakeDiscordAPI) EditWebhook(webhookID, token, messageID, threadID, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	call := webhookEditCall{messageID: messageID, threadID: threadID, content: content}
	if f.editErr != nil {
		if err := f.editErr(call); err != nil {
			return err
		}
	}
	if f.sentContents == nil {
		f.sentContents = map[string]string{}
	}
	f.sentContents[messageID] = content
	f.webhookEdits = append(f.webhookEdits, call)
	return nil
}
func (f *fakeDiscordAPI) EditWebhookAttachments(webhookID, token, messageID, threadID string, attachments []WebhookAttachmentEdit) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	copied := append([]WebhookAttachmentEdit(nil), attachments...)
	f.webhookAttachmentEdits = append(f.webhookAttachmentEdits, webhookAttachmentEditCall{
		messageID: messageID, threadID: threadID, attachments: copied,
	})
	return nil
}
func (f *fakeDiscordAPI) DeleteWebhook(webhookID, token, messageID, threadID string) error {
	f.webhookDeletes = append(f.webhookDeletes, webhookDeleteCall{messageID: messageID, threadID: threadID})
	return nil
}
func (f *fakeDiscordAPI) DeleteWebhookWithToken(webhookID, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletedWebhooks = append(f.deletedWebhooks, webhookID)
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
	threadID = fakeThreadID(f.nextID)
	if isThreadOnlyChannelType(channelType) {
		initialMessageID = threadID
	}
	f.threads = append(f.threads, threadCall{channelID: channelID, channelType: channelType, name: name, content: initialMessage, embeds: embeds, appliedTags: append([]string(nil), appliedTags...), files: append([]WebhookFile(nil), files...)})
	f.events = append(f.events, "thread")
	f.registerThread(threadID, channelID)
	return threadID, initialMessageID, nil
}

// registerThread makes a created thread visible to Channel with open
// thread metadata, so archive and lock changes can be observed.
func (f *fakeDiscordAPI) registerThread(threadID, parentID string) {
	if f.channels == nil {
		f.channels = map[string]*discordgo.Channel{}
	}
	f.channels[threadID] = &discordgo.Channel{ID: threadID, ParentID: parentID, ThreadMetadata: &discordgo.ThreadMetadata{}}
}
func (f *fakeDiscordAPI) CreateThreadFromMessage(channelID, messageID, name string) (threadID string, err error) {
	f.nextID++
	f.threads = append(f.threads, threadCall{channelID: channelID, messageID: messageID, name: name})
	f.events = append(f.events, "thread")
	f.registerThread(fakeThreadID(f.nextID), channelID)
	return fakeThreadID(f.nextID), nil
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
func (f *fakeDiscordAPI) SetThreadState(threadID string, archived, locked *bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.threadStates = append(f.threadStates, threadStateCall{threadID: threadID, archived: *archived, locked: *locked})
	if ch, ok := f.channels[threadID]; ok && ch.ThreadMetadata != nil {
		ch.ThreadMetadata.Archived, ch.ThreadMetadata.Locked = *archived, *locked
	}
	return nil
}
func (f *fakeDiscordAPI) ChannelMessagePage(guildID, channelID, beforeID, afterID string) ([]DiscordMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	all := append([]DiscordMessage(nil), f.history[channelID]...)
	slices.SortFunc(all, func(a, b DiscordMessage) int { return compareSnowflakes(a.ID, b.ID) })
	var out []DiscordMessage
	for _, m := range all {
		if afterID != "" && !snowflakeIDLess(afterID, m.ID) {
			continue
		}
		if beforeID != "" && !snowflakeIDLess(m.ID, beforeID) {
			continue
		}
		out = append(out, m)
	}
	if len(out) > discordMessagePageLimit {
		if beforeID != "" {
			out = out[len(out)-discordMessagePageLimit:]
		} else {
			out = out[:discordMessagePageLimit]
		}
	}
	return out, nil
}
func (f *fakeDiscordAPI) PublicThreads(guildID string, parentIDs []string, includeArchived bool, archivedLimit int) (DiscordThreads, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out DiscordThreads
	archived := 0
	for _, thread := range f.publicThreads {
		if !slices.Contains(parentIDs, thread.ParentID) || (!includeArchived && thread.Archived) {
			continue
		}
		if thread.Archived {
			if archivedLimit > 0 && archived >= archivedLimit {
				out.Truncated = true
				continue
			}
			archived++
		}
		out.Threads = append(out.Threads, thread)
	}
	return out, nil
}

// ForgetMemberCache implements DiscordAPI; the fake keeps no member cache but
// records that the cache was dropped.
func (f *fakeDiscordAPI) ForgetMemberCache() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.forgetMemberCacheCalls++
}

type echoTranslator struct {
	mu             sync.Mutex
	contexts       []TranslationContext
	altContexts    []TranslationContext
	pollContexts   []TranslationContext
	visions        [][]visionImage
	altVisions     [][]visionImage
	userPrompts    []string
	altUserPrompts []string
}

func (e *echoTranslator) TranslateMulti(ctx context.Context, prepared preparedTranslation) (MultiTranslationResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.contexts = append(e.contexts, prepared.translationContext)
	e.visions = append(e.visions, append([]visionImage(nil), prepared.visionImages...))
	e.userPrompts = append(e.userPrompts, prepared.userPrompt())
	out := make(map[string]string, len(prepared.targetLanguages))
	for _, lang := range prepared.targetLanguages {
		text := prepared.content
		if hasTranslatableText(text) || strings.TrimSpace(text) != "" {
			out[lang] = "[" + lang + "] " + text
		} else {
			out[lang] = text
		}
	}
	return MultiTranslationResult{Translations: out}, nil
}

func echoAttachmentAlts(prepared preparedTranslation) map[string][]string {
	if translatableAttachmentCount(prepared.translationContext.Attachments) == 0 {
		return nil
	}
	alts := make(map[string][]string, len(prepared.targetLanguages))
	for _, lang := range prepared.targetLanguages {
		descriptions := make([]string, len(prepared.translationContext.Attachments))
		for i, attachment := range prepared.translationContext.Attachments {
			if hasTranslatableText(attachment.Description) {
				descriptions[i] = "[" + lang + "] " + attachment.Description
			} else {
				descriptions[i] = attachment.Description
			}
		}
		alts[lang] = descriptions
	}
	return alts
}

func (e *echoTranslator) TranslateAttachmentAlts(ctx context.Context, prepared preparedTranslation) (AttachmentAltTranslationResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.altContexts = append(e.altContexts, prepared.translationContext)
	e.altVisions = append(e.altVisions, append([]visionImage(nil), prepared.visionImages...))
	e.altUserPrompts = append(e.altUserPrompts, prepared.userPrompt())
	return AttachmentAltTranslationResult{Descriptions: echoAttachmentAlts(prepared)}, nil
}
func (e *echoTranslator) TranslatePollMulti(ctx context.Context, prepared preparedTranslation) (PollMultiTranslationResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
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
	e.mu.Lock()
	defer e.mu.Unlock()
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

func echoBatch(prepared preparedTranslation) BatchTranslationResult {
	lang := prepared.targetLanguages[0]
	out := make([]string, len(prepared.batchSources))
	for i, source := range prepared.batchSources {
		out[i] = "[" + lang + "] " + source
	}
	return BatchTranslationResult{Translations: out}
}

func (e *echoTranslator) TranslateBatch(ctx context.Context, prepared preparedTranslation) (BatchTranslationResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.contexts = append(e.contexts, prepared.translationContext)
	e.userPrompts = append(e.userPrompts, prepared.userPrompt())
	return echoBatch(prepared), nil
}

type delayedAltTranslator struct {
	echoTranslator
	altStarted chan struct{}
	altRelease chan struct{}
}

func (d *delayedAltTranslator) TranslateAttachmentAlts(ctx context.Context, prepared preparedTranslation) (AttachmentAltTranslationResult, error) {
	if d.altStarted != nil {
		select {
		case <-d.altStarted:
		default:
			close(d.altStarted)
		}
	}
	if d.altRelease != nil {
		select {
		case <-d.altRelease:
		case <-ctx.Done():
			return AttachmentAltTranslationResult{}, ctx.Err()
		}
	}
	return d.echoTranslator.TranslateAttachmentAlts(ctx, prepared)
}

type failingAltTranslator struct {
	echoTranslator
	err error
}

func (f *failingAltTranslator) TranslateAttachmentAlts(ctx context.Context, prepared preparedTranslation) (AttachmentAltTranslationResult, error) {
	if f.err != nil {
		return AttachmentAltTranslationResult{}, f.err
	}
	return f.echoTranslator.TranslateAttachmentAlts(ctx, prepared)
}

func mirroredImageDescription(discord *fakeDiscordAPI) string {
	if len(discord.webhookAttachmentEdits) > 0 && len(discord.webhookAttachmentEdits[0].attachments) > 0 {
		return discord.webhookAttachmentEdits[0].attachments[0].Description
	}
	if len(discord.sent) > 0 && len(discord.sent[0].Files) > 0 {
		return discord.sent[0].Files[0].Description
	}
	return ""
}

type topicSummaryTranslator struct {
	echoTranslator
	mu       sync.Mutex
	requests []preparedTranslation
	summary  string
	block    <-chan struct{}
	started  chan struct{}
	err      error
}

func (t *topicSummaryTranslator) SummarizeTopic(ctx context.Context, prepared preparedTranslation) (TopicSummaryResult, error) {
	if t.started != nil {
		select {
		case <-t.started:
		default:
			close(t.started)
		}
	}
	if t.block != nil {
		select {
		case <-t.block:
		case <-ctx.Done():
			return TopicSummaryResult{}, ctx.Err()
		}
	}
	t.mu.Lock()
	t.requests = append(t.requests, prepared)
	t.mu.Unlock()
	if t.err != nil {
		return TopicSummaryResult{}, t.err
	}
	summary := t.summary
	if summary == "" {
		summary = "ongoing topic"
	}
	return TopicSummaryResult{Summary: summary, InputTokens: 10, OutputTokens: 5}, nil
}

func (t *topicSummaryTranslator) summaryRequests() []preparedTranslation {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]preparedTranslation(nil), t.requests...)
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
	if slices.Contains(prepared.targetLanguages, s.failLanguage) {
		return MultiTranslationResult{}, errors.New("translation failed")
	}
	out := make(map[string]string, len(prepared.targetLanguages))
	for _, lang := range prepared.targetLanguages {
		out[lang] = "[" + lang + "] " + prepared.content
	}
	return MultiTranslationResult{Translations: out}, nil
}
func (s *selectiveFailTranslator) TranslateAttachmentAlts(ctx context.Context, prepared preparedTranslation) (AttachmentAltTranslationResult, error) {
	if slices.Contains(prepared.targetLanguages, s.failLanguage) {
		return AttachmentAltTranslationResult{}, errors.New("translation failed")
	}
	return AttachmentAltTranslationResult{Descriptions: echoAttachmentAlts(prepared)}, nil
}
func (s *selectiveFailTranslator) TranslatePollMulti(ctx context.Context, prepared preparedTranslation) (PollMultiTranslationResult, error) {
	if slices.Contains(prepared.targetLanguages, s.failLanguage) {
		return PollMultiTranslationResult{}, errors.New("translation failed")
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
	if slices.Contains(prepared.targetLanguages, s.failLanguage) {
		return ThreadCreateMultiTranslationResult{}, errors.New("translation failed")
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

func (s *selectiveFailTranslator) TranslateBatch(ctx context.Context, prepared preparedTranslation) (BatchTranslationResult, error) {
	if slices.Contains(prepared.targetLanguages, s.failLanguage) {
		return BatchTranslationResult{}, errors.New("translation failed")
	}
	return echoBatch(prepared), nil
}

type stickyErrorTranslator struct {
	echoTranslator
	mu  sync.Mutex
	err error
}

func (s *stickyErrorTranslator) setErr(err error) {
	s.mu.Lock()
	s.err = err
	s.mu.Unlock()
}

func (s *stickyErrorTranslator) currentErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *stickyErrorTranslator) TranslateMulti(ctx context.Context, prepared preparedTranslation) (MultiTranslationResult, error) {
	if err := s.currentErr(); err != nil {
		return MultiTranslationResult{}, err
	}
	return s.echoTranslator.TranslateMulti(ctx, prepared)
}

func (s *stickyErrorTranslator) TranslateAttachmentAlts(ctx context.Context, prepared preparedTranslation) (AttachmentAltTranslationResult, error) {
	if err := s.currentErr(); err != nil {
		return AttachmentAltTranslationResult{}, err
	}
	return s.echoTranslator.TranslateAttachmentAlts(ctx, prepared)
}

func (s *stickyErrorTranslator) TranslatePollMulti(ctx context.Context, prepared preparedTranslation) (PollMultiTranslationResult, error) {
	if err := s.currentErr(); err != nil {
		return PollMultiTranslationResult{}, err
	}
	return s.echoTranslator.TranslatePollMulti(ctx, prepared)
}

func (s *stickyErrorTranslator) TranslateThreadCreateMulti(ctx context.Context, prepared preparedTranslation) (ThreadCreateMultiTranslationResult, error) {
	if err := s.currentErr(); err != nil {
		return ThreadCreateMultiTranslationResult{}, err
	}
	return s.echoTranslator.TranslateThreadCreateMulti(ctx, prepared)
}
func (s *stickyErrorTranslator) TranslateBatch(ctx context.Context, prepared preparedTranslation) (BatchTranslationResult, error) {
	if err := s.currentErr(); err != nil {
		return BatchTranslationResult{}, err
	}
	return s.echoTranslator.TranslateBatch(ctx, prepared)
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
