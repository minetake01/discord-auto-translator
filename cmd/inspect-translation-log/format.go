package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

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

func formatTimingDetail(entry logEntry, usage loggedUsage) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("duration=%dms", entry.DurationMS))
	if entry.WaitMS != nil {
		parts = append(parts, fmt.Sprintf("wait=%dms", *entry.WaitMS))
	}
	if entry.ReadMS != nil {
		parts = append(parts, fmt.Sprintf("read=%dms", *entry.ReadMS))
	}
	if entry.Attempt > 0 {
		parts = append(parts, fmt.Sprintf("attempt=%d", entry.Attempt))
	}
	if entry.ProcessingMS != nil {
		parts = append(parts, fmt.Sprintf("processing=%dms", *entry.ProcessingMS))
	}
	if usage.TotalTime != nil {
		parts = append(parts, "provider_total="+formatSeconds(*usage.TotalTime))
	}
	if usage.QueueTime != nil {
		parts = append(parts, "queue="+formatSeconds(*usage.QueueTime))
	}
	if usage.PromptTime != nil {
		parts = append(parts, "prompt="+formatSeconds(*usage.PromptTime))
	}
	if usage.CompletionTime != nil {
		parts = append(parts, "completion="+formatSeconds(*usage.CompletionTime))
	}
	if created := resolvedResponseCreated(entry); created != nil {
		parts = append(parts, "created="+time.Unix(*created, 0).UTC().Format(time.RFC3339))
	}
	if !entry.Ended.IsZero() {
		parts = append(parts, "ended="+entry.Ended.UTC().Format(time.RFC3339Nano))
	}
	if entry.ServerTiming != "" {
		parts = append(parts, "server_timing="+entry.ServerTiming)
	}
	return strings.Join(parts, "  ")
}

func formatSeconds(v float64) string {
	s := strconv.FormatFloat(v, 'f', 4, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s + "s"
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
