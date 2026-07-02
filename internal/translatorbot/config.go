package translatorbot

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DiscordToken                string
	GeminiAPIKey                string
	DBPath                      string
	HTTPAddr                    string
	PublicBaseURL               string
	GeminiRateLimitTokensPerMin int
	AdminRoleIDs                []string
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
	if err := validateHTTPAddr(cfg.HTTPAddr); err != nil {
		return cfg, err
	}
	if err := validatePublicBaseURL(cfg.PublicBaseURL); err != nil {
		return cfg, err
	}
	adminRoleIDs, err := parseSnowflakeIDs("ADMIN_ROLE_IDS", os.Getenv("ADMIN_ROLE_IDS"))
	if err != nil {
		return cfg, err
	}
	cfg.AdminRoleIDs = adminRoleIDs
	return cfg, nil
}

func parseSnowflakeIDs(envKey, raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	ids := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		id := strings.TrimSpace(part)
		if id == "" {
			continue
		}
		for _, c := range id {
			if c < '0' || c > '9' {
				return nil, fmt.Errorf("%s contains invalid role ID %q", envKey, id)
			}
		}
		if len(id) < 17 || len(id) > 20 {
			return nil, fmt.Errorf("%s contains invalid role ID %q", envKey, id)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}

func validateHTTPAddr(addr string) error {
	if _, err := net.ResolveTCPAddr("tcp", addr); err != nil {
		return fmt.Errorf("HTTP_ADDR is invalid: %w", err)
	}
	return nil
}

func validatePublicBaseURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("PUBLIC_BASE_URL is invalid: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("PUBLIC_BASE_URL must use http or https")
	}
	if u.Host == "" {
		return errors.New("PUBLIC_BASE_URL must include a host")
	}
	return nil
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
