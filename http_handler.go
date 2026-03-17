package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

type HTTPHandler struct {
	store              *Store
	bots               map[string]handlerBotConfig
	globalAllowlist    map[int64]struct{}
	defaultMaxAttempts int
}

type handlerBotConfig struct {
	token     string
	enabled   bool
	allowlist map[int64]struct{}
}

type sendRequest struct {
	Bot                 string `json:"bot"`
	ChatID              int64  `json:"chat_id"`
	Text                string `json:"text"`
	MaxAttempts         int    `json:"max_attempts,omitempty"`
	ReplyToMessageID    *int64 `json:"reply_to_message_id,omitempty"`
	DisableNotification bool   `json:"disable_notification,omitempty"`
}

type sendResponse struct {
	JobID  int64  `json:"job_id"`
	Status string `json:"status"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func NewHTTPHandler(store *Store, telegramCfg TelegramRuntimeConfig, defaultMaxAttempts int) http.Handler {
	bots := make(map[string]handlerBotConfig, len(telegramCfg.Bots))
	for name, botCfg := range telegramCfg.Bots {
		bots[name] = handlerBotConfig{
			token:     botCfg.Token,
			enabled:   botCfg.Enabled,
			allowlist: toAllowlistSet(botCfg.AllowUserIDs),
		}
	}

	return &HTTPHandler{
		store:              store,
		bots:               bots,
		globalAllowlist:    toAllowlistSet(telegramCfg.GlobalAllowUserIDs),
		defaultMaxAttempts: defaultMaxAttempts,
	}
}

func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/send" {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "not found"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var request sendRequest
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid json body"})
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid json body"})
		return
	}

	request.Bot = normalizeBotName(request.Bot)
	if request.Bot == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "bot is required"})
		return
	}
	bot, ok := h.bots[request.Bot]
	if !ok {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "unknown bot"})
		return
	}
	if !bot.enabled {
		writeJSON(w, http.StatusForbidden, errorResponse{Error: "bot is disabled"})
		return
	}
	if request.ChatID == 0 {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "chat_id is required"})
		return
	}
	if !allowlisted(request.ChatID, h.globalAllowlist, bot.allowlist) {
		writeJSON(w, http.StatusForbidden, errorResponse{Error: "chat_id is not allowed"})
		return
	}
	if strings.TrimSpace(request.Text) == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "text is required"})
		return
	}
	if request.MaxAttempts == 0 {
		request.MaxAttempts = h.defaultMaxAttempts
	}
	if request.MaxAttempts < minMaxAttempts || request.MaxAttempts > maxMaxAttempts {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "max_attempts must be between 1 and 5"})
		return
	}

	job, err := h.store.Enqueue(r.Context(), CreateJob{
		BotName:             request.Bot,
		ChatID:              request.ChatID,
		Text:                request.Text,
		MaxAttempts:         request.MaxAttempts,
		ReplyToMessageID:    request.ReplyToMessageID,
		DisableNotification: request.DisableNotification,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "failed to enqueue job"})
		return
	}

	writeJSON(w, http.StatusAccepted, sendResponse{JobID: job.ID, Status: job.Status})
}

func writeJSON(w http.ResponseWriter, statusCode int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(body)
}

func toAllowlistSet(ids []int64) map[int64]struct{} {
	if len(ids) == 0 {
		return nil
	}

	allowlist := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		allowlist[id] = struct{}{}
	}

	return allowlist
}

func allowlisted(chatID int64, globalAllowlist map[int64]struct{}, botAllowlist map[int64]struct{}) bool {
	if _, ok := globalAllowlist[chatID]; ok {
		return true
	}
	if _, ok := botAllowlist[chatID]; ok {
		return true
	}
	return false
}

