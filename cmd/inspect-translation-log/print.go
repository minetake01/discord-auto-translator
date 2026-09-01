package main

import (
	"fmt"
	"strings"
)

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
	if timing := formatTimingDetail(entry, usage); timing != "" {
		fmt.Printf("  timing: %s\n", timing)
	}
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
	system, stable, history, variable := resolvedPrompts(entry)
	if system != "" || stable != "" || history != "" || variable != "" {
		fmt.Printf("  prompt: system=%dB stable=%dB history=%dB variable=%dB images=%d\n",
			len(system), len(stable), len(history), len(variable), entry.VisionImageCount)
	}
}

func printPrompt(entry logEntry) {
	system, stable, history, variable := resolvedPrompts(entry)
	printPromptSection("system", system)
	printPromptSection("user stable", stable)
	printPromptSection("user history", history)
	printPromptSection("user variable", variable)
}

func printPromptSection(name, body string) {
	fmt.Printf("  --- %s ---\n", name)
	if strings.TrimSpace(body) == "" {
		fmt.Printf("  (empty)\n")
		return
	}
	for line := range strings.SplitSeq(body, "\n") {
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
	fmt.Printf("  cache: hit=%d miss=%d unknown=%d hit_rate=%s\n",
		hits, misses, unknown, hitRate)
	printDurationStats("duration_ms", collectInt64(entries, func(e logEntry) (int64, bool) { return e.DurationMS, true }))
	printDurationStats("wait_ms", collectInt64(entries, func(e logEntry) (int64, bool) {
		if e.WaitMS == nil {
			return 0, false
		}
		return *e.WaitMS, true
	}))
}

func printDurationStats(label string, values []int64) {
	if len(values) == 0 {
		return
	}
	sum := values[0]
	min := values[0]
	max := values[0]
	for _, v := range values[1:] {
		sum += v
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	fmt.Printf("  %s: avg=%d min=%d max=%d n=%d\n", label, sum/int64(len(values)), min, max, len(values))
}

func collectInt64(entries []logEntry, value func(logEntry) (int64, bool)) []int64 {
	out := make([]int64, 0, len(entries))
	for _, entry := range entries {
		v, ok := value(entry)
		if !ok {
			continue
		}
		out = append(out, v)
	}
	return out
}
