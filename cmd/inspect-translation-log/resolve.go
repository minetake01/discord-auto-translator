package main

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

func resolvedPrompts(entry logEntry) (system, stable, history, variable string) {
	system = entry.SystemInstruction
	stable = entry.UserPromptStable
	history = entry.UserPromptHistory
	variable = entry.UserPromptVariable
	if stable == "" && history == "" {
		stable = entry.UserPromptFrozen
	}
	if system != "" || stable != "" || history != "" || variable != "" {
		return system, stable, history, variable
	}
	return promptsFromRequest(entry.Request)
}

func promptsFromRequest(request json.RawMessage) (system, stable, history, variable string) {
	if len(request) == 0 {
		return "", "", "", ""
	}
	var payload requestPayload
	if err := json.Unmarshal(request, &payload); err != nil {
		return "", "", "", ""
	}
	for _, msg := range payload.Messages {
		switch msg.Role {
		case "system":
			system = messageText(msg.Content)
		case "user":
			stable, history, variable = splitUserPromptParts(msg.Content)
		}
	}
	return system, stable, history, variable
}

func splitUserPromptParts(raw json.RawMessage) (stable, history, variable string) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, "", ""
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", "", ""
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
		return "", "", ""
	case 1:
		return texts[0], "", ""
	case 2:
		return texts[0], "", texts[1]
	default:
		return texts[0], strings.Join(texts[1:len(texts)-1], ""), texts[len(texts)-1]
	}
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

var finalMessageRE = regexp.MustCompile(`(?s)<final_message[^>]*>(.*?)</final_message>`)

func extractFinalMessage(entry logEntry) string {
	_, stable, history, variable := resolvedPrompts(entry)
	text := stable + history + variable
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
		QueueTime:        u.QueueTime,
		PromptTime:       u.PromptTime,
		CompletionTime:   u.CompletionTime,
		TotalTime:        u.TotalTime,
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

func resolvedResponseCreated(entry logEntry) *int64 {
	if entry.ResponseCreated != nil {
		return entry.ResponseCreated
	}
	if len(entry.Response) == 0 {
		return nil
	}
	var payload struct {
		Created *int64 `json:"created"`
	}
	if err := json.Unmarshal(entry.Response, &payload); err != nil {
		return nil
	}
	return payload.Created
}
