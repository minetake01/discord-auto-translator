package translatorbot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigRequiresTokens(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "")
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	t.Setenv("CLOUDFLARE_AI_GATEWAY_ID", "")

	_, err := LoadConfig(filepath.Join(t.TempDir(), "missing.env"))
	if err == nil || !strings.Contains(err.Error(), "DISCORD_TOKEN") {
		t.Fatalf("got %v, want DISCORD_TOKEN error", err)
	}
}

func TestLoadConfigReadsDotEnvWithoutOverridingExistingEnv(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("DISCORD_TOKEN=from-file\nCLOUDFLARE_ACCOUNT_ID=account-id\nCLOUDFLARE_API_TOKEN=api-token\nCLOUDFLARE_AI_GATEWAY_ID=gateway-id\nDB_PATH=./from-file.db\nHTTP_ADDR=:9090\nPUBLIC_BASE_URL=https://example.test\nTRANSLATION_RATE_LIMIT_TOKENS_PER_MIN=12345\nAVATAR_RATE_LIMIT_REQUESTS_PER_MIN=60\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("DISCORD_TOKEN", "existing-token")
	cfg, err := LoadConfig(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DiscordToken != "existing-token" {
		t.Fatalf("DiscordToken = %q, want existing-token", cfg.DiscordToken)
	}
	if cfg.CloudflareAccountID != "account-id" || cfg.CloudflareAPIToken != "api-token" || cfg.CloudflareAIGatewayID != "gateway-id" {
		t.Fatalf("unexpected Cloudflare config: account=%q token=%q gateway=%q", cfg.CloudflareAccountID, cfg.CloudflareAPIToken, cfg.CloudflareAIGatewayID)
	}
	if cfg.DBPath != "./from-file.db" {
		t.Fatalf("DBPath = %q", cfg.DBPath)
	}
	if cfg.HTTPAddr != ":9090" {
		t.Fatalf("HTTPAddr = %q", cfg.HTTPAddr)
	}
	if cfg.PublicBaseURL != "https://example.test" {
		t.Fatalf("PublicBaseURL = %q", cfg.PublicBaseURL)
	}
	if cfg.TranslationRateLimitTokensPerMin != 12345 {
		t.Fatalf("TranslationRateLimitTokensPerMin = %d", cfg.TranslationRateLimitTokensPerMin)
	}
	if cfg.AvatarRateLimitRequestsPerMin != 60 {
		t.Fatalf("AvatarRateLimitRequestsPerMin = %d", cfg.AvatarRateLimitRequestsPerMin)
	}
}

func TestLoadConfigRejectsInvalidRateLimit(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "token")
	setRequiredCloudflareConfig(t)
	t.Setenv("TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN", "not-a-number")

	_, err := LoadConfig(filepath.Join(t.TempDir(), "missing.env"))
	if err == nil || !strings.Contains(err.Error(), "TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN") {
		t.Fatalf("got %v, want rate limit parse error", err)
	}
}

func TestLoadConfigRejectsInvalidAvatarRateLimit(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "token")
	setRequiredCloudflareConfig(t)
	t.Setenv("AVATAR_RATE_LIMIT_REQUESTS_PER_MIN", "not-a-number")

	_, err := LoadConfig(filepath.Join(t.TempDir(), "missing.env"))
	if err == nil || !strings.Contains(err.Error(), "AVATAR_RATE_LIMIT_REQUESTS_PER_MIN") {
		t.Fatalf("got %v, want avatar rate limit parse error", err)
	}
}

func TestLoadConfigGuildDataRetentionDays(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    int
		wantErr string
	}{
		{name: "unset"},
		{name: "zero disables purge", value: "0"},
		{name: "positive days", value: "30", want: 30},
		{name: "maximum safe days", value: "106751", want: 106751},
		{name: "duration overflow", value: "106752", wantErr: "GUILD_DATA_RETENTION_DAYS must not exceed 106751"},
		{name: "negative", value: "-1", wantErr: "GUILD_DATA_RETENTION_DAYS must be non-negative"},
		{name: "non-integer", value: "thirty", wantErr: "GUILD_DATA_RETENTION_DAYS must be an integer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DISCORD_TOKEN", "token")
			setRequiredCloudflareConfig(t)
			t.Setenv("GUILD_DATA_RETENTION_DAYS", tt.value)

			cfg, err := LoadConfig(filepath.Join(t.TempDir(), "missing.env"))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("got %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.GuildDataRetentionDays != tt.want {
				t.Fatalf("GuildDataRetentionDays = %d, want %d", cfg.GuildDataRetentionDays, tt.want)
			}
		})
	}
}

func TestLoadConfigMessageLinkRetentionDays(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    int
		wantErr string
	}{
		{name: "unset"},
		{name: "zero disables purge", value: "0"},
		{name: "positive days", value: "60", want: 60},
		{name: "maximum safe days", value: "106751", want: 106751},
		{name: "duration overflow", value: "106752", wantErr: "MESSAGE_LINK_RETENTION_DAYS must not exceed 106751"},
		{name: "negative", value: "-1", wantErr: "MESSAGE_LINK_RETENTION_DAYS must be non-negative"},
		{name: "non-integer", value: "sixty", wantErr: "MESSAGE_LINK_RETENTION_DAYS must be an integer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DISCORD_TOKEN", "token")
			setRequiredCloudflareConfig(t)
			t.Setenv("MESSAGE_LINK_RETENTION_DAYS", tt.value)

			cfg, err := LoadConfig(filepath.Join(t.TempDir(), "missing.env"))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("got %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.MessageLinkRetentionDays != tt.want {
				t.Fatalf("MessageLinkRetentionDays = %d, want %d", cfg.MessageLinkRetentionDays, tt.want)
			}
		})
	}
}

func TestLoadConfigRejectsInvalidHTTPAddr(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "token")
	setRequiredCloudflareConfig(t)
	t.Setenv("HTTP_ADDR", "not-a-listen-addr")

	_, err := LoadConfig(filepath.Join(t.TempDir(), "missing.env"))
	if err == nil || !strings.Contains(err.Error(), "HTTP_ADDR") {
		t.Fatalf("got %v, want HTTP_ADDR error", err)
	}
}

func TestLoadConfigRejectsInvalidPublicBaseURL(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "token")
	setRequiredCloudflareConfig(t)
	t.Setenv("PUBLIC_BASE_URL", "ftp://example.com")

	_, err := LoadConfig(filepath.Join(t.TempDir(), "missing.env"))
	if err == nil || !strings.Contains(err.Error(), "PUBLIC_BASE_URL") {
		t.Fatalf("got %v, want PUBLIC_BASE_URL error", err)
	}
}

func TestLoadConfigRequiresEveryCloudflareValue(t *testing.T) {
	tests := []struct {
		name    string
		missing string
	}{
		{name: "account", missing: "CLOUDFLARE_ACCOUNT_ID"},
		{name: "token", missing: "CLOUDFLARE_API_TOKEN"},
		{name: "gateway", missing: "CLOUDFLARE_AI_GATEWAY_ID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DISCORD_TOKEN", "discord-token")
			setRequiredCloudflareConfig(t)
			t.Setenv(tt.missing, "")
			_, err := LoadConfig(filepath.Join(t.TempDir(), "missing.env"))
			if err == nil || !strings.Contains(err.Error(), tt.missing) {
				t.Fatalf("error = %v, want %s", err, tt.missing)
			}
		})
	}
}

func setRequiredCloudflareConfig(t *testing.T) {
	t.Helper()
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "account-id")
	t.Setenv("CLOUDFLARE_API_TOKEN", "api-token")
	t.Setenv("CLOUDFLARE_AI_GATEWAY_ID", "gateway-id")
}
