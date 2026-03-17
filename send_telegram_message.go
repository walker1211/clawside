package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type TelegramClient struct {
	baseURL    string
	httpClient *http.Client
}

type TelegramSendMessageRequest struct {
	ChatID              int64  `json:"chat_id"`
	Text                string `json:"text"`
	ReplyToMessageID    *int64 `json:"reply_to_message_id,omitempty"`
	DisableNotification bool   `json:"disable_notification,omitempty"`
}

type telegramResponse struct {
	OK          bool            `json:"ok"`
	ErrorCode   int             `json:"error_code,omitempty"`
	Description string          `json:"description,omitempty"`
	Parameters  telegramPayload `json:"parameters,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
}

type telegramPayload struct {
	RetryAfter int `json:"retry_after,omitempty"`
}

type TelegramError struct {
	StatusCode  int
	Description string
	Retryable   bool
	RetryAfter  time.Duration
}

func NewTelegramClient(baseURL string, httpClient *http.Client) *TelegramClient {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://api.telegram.org"
	}
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &TelegramClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

func (e *TelegramError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("telegram api error (%d): %s", e.StatusCode, e.Description)
	}
	return fmt.Sprintf("telegram api error: %s", e.Description)
}

func (c *TelegramClient) SendMessage(ctx context.Context, botToken string, request TelegramSendMessageRequest) error {
	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("marshal telegram request: %w", err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/bot%s/sendMessage", c.baseURL, botToken), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build telegram request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return &TelegramError{Description: err.Error(), Retryable: false}
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return &TelegramError{StatusCode: response.StatusCode, Description: fmt.Sprintf("read telegram response: %v", err), Retryable: response.StatusCode >= http.StatusInternalServerError}
	}

	var telegramResponse telegramResponse
	if len(responseBody) > 0 {
		if err := json.Unmarshal(responseBody, &telegramResponse); err != nil {
			return &TelegramError{StatusCode: response.StatusCode, Description: fmt.Sprintf("decode telegram response: %v", err), Retryable: response.StatusCode >= http.StatusInternalServerError}
		}
	}

	if response.StatusCode >= http.StatusBadRequest || !telegramResponse.OK {
		description := telegramResponse.Description
		if description == "" {
			description = http.StatusText(response.StatusCode)
		}
		return &TelegramError{
			StatusCode:  response.StatusCode,
			Description: description,
			Retryable:   response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError,
			RetryAfter:  time.Duration(telegramResponse.Parameters.RetryAfter) * time.Second,
		}
	}

	return nil
}
