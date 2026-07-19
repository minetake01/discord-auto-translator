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
	"time"
)

const MaxRetentionDays = int((1<<63 - 1) / (24 * time.Hour))

type Config struct {
	DiscordToken                     string
	AWSAccessKeyID                   string
	AWSSecretAccessKey               string
	AWSBedrockRegion                 string
	AWSBedrockProjectID              string
	DBPath                           string
	HTTPAddr                         string
	PublicBaseURL                    string
	TranslationRateLimitTokensPerMin int
	AvatarRateLimitRequestsPerMin    int
	MessageLinkRetentionDays         int
	GuildDataRetentionDays           int
}

func LoadConfig(path string) (Config, error) {
	_ = loadDotEnv(path)
	cfg := Config{
		DiscordToken:        os.Getenv("DISCORD_TOKEN"),
		AWSAccessKeyID:      os.Getenv("AWS_ACCESS_KEY_ID"),
		AWSSecretAccessKey:  os.Getenv("AWS_SECRET_ACCESS_KEY"),
		AWSBedrockRegion:    strings.TrimSpace(os.Getenv("AWS_BEDROCK_REGION")),
		AWSBedrockProjectID: strings.TrimSpace(os.Getenv("AWS_BEDROCK_PROJECT_ID")),
		DBPath:              os.Getenv("DB_PATH"),
		HTTPAddr:            os.Getenv("HTTP_ADDR"),
		PublicBaseURL:       strings.TrimRight(os.Getenv("PUBLIC_BASE_URL"), "/"),
	}
	if raw := strings.TrimSpace(os.Getenv("TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			return cfg, errors.New("TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN must be an integer")
		}
		cfg.TranslationRateLimitTokensPerMin = limit
	} else {
		cfg.TranslationRateLimitTokensPerMin = defaultRateLimitTokensPerMinute
	}
	if raw := strings.TrimSpace(os.Getenv("AVATAR_RATE_LIMIT_REQUESTS_PER_MIN")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			return cfg, errors.New("AVATAR_RATE_LIMIT_REQUESTS_PER_MIN must be an integer")
		}
		cfg.AvatarRateLimitRequestsPerMin = limit
	} else {
		cfg.AvatarRateLimitRequestsPerMin = defaultAvatarRateLimitRequestsPerMinute
	}
	if raw := strings.TrimSpace(os.Getenv("MESSAGE_LINK_RETENTION_DAYS")); raw != "" {
		days, err := strconv.Atoi(raw)
		if err != nil {
			return cfg, errors.New("MESSAGE_LINK_RETENTION_DAYS must be an integer")
		}
		if days < 0 {
			return cfg, errors.New("MESSAGE_LINK_RETENTION_DAYS must be non-negative")
		}
		if days > MaxRetentionDays {
			return cfg, fmt.Errorf("MESSAGE_LINK_RETENTION_DAYS must not exceed %d", MaxRetentionDays)
		}
		cfg.MessageLinkRetentionDays = days
	}
	if raw := strings.TrimSpace(os.Getenv("GUILD_DATA_RETENTION_DAYS")); raw != "" {
		days, err := strconv.Atoi(raw)
		if err != nil {
			return cfg, errors.New("GUILD_DATA_RETENTION_DAYS must be an integer")
		}
		if days < 0 {
			return cfg, errors.New("GUILD_DATA_RETENTION_DAYS must be non-negative")
		}
		if days > MaxRetentionDays {
			return cfg, fmt.Errorf("GUILD_DATA_RETENTION_DAYS must not exceed %d", MaxRetentionDays)
		}
		cfg.GuildDataRetentionDays = days
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
	if cfg.AWSAccessKeyID == "" {
		return cfg, errors.New("AWS_ACCESS_KEY_ID is required")
	}
	if cfg.AWSSecretAccessKey == "" {
		return cfg, errors.New("AWS_SECRET_ACCESS_KEY is required")
	}
	if cfg.AWSBedrockRegion == "" {
		return cfg, errors.New("AWS_BEDROCK_REGION is required")
	}
	if !validBedrockRegion.MatchString(cfg.AWSBedrockRegion) {
		return cfg, errors.New("AWS_BEDROCK_REGION is invalid")
	}
	if cfg.AWSBedrockProjectID == "" {
		return cfg, errors.New("AWS_BEDROCK_PROJECT_ID is required")
	}
	if !validBedrockProjectID.MatchString(cfg.AWSBedrockProjectID) {
		return cfg, errors.New("AWS_BEDROCK_PROJECT_ID is invalid")
	}
	if err := validateHTTPAddr(cfg.HTTPAddr); err != nil {
		return cfg, err
	}
	if err := validatePublicBaseURL(cfg.PublicBaseURL); err != nil {
		return cfg, err
	}
	return cfg, nil
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
