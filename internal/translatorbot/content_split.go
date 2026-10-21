package translatorbot

import "strings"

// discordContentLength measures a text conservatively against the per-message
// content limit: one unit per UTF-16 code unit, so a character outside the BMP
// costs two. It never undercounts what Discord charges.
func discordContentLength(text string) int {
	units := 0
	for _, r := range text {
		units += utf16Units(r)
	}
	return units
}

func utf16Units(r rune) int {
	if r > 0xFFFF {
		return 2
	}
	return 1
}

// codeFence is the fenced block a split part starts inside.
type codeFence struct {
	open  string
	close string
}

// fenceMarker returns the fence marker a line starts with. Discord follows
// CommonMark, which allows up to three leading spaces before the fence.
func fenceMarker(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 {
		return "", false
	}
	switch {
	case strings.HasPrefix(trimmed, "```"):
		return "```", true
	case strings.HasPrefix(trimmed, "~~~"):
		return "~~~", true
	default:
		return "", false
	}
}

// fenceFits reports whether a part can hold a fence line, its closing fence,
// and at least one more character. A line that cannot is mirrored as plain
// text instead of being treated as a fence.
func fenceFits(line, marker string, limit int) bool {
	return discordContentLength(line)+1+len(marker)+3 <= limit
}

// closingFence reports whether line closes a fenced block: the marker
// character repeated, with trailing spaces only. A line that carries text
// after the marker stays inside the block as code.
func closingFence(line, marker string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 3 {
		return false
	}
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] != marker[0] {
			return false
		}
	}
	return true
}

// splitMessageContent splits a mirror body into the messages Discord accepts.
// It breaks at line boundaries where possible, hard-splits a single line that
// cannot fit, and closes and reopens a fenced code block at the boundary so
// each part still renders as code.
func splitMessageContent(text string, limit int) []string {
	if limit < 2 {
		panic("splitMessageContent: limit must be at least 2 UTF-16 code units")
	}
	if discordContentLength(text) <= limit {
		return []string{text}
	}

	lines := strings.Split(text, "\n")
	parts := make([]string, 0, 2)
	var b strings.Builder
	bLen := 0
	lineOpen := false
	var fence *codeFence

	closeCost := func() int {
		if fence == nil {
			return 0
		}
		return 1 + len(fence.close)
	}
	reopen := func() {
		if fence == nil {
			return
		}
		b.WriteString(fence.open)
		b.WriteByte('\n')
		bLen += discordContentLength(fence.open) + 1
		lineOpen = false
	}
	flush := func() {
		if bLen == 0 {
			return
		}
		if fence != nil {
			b.WriteByte('\n')
			b.WriteString(fence.close)
			bLen += 1 + len(fence.close)
		}
		parts = append(parts, b.String())
		b.Reset()
		bLen = 0
		lineOpen = false
		reopen()
	}

	appendText := func(text string) {
		if lineOpen {
			b.WriteByte('\n')
			bLen++
		}
		b.WriteString(text)
		bLen += discordContentLength(text)
		lineOpen = true
	}
	// appendLineChunk appends a piece of a line, adding the newline that
	// separates it from earlier content unless the part was just reopened.
	appendLineChunk := func(text string) {
		if lineOpen {
			b.WriteByte('\n')
			bLen++
		}
		b.WriteString(text)
		bLen += discordContentLength(text)
		lineOpen = true
	}

	for i, line := range lines {
		marker, delimiter := fenceMarker(line)
		closes := fence != nil && delimiter && marker == fence.close && closingFence(line, marker)
		opens := fence == nil && delimiter && fenceFits(line, marker, limit)

		// An opening fence that leaves no room for the first line of its
		// block belongs at the start of the next part, not alone at the end
		// of this one as an empty block.
		if opens && bLen > 0 && i+1 < len(lines) && discordContentLength(lines[i+1]) > 0 {
			separator := 0
			if lineOpen {
				separator = 1
			}
			afterOpen := bLen + separator + discordContentLength(line)
			if limit-afterOpen-(1+len(marker)) < 2 {
				flush()
			}
		}

		reserve := 0
		switch {
		case fence != nil && !closes:
			reserve = closeCost()
		case opens:
			reserve = 1 + len(marker)
		}
		needed := discordContentLength(line) + reserve
		if lineOpen {
			needed++
		}
		if bLen+needed > limit {
			// Use whatever room the open part still has before starting a new
			// one, so an opening fence is not stranded alone in a block.
			if limit-bLen-closeCost() <= 0 {
				flush()
			}
			whole := bLen + discordContentLength(line) + reserve
			if lineOpen {
				whole++
			}
			if whole <= limit {
				appendText(line)
			} else {
				rest := line
				for rest != "" {
					available := limit - bLen - closeCost()
					if lineOpen {
						available--
					}
					chunk, remaining := takeContentPrefix(rest, available)
					if chunk == "" {
						// The open part cannot hold even one character;
						// close it and continue in a fresh one.
						flush()
						continue
					}
					appendLineChunk(chunk)
					rest = remaining
					if rest != "" {
						flush()
					}
				}
			}
		} else {
			appendText(line)
		}

		switch {
		case opens:
			fence = &codeFence{open: line, close: marker}
		case closes:
			fence = nil
		}
	}
	flush()
	return parts
}

// takeContentPrefix returns the longest prefix of text that fits maxUnits
// UTF-16 code units, and the rest. It returns an empty prefix when even the
// first character does not fit.
func takeContentPrefix(text string, maxUnits int) (string, string) {
	units := 0
	for i, r := range text {
		width := utf16Units(r)
		if units+width > maxUnits {
			return text[:i], text[i:]
		}
		units += width
	}
	return text, ""
}
