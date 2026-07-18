package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// RateLimits holds configurations for rate limiters
type RateLimits struct {
	GlobalPerSecond float64 `json:"global_per_second"`
	ChatPerSecond   float64 `json:"chat_per_second"`
}

// Config holds all gateway configuration loaded from a JSON file
type Config struct {
	TelegramBotToken string            `json:"telegram_bot_token"`
	TelegramChatID   int64             `json:"telegram_chat_id"`
	Port             string            `json:"port"`
	GatewayAPIKey    string            `json:"gateway_api_key"`
	Routes           map[string]string `json:"routes"`
	RateLimits       RateLimits        `json:"rate_limits"`
}

// LoadConfig loads the configuration from a JSON file path
func LoadConfig(path string) (*Config, error) {
	if path == "" {
		path = "config.json"
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file at %s: %w", path, err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config json: %w", err)
	}

	// Validate required fields
	if cfg.TelegramBotToken == "" {
		return nil, fmt.Errorf("telegram_bot_token is required")
	}

	if cfg.Port == "" {
		cfg.Port = "8000" // Default port
	}

	if cfg.Routes == nil {
		cfg.Routes = make(map[string]string)
	}

	// Defaults for rate limits
	if cfg.RateLimits.GlobalPerSecond == 0 {
		cfg.RateLimits.GlobalPerSecond = 30.0
	}
	if cfg.RateLimits.ChatPerSecond == 0 {
		cfg.RateLimits.ChatPerSecond = 1.0
	}

	return &cfg, nil
}
