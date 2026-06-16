package main

import (
	"os"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	// Set required env var
	os.Setenv("TELEGRAM_BOT_TOKEN", "test-token")
	defer os.Unsetenv("TELEGRAM_BOT_TOKEN")

	// Set optional env vars
	os.Setenv("PORT", "9000")
	defer os.Unsetenv("PORT")
	os.Setenv("COMB_ARB_URL", "http://comb:8001/callback")
	defer os.Unsetenv("COMB_ARB_URL")
	os.Setenv("BOOK_ARB_URL", "http://book:8002/callback")
	defer os.Unsetenv("BOOK_ARB_URL")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.TelegramBotToken != "test-token" {
		t.Errorf("expected TelegramBotToken to be 'test-token', got '%s'", cfg.TelegramBotToken)
	}
	if cfg.Port != "9000" {
		t.Errorf("expected Port to be '9000', got '%s'", cfg.Port)
	}
	if cfg.CombArbURL != "http://comb:8001/callback" {
		t.Errorf("expected CombArbURL to be 'http://comb:8001/callback', got '%s'", cfg.CombArbURL)
	}
	if cfg.BookArbURL != "http://book:8002/callback" {
		t.Errorf("expected BookArbURL to be 'http://book:8002/callback', got '%s'", cfg.BookArbURL)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	os.Setenv("TELEGRAM_BOT_TOKEN", "test-token")
	defer os.Unsetenv("TELEGRAM_BOT_TOKEN")

	// Clear optional env vars to test defaults
	os.Unsetenv("PORT")
	os.Unsetenv("COMB_ARB_URL")
	os.Unsetenv("BOOK_ARB_URL")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Port != "8000" {
		t.Errorf("expected default Port '8000', got '%s'", cfg.Port)
	}
	if cfg.CombArbURL != "http://localhost:8001/callback" {
		t.Errorf("expected default CombArbURL 'http://localhost:8001/callback', got '%s'", cfg.CombArbURL)
	}
	if cfg.BookArbURL != "http://localhost:8002/callback" {
		t.Errorf("expected default BookArbURL 'http://localhost:8002/callback', got '%s'", cfg.BookArbURL)
	}
}

func TestLoadConfigMissingToken(t *testing.T) {
	os.Unsetenv("TELEGRAM_BOT_TOKEN")

	_, err := LoadConfig()
	if err == nil {
		t.Error("expected error when TELEGRAM_BOT_TOKEN is missing")
	}
}
