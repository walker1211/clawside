package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type telegramAPI interface {
	getUpdates(ctx context.Context, token string, offset int64, timeoutSeconds int) ([]telegramUpdate, error)
	sendMessage(ctx context.Context, token string, req telegramSendMessageRequest) error
}

type telegramClient struct {
	baseURL    string
	httpClient *http.Client
}

type telegramUpdate struct {
	UpdateID int64            `json:"update_id"`
	Message  *telegramMessage `json:"message,omitempty"`
}

type telegramMessage struct {
	MessageID int64         `json:"message_id"`
	Chat      telegramChat  `json:"chat"`
	From      *telegramUser `json:"from,omitempty"`
	Text      string        `json:"text,omitempty"`
}

type telegramChat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type telegramUser struct {
	ID       int64  `json:"id"`
	UserName string `json:"username,omitempty"`
}

type telegramSendMessageRequest struct {
	ChatID           int64  `json:"chat_id"`
	Text             string `json:"text"`
	ReplyToMessageID *int64 `json:"reply_to_message_id,omitempty"`
}

type telegramAPIResponse[T any] struct {
	OK          bool   `json:"ok"`
	Description string `json:"description,omitempty"`
	Result      T      `json:"result,omitempty"`
}

func newTelegramClient(baseURL string, httpClient *http.Client) *telegramClient {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultTelegramBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &telegramClient{baseURL: strings.TrimRight(baseURL, "/"), httpClient: httpClient}
}

func (c *telegramClient) getUpdates(ctx context.Context, token string, offset int64, timeoutSeconds int) ([]telegramUpdate, error) {
	endpoint := fmt.Sprintf("%s/bot%s/getUpdates", c.baseURL, token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build telegram getUpdates request: %s", redactTelegramToken(err.Error(), token))
	}
	query := req.URL.Query()
	if offset > 0 {
		query.Set("offset", strconv.FormatInt(offset, 10))
	}
	if timeoutSeconds > 0 {
		query.Set("timeout", strconv.Itoa(timeoutSeconds))
	}
	req.URL.RawQuery = query.Encode()

	body, statusCode, err := c.doTelegramHTTP(req, token)
	if err != nil {
		return nil, err
	}
	var apiResponse telegramAPIResponse[[]telegramUpdate]
	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return nil, fmt.Errorf("decode telegram getUpdates response: %s", redactTelegramToken(err.Error(), token))
	}
	if statusCode >= http.StatusBadRequest || !apiResponse.OK {
		return nil, telegramAPIError("getUpdates", statusCode, apiResponse.Description, token)
	}
	return apiResponse.Result, nil
}

func (c *telegramClient) sendMessage(ctx context.Context, token string, request telegramSendMessageRequest) error {
	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("marshal telegram sendMessage request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/bot%s/sendMessage", c.baseURL, token), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build telegram sendMessage request: %s", redactTelegramToken(err.Error(), token))
	}
	req.Header.Set("Content-Type", "application/json")

	responseBody, statusCode, err := c.doTelegramHTTP(req, token)
	if err != nil {
		return err
	}
	var apiResponse telegramAPIResponse[json.RawMessage]
	if err := json.Unmarshal(responseBody, &apiResponse); err != nil {
		return fmt.Errorf("decode telegram sendMessage response: %s", redactTelegramToken(err.Error(), token))
	}
	if statusCode >= http.StatusBadRequest || !apiResponse.OK {
		return telegramAPIError("sendMessage", statusCode, apiResponse.Description, token)
	}
	return nil
}

func (c *telegramClient) doTelegramHTTP(req *http.Request, token string) ([]byte, int, error) {
	response, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("telegram request failed: %s", redactTelegramToken(err.Error(), token))
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, response.StatusCode, fmt.Errorf("read telegram response: %s", redactTelegramToken(err.Error(), token))
	}
	return body, response.StatusCode, nil
}

func telegramAPIError(method string, statusCode int, description string, token string) error {
	description = strings.TrimSpace(description)
	if description == "" {
		description = http.StatusText(statusCode)
	}
	return fmt.Errorf("telegram %s error (%d): %s", method, statusCode, redactTelegramToken(description, token))
}

func redactTelegramToken(value string, token string) string {
	if strings.TrimSpace(token) == "" {
		return value
	}
	return strings.ReplaceAll(value, token, "<redacted>")
}
