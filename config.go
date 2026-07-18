package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
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
	WebhookSecret    string            `json:"webhook_secret"`
	LogLevel         string            `json:"log_level"`
}

// LoadConfig loads the configuration from a JSON file path
func LoadConfig(path string) (*Config, error) {
	if path == "" {
		path = "config.json"
	}

	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Info("config.json not found — falling back to environment variables")
			cfg.TelegramBotToken = os.Getenv("TELEGRAM_BOT_TOKEN")
			cfg.Port = os.Getenv("PORT")
			cfg.GatewayAPIKey = os.Getenv("GATEWAY_API_KEY")
			cfg.WebhookSecret = os.Getenv("WEBHOOK_SECRET")
			cfg.LogLevel = os.Getenv("LOG_LEVEL")

			if chatIDStr := os.Getenv("TELEGRAM_CHAT_ID"); chatIDStr != "" {
				if parsed, err := strconv.ParseInt(chatIDStr, 10, 64); err == nil {
					cfg.TelegramChatID = parsed
				}
			}

			cfg.Routes = make(map[string]string)
			if combArb := os.Getenv("COMB_ARB_URL"); combArb != "" {
				cfg.Routes["combo"] = combArb
			}
			if bookArb := os.Getenv("BOOK_ARB_URL"); bookArb != "" {
				cfg.Routes["book"] = bookArb
			}

			var globalLimit, chatLimit float64
			if glStr := os.Getenv("GLOBAL_RATE_LIMIT"); glStr != "" {
				if parsed, err := strconv.ParseFloat(glStr, 64); err == nil {
					globalLimit = parsed
				}
			}
			if clStr := os.Getenv("CHAT_RATE_LIMIT"); clStr != "" {
				if parsed, err := strconv.ParseFloat(clStr, 64); err == nil {
					chatLimit = parsed
				}
			}
			cfg.RateLimits.GlobalPerSecond = globalLimit
			cfg.RateLimits.ChatPerSecond = chatLimit

		} else {
			return nil, fmt.Errorf("failed to read config file at %s: %w", path, err)
		}
	} else {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config json: %w", err)
		}
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

	// Default log level
	if cfg.LogLevel == "" {
		cfg.LogLevel = "INFO"
	}

	return &cfg, nil
}
