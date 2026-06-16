package main

import (
	"encoding/json"
	"net/http"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Gateway holds the state for the HTTP server and Telegram bot client
type Gateway struct {
	Bot    *tgbotapi.BotAPI
	Config *Config
	Client *http.Client
}

// SendRequest represents the payload for POST /send
type SendRequest struct {
	ChatID      int64                         `json:"chat_id"`
	Text        string                        `json:"text"`
	ReplyMarkup *tgbotapi.InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	ParseMode   string                        `json:"parse_mode,omitempty"`
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
		gw.writeError(w, http.StatusBadRequest, "chat_id is required")
		return
	}
	if req.Text == "" {
		gw.writeError(w, http.StatusBadRequest, "text is required")
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

	sentMsg, err := gw.Bot.Send(msg)
	if err != nil {
		gw.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := SendResponse{
		MessageID: sentMsg.MessageID,
		ChatID:    sentMsg.Chat.ID,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (gw *Gateway) writeError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(ErrorResponse{Error: message})
}
