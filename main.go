package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func main() {
	log.Println("Starting Telegram Gateway...")

	cfg, err := LoadConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	// Safe printing of token
	tokenDisplay := "not set"
	if len(cfg.TelegramBotToken) > 8 {
		tokenDisplay = cfg.TelegramBotToken[:4] + "..." + cfg.TelegramBotToken[len(cfg.TelegramBotToken)-4:]
	} else if len(cfg.TelegramBotToken) > 0 {
		tokenDisplay = "set (too short to mask safely)"
	}

	log.Printf("Config loaded: PORT=%s, COMB_ARB_URL=%s, BOOK_ARB_URL=%s, TELEGRAM_BOT_TOKEN=%s",
		cfg.Port, cfg.CombArbURL, cfg.BookArbURL, tokenDisplay)

	// Initialize Telegram Bot Client
	bot, err := tgbotapi.NewBotAPI(cfg.TelegramBotToken)
	if err != nil {
		log.Fatalf("Failed to initialize Telegram Bot: %v", err)
	}

	log.Printf("Telegram Bot initialized: @%s (ID: %d)", bot.Self.UserName, bot.Self.ID)

	// Initialize Gateway
	gw := &Gateway{
		Bot:    bot,
		Config: cfg,
		Client: &http.Client{
			Timeout: 10 * time.Second, // fallback timeout for http client
		},
	}

	// Setup HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/send", gw.HandleSend)
	mux.HandleFunc("/health", gw.HandleHealth)

	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: mux,
	}

	// Listen for shutdown signals
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	// Context to control the update polling loop goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start updates polling loop in background
	go gw.StartUpdateLoop(ctx)

	// Start HTTP server in background
	go func() {
		log.Printf("HTTP Gateway listening on port %s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Block until we receive a signal
	sig := <-stopChan
	log.Printf("Received signal %v. Initiating graceful shutdown...", sig)

	// Stop updates polling loop
	cancel()

	// Shut down HTTP server with a 5-second timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Error shutting down HTTP server: %v", err)
	} else {
		log.Println("HTTP server shut down successfully.")
	}

	log.Println("Telegram Gateway stopped.")
}
