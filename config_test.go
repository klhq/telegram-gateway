package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
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
	_, err := LoadConfig("nonexistent_config.json")
	if err == nil {
		t.Error("expected error when config file does not exist")
	}
}

