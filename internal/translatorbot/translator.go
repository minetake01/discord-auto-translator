package translatorbot

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/genai"
)

const geminiModel = "gemini-3.1-flash-lite"

type ChatContextMessage struct {
	Author   string
	Language string
	Content  string
}

type TranslationContext struct {
	ServerName        string
	ServerDescription string
	ChannelName       string
	ChannelTopic      string
	History           []ChatContextMessage
}

type Translator interface {
	Translate(ctx context.Context, targetLanguage string, content string, context TranslationContext) (string, error)
}

type GeminiTranslator struct {
	client *genai.Client
	model  string
}

func NewGeminiTranslator(ctx context.Context, apiKey string) (*GeminiTranslator, error) {
	c, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: apiKey, Backend: genai.BackendGeminiAPI})
	if err != nil {
		return nil, err
	}
	return &GeminiTranslator{client: c, model: geminiModel}, nil
}

func (t *GeminiTranslator) Translate(ctx context.Context, targetLanguage string, content string, translationContext TranslationContext) (string, error) {
	p := NewProtector()
	protected := p.Protect(content)
	prompt := BuildTranslationPrompt(targetLanguage, protected, translationContext)
	resp, err := t.client.Models.GenerateContent(ctx, t.model, genai.Text(prompt), &genai.GenerateContentConfig{
		Temperature: genai.Ptr[float32](0.2),
	})
	if err != nil {
		return "", err
	}
	return p.Restore(strings.TrimSpace(resp.Text())), nil
}

func BuildTranslationPrompt(targetLanguage, content string, translationContext TranslationContext) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Translate the final message into %s for Discord chat.\n", targetLanguage)
	b.WriteString("Preserve URLs, mentions, markdown, custom emoji, code blocks, placeholders, line breaks, and tone. Return only the translated final message.\n")
	if translationContext.ServerName != "" || translationContext.ServerDescription != "" || translationContext.ChannelName != "" || translationContext.ChannelTopic != "" {
		b.WriteString("\nDiscord context:\n")
		if translationContext.ServerName != "" {
			fmt.Fprintf(&b, "- Server name: %s\n", translationContext.ServerName)
		}
		if translationContext.ServerDescription != "" {
			fmt.Fprintf(&b, "- Server overview: %s\n", translationContext.ServerDescription)
		}
		if translationContext.ChannelName != "" {
			fmt.Fprintf(&b, "- Channel name: %s\n", translationContext.ChannelName)
		}
		if translationContext.ChannelTopic != "" {
			fmt.Fprintf(&b, "- Channel topic: %s\n", translationContext.ChannelTopic)
		}
	}
	if len(translationContext.History) > 0 {
		b.WriteString("\nRecent context:\n")
		for _, h := range translationContext.History {
			fmt.Fprintf(&b, "- %s [%s]: %s\n", h.Author, h.Language, h.Content)
		}
	}
	b.WriteString("\nFinal message:\n")
	b.WriteString(content)
	return b.String()
}
