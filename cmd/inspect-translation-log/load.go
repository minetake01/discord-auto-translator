package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

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
	entry.UserPromptStable = decodeString(raw["user_prompt_stable"])
	entry.UserPromptHistory = decodeString(raw["user_prompt_history"])
	entry.UserPromptFrozen = decodeString(raw["user_prompt_frozen"])
	entry.UserPromptVariable = decodeString(raw["user_prompt_variable"])
	if v, ok := raw["duration_ms"]; ok {
		if err := json.Unmarshal(v, &entry.DurationMS); err != nil {
			return logEntry{}, fmt.Errorf("duration_ms: %w", err)
		}
	}
	if v, ok := raw["wait_ms"]; ok {
		var wait int64
		if err := json.Unmarshal(v, &wait); err != nil {
			return logEntry{}, fmt.Errorf("wait_ms: %w", err)
		}
		entry.WaitMS = &wait
	}
	if v, ok := raw["read_ms"]; ok {
		var read int64
		if err := json.Unmarshal(v, &read); err != nil {
			return logEntry{}, fmt.Errorf("read_ms: %w", err)
		}
		entry.ReadMS = &read
	}
	if v, ok := raw["ended"]; ok {
		var ended time.Time
		if err := json.Unmarshal(v, &ended); err != nil {
			return logEntry{}, fmt.Errorf("ended: %w", err)
		}
		entry.Ended = ended
	}
	if v, ok := raw["attempt"]; ok {
		if err := json.Unmarshal(v, &entry.Attempt); err != nil {
			return logEntry{}, fmt.Errorf("attempt: %w", err)
		}
	}
	if v, ok := raw["response_created"]; ok {
		var created int64
		if err := json.Unmarshal(v, &created); err != nil {
			return logEntry{}, fmt.Errorf("response_created: %w", err)
		}
		entry.ResponseCreated = &created
	}
	if v, ok := raw["processing_ms"]; ok {
		var processing int64
		if err := json.Unmarshal(v, &processing); err != nil {
			return logEntry{}, fmt.Errorf("processing_ms: %w", err)
		}
		entry.ProcessingMS = &processing
	}
	entry.ServerTiming = decodeString(raw["server_timing"])
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
		QueueTime        *float64 `json:"queue_time"`
		PromptTime       *float64 `json:"prompt_time"`
		CompletionTime   *float64 `json:"completion_time"`
		TotalTime        *float64 `json:"total_time"`
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
		QueueTime:        parsed.QueueTime,
		PromptTime:       parsed.PromptTime,
		CompletionTime:   parsed.CompletionTime,
		TotalTime:        parsed.TotalTime,
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
