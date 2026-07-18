package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	// Initialize structured logging (JSON handler by default for production)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	slog.Info("Starting Telegram Gateway...")

	// Flag & environment configuration path parsing
	var configPath string
	flag.StringVar(&configPath, "config", "config.json", "path to config.json file")
	flag.Parse()

	if configPath == "config.json" {
		if envPath := os.Getenv("CONFIG_PATH"); envPath != "" {
			configPath = envPath
		}
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		slog.Error("Configuration error", "error", err)
		os.Exit(1)
	}

	// Re-initialize structured logging with configured log level
	var level slog.Level
	switch strings.ToUpper(cfg.LogLevel) {
	case "DEBUG":
		level = slog.LevelDebug
	case "WARN", "WARNING":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))

	// Safe printing of token
	tokenDisplay := "not set"
	if len(cfg.TelegramBotToken) > 8 {
		tokenDisplay = cfg.TelegramBotToken[:4] + "..." + cfg.TelegramBotToken[len(cfg.TelegramBotToken)-4:]
	} else if len(cfg.TelegramBotToken) > 0 {
		tokenDisplay = "set (too short to mask safely)"
	}

	slog.Info("Config loaded successfully",
		"port", cfg.Port,
		"routes_count", len(cfg.Routes),
		"telegram_bot_token", tokenDisplay,
	)

	// Initialize Telegram Bot Client
	bot, err := tgbotapi.NewBotAPI(cfg.TelegramBotToken)
	if err != nil {
		slog.Error("Failed to initialize Telegram Bot", "error", err)
		os.Exit(1)
	}

	slog.Info("Telegram Bot initialized", "username", bot.Self.UserName, "id", bot.Self.ID)

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
	mux.HandleFunc("/send", gw.requireAuth(gw.HandleSend))
	mux.HandleFunc("/health", gw.HandleHealth)
	mux.Handle("/metrics", promhttp.Handler())

	if cfg.GatewayAPIKey == "" {
		slog.Warn("GATEWAY_API_KEY is not set — /send endpoint is unauthenticated")
	}

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
		slog.Info("HTTP Gateway listening", "port", cfg.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP server error", "error", err)
			os.Exit(1)
		}
	}()

	// Block until we receive a signal
	sig := <-stopChan
	slog.Info("Received shutdown signal. Initiating graceful shutdown...", "signal", sig.String())

	// Stop updates polling loop
	cancel()

	// Wait for all in-flight update handlers to finish
	slog.Info("Waiting for active update handlers to complete...")
	gw.WG.Wait()
	slog.Info("All active update handlers completed.")

	// Shut down HTTP server with a 5-second timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("Error shutting down HTTP server", "error", err)
	} else {
		slog.Info("HTTP server shut down successfully.")
	}

	slog.Info("Telegram Gateway stopped.")
}

