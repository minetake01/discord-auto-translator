package translatorbot

import "testing"

func TestHasTranslatableText(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "blank", text: " \n\t", want: false},
		{name: "URLs", text: "https://example.com/a\n<https://example.com/b>", want: false},
		{name: "Discord elements", text: "<@123> <@!456> <#789> <@&101> <:wave:202> <a:dance_1:303>", want: false},
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
