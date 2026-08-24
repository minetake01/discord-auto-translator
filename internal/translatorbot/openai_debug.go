package translatorbot

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// openaiDebugEntry records one Chat Completions round trip. The raw request
// and response keep unknown fields intact. First-class prompt, cache, and
// usage fields make accuracy and cache-efficiency measurement possible without
// re-parsing the provider payload. GuildID and MessageID are local
// correlation keys; they are not sent to the provider.
type openaiDebugEntry struct {
	Time               time.Time          `json:"time"`
	GuildID            string             `json:"guild_id,omitempty"`
	MessageID          string             `json:"message_id,omitempty"`
	Model              string             `json:"model,omitempty"`
	SchemaName         string             `json:"schema_name,omitempty"`
	TargetLanguages    []string           `json:"target_languages"`
	DurationMS         int64              `json:"duration_ms"`
	WaitMS             *int64             `json:"wait_ms,omitempty"`
	ReadMS             *int64             `json:"read_ms,omitempty"`
	Ended              *time.Time         `json:"ended,omitempty"`
	Attempt            int                `json:"attempt,omitempty"`
	ResponseCreated    *int64             `json:"response_created,omitempty"`
	ProcessingMS       *int64             `json:"processing_ms,omitempty"`
	ServerTiming       string             `json:"server_timing,omitempty"`
	PromptCacheKey     string             `json:"prompt_cache_key,omitempty"`
	PromptCacheHit     *bool              `json:"prompt_cache_hit,omitempty"`
	SystemInstruction  string             `json:"system_instruction,omitempty"`
	UserPromptStable   string             `json:"user_prompt_stable,omitempty"`
	UserPromptHistory  string             `json:"user_prompt_history,omitempty"`
	UserPromptVariable string             `json:"user_prompt_variable,omitempty"`
	VisionImageCount   int                `json:"vision_image_count,omitempty"`
	Usage              *openaiLoggedUsage `json:"usage,omitempty"`
	Request            json.RawMessage    `json:"request,omitempty"`
	HTTPStatus         int                `json:"http_status,omitempty"`
	Response           json.RawMessage    `json:"response,omitempty"`
	ResponseText       string             `json:"response_text,omitempty"`
	Error              string             `json:"error,omitempty"`
}

// openaiLoggedUsage is the subset of Chat Completions usage used for
// measurement. CachedTokens and CostUSD are pointers so a reported zero stays
// distinct from a provider that omitted the field.
type openaiLoggedUsage struct {
	PromptTokens     int      `json:"prompt_tokens,omitempty"`
	CompletionTokens int      `json:"completion_tokens,omitempty"`
	TotalTokens      int      `json:"total_tokens,omitempty"`
	CachedTokens     *int     `json:"cached_tokens,omitempty"`
	CacheWriteTokens *int     `json:"cache_write_tokens,omitempty"`
	ReasoningTokens  *int     `json:"reasoning_tokens,omitempty"`
	CostUSD          *float64 `json:"cost_usd,omitempty"`
	QueueTime        *float64 `json:"queue_time,omitempty"`
	PromptTime       *float64 `json:"prompt_time,omitempty"`
	CompletionTime   *float64 `json:"completion_time,omitempty"`
	TotalTime        *float64 `json:"total_time,omitempty"`
}

// SetDebugLog records every Chat Completions round trip for diagnosis and
// measurement (synthesized prompts, cache hit, token usage, and provider cost).
// It is off unless configured.
func (t *OpenAITranslator) SetDebugLog(debugLog *DebugLog) {
	t.debugLog = debugLog
}

func (e *openaiDebugEntry) recordResponse(body []byte) {
	if json.Valid(body) {
		e.Response = body
		return
	}
	e.ResponseText = string(body)
}

func extractLoggedUsage(body []byte) *openaiLoggedUsage {
	if !json.Valid(body) {
		return nil
	}
	var payload struct {
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
	if err := json.Unmarshal(body, &payload); err != nil || payload.Usage == nil {
		return nil
	}
	u := payload.Usage
	out := &openaiLoggedUsage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.TotalTokens,
		CostUSD:          u.Cost,
		CachedTokens:     u.CachedTokens,
		CacheWriteTokens: u.CacheWriteTokens,
		QueueTime:        u.QueueTime,
		PromptTime:       u.PromptTime,
		CompletionTime:   u.CompletionTime,
		TotalTime:        u.TotalTime,
	}
	if details := u.PromptTokensDetails; details != nil {
		if details.CachedTokens != nil {
			out.CachedTokens = details.CachedTokens
		}
		if details.CacheWriteTokens != nil {
			out.CacheWriteTokens = details.CacheWriteTokens
		}
	}
	if u.CompletionTokensDetails != nil {
		out.ReasoningTokens = u.CompletionTokensDetails.ReasoningTokens
	}
	return out
}

func promptCacheHit(usage *openaiLoggedUsage) *bool {
	if usage == nil || usage.CachedTokens == nil {
		return nil
	}
	hit := *usage.CachedTokens > 0
	return &hit
}

func extractResponseCreated(body []byte) *int64 {
	if !json.Valid(body) {
		return nil
	}
	var payload struct {
		Created *int64 `json:"created"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Created == nil {
		return nil
	}
	return payload.Created
}

func elapsedMS(start time.Time) int64 {
	ms := time.Since(start).Milliseconds()
	if ms < 0 {
		return 0
	}
	return ms
}

func headerInt64(h http.Header, names ...string) *int64 {
	if h == nil {
		return nil
	}
	for _, name := range names {
		raw := strings.TrimSpace(h.Get(name))
		if raw == "" {
			continue
		}
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			continue
		}
		return &v
	}
	return nil
}
