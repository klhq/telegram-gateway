package main

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

// Config holds all gateway configuration loaded from environment variables
type Config struct {
	TelegramBotToken string
	Port             string
	CombArbURL       string
	BookArbURL       string
}

// LoadConfig loads the configuration from the environment and optional .env file
func LoadConfig() (*Config, error) {
	// Attempt to load .env file if it exists, but don't error if it's missing
	_ = godotenv.Load()

	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN environment variable is required")
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

	return &Config{
		TelegramBotToken: token,
		Port:             port,
		CombArbURL:       combArbURL,
		BookArbURL:       bookArbURL,
	}, nil
}
