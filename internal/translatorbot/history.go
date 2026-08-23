package translatorbot

import (
	"strings"
	"time"
)

const (
	historyIdleGap             = 15 * time.Minute
	historyCountHigh           = 16
	historyCountLow            = 8
	historySpanHigh            = 30 * time.Minute
	historySpanLow             = 15 * time.Minute
	historyTokenHigh           = 800
	historyTokenLow            = 400
	historyFetchLimit          = 512
	translationReplyChainLimit = 3

	mergeShortMessageMaxRunes = 60
	mergeMaxCombinedRunes     = 150
	mergeMaxCount             = 4
	mergeMaxInterval          = 5 * time.Minute
)

type historyMergeSlot struct {
	author          string
	content         string
	firstTime       time.Time
	lastTime        time.Time
	sourceChannelID string
	sourceMessageID string
	keys            []string
	count           int
	attachments     []DiscordAttachment
}

func (slot historyMergeSlot) toMessage() ChatContextMessage {
	return ChatContextMessage{
		Author:          slot.author,
		Content:         slot.content,
		SourceChannelID: slot.sourceChannelID,
		SourceMessageID: slot.sourceMessageID,
		Attachments:     append([]DiscordAttachment(nil), slot.attachments...),
	}
}

func selectRecentHistory(links []MessageLink, currentMessageID string, excludeReplyKeys map[string]bool) ([]ChatContextMessage, int, []ChatContextMessage) {
	session := truncateIdleSession(links, currentMessageID)
	slots, discardedSlots := replayHistoryHysteresis(mergeConsecutiveMessages(session))
	history, frozenCount := splitFrozenHistory(slots, excludeReplyKeys)
	discarded := make([]ChatContextMessage, 0, len(discardedSlots))
	for _, slot := range discardedSlots {
		discarded = append(discarded, slot.toMessage())
	}
	return history, frozenCount, discarded
}

func truncateIdleSession(links []MessageLink, currentMessageID string) []MessageLink {
	filtered := make([]MessageLink, 0, len(links))
	for _, link := range links {
		if !historyLinkHasContext(link) {
			continue
		}
		filtered = append(filtered, link)
	}
	if len(filtered) == 0 {
		return nil
	}
	newest := len(filtered) - 1
	currentTime, hasCurrent := discordSnowflakeTime(currentMessageID)
	newestTime, hasNewest := discordSnowflakeTime(filtered[newest].SourceMessageID)
	if hasCurrent && hasNewest && currentTime.Sub(newestTime) > historyIdleGap {
		return nil
	}
	sessionStart := 0
	for i := newest; i > 0; i-- {
		newerTime, hasNewer := discordSnowflakeTime(filtered[i].SourceMessageID)
		olderTime, hasOlder := discordSnowflakeTime(filtered[i-1].SourceMessageID)
		if !hasNewer || !hasOlder {
			continue
		}
		if newerTime.Sub(olderTime) > historyIdleGap {
			sessionStart = i
			break
		}
	}
	return filtered[sessionStart:]
}

func mergeConsecutiveMessages(links []MessageLink) []historyMergeSlot {
	slots := make([]historyMergeSlot, 0, len(links))
	for _, link := range links {
		if !historyLinkHasContext(link) {
			continue
		}
		content := link.SourceContentSnapshot
		images := imageAttachmentsOnly(link.SourceImageAttachments)
		messageTime, hasTime := discordSnowflakeTime(link.SourceMessageID)
		author := strings.TrimSpace(link.SourceAuthorDisplayName)
		contentRunes := len([]rune(content))
		key := messageRefKey(link.SourceChannelID, link.SourceMessageID)
		if len(slots) > 0 {
			last := &slots[len(slots)-1]
			combinedRunes := len([]rune(last.content)) + 1 + contentRunes
			if last.author == author &&
				contentRunes <= mergeShortMessageMaxRunes &&
				combinedRunes <= mergeMaxCombinedRunes &&
				last.count < mergeMaxCount &&
				hasTime &&
				!last.lastTime.IsZero() &&
				messageTime.Sub(last.lastTime) <= mergeMaxInterval {
				if strings.TrimSpace(content) != "" {
					if strings.TrimSpace(last.content) != "" {
						last.content += "\n" + content
					} else {
						last.content = content
					}
				}
				last.attachments = append(last.attachments, images...)
				last.lastTime = messageTime
				last.keys = append(last.keys, key)
				last.count++
				continue
			}
		}
		slot := historyMergeSlot{
			author:          author,
			content:         content,
			sourceChannelID: link.SourceChannelID,
			sourceMessageID: link.SourceMessageID,
			keys:            []string{key},
			count:           1,
			attachments:     append([]DiscordAttachment(nil), images...),
		}
		if hasTime {
			slot.firstTime = messageTime
			slot.lastTime = messageTime
		}
		slots = append(slots, slot)
	}
	return slots
}

func replayHistoryHysteresis(slots []historyMergeSlot) (kept, discarded []historyMergeSlot) {
	if len(slots) == 0 {
		return nil, nil
	}
	start := 0
	for k := 1; k <= len(slots); k++ {
		if !historyExceedsHigh(slots[start:k]) {
			continue
		}
		for start < k-1 && historyExceedsLow(slots[start:k]) {
			start++
		}
	}
	if start == 0 {
		return slots, nil
	}
	return slots[start:], slots[:start]
}

func historyExceedsHigh(slots []historyMergeSlot) bool {
	return len(slots) > historyCountHigh || historySpan(slots) > historySpanHigh || historyTokens(slots) > historyTokenHigh
}

func historyExceedsLow(slots []historyMergeSlot) bool {
	return len(slots) > historyCountLow || historySpan(slots) > historySpanLow || historyTokens(slots) > historyTokenLow
}

func historySpan(slots []historyMergeSlot) time.Duration {
	if len(slots) == 0 {
		return 0
	}
	first := slots[0].firstTime
	last := slots[len(slots)-1].lastTime
	if first.IsZero() || last.IsZero() || last.Before(first) {
		return 0
	}
	return last.Sub(first)
}

func historyTokens(slots []historyMergeSlot) int {
	total := 0
	for _, slot := range slots {
		total += EstimateTranslationTokens(slot.content, "")
	}
	return total
}

func splitFrozenHistory(slots []historyMergeSlot, excludeReplyKeys map[string]bool) ([]ChatContextMessage, int) {
	n := len(slots)
	if n == 0 {
		return nil, 0
	}
	frozen := slots[:n-1]
	out := make([]ChatContextMessage, 0, n)
	for _, slot := range frozen {
		out = append(out, slot.toMessage())
	}
	tail := slots[n-1]
	if slotMatchesReply(tail, excludeReplyKeys) {
		return out, len(out)
	}
	out = append(out, tail.toMessage())
	return out, len(frozen)
}

func slotMatchesReply(slot historyMergeSlot, excludeReplyKeys map[string]bool) bool {
	if excludeReplyKeys == nil {
		return false
	}
	for _, key := range slot.keys {
		if excludeReplyKeys[key] {
			return true
		}
	}
	return false
}

func historyLinkHasContext(link MessageLink) bool {
	return strings.TrimSpace(link.SourceContentSnapshot) != "" || len(imageAttachmentsOnly(link.SourceImageAttachments)) > 0
}

func historyGenerationID(history []ChatContextMessage, currentChannelID, currentMessageID string) string {
	if len(history) > 0 {
		return history[0].SourceChannelID + history[0].SourceMessageID
	}
	channelID := strings.TrimSpace(currentChannelID)
	messageID := strings.TrimSpace(currentMessageID)
	if channelID != "" && messageID != "" {
		return channelID + messageID
	}
	if messageID != "" {
		return messageID
	}
	return "empty"
}
