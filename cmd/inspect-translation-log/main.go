// Command inspect-translation-log summarizes TRANSLATION_DEBUG_LOG_PATH JSON Lines.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	pathFlag := flag.String("path", "", "debug log path (default: TRANSLATION_DEBUG_LOG_PATH, else .env, else ./translation-debug.log)")
	errorsOnly := flag.Bool("errors", false, "show only entries with an error")
	guildID := flag.String("guild-id", "", "filter by guild_id")
	messageID := flag.String("message-id", "", "filter by message_id")
	limit := flag.Int("limit", 50, "max entries to print after filtering (0 = all; most recent)")
	detail := flag.Bool("detail", false, "print source excerpt, translations, cache, cost, timing, usage, reasoning, synthesized prompts, and error text")
	prompt := flag.Bool("prompt", false, "print the full synthesized system and user prompts")
	stats := flag.Bool("stats", false, "print cache-hit, cost, and duration aggregates for the filtered entries")
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
