package a2adelivery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type SenderClient struct {
	baseURL    string
	authKey    string
	httpClient *http.Client
}

var telegramBotTokenInSenderErrorPattern = regexp.MustCompile(`bot[0-9]+:[A-Za-z0-9_-]+`)

type SenderHealth struct {
	Status string `json:"status"`
}

type SenderStats struct {
	PendingCount            int     `json:"pending_count"`
	RetryCount              int     `json:"retry_count"`
	SendingCount            int     `json:"sending_count"`
	FailedCount             int     `json:"failed_count"`
	SentCount               int     `json:"sent_count"`
	OldestPendingAgeSeconds *int64  `json:"oldest_pending_age_seconds"`
	LastLoopAt              *string `json:"last_loop_at"`
	LastJobClaimAt          *string `json:"last_job_claim_at"`
	LastSuccessAt           *string `json:"last_success_at"`
	LastFailureAt           *string `json:"last_failure_at"`
	WorkerRunning           bool    `json:"worker_running"`
}

type SenderJobList struct {
	Jobs []SenderJobListItem `json:"jobs"`
}

type SenderJobListItem struct {
	JobID        int64   `json:"job_id"`
	Bot          string  `json:"bot"`
	ChatID       int64   `json:"chat_id"`
	Status       string  `json:"status"`
	AttemptCount int     `json:"attempt_count"`
	MaxAttempts  int     `json:"max_attempts"`
	LastError    string  `json:"last_error"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
	SentAt       *string `json:"sent_at"`
}

type SenderJob struct {
	JobID        int64   `json:"job_id"`
	Status       string  `json:"status"`
	AttemptCount int     `json:"attempt_count"`
	LastError    string  `json:"last_error"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
	SentAt       *string `json:"sent_at"`
}

type senderAPIError struct {
	StatusCode int
	Message    string
}

func (e *senderAPIError) Error() string {
	if strings.TrimSpace(e.Message) == "" {
		return fmt.Sprintf("sender API returned status %d", e.StatusCode)
	}
	return fmt.Sprintf("sender API returned status %d: %s", e.StatusCode, e.Message)
}

func (e *senderAPIError) Retryable() bool {
	return e.StatusCode >= 500
}

func (e *senderAPIError) NotFound() bool {
	return e.StatusCode == http.StatusNotFound
}

func NewSenderClient(baseURL string, senderAuthKey string, httpClient *http.Client) *SenderClient {
	trimmedBaseURL := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &SenderClient{
		baseURL:    trimmedBaseURL,
		authKey:    strings.TrimSpace(senderAuthKey),
		httpClient: httpClient,
	}
}

func (c *SenderClient) Send(ctx context.Context, bot string, chatID int64, text string, idempotencyKey string) (int64, string, error) {
	payload := struct {
		Bot            string `json:"bot"`
		ChatID         int64  `json:"chat_id"`
		Text           string `json:"text"`
		IdempotencyKey string `json:"idempotency_key,omitempty"`
	}{
		Bot:            bot,
		ChatID:         chatID,
		Text:           text,
		IdempotencyKey: strings.TrimSpace(idempotencyKey),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return 0, "", fmt.Errorf("marshal /send payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/send", bytes.NewReader(body))
	if err != nil {
		return 0, "", fmt.Errorf("build /send request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.authKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.authKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return 0, "", decodeSenderAPIError(resp)
	}

	var response struct {
		JobID  int64  `json:"job_id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return 0, "", fmt.Errorf("decode /send response: %w", err)
	}
	if response.JobID <= 0 {
		return 0, "", fmt.Errorf("invalid /send response: missing job_id")
	}
	return response.JobID, response.Status, nil
}

func (c *SenderClient) Health(ctx context.Context) (SenderHealth, error) {
	var health SenderHealth
	if err := c.getJSON(ctx, "/healthz", http.StatusOK, &health, "/healthz"); err != nil {
		return SenderHealth{}, err
	}
	return health, nil
}

func (c *SenderClient) Readiness(ctx context.Context) (SenderHealth, error) {
	var readiness SenderHealth
	if err := c.getJSON(ctx, "/readyz", http.StatusOK, &readiness, "/readyz"); err != nil {
		return SenderHealth{}, err
	}
	return readiness, nil
}

func (c *SenderClient) GetStats(ctx context.Context) (SenderStats, error) {
	var stats SenderStats
	if err := c.getJSON(ctx, "/stats", http.StatusOK, &stats, "/stats"); err != nil {
		return SenderStats{}, err
	}
	return stats, nil
}

func (c *SenderClient) ListJobs(ctx context.Context, status string, limit int) ([]SenderJobListItem, error) {
	path := fmt.Sprintf("/jobs?status=%s&limit=%d", url.QueryEscape(status), limit)
	var list SenderJobList
	if err := c.getJSON(ctx, path, http.StatusOK, &list, "/jobs"); err != nil {
		return nil, err
	}
	for i := range list.Jobs {
		list.Jobs[i].LastError = sanitizeSenderLastError(list.Jobs[i].LastError)
	}
	return list.Jobs, nil
}

func (c *SenderClient) GetJob(ctx context.Context, jobID int64) (SenderJob, error) {
	var job SenderJob
	if err := c.getJSON(ctx, fmt.Sprintf("/jobs/%d", jobID), http.StatusOK, &job, "/jobs"); err != nil {
		return SenderJob{}, err
	}
	job.LastError = sanitizeSenderLastError(job.LastError)
	return job, nil
}

func (c *SenderClient) getJSON(ctx context.Context, path string, expectedStatus int, target any, responseName string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("build %s request: %w", responseName, err)
	}
	if c.authKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.authKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != expectedStatus {
		return decodeSenderAPIError(resp)
	}

	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decode %s response: %w", responseName, err)
	}
	return nil
}

func decodeSenderAPIError(resp *http.Response) error {
	var payload struct {
		Error string `json:"error"`
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	_ = json.Unmarshal(data, &payload)
	msg := strings.TrimSpace(payload.Error)
	if msg == "" {
		msg = strings.TrimSpace(string(data))
	}
	return &senderAPIError{StatusCode: resp.StatusCode, Message: sanitizeSenderLastError(msg)}
}

func sanitizeSenderLastError(lastError string) string {
	if strings.TrimSpace(lastError) == "" {
		return ""
	}
	return telegramBotTokenInSenderErrorPattern.ReplaceAllString(lastError, "bot[redacted]")
}

func SanitizeForSmokeReport(detail string) string {
	return sanitizeSenderLastError(detail)
}

func IsRetryablePollError(err error) bool {
	if err == nil {
		return false
	}

	var apiErr *senderAPIError
	if ok := AsSenderAPIError(err, &apiErr); ok {
		return apiErr.Retryable()
	}

	if isNetworkError(err) {
		return true
	}

	return false
}

func IsPostAcceptNotFound(err error) bool {
	var apiErr *senderAPIError
	if ok := AsSenderAPIError(err, &apiErr); ok {
		return apiErr.NotFound()
	}
	return false
}

func AsSenderAPIError(err error, target **senderAPIError) bool {
	if err == nil {
		return false
	}
	return errors.As(err, target)
}

func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if ok := AsNetError(err, &netErr); ok {
		return true
	}
	return false
}

func AsNetError(err error, target *net.Error) bool {
	if err == nil {
		return false
	}
	return errors.As(err, target)
}
