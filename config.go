package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
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
		} else {
			return nil, fmt.Errorf("failed to read config file at %s: %w", path, err)
		}
	} else {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config json: %w", err)
		}
	}

	applyEnvironmentOverrides(&cfg)

	for _, secret := range []struct {
		value   *string
		envName string
	}{
		{&cfg.TelegramBotToken, "TELEGRAM_BOT_TOKEN"},
		{&cfg.GatewayAPIKey, "GATEWAY_API_KEY"},
		{&cfg.WebhookSecret, "WEBHOOK_SECRET"},
	} {
		if err := applySecretSource(secret.value, secret.envName); err != nil {
			return nil, err
		}
	}

	if err := applyRouteSources(&cfg); err != nil {
		return nil, err
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

func applyEnvironmentOverrides(cfg *Config) {
	if port := os.Getenv("PORT"); port != "" {
		cfg.Port = port
	}
	if logLevel := os.Getenv("LOG_LEVEL"); logLevel != "" {
		cfg.LogLevel = logLevel
	}
	if chatID := os.Getenv("TELEGRAM_CHAT_ID"); chatID != "" {
		if parsed, err := strconv.ParseInt(chatID, 10, 64); err == nil {
			cfg.TelegramChatID = parsed
		}
	}
	if globalLimit := os.Getenv("GLOBAL_RATE_LIMIT"); globalLimit != "" {
		if parsed, err := strconv.ParseFloat(globalLimit, 64); err == nil {
			cfg.RateLimits.GlobalPerSecond = parsed
		}
	}
	if chatLimit := os.Getenv("CHAT_RATE_LIMIT"); chatLimit != "" {
		if parsed, err := strconv.ParseFloat(chatLimit, 64); err == nil {
			cfg.RateLimits.ChatPerSecond = parsed
		}
	}
}

func applyRouteSources(cfg *Config) error {
	if cfg.Routes == nil {
		cfg.Routes = make(map[string]string)
	}
	if comboURL := os.Getenv("COMB_ARB_URL"); comboURL != "" {
		cfg.Routes["combo"] = comboURL
	}
	if bookURL := os.Getenv("BOOK_ARB_URL"); bookURL != "" {
		cfg.Routes["book"] = bookURL
	}

	routesJSON := os.Getenv("ROUTES_JSON")
	routesPath := os.Getenv("ROUTES_FILE")
	if routesJSON != "" && routesPath != "" {
		return fmt.Errorf("ROUTES_JSON and ROUTES_FILE cannot both be set")
	}

	var (
		data   []byte
		source string
	)
	if routesPath != "" {
		var err error
		data, err = os.ReadFile(routesPath)
		if err != nil {
			return fmt.Errorf("failed to read ROUTES_FILE at %s: %w", routesPath, err)
		}
		source = "ROUTES_FILE at " + routesPath
	} else if routesJSON != "" {
		data = []byte(routesJSON)
		source = "ROUTES_JSON"
	} else {
		return nil
	}

	var routes map[string]string
	if err := json.Unmarshal(data, &routes); err != nil {
		return fmt.Errorf("failed to parse %s: %w", source, err)
	}
	if routes == nil {
		return fmt.Errorf("%s must be a JSON object", source)
	}
	cfg.Routes = routes
	return nil
}

func applySecretSource(value *string, envName string) error {
	if environmentValue := os.Getenv(envName); environmentValue != "" {
		*value = environmentValue
	}

	fileEnvName := envName + "_FILE"
	secretPath := os.Getenv(fileEnvName)
	if secretPath == "" {
		return nil
	}

	secret, err := os.ReadFile(secretPath)
	if err != nil {
		return fmt.Errorf("failed to read %s at %s: %w", fileEnvName, secretPath, err)
	}
	secretValue := strings.TrimSpace(string(secret))
	if secretValue == "" {
		return fmt.Errorf("%s is empty", fileEnvName)
	}
	*value = secretValue
	return nil
}
