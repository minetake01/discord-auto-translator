package translatorbot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigRequiresTokens(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "")
	t.Setenv("GEMINI_API_KEY", "")

	_, err := LoadConfig(filepath.Join(t.TempDir(), "missing.env"))
	if err == nil || !strings.Contains(err.Error(), "DISCORD_TOKEN") {
		t.Fatalf("got %v, want DISCORD_TOKEN error", err)
	}
}

func TestLoadConfigReadsDotEnvWithoutOverridingExistingEnv(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("DISCORD_TOKEN=from-file\nGEMINI_API_KEY=file-key\nDB_PATH=./from-file.db\nHTTP_ADDR=:9090\nPUBLIC_BASE_URL=https://example.test\nGEMINI_RATE_LIMIT_TOKENS_PER_MIN=12345\n"), 0o600); err != nil {
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
	if cfg.GeminiAPIKey != "file-key" {
		t.Fatalf("GeminiAPIKey = %q, want file-key", cfg.GeminiAPIKey)
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
	if cfg.GeminiRateLimitTokensPerMin != 12345 {
		t.Fatalf("GeminiRateLimitTokensPerMin = %d", cfg.GeminiRateLimitTokensPerMin)
	}
}

func TestLoadConfigRejectsInvalidRateLimit(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "token")
	t.Setenv("GEMINI_API_KEY", "key")
	t.Setenv("GEMINI_RATE_LIMIT_TOKENS_PER_MIN", "not-a-number")

	_, err := LoadConfig(filepath.Join(t.TempDir(), "missing.env"))
	if err == nil || !strings.Contains(err.Error(), "GEMINI_RATE_LIMIT_TOKENS_PER_MIN") {
		t.Fatalf("got %v, want rate limit parse error", err)
	}
}

func TestLoadConfigRejectsInvalidHTTPAddr(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "token")
	t.Setenv("GEMINI_API_KEY", "key")
	t.Setenv("HTTP_ADDR", "not-a-listen-addr")

	_, err := LoadConfig(filepath.Join(t.TempDir(), "missing.env"))
	if err == nil || !strings.Contains(err.Error(), "HTTP_ADDR") {
		t.Fatalf("got %v, want HTTP_ADDR error", err)
	}
}

func TestLoadConfigRejectsInvalidPublicBaseURL(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "token")
	t.Setenv("GEMINI_API_KEY", "key")
	t.Setenv("PUBLIC_BASE_URL", "ftp://example.com")

	_, err := LoadConfig(filepath.Join(t.TempDir(), "missing.env"))
	if err == nil || !strings.Contains(err.Error(), "PUBLIC_BASE_URL") {
		t.Fatalf("got %v, want PUBLIC_BASE_URL error", err)
	}
}

func TestLoadConfigParsesAdminRoleIDs(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "token")
	t.Setenv("GEMINI_API_KEY", "key")
	t.Setenv("ADMIN_ROLE_IDS", " 123456789012345678 , 987654321098765432 , 123456789012345678 ")

	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "missing.env"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AdminRoleIDs) != 2 {
		t.Fatalf("AdminRoleIDs = %#v, want 2 entries", cfg.AdminRoleIDs)
	}
	if cfg.AdminRoleIDs[0] != "123456789012345678" || cfg.AdminRoleIDs[1] != "987654321098765432" {
		t.Fatalf("AdminRoleIDs = %#v", cfg.AdminRoleIDs)
	}
}

func TestLoadConfigRejectsInvalidAdminRoleIDs(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "token")
	t.Setenv("GEMINI_API_KEY", "key")
	t.Setenv("ADMIN_ROLE_IDS", "not-a-snowflake")

	_, err := LoadConfig(filepath.Join(t.TempDir(), "missing.env"))
	if err == nil || !strings.Contains(err.Error(), "ADMIN_ROLE_IDS") {
		t.Fatalf("got %v, want ADMIN_ROLE_IDS error", err)
	}
}
