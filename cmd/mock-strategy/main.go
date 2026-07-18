package main

import (
	"encoding/json"
	"flag"
	"log/slog"
	"net/http"
	"os"
)

type CallbackPayload struct {
	CallbackQueryID string `json:"callback_query_id"`
	FromID          int64  `json:"from_id"`
	Username        string `json:"username"`
	ChatID          int64  `json:"chat_id"`
	MessageID       int    `json:"message_id"`
	Data            string `json:"data"`
}

type StrategyResponse struct {
	Text      string `json:"text,omitempty"`
	ShowAlert bool   `json:"show_alert,omitempty"`
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	port := flag.String("port", "8081", "Port to listen on")
	flag.Parse()

	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var payload CallbackPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			slog.Error("Failed to decode callback payload", "error", err)
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		slog.Info("Received callback request from Gateway",
			"callback_query_id", payload.CallbackQueryID,
			"from_id", payload.FromID,
			"username", payload.Username,
			"data", payload.Data,
		)

		resp := StrategyResponse{
			Text:      "Mock Response: Action Executed!",
			ShowAlert: false,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})

	slog.Info("Starting Mock Strategy Server", "port", *port)
	if err := http.ListenAndServe(":"+*port, nil); err != nil {
		slog.Error("Mock Strategy Server error", "error", err)
		os.Exit(1)
	}
}
