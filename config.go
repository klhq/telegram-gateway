package main

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

// Config holds all gateway configuration loaded from environment variables
type Config struct {
	TelegramBotToken string
	TelegramChatID   int64
	Port             string
	CombArbURL       string
	BookArbURL       string
	GatewayAPIKey    string // optional shared secret for POST /send
}

// LoadConfig loads the configuration from the environment and optional .env file
func LoadConfig() (*Config, error) {
	// Attempt to load .env file if it exists, but don't error if it's missing
	_ = godotenv.Load()

	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN environment variable is required")
	}

	var chatID int64
	chatIDStr := os.Getenv("TELEGRAM_CHAT_ID")
	if chatIDStr != "" {
		_, err := fmt.Sscan(chatIDStr, &chatID)
		if err != nil {
			return nil, fmt.Errorf("failed to parse TELEGRAM_CHAT_ID: %w", err)
		}
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}

	combArbURL := os.Getenv("COMB_ARB_URL")
	if combArbURL == "" {
		combArbURL = "http://localhost:8001/callback"
	}

	bookArbURL := os.Getenv("BOOK_ARB_URL")
	if bookArbURL == "" {
		bookArbURL = "http://localhost:8002/callback"
	}

	gatewayAPIKey := os.Getenv("GATEWAY_API_KEY")

	return &Config{
		TelegramBotToken: token,
		TelegramChatID:   chatID,
		Port:             port,
		CombArbURL:       combArbURL,
		BookArbURL:       bookArbURL,
		GatewayAPIKey:    gatewayAPIKey,
	}, nil
}
