package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
		if text != "🚨 ALERT" {
			t.Errorf("expected text '🚨 ALERT', got '%s'", text)
		}

		parseMode := r.FormValue("parse_mode")
		if parseMode != "Markdown" {
			t.Errorf("expected parse_mode 'Markdown', got '%s'", parseMode)
		}

		replyMarkupStr := r.FormValue("reply_markup")
		if replyMarkupStr == "" {
			t.Errorf("expected reply_markup to not be empty")
		}

		disableWebPagePreview := r.FormValue("disable_web_page_preview")
		if disableWebPagePreview != "true" {
			t.Errorf("expected disable_web_page_preview 'true', got '%s'", disableWebPagePreview)
		}

		disableNotification := r.FormValue("disable_notification")
		if disableNotification != "true" {
			t.Errorf("expected disable_notification 'true', got '%s'", disableNotification)
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
				"text": "🚨 ALERT"
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
		"text":    "🚨 ALERT",
		"reply_markup": map[string]interface{}{
			"inline_keyboard": []interface{}{
				[]interface{}{
					map[string]interface{}{
						"text":          "🟢 Approve",
						"callback_data": "receiver-a:approve:ev1",
					},
				},
			},
		},
		"parse_mode":               "Markdown",
		"disable_web_page_preview": true,
		"disable_notification":     true,
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

func TestSendEndpointDefaultsToPlainText(t *testing.T) {
	telegramServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/botmock-token/getMe" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":123456,"is_bot":true,"first_name":"TestBot","username":"test_bot"}}`))
			return
		}
		if r.URL.Path != "/botmock-token/sendMessage" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("failed to parse form: %v", err)
		}
		if parseMode := r.FormValue("parse_mode"); parseMode != "" {
			t.Errorf("expected no default parse mode, got %q", parseMode)
		}

		respBytes, _ := json.Marshal(tgbotapi.APIResponse{
			Ok:     true,
			Result: json.RawMessage(`{"message_id":999,"chat":{"id":123456789,"type":"private"},"date":1600000000}`),
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(respBytes)
	}))
	defer telegramServer.Close()

	bot, err := tgbotapi.NewBotAPIWithClient("mock-token", telegramServer.URL+"/bot%s/%s", http.DefaultClient)
	if err != nil {
		t.Fatalf("failed to create BotAPI: %v", err)
	}
	gw := &Gateway{Bot: bot, Config: &Config{TelegramBotToken: "mock-token", Port: "8000"}, Client: http.DefaultClient}

	req := httptest.NewRequest(http.MethodPost, "/send", strings.NewReader(`{"chat_id":123456789,"text":"untrusted *text*"}`))
	rr := httptest.NewRecorder()
	gw.HandleSend(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
	}
}

func TestSendEndpointRejectsUnsupportedParseMode(t *testing.T) {
	gw := &Gateway{Config: &Config{TelegramBotToken: "mock-token", Port: "8000"}}
	req := httptest.NewRequest(http.MethodPost, "/send", strings.NewReader(`{"chat_id":123456789,"text":"message","parse_mode":"PlainText"}`))
	rr := httptest.NewRecorder()

	gw.HandleSend(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d. Body: %s", rr.Code, rr.Body.String())
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

func TestCallbackQueryRoutingReceiverASuccess(t *testing.T) {
	// 1. Start a mock receiver server for receiver-a
	receiverCalled := false
	receiverServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receiverCalled = true
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/callback" {
			t.Errorf("expected path /callback, got %s", r.URL.Path)
		}

		var payload CallbackPayload
		err := json.NewDecoder(r.Body).Decode(&payload)
		if err != nil {
			t.Errorf("failed to decode callback payload: %v", err)
		}

		if payload.Data != "receiver-a:approve:ev1" {
			t.Errorf("expected data 'receiver-a:approve:ev1', got '%s'", payload.Data)
		}
		if payload.CallbackQueryID != "cb-123" {
			t.Errorf("expected callback_query_id 'cb-123', got '%s'", payload.CallbackQueryID)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"text":"Receiver A Approved!","show_alert":true}`))
	}))
	defer receiverServer.Close()

	// 2. Start mock Telegram server
	telegramAnswered := false
	telegramServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/botmock-token/getMe" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true,"result":{"id":123456,"is_bot":true,"first_name":"TestBot","username":"test_bot"}}`))
			return
		}

		if r.URL.Path == "/botmock-token/answerCallbackQuery" {
			telegramAnswered = true
			err := r.ParseForm()
			if err != nil {
				t.Errorf("failed to parse form: %v", err)
			}
			if r.FormValue("callback_query_id") != "cb-123" {
				t.Errorf("expected callback_query_id 'cb-123', got '%s'", r.FormValue("callback_query_id"))
			}
			if r.FormValue("text") != "Receiver A Approved!" {
				t.Errorf("expected text 'Receiver A Approved!', got '%s'", r.FormValue("text"))
			}
			if r.FormValue("show_alert") != "true" {
				t.Errorf("expected show_alert 'true', got '%s'", r.FormValue("show_alert"))
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true,"result":true}`))
			return
		}
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
		Routes: map[string]string{
			"receiver-a": receiverServer.URL + "/callback",
			"receiver-b": "http://localhost:9999/callback",
		},
	}
	gw := &Gateway{
		Bot:    bot,
		Config: cfg,
		Client: http.DefaultClient,
	}

	// 3. Simulate receiving an update
	update := tgbotapi.Update{
		UpdateID: 1,
		CallbackQuery: &tgbotapi.CallbackQuery{
			ID:   "cb-123",
			Data: "receiver-a:approve:ev1",
			From: &tgbotapi.User{
				ID:       555,
				UserName: "user555",
			},
			Message: &tgbotapi.Message{
				MessageID: 777,
				Chat: &tgbotapi.Chat{
					ID: 888,
				},
			},
		},
	}

	gw.HandleUpdate(update)

	if !receiverCalled {
		t.Error("expected receiver callback to be called")
	}
	if !telegramAnswered {
		t.Error("expected answerCallbackQuery to be called")
	}
}

func TestCallbackQueryRoutingTimeout(t *testing.T) {
	// 1. Mock a slow receiver server
	receiverServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep for 6 seconds to trigger the 5-second gateway timeout
		select {
		case <-r.Context().Done():
			// Request cancelled by client, which is expected
		case <-time.After(6 * time.Second):
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer receiverServer.Close()

	// 2. Start mock Telegram server
	telegramAnswered := false
	telegramServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/botmock-token/getMe" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true,"result":{"id":123456,"is_bot":true,"first_name":"TestBot","username":"test_bot"}}`))
			return
		}

		if r.URL.Path == "/botmock-token/answerCallbackQuery" {
			telegramAnswered = true
			err := r.ParseForm()
			if err != nil {
				t.Errorf("failed to parse form: %v", err)
			}
			if r.FormValue("text") != "Receiver backend unreachable" {
				t.Errorf("expected warning text, got '%s'", r.FormValue("text"))
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true,"result":true}`))
			return
		}
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
		Routes: map[string]string{
			"receiver-a": receiverServer.URL + "/callback",
		},
	}
	gw := &Gateway{
		Bot:    bot,
		Config: cfg,
		Client: http.DefaultClient,
	}

	update := tgbotapi.Update{
		UpdateID: 1,
		CallbackQuery: &tgbotapi.CallbackQuery{
			ID:   "cb-timeout",
			Data: "receiver-a:slow",
			From: &tgbotapi.User{
				ID: 555,
			},
			Message: &tgbotapi.Message{
				MessageID: 777,
				Chat: &tgbotapi.Chat{
					ID: 888,
				},
			},
		},
	}

	gw.HandleUpdate(update)

	if !telegramAnswered {
		t.Error("expected answerCallbackQuery to be called on timeout")
	}
}

func TestCallbackQueryRoutingReceiverBSuccess(t *testing.T) {
	receiverCalled := false
	receiverServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receiverCalled = true
		var payload CallbackPayload
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload.Data != "receiver-b:decline:ev2" {
			t.Errorf("expected data 'receiver-b:decline:ev2', got '%s'", payload.Data)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"text":"Receiver B Declined!","show_alert":false}`))
	}))
	defer receiverServer.Close()

	telegramAnswered := false
	telegramServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/botmock-token/getMe" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true,"result":{"id":123456,"is_bot":true,"first_name":"TestBot","username":"test_bot"}}`))
			return
		}
		if r.URL.Path == "/botmock-token/answerCallbackQuery" {
			telegramAnswered = true
			if r.FormValue("text") != "Receiver B Declined!" {
				t.Errorf("expected 'Receiver B Declined!', got '%s'", r.FormValue("text"))
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true,"result":true}`))
			return
		}
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
		Routes: map[string]string{
			"receiver-b": receiverServer.URL + "/callback",
		},
	}
	gw := &Gateway{
		Bot:    bot,
		Config: cfg,
		Client: http.DefaultClient,
	}

	update := tgbotapi.Update{
		UpdateID: 1,
		CallbackQuery: &tgbotapi.CallbackQuery{
			ID:   "cb-book-123",
			Data: "receiver-b:decline:ev2",
			From: &tgbotapi.User{
				ID: 555,
			},
			Message: &tgbotapi.Message{
				MessageID: 777,
				Chat: &tgbotapi.Chat{
					ID: 888,
				},
			},
		},
	}

	gw.HandleUpdate(update)

	if !receiverCalled {
		t.Error("expected receiver callback to be called")
	}
	if !telegramAnswered {
		t.Error("expected answerCallbackQuery to be called")
	}
}

func TestCallbackQueryRoutingDown(t *testing.T) {
	telegramAnswered := false
	telegramServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/botmock-token/getMe" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true,"result":{"id":123456,"is_bot":true,"first_name":"TestBot","username":"test_bot"}}`))
			return
		}
		if r.URL.Path == "/botmock-token/answerCallbackQuery" {
			telegramAnswered = true
			if r.FormValue("text") != "Receiver backend unreachable" {
				t.Errorf("expected warning, got '%s'", r.FormValue("text"))
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true,"result":true}`))
			return
		}
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
		Routes: map[string]string{
			"receiver-a": "http://localhost:12345/nonexistent-callback",
		},
	}
	gw := &Gateway{
		Bot:    bot,
		Config: cfg,
		Client: http.DefaultClient,
	}

	update := tgbotapi.Update{
		UpdateID: 1,
		CallbackQuery: &tgbotapi.CallbackQuery{
			ID:   "cb-down-123",
			Data: "receiver-a:approve:ev1",
			From: &tgbotapi.User{
				ID: 555,
			},
			Message: &tgbotapi.Message{
				MessageID: 777,
				Chat: &tgbotapi.Chat{
					ID: 888,
				},
			},
		},
	}

	gw.HandleUpdate(update)

	if !telegramAnswered {
		t.Error("expected answerCallbackQuery to be called")
	}
}

func TestCallbackQueryRoutingUnauthorizedChat(t *testing.T) {
	telegramAnswered := false
	telegramServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/botmock-token/getMe" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true,"result":{"id":123456,"is_bot":true,"first_name":"TestBot","username":"test_bot"}}`))
			return
		}
		if r.URL.Path == "/botmock-token/answerCallbackQuery" {
			telegramAnswered = true
			if r.FormValue("text") != "Unauthorized chat source" {
				t.Errorf("expected 'Unauthorized chat source', got '%s'", r.FormValue("text"))
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true,"result":true}`))
			return
		}
	}))
	defer telegramServer.Close()

	botURL := telegramServer.URL + "/bot%s/%s"
	bot, err := tgbotapi.NewBotAPIWithClient("mock-token", botURL, http.DefaultClient)
	if err != nil {
		t.Fatalf("failed to create BotAPI: %v", err)
	}

	cfg := &Config{
		TelegramBotToken: "mock-token",
		TelegramChatID:   11111, // Configured expected chat ID
		Port:             "8000",
		Routes: map[string]string{
			"receiver-a": "http://localhost:12345/callback",
		},
	}
	gw := &Gateway{
		Bot:    bot,
		Config: cfg,
		Client: http.DefaultClient,
	}

	update := tgbotapi.Update{
		UpdateID: 1,
		CallbackQuery: &tgbotapi.CallbackQuery{
			ID:   "cb-unauth-chat",
			Data: "receiver-a:approve:ev1",
			From: &tgbotapi.User{
				ID: 555,
			},
			Message: &tgbotapi.Message{
				MessageID: 777,
				Chat: &tgbotapi.Chat{
					ID: 99999, // Mismatched chat ID (not 11111)
				},
			},
		},
	}

	gw.HandleUpdate(update)

	if !telegramAnswered {
		t.Error("expected answerCallbackQuery to be called")
	}
}

func TestHealthEndpoint(t *testing.T) {
	gw := &Gateway{}
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	handler := http.HandlerFunc(gw.HandleHealth)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	expected := `{"status":"ok"}`
	if strings.TrimSpace(rr.Body.String()) != expected {
		t.Errorf("expected body '%s', got '%s'", expected, rr.Body.String())
	}
}

// ---------- requireAuth middleware tests ----------

func TestRequireAuth_NoKeyConfigured_PassesThrough(t *testing.T) {
	// When GatewayAPIKey is empty the middleware must let requests through.
	gw := &Gateway{Config: &Config{}}
	called := false
	handler := gw.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/send", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if !called {
		t.Error("expected inner handler to be called")
	}
}

func TestRequireAuth_CorrectToken_PassesThrough(t *testing.T) {
	gw := &Gateway{Config: &Config{GatewayAPIKey: "secret123"}}
	called := false
	handler := gw.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/send", nil)
	req.Header.Set("Authorization", "Bearer secret123")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if !called {
		t.Error("expected inner handler to be called")
	}
}

func TestRequireAuth_WrongToken_Returns401(t *testing.T) {
	gw := &Gateway{Config: &Config{GatewayAPIKey: "secret123"}}
	handler := gw.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler should not be called on auth failure")
	})

	for _, tc := range []struct {
		name   string
		header string
	}{
		{"wrong token", "Bearer wrongtoken"},
		{"missing header", ""},
		{"no bearer prefix", "secret123"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/send", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d. Body: %s", rr.Code, rr.Body.String())
			}
		})
	}
}

func TestCallbackQuerySigning(t *testing.T) {
	secret := "test-webhook-secret"
	var signatureReceived string
	var bodyReceived []byte

	receiverServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		signatureReceived = r.Header.Get("X-Gateway-Signature")
		var err error
		bodyReceived, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer receiverServer.Close()

	telegramServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/botmock-token/getMe" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true,"result":{"id":123456,"is_bot":true,"first_name":"TestBot","username":"test_bot"}}`))
			return
		}
		if r.URL.Path == "/botmock-token/answerCallbackQuery" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true,"result":true}`))
			return
		}
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
		WebhookSecret:    secret,
		Routes: map[string]string{
			"receiver-a": receiverServer.URL + "/callback",
		},
	}
	gw := &Gateway{
		Bot:    bot,
		Config: cfg,
		Client: http.DefaultClient,
	}

	update := tgbotapi.Update{
		UpdateID: 1,
		CallbackQuery: &tgbotapi.CallbackQuery{
			ID:   "cb-signing",
			Data: "receiver-a:action",
			From: &tgbotapi.User{
				ID: 555,
			},
		},
	}

	gw.HandleUpdate(update)

	if signatureReceived == "" {
		t.Fatal("expected X-Gateway-Signature header, got empty")
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(bodyReceived)
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if signatureReceived != expectedSig {
		t.Errorf("expected signature '%s', got '%s'", expectedSig, signatureReceived)
	}
}

func TestParseTelegramRetryAfter(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantSecs int
	}{
		{
			name:     "standard rate limit message",
			err:      fmt.Errorf("Too Many Requests: retry after 5"),
			wantSecs: 5,
		},
		{
			name:     "rate limit with longer wait",
			err:      fmt.Errorf("Too Many Requests: retry after 30"),
			wantSecs: 30,
		},
		{
			name:     "bad gateway — not a rate limit",
			err:      fmt.Errorf("Bad Gateway"),
			wantSecs: 0,
		},
		{
			name:     "gateway timeout — not a rate limit",
			err:      fmt.Errorf("Gateway Timeout"),
			wantSecs: 0,
		},
		{
			name:     "nil error",
			err:      nil,
			wantSecs: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseTelegramRetryAfter(tc.err)
			want := time.Duration(tc.wantSecs) * time.Second
			if got != want {
				t.Errorf("parseTelegramRetryAfter(%v) = %v, want %v", tc.err, got, want)
			}
		})
	}
}

func TestReadyEndpoint(t *testing.T) {
	gw := &Gateway{}

	t.Run("uninitialized polling returns 503", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		rr := httptest.NewRecorder()
		gw.HandleReady(rr, req)

		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("expected status 503, got %d", rr.Code)
		}
	})

	t.Run("healthy polling returns 200", func(t *testing.T) {
		gw.lastPollSuccess.Store(time.Now().Unix())
		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		rr := httptest.NewRecorder()
		gw.HandleReady(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rr.Code)
		}
	})

	t.Run("stalled polling returns 503", func(t *testing.T) {
		threeMinutesAgo := time.Now().Add(-3 * time.Minute).Unix()
		gw.lastPollSuccess.Store(threeMinutesAgo)
		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		rr := httptest.NewRecorder()
		gw.HandleReady(rr, req)

		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("expected status 503, got %d", rr.Code)
		}
	})

	t.Run("invalid method returns 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/ready", nil)
		rr := httptest.NewRecorder()
		gw.HandleReady(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected status 405, got %d", rr.Code)
		}
	})
}

func TestCorrelationID(t *testing.T) {
	t.Run("HandleSend preserves existing X-Correlation-ID", func(t *testing.T) {
		telegramServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/botmock-token/getMe" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"ok":true,"result":{"id":123456,"is_bot":true,"first_name":"TestBot","username":"test_bot"}}`))
				return
			}
			if r.URL.Path == "/botmock-token/sendMessage" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"ok":true,"result":{"message_id":42,"chat":{"id":100}}}`))
				return
			}
		}))
		defer telegramServer.Close()

		botURL := telegramServer.URL + "/bot%s/%s"
		bot, err := tgbotapi.NewBotAPIWithClient("mock-token", botURL, http.DefaultClient)
		if err != nil {
			t.Fatalf("failed to create bot: %v", err)
		}

		gw := &Gateway{Bot: bot, Config: &Config{}}
		reqBody := `{"chat_id":100, "text":"hello"}`
		req := httptest.NewRequest(http.MethodPost, "/send", strings.NewReader(reqBody))
		req.Header.Set("X-Correlation-ID", "my-custom-cid")
		rr := httptest.NewRecorder()

		gw.HandleSend(rr, req)

		if got := rr.Header().Get("X-Correlation-ID"); got != "my-custom-cid" {
			t.Errorf("expected X-Correlation-ID 'my-custom-cid', got '%s'", got)
		}
	})

	t.Run("HandleSend generates X-Correlation-ID if missing", func(t *testing.T) {
		telegramServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/botmock-token/getMe" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"ok":true,"result":{"id":123456,"is_bot":true,"first_name":"TestBot","username":"test_bot"}}`))
				return
			}
			if r.URL.Path == "/botmock-token/sendMessage" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"ok":true,"result":{"message_id":42,"chat":{"id":100}}}`))
				return
			}
		}))
		defer telegramServer.Close()

		botURL := telegramServer.URL + "/bot%s/%s"
		bot, err := tgbotapi.NewBotAPIWithClient("mock-token", botURL, http.DefaultClient)
		if err != nil {
			t.Fatalf("failed to create bot: %v", err)
		}

		gw := &Gateway{Bot: bot, Config: &Config{}}
		reqBody := `{"chat_id":100, "text":"hello"}`
		req := httptest.NewRequest(http.MethodPost, "/send", strings.NewReader(reqBody))
		rr := httptest.NewRecorder()

		gw.HandleSend(rr, req)

		got := rr.Header().Get("X-Correlation-ID")
		if !strings.HasPrefix(got, "send-") {
			t.Errorf("expected generated X-Correlation-ID to start with 'send-', got '%s'", got)
		}
	})

	t.Run("forwardCallbackToReceiver forwards X-Correlation-ID", func(t *testing.T) {
		var receivedCorrelationID string
		receiverServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedCorrelationID = r.Header.Get("X-Correlation-ID")
			w.WriteHeader(http.StatusOK)
		}))
		defer receiverServer.Close()

		telegramServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/botmock-token/getMe" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"ok":true,"result":{"id":123456,"is_bot":true,"first_name":"TestBot","username":"test_bot"}}`))
				return
			}
			if r.URL.Path == "/botmock-token/answerCallbackQuery" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"ok":true,"result":true}`))
				return
			}
		}))
		defer telegramServer.Close()

		botURL := telegramServer.URL + "/bot%s/%s"
		bot, _ := tgbotapi.NewBotAPIWithClient("mock-token", botURL, http.DefaultClient)
		gw := &Gateway{
			Bot: bot,
			Config: &Config{
				Routes: map[string]string{
					"rec": receiverServer.URL + "/callback",
				},
			},
			Client: http.DefaultClient,
		}

		update := tgbotapi.Update{
			UpdateID: 1,
			CallbackQuery: &tgbotapi.CallbackQuery{
				ID:   "cb-123",
				Data: "rec:test",
				From: &tgbotapi.User{ID: 1},
			},
		}

		gw.HandleUpdate(update)

		if !strings.HasPrefix(receivedCorrelationID, "cb-") {
			t.Errorf("expected forwarded X-Correlation-ID to start with 'cb-', got '%s'", receivedCorrelationID)
		}
	})
}

func TestSendEndpointRateLimitPropagation(t *testing.T) {
	telegramServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/botmock-token/getMe" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true,"result":{"id":123456,"is_bot":true,"first_name":"TestBot","username":"test_bot"}}`))
			return
		}

		if r.URL.Path == "/botmock-token/sendMessage" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 5"}`))
			return
		}
	}))
	defer telegramServer.Close()

	botURL := telegramServer.URL + "/bot%s/%s"
	bot, err := tgbotapi.NewBotAPIWithClient("mock-token", botURL, http.DefaultClient)
	if err != nil {
		t.Fatalf("failed to create BotAPI: %v", err)
	}

	gw := &Gateway{
		Bot:    bot,
		Config: &Config{},
	}

	reqBody := `{"chat_id":123456789, "text":"test message"}`
	req := httptest.NewRequest(http.MethodPost, "/send", strings.NewReader(reqBody))
	rr := httptest.NewRecorder()

	gw.HandleSend(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected status 429, got %d", rr.Code)
	}

	if retryAfter := rr.Header().Get("Retry-After"); retryAfter != "5" {
		t.Errorf("expected Retry-After header '5', got '%s'", retryAfter)
	}
}
