package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type HTTPHandler struct {
	store         *Store
	queryService  *JobQueryService
	sendService   *SendService
	senderAuthKey string
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

type jobListResponse struct {
	Jobs []JobListItem `json:"jobs"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func NewHTTPHandler(store *Store, telegramCfg TelegramRuntimeConfig, defaultMaxAttempts int, senderAuthKey string, queryService *JobQueryService) http.Handler {
	return &HTTPHandler{
		store:         store,
		queryService:  queryService,
		sendService:   NewSendService(store, telegramCfg, defaultMaxAttempts),
		senderAuthKey: strings.TrimSpace(senderAuthKey),
	}
}

func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		h.handleHealthz(w, r)
		return
	}
	if r.URL.Path == "/readyz" {
		h.handleReadyz(w, r)
		return
	}
	if r.URL.Path == "/stats" {
		h.handleStats(w, r)
		return
	}
	if r.URL.Path == "/jobs" {
		h.handleListJobs(w, r)
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

func (h *HTTPHandler) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
		return
	}
	if h.queryService == nil {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "readiness service is not configured"})
		return
	}
	if err := h.queryService.Readiness(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}

func (h *HTTPHandler) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
		return
	}
	if h.queryService == nil {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "query service is not configured"})
		return
	}
	stats, err := h.queryService.GetStats(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (h *HTTPHandler) handleListJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
		return
	}
	if h.queryService == nil {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "query service is not configured"})
		return
	}

	limit, provided, err := parseJobListLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	if !provided {
		limit = DefaultJobListLimit
	}
	jobs, err := h.queryService.ListJobs(r.Context(), r.URL.Query().Get("status"), limit)
	if err != nil {
		if err.Error() == "invalid status" || err.Error() == "status is required" || err.Error() == "invalid limit" {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "failed to list jobs"})
		return
	}
	writeJSON(w, http.StatusOK, jobListResponse{Jobs: jobs})
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

	job, err := h.sendService.Submit(r.Context(), SendCommand{
		Bot:                 request.Bot,
		ChatID:              request.ChatID,
		Text:                request.Text,
		IdempotencyKey:      request.IdempotencyKey,
		MaxAttempts:         request.MaxAttempts,
		ReplyToMessageID:    request.ReplyToMessageID,
		DisableNotification: request.DisableNotification,
	})
	if err != nil {
		h.writeSendError(w, err)
		return
	}

	writeJSON(w, http.StatusAccepted, sendResponse{JobID: job.ID, Status: job.Status, IdempotencyKey: job.IdempotencyKey})
}

func (h *HTTPHandler) writeSendError(w http.ResponseWriter, err error) {
	statusCode, response := sendErrorResponse(err)
	writeJSON(w, statusCode, response)
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
