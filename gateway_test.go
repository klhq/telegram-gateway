package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestSendEndpointSuccess(t *testing.T) {
	// 1. Start a mock Telegram API server
	telegramServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/botmock-token/getMe" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true,"result":{"id":123456,"is_bot":true,"first_name":"TestBot","username":"test_bot"}}`))
			return
		}

		if r.URL.Path != "/botmock-token/sendMessage" {
			t.Errorf("expected path /botmock-token/sendMessage, got %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if r.Method != http.MethodPost {
			t.Errorf("expected POST request, got %s", r.Method)
		}

		// Read and verify form values
		err := r.ParseForm()
		if err != nil {
			t.Errorf("failed to parse form: %v", err)
		}

		chatID := r.FormValue("chat_id")
		if chatID != "123456789" {
			t.Errorf("expected chat_id '123456789', got '%s'", chatID)
		}

		text := r.FormValue("text")
		if text != "🚨 COMB-ARB ALERT" {
			t.Errorf("expected text '🚨 COMB-ARB ALERT', got '%s'", text)
		}

		parseMode := r.FormValue("parse_mode")
		if parseMode != "Markdown" {
			t.Errorf("expected parse_mode 'Markdown', got '%s'", parseMode)
		}

		replyMarkupStr := r.FormValue("reply_markup")
		if replyMarkupStr == "" {
			t.Errorf("expected reply_markup to not be empty")
		}

		// Mock success Telegram API response
		resp := tgbotapi.APIResponse{
			Ok: true,
			Result: json.RawMessage(`{
				"message_id": 999,
				"chat": {
					"id": 123456789,
					"type": "private"
				},
				"date": 1600000000,
				"text": "🚨 COMB-ARB ALERT"
			}`),
		}
		respBytes, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(respBytes)
	}))
	defer telegramServer.Close()

	// 2. Initialize BotAPI using the mock server URL
	// We need to form a template URL that NewBotAPIWithClient expects:
	// e.g. telegramServer.URL + "/bot%s/%s"
	botURL := telegramServer.URL + "/bot%s/%s"
	bot, err := tgbotapi.NewBotAPIWithClient("mock-token", botURL, http.DefaultClient)
	if err != nil {
		t.Fatalf("failed to create BotAPI: %v", err)
	}

	// 3. Setup Gateway
	cfg := &Config{
		TelegramBotToken: "mock-token",
		Port:             "8000",
	}
	gw := &Gateway{
		Bot:    bot,
		Config: cfg,
		Client: http.DefaultClient,
	}

	// 4. Create request payload for /send
	payload := map[string]interface{}{
		"chat_id": 123456789,
		"text":    "🚨 COMB-ARB ALERT",
		"reply_markup": map[string]interface{}{
			"inline_keyboard": []interface{}{
				[]interface{}{
					map[string]interface{}{
						"text":          "🟢 Approve",
						"callback_data": "combo:approve:ev1",
					},
				},
			},
		},
		"parse_mode": "Markdown",
	}
	payloadBytes, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/send", bytes.NewReader(payloadBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	// 5. Call handler
	handler := http.HandlerFunc(gw.HandleSend)
	handler.ServeHTTP(rr, req)

	// 6. Assert response
	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
	}

	var respPayload map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &respPayload)
	if err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if respPayload["message_id"].(float64) != 999 {
		t.Errorf("expected returned message_id to be 999, got %v", respPayload["message_id"])
	}
	if respPayload["chat_id"].(float64) != 123456789 {
		t.Errorf("expected returned chat_id to be 123456789, got %v", respPayload["chat_id"])
	}
}

func TestSendEndpointTelegramError(t *testing.T) {
	// 1. Mock Telegram server returning error
	telegramServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/botmock-token/getMe" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true,"result":{"id":123456,"is_bot":true,"first_name":"TestBot","username":"test_bot"}}`))
			return
		}

		resp := tgbotapi.APIResponse{
			Ok:          false,
			Description: "Bad Request: chat not found",
			ErrorCode:   400,
		}
		respBytes, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write(respBytes)
	}))
	defer telegramServer.Close()

	botURL := telegramServer.URL + "/bot%s/%s"
	bot, err := tgbotapi.NewBotAPIWithClient("mock-token", botURL, http.DefaultClient)
	if err != nil {
		t.Fatalf("failed to create BotAPI: %v", err)
	}

	cfg := &Config{
		TelegramBotToken: "mock-token",
		Port:             "8000",
	}
	gw := &Gateway{
		Bot:    bot,
		Config: cfg,
		Client: http.DefaultClient,
	}

	payload := map[string]interface{}{
		"chat_id": 999999,
		"text":    "hello",
	}
	payloadBytes, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/send", bytes.NewReader(payloadBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler := http.HandlerFunc(gw.HandleSend)
	handler.ServeHTTP(rr, req)

	// Expect error status code
	if rr.Code != http.StatusBadRequest && rr.Code != http.StatusInternalServerError {
		t.Errorf("expected error status code, got %d", rr.Code)
	}

	var respPayload map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &respPayload)
	if respPayload["error"] == nil {
		t.Errorf("expected error key in response, got %s", rr.Body.String())
	}
}
