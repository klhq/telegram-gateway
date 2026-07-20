package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func clearConfigEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"TELEGRAM_BOT_TOKEN",
		"TELEGRAM_BOT_TOKEN_FILE",
		"TELEGRAM_CHAT_ID",
		"PORT",
		"GATEWAY_API_KEY",
		"GATEWAY_API_KEY_FILE",
		"WEBHOOK_SECRET",
		"WEBHOOK_SECRET_FILE",
		"LOG_LEVEL",
		"GLOBAL_RATE_LIMIT",
		"CHAT_RATE_LIMIT",
		"COMB_ARB_URL",
		"BOOK_ARB_URL",
		"ROUTES_JSON",
		"ROUTES_FILE",
	} {
		t.Setenv(name, "")
	}
}

func TestLoadConfig(t *testing.T) {
	clearConfigEnvironment(t)
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")

	content := `{
		"telegram_bot_token": "test-token",
		"port": "9000",
		"routes": {
			"combo": "http://comb:8001/callback",
			"book": "http://book:8002/callback"
		}
	}`

	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config test file: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.TelegramBotToken != "test-token" {
		t.Errorf("expected TelegramBotToken to be 'test-token', got '%s'", cfg.TelegramBotToken)
	}
	if cfg.Port != "9000" {
		t.Errorf("expected Port to be '9000', got '%s'", cfg.Port)
	}
	if cfg.Routes["combo"] != "http://comb:8001/callback" {
		t.Errorf("expected combo route, got '%s'", cfg.Routes["combo"])
	}
	if cfg.Routes["book"] != "http://book:8002/callback" {
		t.Errorf("expected book route, got '%s'", cfg.Routes["book"])
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	clearConfigEnvironment(t)
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")

	content := `{
		"telegram_bot_token": "test-token"
	}`

	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config test file: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Port != "8000" {
		t.Errorf("expected default Port '8000', got '%s'", cfg.Port)
	}
	if len(cfg.Routes) != 0 {
		t.Errorf("expected empty routes map, got %v", cfg.Routes)
	}
	if cfg.RateLimits.GlobalPerSecond != 30.0 {
		t.Errorf("expected default GlobalPerSecond 30.0, got %f", cfg.RateLimits.GlobalPerSecond)
	}
	if cfg.RateLimits.ChatPerSecond != 1.0 {
		t.Errorf("expected default ChatPerSecond 1.0, got %f", cfg.RateLimits.ChatPerSecond)
	}
	if cfg.LogLevel != "INFO" {
		t.Errorf("expected default LogLevel 'INFO', got '%s'", cfg.LogLevel)
	}
}

func TestLoadConfigMissingToken(t *testing.T) {
	clearConfigEnvironment(t)
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")

	content := `{
		"port": "8000"
	}`

	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config test file: %v", err)
	}

	_, err := LoadConfig(configPath)
	if err == nil {
		t.Error("expected error when telegram_bot_token is missing")
	}
}

func TestLoadConfigFileNotFound(t *testing.T) {
	clearConfigEnvironment(t)
	_, err := LoadConfig("nonexistent_config.json")
	if err == nil {
		t.Error("expected error when config file does not exist")
	}
}

func TestLoadConfigTelegramBotTokenFileOverridesOtherSources(t *testing.T) {
	clearConfigEnvironment(t)
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	secretPath := filepath.Join(tempDir, "telegram_bot_token")

	if err := os.WriteFile(configPath, []byte(`{"telegram_bot_token":"json-token"}`), 0600); err != nil {
		t.Fatalf("failed to write config test file: %v", err)
	}
	if err := os.WriteFile(secretPath, []byte("file-token\n"), 0600); err != nil {
		t.Fatalf("failed to write token secret: %v", err)
	}
	t.Setenv("TELEGRAM_BOT_TOKEN", "environment-token")
	t.Setenv("TELEGRAM_BOT_TOKEN_FILE", secretPath)

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.TelegramBotToken != "file-token" {
		t.Fatalf("expected file-backed token, got %q", cfg.TelegramBotToken)
	}
}

func TestLoadConfigOptionalSecretFilesOverrideOtherSources(t *testing.T) {
	clearConfigEnvironment(t)
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	apiKeyPath := filepath.Join(tempDir, "gateway_api_key")
	webhookSecretPath := filepath.Join(tempDir, "webhook_secret")

	content := `{"telegram_bot_token":"token","gateway_api_key":"json-key","webhook_secret":"json-secret"}`
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write config test file: %v", err)
	}
	if err := os.WriteFile(apiKeyPath, []byte("file-key\n"), 0600); err != nil {
		t.Fatalf("failed to write API key secret: %v", err)
	}
	if err := os.WriteFile(webhookSecretPath, []byte("file-secret\n"), 0600); err != nil {
		t.Fatalf("failed to write webhook secret: %v", err)
	}
	t.Setenv("GATEWAY_API_KEY", "environment-key")
	t.Setenv("GATEWAY_API_KEY_FILE", apiKeyPath)
	t.Setenv("WEBHOOK_SECRET", "environment-secret")
	t.Setenv("WEBHOOK_SECRET_FILE", webhookSecretPath)

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.GatewayAPIKey != "file-key" || cfg.WebhookSecret != "file-secret" {
		t.Fatalf("expected file-backed optional secrets, got API key %q and webhook secret %q", cfg.GatewayAPIKey, cfg.WebhookSecret)
	}
}

func TestLoadConfigEnvironmentOverridesJSON(t *testing.T) {
	clearConfigEnvironment(t)
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	content := `{
		"telegram_bot_token":"json-token",
		"telegram_chat_id":1,
		"port":"8000",
		"gateway_api_key":"json-key",
		"webhook_secret":"json-secret",
		"log_level":"INFO",
		"rate_limits":{"global_per_second":1,"chat_per_second":1}
	}`
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write config test file: %v", err)
	}

	t.Setenv("TELEGRAM_BOT_TOKEN", "environment-token")
	t.Setenv("TELEGRAM_CHAT_ID", "42")
	t.Setenv("PORT", "9000")
	t.Setenv("GATEWAY_API_KEY", "environment-key")
	t.Setenv("WEBHOOK_SECRET", "environment-secret")
	t.Setenv("LOG_LEVEL", "DEBUG")
	t.Setenv("GLOBAL_RATE_LIMIT", "12.5")
	t.Setenv("CHAT_RATE_LIMIT", "2.5")

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.TelegramBotToken != "environment-token" || cfg.TelegramChatID != 42 || cfg.Port != "9000" {
		t.Fatalf("expected core environment overrides, got %#v", cfg)
	}
	if cfg.GatewayAPIKey != "environment-key" || cfg.WebhookSecret != "environment-secret" || cfg.LogLevel != "DEBUG" {
		t.Fatalf("expected security and logging environment overrides, got %#v", cfg)
	}
	if cfg.RateLimits.GlobalPerSecond != 12.5 || cfg.RateLimits.ChatPerSecond != 2.5 {
		t.Fatalf("expected rate limit environment overrides, got %#v", cfg.RateLimits)
	}
}

func TestLoadConfigRoutesJSONOverridesJSONConfig(t *testing.T) {
	clearConfigEnvironment(t)
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	content := `{"telegram_bot_token":"token","routes":{"old":"http://old:8080/callback"}}`
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write config test file: %v", err)
	}
	t.Setenv("ROUTES_JSON", `{"strategy-a":"http://strategy-a:8080/callback"}`)

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if len(cfg.Routes) != 1 || cfg.Routes["strategy-a"] != "http://strategy-a:8080/callback" {
		t.Fatalf("expected ROUTES_JSON to replace config routes, got %#v", cfg.Routes)
	}
}

func TestLoadConfigRoutesFileOverridesJSONConfig(t *testing.T) {
	clearConfigEnvironment(t)
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	routesPath := filepath.Join(tempDir, "routes.json")
	if err := os.WriteFile(configPath, []byte(`{"telegram_bot_token":"token","routes":{"old":"http://old:8080/callback"}}`), 0600); err != nil {
		t.Fatalf("failed to write config test file: %v", err)
	}
	if err := os.WriteFile(routesPath, []byte(`{"strategy-b":"http://strategy-b:8080/callback"}`), 0600); err != nil {
		t.Fatalf("failed to write routes file: %v", err)
	}
	t.Setenv("ROUTES_FILE", routesPath)

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if len(cfg.Routes) != 1 || cfg.Routes["strategy-b"] != "http://strategy-b:8080/callback" {
		t.Fatalf("expected ROUTES_FILE to replace config routes, got %#v", cfg.Routes)
	}
}

func TestLoadConfigRejectsConflictingRouteSources(t *testing.T) {
	clearConfigEnvironment(t)
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"telegram_bot_token":"token"}`), 0600); err != nil {
		t.Fatalf("failed to write config test file: %v", err)
	}
	t.Setenv("ROUTES_JSON", `{}`)
	t.Setenv("ROUTES_FILE", filepath.Join(tempDir, "routes.json"))

	_, err := LoadConfig(configPath)
	if err == nil || !strings.Contains(err.Error(), "ROUTES_JSON and ROUTES_FILE") {
		t.Fatalf("expected conflicting route source error, got %v", err)
	}
}

func TestLoadConfigPreservesLegacyEnvironmentRoutes(t *testing.T) {
	clearConfigEnvironment(t)
	t.Setenv("TELEGRAM_BOT_TOKEN", "token")
	t.Setenv("COMB_ARB_URL", "http://combo:8080/callback")
	t.Setenv("BOOK_ARB_URL", "http://book:8080/callback")

	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.Routes["combo"] != "http://combo:8080/callback" || cfg.Routes["book"] != "http://book:8080/callback" {
		t.Fatalf("expected legacy environment routes, got %#v", cfg.Routes)
	}
}

func TestLoadConfigRejectsEmptySecretFileWithSourceName(t *testing.T) {
	clearConfigEnvironment(t)
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	secretPath := filepath.Join(tempDir, "telegram_bot_token")
	if err := os.WriteFile(configPath, []byte(`{}`), 0600); err != nil {
		t.Fatalf("failed to write config test file: %v", err)
	}
	if err := os.WriteFile(secretPath, []byte("\n"), 0600); err != nil {
		t.Fatalf("failed to write empty secret file: %v", err)
	}
	t.Setenv("TELEGRAM_BOT_TOKEN_FILE", secretPath)

	_, err := LoadConfig(configPath)
	if err == nil || !strings.Contains(err.Error(), "TELEGRAM_BOT_TOKEN_FILE is empty") {
		t.Fatalf("expected source-specific empty secret error, got %v", err)
	}
}

func TestLoadConfigRejectsNonObjectRouteSources(t *testing.T) {
	clearConfigEnvironment(t)
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"telegram_bot_token":"token"}`), 0600); err != nil {
		t.Fatalf("failed to write config test file: %v", err)
	}
	t.Setenv("ROUTES_JSON", "null")

	_, err := LoadConfig(configPath)
	if err == nil || !strings.Contains(err.Error(), "ROUTES_JSON must be a JSON object") {
		t.Fatalf("expected route object error, got %v", err)
	}
}
