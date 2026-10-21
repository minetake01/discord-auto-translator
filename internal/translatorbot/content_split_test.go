package translatorbot

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func assertContentParts(t *testing.T, parts []string, limit int) {
	t.Helper()
	if len(parts) == 0 {
		t.Fatal("split produced no parts")
	}
	for i, part := range parts {
		if part == "" {
			t.Fatalf("part %d is empty", i)
		}
		if got := discordContentLength(part); got > limit {
			t.Fatalf("part %d has %d units, limit %d: %q", i, got, limit, part)
		}
	}
}

func TestSplitMessageContentLeavesFittingTextAlone(t *testing.T) {
	text := "line one\nline two"
	parts := splitMessageContent(text, len(text))
	if len(parts) != 1 || parts[0] != text {
		t.Fatalf("parts = %#v, want the input unchanged", parts)
	}
}

func TestSplitMessageContentBreaksAtLineBoundaries(t *testing.T) {
	text := "aaaaa\nbbbbb\nccccc"
	parts := splitMessageContent(text, 6)
	want := []string{"aaaaa", "bbbbb", "ccccc"}
	if len(parts) != len(want) {
		t.Fatalf("parts = %#v, want %#v", parts, want)
	}
	for i := range want {
		if parts[i] != want[i] {
			t.Fatalf("part %d = %q, want %q", i, parts[i], want[i])
		}
	}
	if got := strings.Join(parts, "\n"); got != text {
		t.Fatalf("split lost text: %q", got)
	}
}

func TestSplitMessageContentHardSplitsOneLongLine(t *testing.T) {
	text := strings.Repeat("a", 25)
	parts := splitMessageContent(text, 10)
	if len(parts) != 3 {
		t.Fatalf("parts = %#v, want three", parts)
	}
	assertContentParts(t, parts, 10)
	if got := strings.Join(parts, ""); got != text {
		t.Fatalf("hard split lost text: %q", got)
	}
}

func TestSplitMessageContentKeepsFencedCodeBlocksRenderable(t *testing.T) {
	code := strings.Repeat("x", 30)
	text := "before\n```go\n" + code + "\n```\nafter"
	parts := splitMessageContent(text, 40)
	assertContentParts(t, parts, 40)
	if !strings.HasPrefix(parts[0], "before") {
		t.Fatalf("first part = %q, want the text before the fence", parts[0])
	}
	if !strings.HasSuffix(parts[len(parts)-1], "after") {
		t.Fatalf("last part = %q, want the text after the fence", parts[len(parts)-1])
	}
	codeX := 0
	for i, part := range parts {
		markers := strings.Count(part, "```")
		if markers%2 != 0 {
			t.Fatalf("part %d leaves a fence open: %q", i, part)
		}
		codeX += strings.Count(part, "x")
	}
	if codeX != len(code) {
		t.Fatalf("code characters across parts = %d, want %d", codeX, len(code))
	}
}

func TestSplitMessageContentStartsAFenceInAFreshPart(t *testing.T) {
	code := strings.Repeat("x", 10)
	parts := splitMessageContent(strings.Repeat("a", 18)+"\n```go\n"+code+"\n```", 20)
	assertContentParts(t, parts, 20)
	if len(parts) != 2 || parts[0] != strings.Repeat("a", 18) {
		t.Fatalf("parts = %#v, want the text then the code block", parts)
	}
	if want := "```go\n" + code + "\n```"; parts[1] != want {
		t.Fatalf("code part = %q, want %q", parts[1], want)
	}
}

func TestSplitMessageContentCountsUTF16CodeUnits(t *testing.T) {
	text := strings.Repeat("😀", 600)
	parts := splitMessageContent(text, 1000)
	if len(parts) != 2 {
		t.Fatalf("parts = %d, want two", len(parts))
	}
	assertContentParts(t, parts, 1000)
	if got := utf8.RuneCountInString(parts[0]); got != 500 {
		t.Fatalf("first part runes = %d, want 500", got)
	}
}

func TestSplitMessageContentNeverExceedsTheLimit(t *testing.T) {
	texts := []string{
		"a\nb\nc",
		strings.Repeat("😀", 20),
		"```\n" + strings.Repeat("😀", 20) + "\n```",
		"text\n```go\n" + strings.Repeat("x", 100) + "\n```\ntail",
		"```\n```",
		"prefix\n~~~\n" + strings.Repeat("y", 70) + "\n~~~\nsuffix",
		strings.Repeat("a", 101) + "\n" + strings.Repeat("b", 101),
	}
	for _, text := range texts {
		for limit := 2; limit <= 40; limit++ {
			parts := splitMessageContent(text, limit)
			for i, part := range parts {
				if part == "" {
					t.Fatalf("limit %d: part %d of %q is empty", limit, i, text)
				}
				if got := discordContentLength(part); got > limit {
					t.Fatalf("limit %d: part %d has %d units: %q", limit, i, got, part)
				}
			}
		}
	}
}

func TestSplitMessageContentDoesNotBreakASurrogatePair(t *testing.T) {
	text := strings.Repeat("a", 1999) + "😀"
	if got := discordContentLength(text); got != 2001 {
		t.Fatalf("fixture length = %d, want 2001", got)
	}
	parts := splitMessageContent(text, discordMessageContentLimit)
	assertContentParts(t, parts, discordMessageContentLimit)
	if len(parts) != 2 {
		t.Fatalf("parts = %d, want two", len(parts))
	}
	if got := strings.Join(parts, ""); got != text {
		t.Fatalf("split lost text: %q", got)
	}
}
