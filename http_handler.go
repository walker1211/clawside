package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"openclaw/internal/deliveryrules"
)

type HTTPHandler struct {
	store              *Store
	bots               map[string]handlerBotConfig
	globalAllowlist    map[int64]struct{}
	defaultMaxAttempts int
	senderAuthKey      string
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
	IdempotencyKey      string `json:"idempotency_key,omitempty"`
	MaxAttempts         int    `json:"max_attempts,omitempty"`
	ReplyToMessageID    *int64 `json:"reply_to_message_id,omitempty"`
	DisableNotification bool   `json:"disable_notification,omitempty"`
}

type sendResponse struct {
	JobID          int64  `json:"job_id"`
	Status         string `json:"status"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

type healthResponse struct {
	Status string `json:"status"`
}

type jobResponse struct {
	JobID        int64   `json:"job_id"`
	Status       string  `json:"status"`
	AttemptCount int     `json:"attempt_count"`
	LastError    string  `json:"last_error"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
	SentAt       *string `json:"sent_at"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func NewHTTPHandler(store *Store, telegramCfg TelegramRuntimeConfig, defaultMaxAttempts int, senderAuthKey string) http.Handler {
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
		senderAuthKey:      strings.TrimSpace(senderAuthKey),
	}
}

func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		h.handleHealthz(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/jobs/") {
		h.handleGetJob(w, r)
		return
	}
	if r.URL.Path == "/send" {
		h.handleSend(w, r)
		return
	}

	writeJSON(w, http.StatusNotFound, errorResponse{Error: "not found"})
}

func (h *HTTPHandler) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
		return
	}
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}

func (h *HTTPHandler) handleGetJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
		return
	}

	idRaw := strings.TrimPrefix(r.URL.Path, "/jobs/")
	if idRaw == "" || strings.Contains(idRaw, "/") {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "not found"})
		return
	}
	jobID, err := strconv.ParseInt(idRaw, 10, 64)
	if err != nil || jobID <= 0 {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "not found"})
		return
	}

	job, err := h.store.GetJob(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, errorResponse{Error: "not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "failed to get job"})
		return
	}

	var sentAt *string
	if job.SentAt != nil {
		formatted := formatTimestamp(*job.SentAt)
		sentAt = &formatted
	}

	writeJSON(w, http.StatusOK, jobResponse{
		JobID:        job.ID,
		Status:       job.Status,
		AttemptCount: job.AttemptCount,
		LastError:    job.LastError,
		CreatedAt:    formatTimestamp(job.CreatedAt),
		UpdatedAt:    formatTimestamp(job.UpdatedAt),
		SentAt:       sentAt,
	})
}

func (h *HTTPHandler) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
		return
	}
	if !h.authorized(r.Header.Get("Authorization")) {
		if strings.TrimSpace(r.Header.Get("Authorization")) == "" {
			writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "missing authorization"})
			return
		}
		writeJSON(w, http.StatusForbidden, errorResponse{Error: "invalid authorization"})
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

	request.Bot = deliveryrules.NormalizeBotName(request.Bot)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
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
	if utf8.RuneCountInString(request.Text) > deliveryrules.TelegramMaxTextLength {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "text exceeds telegram limit"})
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
		IdempotencyKey:      request.IdempotencyKey,
		MaxAttempts:         request.MaxAttempts,
		ReplyToMessageID:    request.ReplyToMessageID,
		DisableNotification: request.DisableNotification,
	})
	if err != nil {
		if request.IdempotencyKey != "" {
			existing, lookupErr := h.store.GetByIdempotencyKey(r.Context(), request.IdempotencyKey)
			if lookupErr == nil && existing != nil {
				writeJSON(w, http.StatusAccepted, sendResponse{JobID: existing.ID, Status: existing.Status, IdempotencyKey: request.IdempotencyKey})
				return
			}
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "failed to enqueue job"})
		return
	}

	writeJSON(w, http.StatusAccepted, sendResponse{JobID: job.ID, Status: job.Status, IdempotencyKey: request.IdempotencyKey})
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

func (h *HTTPHandler) authorized(authHeader string) bool {
	authHeader = strings.TrimSpace(authHeader)
	if authHeader == "" || h.senderAuthKey == "" {
		return false
	}
	const bearerPrefix = "Bearer "
	if !strings.HasPrefix(authHeader, bearerPrefix) {
		return false
	}
	provided := strings.TrimSpace(strings.TrimPrefix(authHeader, bearerPrefix))
	return provided != "" && provided == h.senderAuthKey
}
