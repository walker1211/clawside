package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/walker1211/clawside/internal/a2adelivery"
)

const defaultSenderBaseURL = "http://127.0.0.1:8787"

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	fs := flag.NewFlagSet("a2a-delivery", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var senderBaseURL string
	var senderAuthKey string
	var targetAgent string
	var text string
	var chatIDRaw string
	var idempotencyKey string
	var deliveryContextToRaw string
	var directSessionPeerChatIDRaw string
	var inboundSenderChatIDRaw string

	fs.StringVar(&senderBaseURL, "sender-base-url", defaultSenderBaseURL, "sender base url")
	fs.StringVar(&senderAuthKey, "sender-auth-key", "", "sender auth key")
	fs.StringVar(&targetAgent, "target-agent", "", "target agent")
	fs.StringVar(&text, "text", "", "delivery text")
	fs.StringVar(&chatIDRaw, "chat-id", "", "explicit target chat id")
	fs.StringVar(&idempotencyKey, "idempotency-key", "", "idempotency key")
	fs.StringVar(&deliveryContextToRaw, "delivery-context-to", "", "deliveryContext.to chat id")
	fs.StringVar(&directSessionPeerChatIDRaw, "direct-session-peer-chat-id", "", "direct session peer chat id")
	fs.StringVar(&inboundSenderChatIDRaw, "inbound-sender-chat-id", "", "inbound sender chat id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if strings.TrimSpace(targetAgent) == "" {
		return fmt.Errorf("missing target-agent")
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("missing text")
	}

	senderAuthKey = resolveSenderAuthKey(senderAuthKey)

	chatID, err := parseOptionalInt64(chatIDRaw, "chat-id")
	if err != nil {
		return err
	}
	idempotencyKeyPtr := optionalNonEmptyString(idempotencyKey)
	deliveryContextTo, err := parseOptionalInt64(deliveryContextToRaw, "delivery-context-to")
	if err != nil {
		return err
	}
	directSessionPeerChatID, err := parseOptionalInt64(directSessionPeerChatIDRaw, "direct-session-peer-chat-id")
	if err != nil {
		return err
	}
	inboundSenderChatID, err := parseOptionalInt64(inboundSenderChatIDRaw, "inbound-sender-chat-id")
	if err != nil {
		return err
	}

	client := a2adelivery.NewSenderClient(senderBaseURL, senderAuthKey, nil)
	result, err := a2adelivery.RunA2ADeliveryBridge(context.Background(), client, a2adelivery.SkillInput{
		TargetAgent:    targetAgent,
		Text:           text,
		ChatID:         chatID,
		IdempotencyKey: idempotencyKeyPtr,
	}, a2adelivery.TargetUserContext{
		DeliveryContextTo:       deliveryContextTo,
		DirectSessionPeerChatID: directSessionPeerChatID,
		InboundSenderChatID:     inboundSenderChatID,
	})
	if err != nil {
		return err
	}
	return writeJSON(stdout, result)
}

func resolveSenderAuthKey(flagValue string) string {
	if trimmed := strings.TrimSpace(flagValue); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(os.Getenv("SENDER_AUTH_KEY"))
}

func parseOptionalInt64(raw string, name string) (*int64, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	value, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	return &value, nil
}

func optionalNonEmptyString(raw string) *string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
