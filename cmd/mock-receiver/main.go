package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"io"
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

type ReceiverResponse struct {
	Text      string `json:"text,omitempty"`
	ShowAlert bool   `json:"show_alert,omitempty"`
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	port := flag.String("port", "8081", "Port to listen on")
	secret := flag.String("secret", "", "Shared webhook signing secret key for validating requests")
	flag.Parse()

	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Read the raw body bytes for signature validation
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			slog.Error("Failed to read request body", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Perform Webhook Signature Verification if secret is configured
		if *secret != "" {
			receivedSig := r.Header.Get("X-Gateway-Signature")
			if receivedSig == "" {
				slog.Warn("Rejected callback: X-Gateway-Signature header is missing")
				http.Error(w, "Missing Signature", http.StatusUnauthorized)
				return
			}

			// Compute HMAC-SHA256 signature
			mac := hmac.New(sha256.New, []byte(*secret))
			mac.Write(bodyBytes)
			expectedSig := hex.EncodeToString(mac.Sum(nil))

			// Constant-time comparison to prevent timing attacks
			if !hmac.Equal([]byte(receivedSig), []byte(expectedSig)) {
				slog.Warn("Rejected callback: Invalid signature", "received", receivedSig, "expected", expectedSig)
				http.Error(w, "Invalid Signature", http.StatusUnauthorized)
				return
			}
		}

		// Decode the JSON payload
		var payload CallbackPayload
		if err := json.Unmarshal(bodyBytes, &payload); err != nil {
			slog.Error("Failed to decode callback payload", "error", err)
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		slog.Info("Received callback request from Gateway",
			"callback_query_id", payload.CallbackQueryID,
			"from_id", payload.FromID,
			"username", payload.Username,
			"data", payload.Data,
			"verified", *secret != "",
		)

		resp := ReceiverResponse{
			Text:      "Mock Response: Action Executed!",
			ShowAlert: false,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})

	slog.Info("Starting Mock Receiver Server", "port", *port, "signing_validation_enabled", *secret != "")
	if err := http.ListenAndServe(":"+*port, nil); err != nil {
		slog.Error("Mock Receiver Server error", "error", err)
		os.Exit(1)
	}
}
