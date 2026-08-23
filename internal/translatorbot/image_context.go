package translatorbot

import (
	"context"
	"strings"
)

func (s *Service) loadContextVisionImages(ctx context.Context, tc *TranslationContext, remainingSlots, remainingBytes int) []visionImage {
	if tc == nil || remainingSlots <= 0 || remainingBytes <= 0 {
		return nil
	}
	locations := collectContextImageLocations(tc.History, tc.ReplyChain)
	if len(locations) == 0 {
		return nil
	}
	client := s.imageHTTPClient()
	loaded := make(map[int][]byte, remainingSlots)
	for _, locIdx := range contextImageLoadOrder(locations, len(tc.History), len(tc.ReplyChain)) {
		if remainingSlots <= 0 {
			break
		}
		jpegBytes, ok := s.tryLoadVisionJPEG(ctx, client, locations[locIdx].attachment.URL, remainingBytes)
		if !ok {
			continue
		}
		remainingBytes -= len(jpegBytes)
		remainingSlots--
		loaded[locIdx] = jpegBytes
	}
	indexed := make(map[int]int, len(loaded))
	vision := make([]visionImage, 0, len(loaded))
	nextIndex := 1
	assign := func(locIdx int) {
		jpegBytes, ok := loaded[locIdx]
		if !ok {
			return
		}
		if _, done := indexed[locIdx]; done {
			return
		}
		indexed[locIdx] = nextIndex
		vision = append(vision, visionImage{DataURL: jpegDataURL(jpegBytes)})
		nextIndex++
	}
	for i := range tc.History {
		for locIdx, loc := range locations {
			if loc.historyIdx == i {
				assign(locIdx)
			}
		}
	}
	for i := range tc.ReplyChain {
		for locIdx, loc := range locations {
			if loc.replyIdx == i {
				assign(locIdx)
			}
		}
	}
	stampContextImages(tc.History, locations, indexed, true)
	stampContextImages(tc.ReplyChain, locations, indexed, false)
	return vision
}

func collectContextImageLocations(history, reply []ChatContextMessage) []contextImageLocation {
	order := make([]contextImageLocation, 0)
	seen := make(map[string]int)
	add := func(msg ChatContextMessage, historyIdx, replyIdx int) {
		for _, attachment := range imageAttachmentsOnly(msg.Attachments) {
			url := strings.TrimSpace(attachment.URL)
			if url == "" {
				continue
			}
			key := msg.SourceChannelID + "\x00" + msg.SourceMessageID + "\x00" + url
			if i, ok := seen[key]; ok {
				if historyIdx >= 0 {
					order[i].historyIdx = historyIdx
				}
				if replyIdx >= 0 {
					order[i].replyIdx = replyIdx
				}
				continue
			}
			seen[key] = len(order)
			loc := contextImageLocation{key: key, attachment: attachment, historyIdx: -1, replyIdx: -1}
			if historyIdx >= 0 {
				loc.historyIdx = historyIdx
			}
			if replyIdx >= 0 {
				loc.replyIdx = replyIdx
			}
			order = append(order, loc)
		}
	}
	for i, msg := range history {
		add(msg, i, -1)
	}
	for i, msg := range reply {
		add(msg, -1, i)
	}
	return order
}

func contextImageLoadOrder(locations []contextImageLocation, historyLen, replyLen int) []int {
	order := make([]int, 0, len(locations))
	seen := make(map[int]bool, len(locations))
	appendMatching := func(match func(contextImageLocation) bool) {
		for i, loc := range locations {
			if seen[i] || !match(loc) {
				continue
			}
			seen[i] = true
			order = append(order, i)
		}
	}
	for i := replyLen - 1; i >= 0; i-- {
		idx := i
		appendMatching(func(loc contextImageLocation) bool { return loc.replyIdx == idx })
	}
	for i := historyLen - 1; i >= 0; i-- {
		idx := i
		appendMatching(func(loc contextImageLocation) bool { return loc.historyIdx == idx && loc.replyIdx < 0 })
	}
	return order
}

func stampContextImages(messages []ChatContextMessage, locations []contextImageLocation, indexed map[int]int, history bool) {
	for i := range messages {
		var images []TranslationAttachment
		for locIdx, loc := range locations {
			if history && loc.historyIdx != i {
				continue
			}
			if !history && loc.replyIdx != i {
				continue
			}
			index, ok := indexed[locIdx]
			if !ok {
				continue
			}
			images = append(images, TranslationAttachment{
				Index:       index,
				Filename:    attachmentFileName(loc.attachment),
				Description: strings.TrimSpace(loc.attachment.Description),
			})
		}
		messages[i].Images = images
	}
}
