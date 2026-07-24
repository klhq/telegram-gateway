package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/time/rate"
)

// DefaultHTTPTransport returns a tuned http.Transport for internal receiver services.
func DefaultHTTPTransport() *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   25,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// TelegramHTTPTransport returns a tuned http.Transport for Telegram Bot API communication.
func TelegramHTTPTransport() *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:        50,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     120 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
}

const (
	HeaderCorrelationID       = "X-Correlation-ID"
	HeaderRequestID           = "X-Request-ID"
	HeaderGatewaySignature    = "X-Gateway-Signature"
	HeaderTelegramSecretToken = "X-Telegram-Bot-Api-Secret-Token"
)

// Gateway holds the state for the HTTP server and Telegram bot client
type Gateway struct {
	Bot    *tgbotapi.BotAPI
	Config *Config
	Client *http.Client
	WG     sync.WaitGroup

	// Health / Readiness
	lastPollSuccess atomic.Int64

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

// SendResponse represents the response back to the client on successful send
type SendResponse struct {
	MessageID int   `json:"message_id"`
	ChatID    int64 `json:"chat_id"`
}

// ErrorResponse represents the error response body
type ErrorResponse struct {
	Error string `json:"error"`
}

// CallbackPayload represents the simplified JSON payload forwarded to the downstream receiver service
type CallbackPayload struct {
	CallbackQueryID string `json:"callback_query_id"`
	FromID          int64  `json:"from_id"`
	Username        string `json:"username"`
	ChatID          int64  `json:"chat_id"`
	MessageID       int    `json:"message_id"`
	Data            string `json:"data"`
}

// ReceiverResponse represents the optional JSON response from the receiver service
type ReceiverResponse struct {
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

	correlationID := r.Header.Get(HeaderCorrelationID)
	if correlationID == "" {
		correlationID = r.Header.Get(HeaderRequestID)
	}
	if correlationID == "" {
		correlationID = generateCorrelationID("send")
	}
	w.Header().Set(HeaderCorrelationID, correlationID)

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
	if req.ParseMode != "" && req.ParseMode != "Markdown" && req.ParseMode != "MarkdownV2" && req.ParseMode != "HTML" {
		gw.writeError(w, http.StatusBadRequest, "unsupported parse_mode")
		return
	}

	// Enforce global rate limit
	ctx := r.Context()
	gw.initRateLimiters()
	if err := gw.globalLimiter.Wait(ctx); err != nil {
		slog.Error("Global rate limiter wait error", "error", err, "correlation_id", correlationID)
		w.Header().Set("Retry-After", "1")
		gw.writeError(w, http.StatusTooManyRequests, "Rate limit wait timeout")
		return
	}

	// Enforce per-chat rate limit
	chatLimiter := gw.getChatLimiter(req.ChatID)
	if err := chatLimiter.Wait(ctx); err != nil {
		slog.Error("Chat rate limiter wait error", "error", err, "chat_id", req.ChatID, "correlation_id", correlationID)
		w.Header().Set("Retry-After", "1")
		gw.writeError(w, http.StatusTooManyRequests, "Rate limit wait timeout")
		return
	}

	msg := tgbotapi.NewMessage(req.ChatID, req.Text)

	if req.ParseMode != "" {
		msg.ParseMode = req.ParseMode
	}

	if req.ReplyMarkup != nil {
		msg.ReplyMarkup = req.ReplyMarkup
	}

	msg.DisableWebPagePreview = req.DisableWebPagePreview
	msg.DisableNotification = req.DisableNotification

	sentMsg, err := gw.Bot.Send(msg)
	if err != nil {
		slog.Error("Error sending message via Telegram", "error", err, "chat_id", req.ChatID, "correlation_id", correlationID)
		if retryAfter := parseTelegramRetryAfter(err); retryAfter > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
			gw.writeError(w, http.StatusTooManyRequests, err.Error())
			return
		}
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
				// Honor Telegram's "Too Many Requests: retry after N" directive.
				// If present, use that as the floor; otherwise exponential backoff
				// capped at 30s with ±25% jitter.
				var sleep time.Duration
				if retryAfter := parseTelegramRetryAfter(err); retryAfter > 0 {
					// Add a small buffer on top of Telegram's required wait
					sleep = retryAfter + time.Second
					// Don't increment backoffAttempt — this wasn't a transient failure
				} else {
					// Exponential backoff: 1s, 2s, 4s … capped at 30s, with ±25% jitter
					capSec := 30
					backoffSec := 1 << backoffAttempt
					if backoffSec > capSec {
						backoffSec = capSec
					}
					backoffBase := time.Duration(backoffSec) * time.Second
					jitter := time.Duration(rand.Int63n(int64(backoffBase) / 4)) //nolint:gosec
					sleep = backoffBase + jitter
					backoffAttempt++
				}
				slog.Error("Error getting updates from Telegram", "error", err, "retry_in", sleep)
				select {
				case <-time.After(sleep):
				case <-ctx.Done():
					return
				}
				continue
			}
			// Successful poll — reset backoff and update health timestamp
			backoffAttempt = 0
			gw.lastPollSuccess.Store(time.Now().Unix())

			for _, update := range updates {
				// Record metric for incoming update
				if update.CallbackQuery != nil {
					metricIncomingUpdates.WithLabelValues("callback").Inc()
				} else {
					metricIncomingUpdates.WithLabelValues("other").Inc()
				}

				// Dispatch each update in its own goroutine so a slow or hanging
				// downstream backend cannot stall the polling loop.
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
	correlationID := generateCorrelationID("cb")

	// If telegram_chat_id is configured (non-zero), enforce that the callback query
	// originates from this chat to prevent unauthorized group/private chat callback triggers.
	if gw.Config.TelegramChatID != 0 {
		if cb.Message == nil || cb.Message.Chat.ID != gw.Config.TelegramChatID {
			slog.Warn("Rejected callback query: originating chat ID does not match configured telegram_chat_id",
				"expected", gw.Config.TelegramChatID,
				"received", func() int64 {
					if cb.Message != nil {
						return cb.Message.Chat.ID
					}
					return 0
				}(),
				"callback_id", cb.ID,
				"correlation_id", correlationID,
			)
			gw.answerCallback(cb.ID, "Unauthorized chat source", true)
			metricCallbackForward.WithLabelValues("unauthorized", "unauthorized_chat").Inc()
			return
		}
	}

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
		slog.Warn("Received callback query with unknown prefix", "data", data, "callback_id", cb.ID, "correlation_id", correlationID)
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

	// Forward payload to the receiver backend via POST with 5s timeout
	err := gw.forwardCallbackToReceiver(matchedPrefix, targetURL, payload, correlationID)
	if err != nil {
		slog.Error("Error forwarding callback to receiver", "prefix", matchedPrefix, "target_url", targetURL, "correlation_id", correlationID, "error", err)
		gw.answerCallback(cb.ID, "Receiver backend unreachable", true)
	}
}

// forwardCallbackToReceiver POSTs payload to receiver with transient retries and answers the Telegram callback query accordingly
func (gw *Gateway) forwardCallbackToReceiver(prefix string, targetURL string, payload CallbackPayload, correlationID string) error {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		metricCallbackForward.WithLabelValues(prefix, "marshal_error").Inc()
		return fmt.Errorf("failed to marshal callback payload: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	maxAttempts := 3
	var lastErr error
	var resp *http.Response

	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime).Seconds()
		metricCallbackLatency.WithLabelValues(prefix).Observe(duration)
	}()

	// If webhook secret is configured, pre-calculate HMAC-SHA256 signature once outside retry loop
	var signature string
	if gw.Config.WebhookSecret != "" {
		mac := hmac.New(sha256.New, []byte(gw.Config.WebhookSecret))
		mac.Write(bodyBytes)
		signature = hex.EncodeToString(mac.Sum(nil))
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(bodyBytes))
		if err != nil {
			metricCallbackForward.WithLabelValues(prefix, "request_create_error").Inc()
			return fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(HeaderCorrelationID, correlationID)
		req.Header.Set(HeaderRequestID, correlationID)

		if signature != "" {
			req.Header.Set(HeaderGatewaySignature, signature)
		}

		resp, err = gw.Client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			if attempt < maxAttempts {
				slog.Warn("Transient error forwarding callback, retrying...", "prefix", prefix, "attempt", attempt, "error", err, "correlation_id", correlationID)
				select {
				case <-time.After(time.Duration(attempt*100) * time.Millisecond):
					continue
				case <-ctx.Done():
					metricCallbackForward.WithLabelValues(prefix, "timeout").Inc()
					return fmt.Errorf("request timeout during retry: %w", ctx.Err())
				}
			}
			metricCallbackForward.WithLabelValues(prefix, "transport_error").Inc()
			return lastErr
		}

		// 5xx Server Errors are retryable; 4xx Client Errors are not
		if resp.StatusCode >= 500 && resp.StatusCode <= 599 {
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("receiver returned status code %d", resp.StatusCode)
			if attempt < maxAttempts {
				slog.Warn("Receiver returned 5xx status code, retrying...", "prefix", prefix, "attempt", attempt, "status_code", resp.StatusCode, "correlation_id", correlationID)
				select {
				case <-time.After(time.Duration(attempt*100) * time.Millisecond):
					continue
				case <-ctx.Done():
					metricCallbackForward.WithLabelValues(prefix, "timeout").Inc()
					return fmt.Errorf("request timeout during retry: %w", ctx.Err())
				}
			}
			metricCallbackForward.WithLabelValues(prefix, "status_"+strconv.Itoa(resp.StatusCode)).Inc()
			return lastErr
		}

		if resp.StatusCode != http.StatusOK {
			defer resp.Body.Close()
			metricCallbackForward.WithLabelValues(prefix, "status_"+strconv.Itoa(resp.StatusCode)).Inc()
			return fmt.Errorf("receiver returned status code %d", resp.StatusCode)
		}

		// Successful 200 OK response
		break
	}
	defer resp.Body.Close()

	// Read optional response JSON
	var rxResp ReceiverResponse
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&rxResp); err != nil {
		// Response is empty or not JSON, which is acceptable. Acknowledge with empty string.
		gw.answerCallback(payload.CallbackQueryID, "", false)
		metricCallbackForward.WithLabelValues(prefix, "success_empty").Inc()
		return nil
	}

	gw.answerCallback(payload.CallbackQueryID, rxResp.Text, rxResp.ShowAlert)
	metricCallbackForward.WithLabelValues(prefix, "success_response").Inc()
	return nil
}

// answerCallback calls Telegram's answerCallbackQuery method to acknowledge the callback
func (gw *Gateway) answerCallback(callbackQueryID string, text string, showAlert bool) {
	if gw.Bot == nil {
		return
	}
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

// HandleHealth handles GET /health requests (Liveness check)
func (gw *Gateway) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		gw.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// HandleReady handles GET /ready requests and returns 200 OK if polling is healthy (poll within 2 minutes),
// or 503 Service Unavailable if polling has stalled.
func (gw *Gateway) HandleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		gw.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	w.Header().Set("Content-Type", "application/json")

	lastPoll := gw.lastPollSuccess.Load()
	if lastPoll == 0 {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "not_ready",
			"reason": "polling loop not active or first poll pending",
		})
		return
	}

	stalledThreshold := 2 * time.Minute
	timeSinceLastPoll := time.Since(time.Unix(lastPoll, 0))

	if timeSinceLastPoll > stalledThreshold {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":             "not_ready",
			"reason":             "polling_stalled",
			"seconds_since_poll": int(timeSinceLastPoll.Seconds()),
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":             "ok",
		"seconds_since_poll": int(timeSinceLastPoll.Seconds()),
	})
}

// HandleWebhook handles incoming POST update payloads sent by Telegram Webhook.
func (gw *Gateway) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		gw.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Validate secret token if TELEGRAM_WEBHOOK_SECRET is set
	if gw.Config.TelegramWebhookSecret != "" {
		secretHeader := r.Header.Get(HeaderTelegramSecretToken)
		if secretHeader != gw.Config.TelegramWebhookSecret {
			slog.Warn("Unauthorized webhook request: secret token mismatch")
			gw.writeError(w, http.StatusUnauthorized, "Unauthorized webhook request")
			return
		}
	}

	var update tgbotapi.Update
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		slog.Error("Failed to decode webhook JSON update", "error", err)
		gw.writeError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	// Record successful health timestamp
	gw.lastPollSuccess.Store(time.Now().Unix())

	// Dispatch update handling asynchronously
	gw.WG.Add(1)
	go func(up tgbotapi.Update) {
		defer gw.WG.Done()
		gw.HandleUpdate(up)
	}(update)

	w.WriteHeader(http.StatusOK)
}

// Routes returns the configured HTTP handler for all gateway endpoints
func (gw *Gateway) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/send", gw.requireAuth(gw.HandleSend))
	mux.HandleFunc("/health", gw.HandleHealth)
	mux.HandleFunc("/ready", gw.HandleReady)
	mux.Handle("/metrics", promhttp.Handler())

	if gw.Config != nil && strings.ToLower(gw.Config.Mode) == "webhook" {
		webhookPath := gw.Config.WebhookPath
		if webhookPath == "" {
			webhookPath = "/webhook"
		}
		mux.HandleFunc(webhookPath, gw.HandleWebhook)
	}

	return mux
}

// parseTelegramRetryAfter extracts the wait duration from a Telegram
// "Too Many Requests: retry after N" error. Returns 0 if not applicable.
func parseTelegramRetryAfter(err error) time.Duration {
	if err == nil {
		return 0
	}
	msg := err.Error()
	// Telegram sends "Too Many Requests: retry after N" where N is seconds
	const prefix = "Too Many Requests: retry after "
	idx := strings.Index(msg, prefix)
	if idx == -1 {
		return 0
	}
	numStr := strings.TrimSpace(msg[idx+len(prefix):])
	// numStr may have trailing content; take only the leading integer
	if i := strings.IndexFunc(numStr, func(r rune) bool {
		return r < '0' || r > '9'
	}); i != -1 {
		numStr = numStr[:i]
	}
	n, parseErr := strconv.Atoi(numStr)
	if parseErr != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

// RegisterWebhook registers the configured webhook URL and secret token with the Telegram Bot API.
func (gw *Gateway) RegisterWebhook() error {
	if gw.Config == nil || strings.ToLower(gw.Config.Mode) != "webhook" {
		return nil
	}
	fullWebhookURL := strings.TrimRight(gw.Config.WebhookURL, "/") + gw.Config.WebhookPath
	slog.Info("Configuring Telegram Webhook...", "url", fullWebhookURL)

	params := tgbotapi.Params{
		"url": fullWebhookURL,
	}
	if gw.Config.TelegramWebhookSecret != "" {
		params["secret_token"] = gw.Config.TelegramWebhookSecret
	}
	if _, err := gw.Bot.MakeRequest("setWebhook", params); err != nil {
		return fmt.Errorf("failed to register Telegram Webhook: %w", err)
	}
	slog.Info("Telegram Webhook registered successfully", "path", gw.Config.WebhookPath)
	return nil
}

// generateCorrelationID creates a unique correlation identifier for end-to-end tracing.
func generateCorrelationID(prefix string) string {
	b := make([]byte, 8)
	_, _ = crand.Read(b)
	return fmt.Sprintf("%s-%x", prefix, b)
}
