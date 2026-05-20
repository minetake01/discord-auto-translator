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

type Translator interface {
	Translate(ctx context.Context, targetLanguage string, content string, context []ChatContextMessage) (string, error)
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

func (t *GeminiTranslator) Translate(ctx context.Context, targetLanguage string, content string, history []ChatContextMessage) (string, error) {
	p := NewProtector()
	protected := p.Protect(content)
	prompt := BuildTranslationPrompt(targetLanguage, protected, history)
	resp, err := t.client.Models.GenerateContent(ctx, t.model, genai.Text(prompt), &genai.GenerateContentConfig{
		Temperature: genai.Ptr[float32](0.2),
	})
	if err != nil {
		return "", err
	}
	return p.Restore(strings.TrimSpace(resp.Text())), nil
}

func BuildTranslationPrompt(targetLanguage, content string, history []ChatContextMessage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Translate the final message into %s for Discord chat.\n", targetLanguage)
	b.WriteString("Preserve URLs, mentions, markdown, custom emoji, code blocks, placeholders, line breaks, and tone. Return only the translated final message.\n")
	if len(history) > 0 {
		b.WriteString("\nRecent context:\n")
		for _, h := range history {
			fmt.Fprintf(&b, "- %s [%s]: %s\n", h.Author, h.Language, h.Content)
		}
	}
	b.WriteString("\nFinal message:\n")
	b.WriteString(content)
	return b.String()
}
