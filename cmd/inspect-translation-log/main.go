// Command inspect-translation-log summarizes TRANSLATION_DEBUG_LOG_PATH JSON Lines.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func main() {
	pathFlag := flag.String("path", "", "debug log path (default: TRANSLATION_DEBUG_LOG_PATH, else .env, else ./translation-debug.log)")
	errorsOnly := flag.Bool("errors", false, "show only entries with an error")
	guildID := flag.String("guild-id", "", "filter by guild_id")
	messageID := flag.String("message-id", "", "filter by message_id")
	limit := flag.Int("limit", 50, "max entries to print after filtering (0 = all; most recent)")
	detail := flag.Bool("detail", false, "print source excerpt, translations, cache, cost, usage, reasoning, synthesized prompts, and error text")
	prompt := flag.Bool("prompt", false, "print the full synthesized system and user prompts")
	stats := flag.Bool("stats", false, "print cache-hit and cost aggregates for the filtered entries")
	raw := flag.Bool("raw", false, "print the full JSON object for each matching entry")
	flag.Parse()

	path, pathSource, err := resolveLogPath(*pathFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "inspect-translation-log: %v\n", err)
		os.Exit(1)
	}

	entries, err := loadEntries(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "inspect-translation-log: %v\n", missingLogHint(path, pathSource, err))
		os.Exit(1)
	}

	filtered := make([]logEntry, 0, len(entries))
	var errorCount int
	var durationSum int64
	for _, entry := range entries {
		if entry.Error != "" {
			errorCount++
		}
		durationSum += entry.DurationMS
		if *errorsOnly && entry.Error == "" {
			continue
		}
		if *guildID != "" && entry.GuildID != *guildID {
			continue
		}
		if *messageID != "" && entry.MessageID != *messageID {
			continue
		}
		filtered = append(filtered, entry)
	}

	start := 0
	if *limit > 0 && len(filtered) > *limit {
		start = len(filtered) - *limit
	}
	shown := filtered[start:]

	fmt.Printf("path=%s (%s)\n", path, pathSource)
	fmt.Printf("entries=%d errors=%d shown=%d", len(entries), errorCount, len(shown))
	if len(entries) > 0 {
		fmt.Printf(" avg_duration_ms=%d", durationSum/int64(len(entries)))
	}
	fmt.Println()
	if *stats {
		printStats(filtered)
	}
	if len(shown) == 0 {
		return
	}
	fmt.Println()

	for i, entry := range shown {
		if *raw {
			line, err := json.Marshal(entry.raw)
			if err != nil {
				fmt.Fprintf(os.Stderr, "inspect-translation-log: encode entry: %v\n", err)
				os.Exit(1)
			}
			fmt.Println(string(line))
			continue
		}
		printSummary(entry)
		if *detail {
			printDetail(entry)
		}
		if *prompt {
			printPrompt(entry)
		}
		if (*detail || *prompt) && i != len(shown)-1 {
			fmt.Println(strings.Repeat("-", 72))
		}
	}
}

type logEntry struct {
	Time               time.Time
	GuildID            string
	MessageID          string
	Model              string
	SchemaName         string
	TargetLanguages    []string
	DurationMS         int64
	HTTPStatus         int
	Error              string
	PromptCacheKey     string
	PromptCacheTTLSent *bool
	PromptCacheHit     *bool
	SystemInstruction  string
	UserPromptFrozen   string
	UserPromptVariable string
	VisionImageCount   int
	Usage              *loggedUsage
	Request            json.RawMessage
	Response           json.RawMessage
	ResponseText       string
	raw                map[string]json.RawMessage
}

type loggedUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CachedTokens     *int
	CacheWriteTokens *int
	ReasoningTokens  *int
	CostUSD          *float64
}

type requestPayload struct {
	Messages           []chatMessage   `json:"messages"`
	PromptCacheKey     string          `json:"prompt_cache_key"`
	PromptCacheOptions json.RawMessage `json:"prompt_cache_options"`
}

type chatMessage struct {
	Role             string          `json:"role"`
	Content          json.RawMessage `json:"content"`
	Reasoning        string          `json:"reasoning"`
	ReasoningContent string          `json:"reasoning_content"`
}

type responsePayload struct {
	Choices []struct {
		FinishReason string      `json:"finish_reason"`
		Message      chatMessage `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens        int      `json:"prompt_tokens"`
		CompletionTokens    int      `json:"completion_tokens"`
		TotalTokens         int      `json:"total_tokens"`
		CachedTokens        *int     `json:"cached_tokens"`
		CacheWriteTokens    *int     `json:"cache_write_tokens"`
		Cost                *float64 `json:"cost"`
		PromptTokensDetails *struct {
			CachedTokens     *int `json:"cached_tokens"`
			CacheWriteTokens *int `json:"cache_write_tokens"`
		} `json:"prompt_tokens_details"`
		CompletionTokensDetails *struct {
			ReasoningTokens *int `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
	} `json:"usage"`
}

type translationPayload struct {
	Translations []struct {
		Language       string `json:"language"`
		TranslatedText string `json:"translated_text"`
	} `json:"translations"`
}

var finalMessageRE = regexp.MustCompile(`(?s)<final_message[^>]*>(.*?)</final_message>`)

func resolveLogPath(flagPath string) (path string, source string, err error) {
	if strings.TrimSpace(flagPath) != "" {
		return filepath.Clean(flagPath), "--path", nil
	}
	if envPath := strings.TrimSpace(os.Getenv("TRANSLATION_DEBUG_LOG_PATH")); envPath != "" {
		return filepath.Clean(envPath), "TRANSLATION_DEBUG_LOG_PATH", nil
	}
	if envPath, ok := readDotEnvValue(".env", "TRANSLATION_DEBUG_LOG_PATH"); ok {
		return filepath.Clean(envPath), ".env", nil
	}
	return "translation-debug.log", "default", nil
}

func readDotEnvValue(path, key string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) != key {
			continue
		}
		value := strings.TrimSpace(parts[1])
		value = strings.Trim(value, `"'`)
		if value == "" {
			return "", false
		}
		return value, true
	}
	return "", false
}

func missingLogHint(path, source string, err error) error {
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return fmt.Errorf("%w\n"+
		"  resolved path: %s (from %s)\n"+
		"  No debug log file exists yet. Set TRANSLATION_DEBUG_LOG_PATH, restart the bot, and trigger a translation.",
		err, path, source)
}

func loadEntries(path string) ([]logEntry, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	paths := []string{}
	if _, err := os.Stat(path + ".1"); err == nil {
		paths = append(paths, path+".1")
	}
	if _, err := os.Stat(path); err != nil {
		if len(paths) == 0 {
			return nil, err
		}
	} else {
		paths = append(paths, path)
	}

	var entries []logEntry
	for _, p := range paths {
		fileEntries, err := readFileEntries(p)
		if err != nil {
			return nil, err
		}
		entries = append(entries, fileEntries...)
	}
	return entries, nil
}

func readFileEntries(path string) ([]logEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	var entries []logEntry
	lineNo := 0
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			lineNo++
			trimmed := bytes.TrimSpace(line)
			if len(trimmed) == 0 {
				continue
			}
			entry, parseErr := parseEntry(trimmed)
			if parseErr != nil {
				return nil, fmt.Errorf("%s:%d: %w", path, lineNo, parseErr)
			}
			entries = append(entries, entry)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	return entries, nil
}

func parseEntry(line []byte) (logEntry, error) {
	raw := make(map[string]json.RawMessage)
	if err := json.Unmarshal(line, &raw); err != nil {
		return logEntry{}, fmt.Errorf("invalid JSON: %w", err)
	}
	entry := logEntry{raw: raw}
	if v, ok := raw["time"]; ok {
		var t time.Time
		if err := json.Unmarshal(v, &t); err != nil {
			return logEntry{}, fmt.Errorf("time: %w", err)
		}
		entry.Time = t
	}
	entry.GuildID = decodeString(raw["guild_id"])
	entry.MessageID = decodeString(raw["message_id"])
	entry.Model = decodeString(raw["model"])
	entry.SchemaName = decodeString(raw["schema_name"])
	entry.Error = decodeString(raw["error"])
	entry.ResponseText = decodeString(raw["response_text"])
	entry.PromptCacheKey = decodeString(raw["prompt_cache_key"])
	entry.SystemInstruction = decodeString(raw["system_instruction"])
	entry.UserPromptFrozen = decodeString(raw["user_prompt_frozen"])
	entry.UserPromptVariable = decodeString(raw["user_prompt_variable"])
	if v, ok := raw["duration_ms"]; ok {
		if err := json.Unmarshal(v, &entry.DurationMS); err != nil {
			return logEntry{}, fmt.Errorf("duration_ms: %w", err)
		}
	}
	if v, ok := raw["http_status"]; ok {
		if err := json.Unmarshal(v, &entry.HTTPStatus); err != nil {
			return logEntry{}, fmt.Errorf("http_status: %w", err)
		}
	}
	if v, ok := raw["target_languages"]; ok {
		if err := json.Unmarshal(v, &entry.TargetLanguages); err != nil {
			return logEntry{}, fmt.Errorf("target_languages: %w", err)
		}
	}
	if v, ok := raw["prompt_cache_ttl_sent"]; ok {
		var sent bool
		if err := json.Unmarshal(v, &sent); err != nil {
			return logEntry{}, fmt.Errorf("prompt_cache_ttl_sent: %w", err)
		}
		entry.PromptCacheTTLSent = &sent
	}
	if v, ok := raw["prompt_cache_hit"]; ok {
		var hit bool
		if err := json.Unmarshal(v, &hit); err != nil {
			return logEntry{}, fmt.Errorf("prompt_cache_hit: %w", err)
		}
		entry.PromptCacheHit = &hit
	}
	if v, ok := raw["vision_image_count"]; ok {
		if err := json.Unmarshal(v, &entry.VisionImageCount); err != nil {
			return logEntry{}, fmt.Errorf("vision_image_count: %w", err)
		}
	}
	if v, ok := raw["usage"]; ok {
		usage, err := decodeLoggedUsage(v)
		if err != nil {
			return logEntry{}, fmt.Errorf("usage: %w", err)
		}
		entry.Usage = usage
	}
	entry.Request = raw["request"]
	entry.Response = raw["response"]
	return entry, nil
}

func decodeLoggedUsage(raw json.RawMessage) (*loggedUsage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var parsed struct {
		PromptTokens     int      `json:"prompt_tokens"`
		CompletionTokens int      `json:"completion_tokens"`
		TotalTokens      int      `json:"total_tokens"`
		CachedTokens     *int     `json:"cached_tokens"`
		CacheWriteTokens *int     `json:"cache_write_tokens"`
		ReasoningTokens  *int     `json:"reasoning_tokens"`
		CostUSD          *float64 `json:"cost_usd"`
		Cost             *float64 `json:"cost"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	cost := parsed.CostUSD
	if cost == nil {
		cost = parsed.Cost
	}
	return &loggedUsage{
		PromptTokens:     parsed.PromptTokens,
		CompletionTokens: parsed.CompletionTokens,
		TotalTokens:      parsed.TotalTokens,
		CachedTokens:     parsed.CachedTokens,
		CacheWriteTokens: parsed.CacheWriteTokens,
		ReasoningTokens:  parsed.ReasoningTokens,
		CostUSD:          cost,
	}, nil
}

func decodeString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func printSummary(entry logEntry) {
	status := "ok"
	if entry.Error != "" {
		status = "ERROR"
	}
	langs := strings.Join(entry.TargetLanguages, ",")
	if langs == "" {
		langs = "-"
	}
	httpStatus := "-"
	if entry.HTTPStatus != 0 {
		httpStatus = fmt.Sprintf("%d", entry.HTTPStatus)
	}
	usage := resolvedUsage(entry)
	errHint := ""
	if entry.Error != "" {
		errHint = " " + oneLine(entry.Error, 80)
	}
	fmt.Printf("%s  %-5s  http=%-3s  %6dms  guild=%s  msg=%s  langs=%s  cache=%s  cost=%s%s\n",
		entry.Time.Local().Format("2006-01-02 15:04:05"),
		status,
		httpStatus,
		entry.DurationMS,
		dash(entry.GuildID),
		dash(entry.MessageID),
		langs,
		cacheStatusLabel(resolvedCacheHit(entry, usage)),
		formatCost(usage.CostUSD),
		errHint,
	)
}

func printDetail(entry logEntry) {
	usage := resolvedUsage(entry)
	if schema := dash(entry.SchemaName); schema != "-" {
		fmt.Printf("  schema: %s\n", entry.SchemaName)
	}
	if model := dash(entry.Model); model != "-" {
		fmt.Printf("  model: %s\n", entry.Model)
	}
	if key := resolvedCacheKey(entry); key != "" {
		fmt.Printf("  cache_key: %s\n", key)
	}
	fmt.Printf("  cache: %s\n", formatCacheDetail(entry, usage))
	if usage.CostUSD != nil {
		fmt.Printf("  cost_usd: %s\n", formatUSD(*usage.CostUSD))
	}
	if source := extractFinalMessage(entry); source != "" {
		fmt.Printf("  source: %s\n", oneLine(source, 200))
	}
	resp := parseResponse(entry.Response)
	if reason := firstFinishReason(resp); reason != "" {
		fmt.Printf("  finish_reason: %s\n", reason)
	}
	if tokenLine := formatTokenLine(usage); tokenLine != "" {
		fmt.Printf("  tokens: %s\n", tokenLine)
	}
	if reasoning := extractReasoningText(resp); reasoning != "" {
		fmt.Printf("  reasoning: %s\n", oneLine(reasoning, 0))
	}
	if text := extractOutputText(resp); text != "" {
		if translations := parseTranslations(text); len(translations) > 0 {
			for _, item := range translations {
				fmt.Printf("  [%s] %s\n", item.Language, oneLine(item.TranslatedText, 200))
			}
		} else {
			fmt.Printf("  output_text: %s\n", oneLine(text, 200))
		}
	} else if entry.ResponseText != "" {
		fmt.Printf("  response_text: %s\n", oneLine(entry.ResponseText, 200))
	}
	if entry.Error != "" {
		fmt.Printf("  error: %s\n", entry.Error)
	}
	system, frozen, variable := resolvedPrompts(entry)
	if system != "" || frozen != "" || variable != "" {
		fmt.Printf("  prompt: system=%dB frozen=%dB variable=%dB images=%d\n",
			len(system), len(frozen), len(variable), entry.VisionImageCount)
	}
}

func printPrompt(entry logEntry) {
	system, frozen, variable := resolvedPrompts(entry)
	printPromptSection("system", system)
	printPromptSection("user frozen", frozen)
	printPromptSection("user variable", variable)
}

func printPromptSection(name, body string) {
	fmt.Printf("  --- %s ---\n", name)
	if strings.TrimSpace(body) == "" {
		fmt.Printf("  (empty)\n")
		return
	}
	for _, line := range strings.Split(body, "\n") {
		fmt.Printf("  %s\n", line)
	}
}

func printStats(entries []logEntry) {
	var (
		costCount             int
		promptSum, cachedSum  int
		completionSum         int
		reasoningSum          int
		cacheWriteSum         int
		hits, misses, unknown int
		ttlWrites             int
		costTotal             float64
		cachedKnown           bool
	)
	for _, entry := range entries {
		usage := resolvedUsage(entry)
		promptSum += usage.PromptTokens
		completionSum += usage.CompletionTokens
		if usage.CachedTokens != nil {
			cachedKnown = true
			cachedSum += *usage.CachedTokens
		}
		if usage.CacheWriteTokens != nil {
			cacheWriteSum += *usage.CacheWriteTokens
		}
		if usage.ReasoningTokens != nil {
			reasoningSum += *usage.ReasoningTokens
		}
		if usage.CostUSD != nil {
			costCount++
			costTotal += *usage.CostUSD
		}
		switch hit := resolvedCacheHit(entry, usage); {
		case hit == nil:
			unknown++
		case *hit:
			hits++
		default:
			misses++
		}
		if sent := resolvedTTLSent(entry); sent != nil && *sent {
			ttlWrites++
		}
	}
	fmt.Printf("stats (filtered n=%d):\n", len(entries))
	if costCount > 0 {
		fmt.Printf("  cost_usd=%s (%d/%d reported)\n", formatUSD(costTotal), costCount, len(entries))
	} else {
		fmt.Printf("  cost_usd=(provider did not report)\n")
	}
	cachedPart := "cached=?"
	if cachedKnown {
		ratio := "n/a"
		if promptSum > 0 {
			ratio = fmt.Sprintf("%.1f%%", 100*float64(cachedSum)/float64(promptSum))
		}
		cachedPart = fmt.Sprintf("cached=%d (%s)", cachedSum, ratio)
	}
	fmt.Printf("  tokens: prompt=%d %s completion=%d reasoning=%d cache_write=%d\n",
		promptSum, cachedPart, completionSum, reasoningSum, cacheWriteSum)
	hitRate := "n/a"
	if hits+misses > 0 {
		hitRate = fmt.Sprintf("%.1f%%", 100*float64(hits)/float64(hits+misses))
	}
	fmt.Printf("  cache: hit=%d miss=%d unknown=%d hit_rate=%s ttl_writes=%d\n",
		hits, misses, unknown, hitRate, ttlWrites)
}

func extractFinalMessage(entry logEntry) string {
	_, frozen, variable := resolvedPrompts(entry)
	text := frozen + variable
	if text == "" {
		text = userPromptText(entry.Request)
	}
	if text == "" {
		return ""
	}
	if match := finalMessageRE.FindStringSubmatch(text); len(match) == 2 {
		return strings.TrimSpace(match[1])
	}
	return strings.TrimSpace(text)
}

func resolvedPrompts(entry logEntry) (system, frozen, variable string) {
	system = entry.SystemInstruction
	frozen = entry.UserPromptFrozen
	variable = entry.UserPromptVariable
	if system != "" || frozen != "" || variable != "" {
		return system, frozen, variable
	}
	return promptsFromRequest(entry.Request)
}

func promptsFromRequest(request json.RawMessage) (system, frozen, variable string) {
	if len(request) == 0 {
		return "", "", ""
	}
	var payload requestPayload
	if err := json.Unmarshal(request, &payload); err != nil {
		return "", "", ""
	}
	for _, msg := range payload.Messages {
		switch msg.Role {
		case "system":
			system = messageText(msg.Content)
		case "user":
			frozen, variable = splitUserPromptParts(msg.Content)
		}
	}
	return system, frozen, variable
}

func splitUserPromptParts(raw json.RawMessage) (frozen, variable string) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, ""
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", ""
	}
	var texts []string
	for _, part := range parts {
		if part.Type != "" && part.Type != "text" {
			continue
		}
		texts = append(texts, part.Text)
	}
	switch len(texts) {
	case 0:
		return "", ""
	case 1:
		return texts[0], ""
	default:
		return texts[0], strings.Join(texts[1:], "")
	}
}

func resolvedUsage(entry logEntry) loggedUsage {
	if entry.Usage != nil {
		return *entry.Usage
	}
	return usageFromResponse(entry.Response)
}

func usageFromResponse(raw json.RawMessage) loggedUsage {
	resp := parseResponse(raw)
	if resp.Usage == nil {
		return loggedUsage{}
	}
	u := resp.Usage
	out := loggedUsage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.TotalTokens,
		CachedTokens:     u.CachedTokens,
		CacheWriteTokens: u.CacheWriteTokens,
		CostUSD:          u.Cost,
	}
	if details := u.PromptTokensDetails; details != nil {
		if details.CachedTokens != nil {
			out.CachedTokens = details.CachedTokens
		}
		if details.CacheWriteTokens != nil {
			out.CacheWriteTokens = details.CacheWriteTokens
		}
	}
	if u.CompletionTokensDetails != nil {
		out.ReasoningTokens = u.CompletionTokensDetails.ReasoningTokens
	}
	return out
}

func resolvedCacheKey(entry logEntry) string {
	if entry.PromptCacheKey != "" {
		return entry.PromptCacheKey
	}
	if len(entry.Request) == 0 {
		return ""
	}
	var payload requestPayload
	if err := json.Unmarshal(entry.Request, &payload); err != nil {
		return ""
	}
	return payload.PromptCacheKey
}

func resolvedTTLSent(entry logEntry) *bool {
	if entry.PromptCacheTTLSent != nil {
		return entry.PromptCacheTTLSent
	}
	if len(entry.Request) == 0 {
		return nil
	}
	var payload requestPayload
	if err := json.Unmarshal(entry.Request, &payload); err != nil {
		return nil
	}
	sent := len(bytes.TrimSpace(payload.PromptCacheOptions)) > 0 && string(payload.PromptCacheOptions) != "null"
	return &sent
}

func resolvedCacheHit(entry logEntry, usage loggedUsage) *bool {
	if entry.PromptCacheHit != nil {
		return entry.PromptCacheHit
	}
	if usage.CachedTokens == nil {
		return nil
	}
	hit := *usage.CachedTokens > 0
	return &hit
}

func cacheStatusLabel(hit *bool) string {
	if hit == nil {
		return "?"
	}
	if *hit {
		return "hit"
	}
	return "miss"
}

func formatCacheDetail(entry logEntry, usage loggedUsage) string {
	parts := []string{cacheStatusLabel(resolvedCacheHit(entry, usage))}
	if usage.CachedTokens != nil {
		if usage.PromptTokens > 0 {
			parts = append(parts, fmt.Sprintf("cached=%d/%d", *usage.CachedTokens, usage.PromptTokens))
		} else {
			parts = append(parts, fmt.Sprintf("cached=%d", *usage.CachedTokens))
		}
	}
	if sent := resolvedTTLSent(entry); sent != nil {
		parts = append(parts, fmt.Sprintf("ttl_sent=%t", *sent))
	}
	return strings.Join(parts, "  ")
}

func formatTokenLine(usage loggedUsage) string {
	if usage.PromptTokens == 0 && usage.CompletionTokens == 0 && usage.CachedTokens == nil && usage.ReasoningTokens == nil {
		return ""
	}
	parts := []string{fmt.Sprintf("in=%d", usage.PromptTokens)}
	if usage.CachedTokens != nil {
		parts = append(parts, fmt.Sprintf("cached=%d", *usage.CachedTokens))
	}
	parts = append(parts, fmt.Sprintf("out=%d", usage.CompletionTokens))
	if usage.ReasoningTokens != nil {
		parts = append(parts, fmt.Sprintf("reasoning=%d", *usage.ReasoningTokens))
	}
	return strings.Join(parts, " ")
}

func formatCost(cost *float64) string {
	if cost == nil {
		return "-"
	}
	return formatUSD(*cost)
}

func formatUSD(v float64) string {
	s := strconv.FormatFloat(v, 'f', 8, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" || s == "-" {
		s = "0"
	}
	return "$" + s
}

func userPromptText(request json.RawMessage) string {
	if len(request) == 0 {
		return ""
	}
	var payload requestPayload
	if err := json.Unmarshal(request, &payload); err != nil {
		return ""
	}
	var b strings.Builder
	for _, msg := range payload.Messages {
		if msg.Role != "user" {
			continue
		}
		b.WriteString(messageText(msg.Content))
	}
	return b.String()
}

func messageText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ""
	}
	var b strings.Builder
	for _, part := range parts {
		if part.Type != "" && part.Type != "text" {
			continue
		}
		b.WriteString(part.Text)
	}
	return b.String()
}

func parseResponse(raw json.RawMessage) responsePayload {
	var payload responsePayload
	if len(raw) == 0 {
		return payload
	}
	_ = json.Unmarshal(raw, &payload)
	return payload
}

func firstFinishReason(resp responsePayload) string {
	if len(resp.Choices) == 0 {
		return ""
	}
	return strings.TrimSpace(resp.Choices[0].FinishReason)
}

func extractOutputText(resp responsePayload) string {
	for _, choice := range resp.Choices {
		if text := strings.TrimSpace(messageText(choice.Message.Content)); text != "" {
			return text
		}
	}
	return ""
}

func extractReasoningText(resp responsePayload) string {
	var parts []string
	for _, choice := range resp.Choices {
		if text := strings.TrimSpace(choice.Message.Reasoning); text != "" {
			parts = append(parts, text)
			continue
		}
		if text := strings.TrimSpace(choice.Message.ReasoningContent); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func parseTranslations(text string) []struct {
	Language       string
	TranslatedText string
} {
	trimmed := strings.TrimSpace(text)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)

	var payload translationPayload
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil
	}
	out := make([]struct {
		Language       string
		TranslatedText string
	}, 0, len(payload.Translations))
	for _, item := range payload.Translations {
		out = append(out, struct {
			Language       string
			TranslatedText string
		}{Language: item.Language, TranslatedText: item.TranslatedText})
	}
	return out
}

func oneLine(s string, max int) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.Join(strings.Fields(s), " ")
	if max > 0 && len([]rune(s)) > max {
		runes := []rune(s)
		return string(runes[:max]) + "…"
	}
	return s
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
