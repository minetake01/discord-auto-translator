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
	"strings"
	"time"
)

func main() {
	pathFlag := flag.String("path", "", "debug log path (default: TRANSLATION_DEBUG_LOG_PATH, else .env, else ./translation-debug.log)")
	errorsOnly := flag.Bool("errors", false, "show only entries with an error")
	guildID := flag.String("guild-id", "", "filter by guild_id")
	messageID := flag.String("message-id", "", "filter by message_id")
	limit := flag.Int("limit", 50, "max entries to print after filtering (0 = all; most recent)")
	detail := flag.Bool("detail", false, "print source excerpt, translations, usage (incl. reasoning tokens), reasoning text, and error text")
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
			if i != len(shown)-1 {
				fmt.Println(strings.Repeat("-", 72))
			}
		}
	}
}

type logEntry struct {
	Time            time.Time
	GuildID         string
	MessageID       string
	TargetLanguages []string
	DurationMS      int64
	HTTPStatus      int
	Error           string
	Request         json.RawMessage
	Response        json.RawMessage
	ResponseText    string
	raw             map[string]json.RawMessage
}

type requestPayload struct {
	Input []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"input"`
}

type responsePayload struct {
	Status string `json:"status"`
	Usage  *struct {
		InputTokens         int `json:"input_tokens"`
		OutputTokens        int `json:"output_tokens"`
		OutputTokensDetails *struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"output_tokens_details"`
	} `json:"usage"`
	Output []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
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
	entry.Error = decodeString(raw["error"])
	entry.ResponseText = decodeString(raw["response_text"])
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
	entry.Request = raw["request"]
	entry.Response = raw["response"]
	return entry, nil
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
	errHint := ""
	if entry.Error != "" {
		errHint = " " + oneLine(entry.Error, 80)
	}
	fmt.Printf("%s  %-5s  http=%-3s  %6dms  guild=%s  msg=%s  langs=%s%s\n",
		entry.Time.Local().Format("2006-01-02 15:04:05"),
		status,
		httpStatus,
		entry.DurationMS,
		dash(entry.GuildID),
		dash(entry.MessageID),
		langs,
		errHint,
	)
}

func printDetail(entry logEntry) {
	if source := extractFinalMessage(entry.Request); source != "" {
		fmt.Printf("  source: %s\n", oneLine(source, 200))
	}
	resp := parseResponse(entry.Response)
	if resp.Status != "" {
		fmt.Printf("  response_status: %s\n", resp.Status)
	}
	if resp.Usage != nil {
		if resp.Usage.OutputTokensDetails != nil {
			fmt.Printf("  tokens: in=%d out=%d reasoning=%d\n",
				resp.Usage.InputTokens, resp.Usage.OutputTokens, resp.Usage.OutputTokensDetails.ReasoningTokens)
		} else {
			fmt.Printf("  tokens: in=%d out=%d\n", resp.Usage.InputTokens, resp.Usage.OutputTokens)
		}
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
}

func extractFinalMessage(request json.RawMessage) string {
	if len(request) == 0 {
		return ""
	}
	var payload requestPayload
	if err := json.Unmarshal(request, &payload); err != nil {
		return ""
	}
	for _, msg := range payload.Input {
		if msg.Role != "user" {
			continue
		}
		if match := finalMessageRE.FindStringSubmatch(msg.Content); len(match) == 2 {
			return strings.TrimSpace(match[1])
		}
		return strings.TrimSpace(msg.Content)
	}
	return ""
}

func parseResponse(raw json.RawMessage) responsePayload {
	var payload responsePayload
	if len(raw) == 0 {
		return payload
	}
	_ = json.Unmarshal(raw, &payload)
	return payload
}

func extractOutputText(resp responsePayload) string {
	for _, item := range resp.Output {
		if item.Type != "" && item.Type != "message" {
			continue
		}
		for _, content := range item.Content {
			if content.Type == "output_text" || content.Type == "text" {
				if text := strings.TrimSpace(content.Text); text != "" {
					return text
				}
			}
		}
	}
	return ""
}

func extractReasoningText(resp responsePayload) string {
	var parts []string
	for _, item := range resp.Output {
		if item.Type != "reasoning" {
			continue
		}
		for _, content := range item.Content {
			if content.Type != "" && content.Type != "reasoning_text" && content.Type != "text" {
				continue
			}
			if text := strings.TrimSpace(content.Text); text != "" {
				parts = append(parts, text)
			}
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
