package main

import (
	"encoding/json"
	"time"
)

type logEntry struct {
	Time               time.Time
	GuildID            string
	MessageID          string
	Model              string
	SchemaName         string
	TargetLanguages    []string
	DurationMS         int64
	WaitMS             *int64
	ReadMS             *int64
	Ended              time.Time
	Attempt            int
	ResponseCreated    *int64
	ProcessingMS       *int64
	ServerTiming       string
	HTTPStatus         int
	Error              string
	PromptCacheKey     string
	PromptCacheTTLSent *bool
	PromptCacheHit     *bool
	SystemInstruction  string
	UserPromptStable   string
	UserPromptHistory  string
	UserPromptVariable string
	UserPromptFrozen   string
	VisionImageCount   int
	Usage              *loggedUsage
	Request            json.RawMessage
	Response           json.RawMessage
	ResponseText       string
	raw                map[string]json.RawMessage
}

type loggedUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CachedTokens     *int
	CacheWriteTokens *int
	ReasoningTokens  *int
	CostUSD          *float64
	QueueTime        *float64
	PromptTime       *float64
	CompletionTime   *float64
	TotalTime        *float64
}

type requestPayload struct {
	Messages           []chatMessage   `json:"messages"`
	PromptCacheKey     string          `json:"prompt_cache_key"`
	PromptCacheOptions json.RawMessage `json:"prompt_cache_options"`
}

type chatMessage struct {
	Role             string          `json:"role"`
	Content          json.RawMessage `json:"content"`
	Reasoning        string          `json:"reasoning"`
	ReasoningContent string          `json:"reasoning_content"`
}

type responsePayload struct {
	Choices []struct {
		FinishReason string      `json:"finish_reason"`
		Message      chatMessage `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens        int      `json:"prompt_tokens"`
		CompletionTokens    int      `json:"completion_tokens"`
		TotalTokens         int      `json:"total_tokens"`
		CachedTokens        *int     `json:"cached_tokens"`
		CacheWriteTokens    *int     `json:"cache_write_tokens"`
		Cost                *float64 `json:"cost"`
		QueueTime           *float64 `json:"queue_time"`
		PromptTime          *float64 `json:"prompt_time"`
		CompletionTime      *float64 `json:"completion_time"`
		TotalTime           *float64 `json:"total_time"`
		PromptTokensDetails *struct {
			CachedTokens     *int `json:"cached_tokens"`
			CacheWriteTokens *int `json:"cache_write_tokens"`
		} `json:"prompt_tokens_details"`
		CompletionTokensDetails *struct {
			ReasoningTokens *int `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
	} `json:"usage"`
}
