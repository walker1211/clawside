package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTelegramClientGetUpdates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botsecret-token/getUpdates" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.URL.Query().Get("offset") != "42" {
			t.Fatalf("unexpected offset %q", r.URL.Query().Get("offset"))
		}
		if r.URL.Query().Get("timeout") != "30" {
			t.Fatalf("unexpected timeout %q", r.URL.Query().Get("timeout"))
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":43,"message":{"message_id":7,"chat":{"id":123,"type":"private"},"from":{"id":1001,"username":"alice"},"text":"/health"}}]}`))
	}))
	defer server.Close()

	client := newTelegramClient(server.URL, server.Client())
	updates, err := client.getUpdates(context.Background(), "secret-token", 42, 30)
	if err != nil {
		t.Fatalf("get updates: %v", err)
	}
	if len(updates) != 1 || updates[0].UpdateID != 43 {
		t.Fatalf("unexpected updates: %#v", updates)
	}
	message := updates[0].Message
	if message == nil || message.MessageID != 7 || message.Chat.ID != 123 || message.Chat.Type != "private" || message.From == nil || message.From.ID != 1001 || message.Text != "/health" {
		t.Fatalf("unexpected message: %#v", message)
	}
}

func TestTelegramClientSendMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botsecret-token/sendMessage" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var req telegramSendMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.ChatID != 123 || req.Text != "ok" || req.ReplyToMessageID == nil || *req.ReplyToMessageID != 7 {
			t.Fatalf("unexpected send request: %#v", req)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer server.Close()

	client := newTelegramClient(server.URL, server.Client())
	replyTo := int64(7)
	if err := client.sendMessage(context.Background(), "secret-token", telegramSendMessageRequest{ChatID: 123, Text: "ok", ReplyToMessageID: &replyTo}); err != nil {
		t.Fatalf("send message: %v", err)
	}
}

func TestTelegramClientErrorsDoNotLeakToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"ok":false,"description":"bad token secret-token"}`))
	}))
	defer server.Close()

	client := newTelegramClient(server.URL, server.Client())
	_, err := client.getUpdates(context.Background(), "secret-token", 0, 1)
	if err == nil {
		t.Fatalf("expected getUpdates error")
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("error leaked token: %v", err)
	}
}
