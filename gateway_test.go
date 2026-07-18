package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
		"parse_mode":                 "Markdown",
		"disable_web_page_preview":   true,
		"disable_notification":       true,
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

func TestCallbackQueryRoutingComboSuccess(t *testing.T) {
	// 1. Start a mock strategy server for COMB_ARB
	strategyCalled := false
	strategyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		strategyCalled = true
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

		if payload.Data != "combo:approve:ev1" {
			t.Errorf("expected data 'combo:approve:ev1', got '%s'", payload.Data)
		}
		if payload.CallbackQueryID != "cb-123" {
			t.Errorf("expected callback_query_id 'cb-123', got '%s'", payload.CallbackQueryID)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"text":"Combo Approved!","show_alert":true}`))
	}))
	defer strategyServer.Close()

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
			if r.FormValue("text") != "Combo Approved!" {
				t.Errorf("expected text 'Combo Approved!', got '%s'", r.FormValue("text"))
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
			"combo": strategyServer.URL + "/callback",
			"book":  "http://localhost:9999/callback",
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
			Data: "combo:approve:ev1",
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

	if !strategyCalled {
		t.Error("expected strategy callback to be called")
	}
	if !telegramAnswered {
		t.Error("expected answerCallbackQuery to be called")
	}
}

func TestCallbackQueryRoutingTimeout(t *testing.T) {
	// 1. Mock a slow strategy server
	strategyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep for 6 seconds to trigger the 5-second gateway timeout
		select {
		case <-r.Context().Done():
			// Request cancelled by client, which is expected
		case <-time.After(6 * time.Second):
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer strategyServer.Close()

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
			if r.FormValue("text") != "Strategy backend unreachable" {
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
			"combo": strategyServer.URL + "/callback",
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
			Data: "combo:slow",
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

func TestCallbackQueryRoutingBookSuccess(t *testing.T) {
	strategyCalled := false
	strategyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		strategyCalled = true
		var payload CallbackPayload
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload.Data != "book:decline:ev2" {
			t.Errorf("expected data 'book:decline:ev2', got '%s'", payload.Data)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"text":"Book Declined!","show_alert":false}`))
	}))
	defer strategyServer.Close()

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
			if r.FormValue("text") != "Book Declined!" {
				t.Errorf("expected 'Book Declined!', got '%s'", r.FormValue("text"))
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
			"book": strategyServer.URL + "/callback",
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
			Data: "book:decline:ev2",
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

	if !strategyCalled {
		t.Error("expected strategy callback to be called")
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
			if r.FormValue("text") != "Strategy backend unreachable" {
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
			"combo": "http://localhost:12345/nonexistent-callback",
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
			Data: "combo:approve:ev1",
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

	strategyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		signatureReceived = r.Header.Get("X-Gateway-Signature")
		var err error
		bodyReceived, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer strategyServer.Close()

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
			"combo": strategyServer.URL + "/callback",
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
			Data: "combo:action",
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
