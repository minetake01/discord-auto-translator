package translatorbot

import (
	"bufio"
	"errors"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DiscordToken               string
	GeminiAPIKey               string
	DBPath                     string
	HTTPAddr                   string
	PublicBaseURL              string
	GeminiRateLimitTokensPerMin int
}

func LoadConfig(path string) (Config, error) {
	_ = loadDotEnv(path)
	cfg := Config{
		DiscordToken:  os.Getenv("DISCORD_TOKEN"),
		GeminiAPIKey:  os.Getenv("GEMINI_API_KEY"),
		DBPath:        os.Getenv("DB_PATH"),
		HTTPAddr:      os.Getenv("HTTP_ADDR"),
		PublicBaseURL: strings.TrimRight(os.Getenv("PUBLIC_BASE_URL"), "/"),
	}
	if raw := strings.TrimSpace(os.Getenv("GEMINI_RATE_LIMIT_TOKENS_PER_MIN")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			return cfg, errors.New("GEMINI_RATE_LIMIT_TOKENS_PER_MIN must be an integer")
		}
		cfg.GeminiRateLimitTokensPerMin = limit
	} else {
		cfg.GeminiRateLimitTokensPerMin = defaultRateLimitTokensPerMinute
	}
	if cfg.DBPath == "" {
		cfg.DBPath = "./translator.db"
	}
	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = ":8080"
	}
	if cfg.DiscordToken == "" {
		return cfg, errors.New("DISCORD_TOKEN is required")
	}
	if cfg.GeminiAPIKey == "" {
		return cfg, errors.New("GEMINI_API_KEY is required")
	}
	return cfg, nil
}

func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if _, exists := os.LookupEnv(k); !exists {
			_ = os.Setenv(k, v)
		}
	}
	return sc.Err()
}
