package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"golang.org/x/time/rate"
)

// Gateway holds the state for the HTTP server and Telegram bot client
type Gateway struct {
	Bot    *tgbotapi.BotAPI
	Config *Config
	Client *http.Client
	WG     sync.WaitGroup

	// Rate Limiting
	limiterOnce   sync.Once
	globalLimiter *rate.Limiter
	chatLimiters  map[int64]*rate.Limiter
	chatMu        sync.RWMutex
}

// initRateLimiters initializes the global and per-chat rate limiters in a thread-safe lazy manner
func (gw *Gateway) initRateLimiters() {
	gw.limiterOnce.Do(func() {
		globalLimit := 30.0
		if gw.Config != nil {
			if gw.Config.RateLimits.GlobalPerSecond > 0 {
				globalLimit = gw.Config.RateLimits.GlobalPerSecond
			}
		}
		gw.globalLimiter = rate.NewLimiter(rate.Limit(globalLimit), int(globalLimit))
		gw.chatLimiters = make(map[int64]*rate.Limiter)
	})
}

func (gw *Gateway) getChatLimiter(chatID int64) *rate.Limiter {
	gw.initRateLimiters()
	gw.chatMu.RLock()
	lim, exists := gw.chatLimiters[chatID]
	gw.chatMu.RUnlock()
	if exists {
		return lim
	}

	gw.chatMu.Lock()
	defer gw.chatMu.Unlock()
	// Double check
	if lim, exists = gw.chatLimiters[chatID]; exists {
		return lim
	}

	chatLimit := 1.0
	if gw.Config != nil && gw.Config.RateLimits.ChatPerSecond > 0 {
		chatLimit = gw.Config.RateLimits.ChatPerSecond
	}
	lim = rate.NewLimiter(rate.Limit(chatLimit), 3)
	gw.chatLimiters[chatID] = lim
	return lim
}

// SendRequest represents the payload for POST /send
type SendRequest struct {
	ChatID                int64                          `json:"chat_id"`
	Text                  string                         `json:"text"`
	ReplyMarkup           *tgbotapi.InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	ParseMode             string                         `json:"parse_mode,omitempty"`
	DisableWebPagePreview bool                           `json:"disable_web_page_preview,omitempty"`
	DisableNotification   bool                           `json:"disable_notification,omitempty"`
}

// SendResponse represents the response back to strategy client on successful send
type SendResponse struct {
	MessageID int   `json:"message_id"`
	ChatID    int64 `json:"chat_id"`
}

// ErrorResponse represents the error response body
type ErrorResponse struct {
	Error string `json:"error"`
}

// CallbackPayload represents the simplified JSON payload forwarded to the strategy service
type CallbackPayload struct {
	CallbackQueryID string `json:"callback_query_id"`
	FromID          int64  `json:"from_id"`
	Username        string `json:"username"`
	ChatID          int64  `json:"chat_id"`
	MessageID       int    `json:"message_id"`
	Data            string `json:"data"`
}

// StrategyResponse represents the optional JSON response from the strategy service
type StrategyResponse struct {
	Text      string `json:"text,omitempty"`
	ShowAlert bool   `json:"show_alert,omitempty"`
}

// requireAuth is middleware that validates the Bearer token in the Authorization header.
// If GatewayAPIKey is empty the gateway is running in unauthenticated mode (warned at startup).
func (gw *Gateway) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if gw.Config.GatewayAPIKey == "" {
			// Auth not configured — allow through (operator was warned at startup)
			next(w, r)
			return
		}
		authorization := r.Header.Get("Authorization")
		expected := "Bearer " + gw.Config.GatewayAPIKey
		if authorization != expected {
			gw.writeError(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		next(w, r)
	}
}

// HandleSend handles POST /send requests and routes them to the Telegram Bot API
func (gw *Gateway) HandleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		gw.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req SendRequest
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil {
		gw.writeError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	if req.ChatID == 0 {
		if gw.Config.TelegramChatID != 0 {
			req.ChatID = gw.Config.TelegramChatID
		} else {
			gw.writeError(w, http.StatusBadRequest, "chat_id is required")
			return
		}
	}
	if req.Text == "" {
		gw.writeError(w, http.StatusBadRequest, "text is required")
		return
	}

	// Enforce global rate limit
	ctx := r.Context()
	gw.initRateLimiters()
	if err := gw.globalLimiter.Wait(ctx); err != nil {
		slog.Error("Global rate limiter wait error", "error", err)
		gw.writeError(w, http.StatusTooManyRequests, "Rate limit wait timeout")
		return
	}

	// Enforce per-chat rate limit
	chatLimiter := gw.getChatLimiter(req.ChatID)
	if err := chatLimiter.Wait(ctx); err != nil {
		slog.Error("Chat rate limiter wait error", "error", err, "chat_id", req.ChatID)
		gw.writeError(w, http.StatusTooManyRequests, "Rate limit wait timeout")
		return
	}

	msg := tgbotapi.NewMessage(req.ChatID, req.Text)

	// Default parse mode to Markdown unless overridden
	if req.ParseMode != "" {
		msg.ParseMode = req.ParseMode
	} else {
		msg.ParseMode = "Markdown"
	}

	if req.ReplyMarkup != nil {
		msg.ReplyMarkup = req.ReplyMarkup
	}

	msg.DisableWebPagePreview = req.DisableWebPagePreview
	msg.DisableNotification = req.DisableNotification

	sentMsg, err := gw.Bot.Send(msg)
	if err != nil {
		slog.Error("Error sending message via Telegram", "error", err, "chat_id", req.ChatID)
		gw.writeError(w, http.StatusInternalServerError, "Failed to send message")
		return
	}

	resp := SendResponse{
		MessageID: sentMsg.MessageID,
		ChatID:    sentMsg.Chat.ID,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
	metricSendRequests.WithLabelValues("200").Inc()
}

// StartUpdateLoop starts the long-polling loop to get updates from Telegram
func (gw *Gateway) StartUpdateLoop(ctx context.Context) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30 // seconds

	slog.Info("Starting Telegram updates polling loop...")
	backoffAttempt := 0
	for {
		select {
		case <-ctx.Done():
			slog.Info("Stopping Telegram updates polling loop...")
			return
		default:
			updates, err := gw.Bot.GetUpdates(u)
			if err != nil {
				// If the loop was canceled during updates fetch, exit cleanly
				select {
				case <-ctx.Done():
					return
				default:
				}
				// Exponential backoff: 1s, 2s, 4s … capped at 30s, with ±25% jitter
				capSec := 30
				backoffSec := 1 << backoffAttempt
				if backoffSec > capSec {
					backoffSec = capSec
				}
				backoffBase := time.Duration(backoffSec) * time.Second
				jitter := time.Duration(rand.Int63n(int64(backoffBase) / 4)) //nolint:gosec
				sleep := backoffBase + jitter
				slog.Error("Error getting updates from Telegram", "error", err, "retry_in", sleep)
				backoffAttempt++
				select {
				case <-time.After(sleep):
				case <-ctx.Done():
					return
				}
				continue
			}
			// Successful poll — reset backoff
			backoffAttempt = 0

			for _, update := range updates {
				// Record metric for incoming update
				if update.CallbackQuery != nil {
					metricIncomingUpdates.WithLabelValues("callback").Inc()
				} else {
					metricIncomingUpdates.WithLabelValues("other").Inc()
				}

				// Dispatch each update in its own goroutine so a slow or hanging
				// strategy backend cannot stall the polling loop.
				update := update // capture loop variable
				gw.WG.Add(1)
				go func(up tgbotapi.Update) {
					defer gw.WG.Done()
					gw.HandleUpdate(up)
				}(update)

				if update.UpdateID >= u.Offset {
					u.Offset = update.UpdateID + 1
				}
			}
		}
	}
}

// HandleUpdate processes a single incoming update from Telegram
func (gw *Gateway) HandleUpdate(update tgbotapi.Update) {
	if update.CallbackQuery == nil {
		slog.Debug("Received non-callback update", "update_id", update.UpdateID)
		return
	}

	cb := update.CallbackQuery
	data := cb.Data

	var targetURL string
	var matchedPrefix string
	for prefix, url := range gw.Config.Routes {
		matchPrefix := prefix
		if !strings.HasSuffix(matchPrefix, ":") {
			matchPrefix = matchPrefix + ":"
		}
		if strings.HasPrefix(data, matchPrefix) {
			targetURL = url
			matchedPrefix = prefix
			break
		}
	}

	if targetURL == "" {
		slog.Warn("Received callback query with unknown prefix", "data", data, "callback_id", cb.ID)
		gw.answerCallback(cb.ID, "Unknown callback query prefix", true)
		metricCallbackForward.WithLabelValues("unknown", "unknown_prefix").Inc()
		return
	}

	// Prepare simplified payload
	payload := CallbackPayload{
		CallbackQueryID: cb.ID,
		FromID:          cb.From.ID,
		Username:        cb.From.UserName,
		Data:            cb.Data,
	}
	if cb.Message != nil {
		payload.ChatID = cb.Message.Chat.ID
		payload.MessageID = cb.Message.MessageID
	}

	// Forward payload to the strategy backend via POST with 5s timeout
	err := gw.forwardCallbackToStrategy(matchedPrefix, targetURL, payload)
	if err != nil {
		slog.Error("Error forwarding callback to strategy", "prefix", matchedPrefix, "target_url", targetURL, "error", err)
		gw.answerCallback(cb.ID, "Strategy backend unreachable", true)
	}
}

// forwardCallbackToStrategy POSTs payload to strategy and answers the Telegram callback query accordingly
func (gw *Gateway) forwardCallbackToStrategy(prefix string, targetURL string, payload CallbackPayload) error {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		metricCallbackForward.WithLabelValues(prefix, "marshal_error").Inc()
		return fmt.Errorf("failed to marshal callback payload: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		metricCallbackForward.WithLabelValues(prefix, "request_create_error").Inc()
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// If webhook secret is configured, sign the request body with HMAC-SHA256
	if gw.Config.WebhookSecret != "" {
		mac := hmac.New(sha256.New, []byte(gw.Config.WebhookSecret))
		mac.Write(bodyBytes)
		signature := hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-Gateway-Signature", signature)
	}

	startTime := time.Now()
	resp, err := gw.Client.Do(req)
	duration := time.Since(startTime).Seconds()
	metricCallbackLatency.WithLabelValues(prefix).Observe(duration)

	if err != nil {
		metricCallbackForward.WithLabelValues(prefix, "transport_error").Inc()
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		metricCallbackForward.WithLabelValues(prefix, "status_"+strconv.Itoa(resp.StatusCode)).Inc()
		return fmt.Errorf("strategy returned status code %d", resp.StatusCode)
	}

	// Read optional response JSON
	var stratResp StrategyResponse
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&stratResp); err != nil {
		// Response is empty or not JSON, which is acceptable. Acknowledge with empty string.
		gw.answerCallback(payload.CallbackQueryID, "", false)
		metricCallbackForward.WithLabelValues(prefix, "success_empty").Inc()
		return nil
	}

	gw.answerCallback(payload.CallbackQueryID, stratResp.Text, stratResp.ShowAlert)
	metricCallbackForward.WithLabelValues(prefix, "success_response").Inc()
	return nil
}

// answerCallback calls Telegram's answerCallbackQuery method to acknowledge the callback
func (gw *Gateway) answerCallback(callbackQueryID string, text string, showAlert bool) {
	callbackConfig := tgbotapi.NewCallback(callbackQueryID, text)
	callbackConfig.ShowAlert = showAlert

	_, err := gw.Bot.Request(callbackConfig)
	if err != nil {
		slog.Error("Error answering callback query", "callback_query_id", callbackQueryID, "error", err)
	}
}

func (gw *Gateway) writeError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(ErrorResponse{Error: message})
	metricSendRequests.WithLabelValues(strconv.Itoa(statusCode)).Inc()
}

// HandleHealth handles GET /health requests
func (gw *Gateway) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		gw.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}
