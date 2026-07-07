package translatorbot

import (
	"strings"
	"testing"
)

func TestHasTranslatableText(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "blank", text: " \n\t", want: false},
		{name: "URLs", text: "https://example.com/a\n<https://example.com/b>", want: false},
		{name: "Discord elements", text: "<@123> <@!456> <#789> <@&101> <:wave:202> <a:dance_1:303>", want: false},
		{name: "slash command and timestamp", text: "</ban:12345> <t:1234567890:F>", want: false},
		{name: "code", text: "`hello`\n```go\nfmt.Println(\"hello\")\n```", want: false},
		{name: "mixed protected elements", text: "<@123> https://example.com `hello` <:wave:202>", want: false},
		{name: "plain text", text: "hello", want: true},
		{name: "text with URL", text: "see https://example.com", want: true},
		{name: "Markdown link label", text: "[documentation](https://example.com)", want: true},
		{name: "unclosed code", text: "`hello", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasTranslatableText(tt.text); got != tt.want {
				t.Fatalf("hasTranslatableText(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestProtectorRestoresURLsAndMarkdown(t *testing.T) {
	p := NewProtector(NameMaps{})
	in := "see https://example.com/a?x=1 and `code`"
	protected := p.Protect(in)
	if strings.Contains(protected, "https://example.com") || strings.Contains(protected, "`code`") {
		t.Fatalf("not protected: %s", protected)
	}
	if !strings.Contains(protected, "[URL:example.com]") || !strings.Contains(protected, "[CODE]") {
		t.Fatalf("unexpected protected form: %s", protected)
	}
	if got := p.Restore(protected); got != in {
		t.Fatalf("got %q want %q", got, in)
	}
}

func TestProtectorEmojiAndMentionNames(t *testing.T) {
	p := NewProtector(NameMaps{
		Users:    map[string]string{"123": "Alice"},
		Channels: map[string]string{"789": "general"},
		Roles:    map[string]string{"101": "mod"},
	})
	in := "hi <@123> in <#789> <:wave:202> <a:dance:303>"
	protected := p.Protect(in)
	want := "hi [USER:Alice] in [CHANNEL:general] [EMOJI:wave] [EMOJI:dance]"
	if protected != want {
		t.Fatalf("got %q want %q", protected, want)
	}
	if got := p.Restore(protected); got != in {
		t.Fatalf("restore got %q want %q", got, in)
	}
}

func TestProtectorSequentialSuffix(t *testing.T) {
	p := NewProtector(NameMaps{})
	in := "<:wave:1> <:wave:2> <t:111> <t:222>"
	protected := p.Protect(in)
	want := "[EMOJI:wave] [EMOJI:wave:2] [TIME] [TIME:2]"
	if protected != want {
		t.Fatalf("got %q want %q", protected, want)
	}
	if got := p.Restore(protected); got != in {
		t.Fatalf("restore got %q want %q", got, in)
	}
}

func TestProtectorSlashCommandAndTimestamp(t *testing.T) {
	p := NewProtector(NameMaps{})
	in := "use </kick user:12345> at <t:1234567890:F>"
	protected := p.Protect(in)
	if !strings.Contains(protected, "[CMD:kick user]") || !strings.Contains(protected, "[TIME]") {
		t.Fatalf("unexpected protected form: %s", protected)
	}
	if got := p.Restore(protected); got != in {
		t.Fatalf("restore got %q want %q", got, in)
	}
}

func TestProtectorDoesNotMaskSpoilers(t *testing.T) {
	p := NewProtector(NameMaps{})
	in := "||secret||"
	protected := p.Protect(in)
	if protected != in {
		t.Fatalf("spoilers should not be masked, got %q", protected)
	}
}

func TestProtectorFallbackWithoutNames(t *testing.T) {
	p := NewProtector(NameMaps{})
	in := "<@999> <#888> <@&777>"
	protected := p.Protect(in)
	want := "[USER] [CHANNEL] [ROLE]"
	if protected != want {
		t.Fatalf("got %q want %q", protected, want)
	}
	if got := p.Restore(protected); got != in {
		t.Fatalf("restore got %q want %q", got, in)
	}
}
