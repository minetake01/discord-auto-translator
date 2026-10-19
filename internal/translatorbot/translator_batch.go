package translatorbot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"regexp"
	"strconv"
	"strings"
)

const (
	// batchMaxMessages caps one backfill request. Production comparison at
	// 10/20/40 messages showed no structural failures and cost flattening
	// out around 40.
	batchMaxMessages = 40
	// batchMaxOutputTokens leaves headroom under openaiMaxTokens for JSON keys
	// and estimation error.
	batchMaxOutputTokens = 2800
	batchReplyContextMax = 5

	batchTranslationTaskIntro = "Translate every <message> inside <batch> into the language in <target_languages>. Return one object whose keys are the <message> id attributes copied character-for-character; each value must include translated_text for that message only. translated_text must be empty when its <message> is empty.\n" +
		"Translate each <message> on its own and completely: never move, merge, split, summarize, or drop content across messages, and never copy text or placeholder tokens from one <message> into another.\n" +
		"Translate each <message> with the meaning it had when it was posted. Resolve it from <topic_summary>, <recent_context>, <reply_context>, and earlier messages in <batch>; use later messages in <batch> only for ambiguity that earlier context cannot resolve, and never let them add details the message itself does not say.\n" +
		"A reply_to attribute names the message being replied to: an id in <batch> or a ref in <reply_context>.\n" +
		"Images supplied after the text prompt are visual background only: the images numbered by attachments attributes in <batch> in index order, then <image> elements from <recent_context> and <reply_context> in index order, then <image> elements inside <site>.\n"
	batchReplyContextDescription = "<reply_context> contains earlier messages, from before <batch>, that batch messages reply to (oldest first)."
)

// BatchMessage is one source message of a backfill batch. Attachments hold
// the vision-loaded images of this message with indexes numbered across the
// whole batch.
type BatchMessage struct {
	Author            string
	Content           string
	Attachments       []TranslationAttachment
	ReplyTo           string // batch id or reply_context ref; empty when none
	MentionedUsers    map[string]string
	MentionedChannels map[string]string
	MentionedRoles    map[string]string
}

type BatchTranslationResult struct {
	Translations []string // indexed like the batch messages
	InputTokens  int
	OutputTokens int
}

// errBatchContent marks a response that violated the batch contract. The
// caller splits the batch and retries smaller pieces.
var errBatchContent = fmt.Errorf("%w: batch contract violated", errTranslationContent)

func batchMessageID(i int) string { return strconv.Itoa(i + 1) }

func buildBatchTranslationSystemInstruction() string {
	return buildTranslationSystemInstructionWithReplies(batchTranslationTaskIntro, "each <message> in <batch>", batchReplyContextDescription) + messageTranslationFewShotExamples
}

// prepareBatchTranslation protects every message with one shared Protector
// so placeholder tokens and [SITE:N] ids are unique across the batch.
func prepareBatchTranslation(targetLanguage string, messages []BatchMessage, translationContext TranslationContext, glossary []GlossaryEntry) (preparedTranslation, error) {
	if len(messages) == 0 {
		return preparedTranslation{}, errors.New("batch translation requires messages")
	}
	translationContext.MentionedUsers = mergeNameMaps(messages, func(m BatchMessage) map[string]string { return m.MentionedUsers })
	translationContext.MentionedChannels = mergeNameMaps(messages, func(m BatchMessage) map[string]string { return m.MentionedChannels })
	translationContext.MentionedRoles = mergeNameMaps(messages, func(m BatchMessage) map[string]string { return m.MentionedRoles })
	normalized, p, err := beginPreparedTranslation([]string{targetLanguage}, &translationContext)
	if err != nil {
		return preparedTranslation{}, err
	}
	if len(normalized) != 1 {
		return preparedTranslation{}, fmt.Errorf("batch translation requires exactly one target language, got %q", targetLanguage)
	}
	protected := make([]string, len(messages))
	sources := make([]string, len(messages))
	for i, m := range messages {
		protected[i] = p.Protect(m.Content)
		sources[i] = m.Content
	}
	assignSiteContext(p, &translationContext)
	alwaysGlossary, matchedGlossary := splitGlossaryEntries(strings.Join(sources, "\n"), glossary)
	items := append([]BatchMessage(nil), messages...)
	prepared := preparedTranslation{
		targetLanguages:    normalized,
		systemInstruction:  buildBatchTranslationSystemInstruction(),
		protector:          p,
		guildID:            translationContext.GuildID,
		messageID:          translationContext.MessageID,
		translationContext: translationContext,
		promptCacheKey:     translationPromptCacheKey(translationContext, "batch"),
		alwaysGlossary:     alwaysGlossary,
		matchedGlossary:    matchedGlossary,
		batchSources:       sources,
		batchProtected:     protected,
		writeSource: func(b *strings.Builder) {
			writeBatchSection(b, items, protected)
		},
	}
	prepared.buildUserPrompt()
	return prepared, nil
}

func mergeNameMaps(messages []BatchMessage, get func(BatchMessage) map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range messages {
		for k, v := range get(m) {
			out[k] = v
		}
	}
	return out
}

func writeBatchSection(b *strings.Builder, messages []BatchMessage, protected []string) {
	b.WriteString("<batch>")
	for i, m := range messages {
		b.WriteString(`<message id="`)
		b.WriteString(batchMessageID(i))
		b.WriteString(`"`)
		if strings.TrimSpace(m.Author) != "" {
			b.WriteString(` author="`)
			writeXMLAttributeValue(b, m.Author)
			b.WriteString(`"`)
		}
		if m.ReplyTo != "" {
			b.WriteString(` reply_to="`)
			writeXMLAttributeValue(b, m.ReplyTo)
			b.WriteString(`"`)
		}
		if len(m.Attachments) > 0 {
			indexes := make([]string, len(m.Attachments))
			for j, attachment := range m.Attachments {
				indexes[j] = strconv.Itoa(attachment.Index)
			}
			b.WriteString(` attachments="`)
			b.WriteString(strings.Join(indexes, ","))
			b.WriteString(`"`)
		}
		b.WriteString(">")
		writeXMLText(b, protected[i])
		b.WriteString("</message>")
	}
	b.WriteString("</batch>")
}

func openaiBatchTranslationSchema(count int) (json.RawMessage, error) {
	if count <= 0 {
		return nil, errors.New("batch translation JSON schema requires messages")
	}
	properties := make(map[string]any, count)
	required := make([]string, count)
	for i := range count {
		id := batchMessageID(i)
		required[i] = id
		properties[id] = openaiObjectSchema([]string{"translated_text"}, map[string]any{
			"translated_text": map[string]any{
				"type":        "string",
				"description": fmt.Sprintf(`<message id="%s"> translated into the target language. Empty when that <message> is empty.`, id),
			},
		})
	}
	return marshalOpenAIJSONSchema(openaiObjectSchema(required, properties))
}

// structuredOutputPadding finds whitespace runs between JSON tokens. Some
// providers occasionally cut a string short and pad the rest of a strict
// JSON response with whitespace; the result still parses but is truncated.
var structuredOutputPadding = regexp.MustCompile(`"\s{16,}[,}\]]|[,{\[:]\s{16,}"`)

var placeholderTokenPattern = regexp.MustCompile(`\[[A-Z]+(?::[^\[\]]*)?\]`)

// parseBatchTranslationResponse enforces the batch contract: exactly one
// translation per id, non-empty when the source has text, no placeholder
// tokens that belong to another message, and no padded truncation.
func parseBatchTranslationResponse(raw string, sources, protected []string, protector *Protector) ([]string, error) {
	if structuredOutputPadding.MatchString(raw) {
		return nil, fmt.Errorf("%w: whitespace-padded structured output", errBatchContent)
	}
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	var parsed map[string]json.RawMessage
	if err := decoder.Decode(&parsed); err != nil {
		return nil, fmt.Errorf("%w: %v", errBatchContent, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("%w: multiple JSON values", errBatchContent)
	}
	if len(parsed) != len(sources) {
		return nil, fmt.Errorf("%w: got %d messages, want %d", errBatchContent, len(parsed), len(sources))
	}
	// The shared Protector numbers every occurrence, so each token belongs
	// to exactly one message.
	owner := make(map[string]int)
	for i, text := range protected {
		for _, token := range placeholderTokenPattern.FindAllString(text, -1) {
			owner[token] = i
		}
	}
	out := make([]string, len(sources))
	for i := range sources {
		id := batchMessageID(i)
		itemRaw, ok := parsed[id]
		if !ok {
			return nil, fmt.Errorf("%w: missing message %q", errBatchContent, id)
		}
		itemDecoder := json.NewDecoder(bytes.NewReader(itemRaw))
		itemDecoder.DisallowUnknownFields()
		var item translationResponseItem
		if err := itemDecoder.Decode(&item); err != nil {
			return nil, fmt.Errorf("%w: message %q: %v", errBatchContent, id, err)
		}
		text := strings.TrimSpace(html.UnescapeString(item.TranslatedText))
		if text == "" {
			if hasTranslatableText(sources[i]) {
				return nil, fmt.Errorf("%w: empty translation for message %q", errBatchContent, id)
			}
			out[i] = sources[i]
			continue
		}
		for _, token := range placeholderTokenPattern.FindAllString(text, -1) {
			if from, known := owner[token]; known && from != i {
				return nil, fmt.Errorf("%w: message %q contains placeholder %s from another message", errBatchContent, id, token)
			}
		}
		out[i] = protector.Restore(text)
	}
	return out, nil
}

func estimateBatchOutputTokens(contents []string) int {
	total := 0
	for _, content := range contents {
		total += estimateMessageOutputTokens(content)
	}
	return total
}

// estimateMessageOutputTokens over-approximates one translated_text entry
// including its JSON key so a batch never approaches max_tokens.
func estimateMessageOutputTokens(content string) int {
	return EstimateTranslationTokens(content, "")*2 + 16
}
