package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/walker1211/clawside/internal/orchestrator"
	"github.com/walker1211/clawside/internal/swarmdriver"
	"github.com/walker1211/clawside/internal/toolserver"
)

type operator struct {
	handlers            *toolserver.Handlers
	inboundAgentBridge  telegramInboundAgentBridge
	executionResultSink telegramExecutionResultSink
}

type telegramExecutionResultSink interface {
	SaveExecutionResult(ctx context.Context, result swarmdriver.ExecutionResult) error
}

type telegramInboundAgentBridge interface {
	Run(ctx context.Context, input telegramInboundAgentInput) (string, error)
}

type telegramInboundAgentInput struct {
	TargetAgent      string
	Text             string
	ChatID           int64
	FromUserID       int64
	ReplyToMessageID int64
}

type operatorCommandKind string

const (
	commandHealth  operatorCommandKind = "health"
	commandStatus  operatorCommandKind = "status"
	commandNext    operatorCommandKind = "next"
	commandBlocked operatorCommandKind = "blocked"
	commandApprove operatorCommandKind = "approve"
)

type operatorCommand struct {
	Kind operatorCommandKind
	Arg  string
}

var forbiddenOperatorVocabulary = map[string]struct{}{
	"command": {},
	"args":    {},
	"cwd":     {},
	"path":    {},
	"prompt":  {},
	"token":   {},
	"secret":  {},
	"session": {},
	"runtime": {},
	"stdout":  {},
	"stderr":  {},
}

var telegramOperatorErrorBackoff = time.Second

func runOperatorLoop(ctx context.Context, cfg operatorConfig, client telegramAPI, op *operator, pollTimeout time.Duration) error {
	timeoutSeconds := int(pollTimeout / time.Second)
	if timeoutSeconds <= 0 {
		timeoutSeconds = 1
	}
	var offset int64
	for {
		if ctx.Err() != nil {
			return nil
		}
		updates, err := client.getUpdates(ctx, cfg.Token, offset, timeoutSeconds)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if err := waitTelegramOperatorBackoff(ctx); err != nil {
				return nil
			}
			continue
		}
		for _, update := range updates {
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
			processTelegramUpdate(ctx, cfg, client, op, update)
		}
	}
}

func waitTelegramOperatorBackoff(ctx context.Context) error {
	timer := time.NewTimer(telegramOperatorErrorBackoff)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func processTelegramUpdate(ctx context.Context, cfg operatorConfig, client telegramAPI, op *operator, update telegramUpdate) {
	message := update.Message
	if message == nil || message.Chat.Type != "private" || message.From == nil {
		return
	}
	if _, ok := cfg.AllowUserIDs[message.From.ID]; !ok {
		return
	}
	text := strings.TrimSpace(message.Text)
	if text == "" {
		return
	}
	replyTo := message.MessageID
	if !strings.HasPrefix(text, "/") {
		if responseText, ok := op.handleExecutionResult(ctx, text); ok {
			_ = client.sendMessage(ctx, cfg.Token, telegramSendMessageRequest{ChatID: message.Chat.ID, Text: responseText, ReplyToMessageID: &replyTo})
			return
		}
		responseText, ok := op.handleInboundAgentText(ctx, cfg, text, *message)
		if !ok {
			return
		}
		_ = client.sendMessage(ctx, cfg.Token, telegramSendMessageRequest{ChatID: message.Chat.ID, Text: responseText, ReplyToMessageID: &replyTo})
		return
	}
	cmd, err := parseOperatorCommand(text)
	responseText := "unsupported command; allowed: /health, /status <workflow_id>, /next <agent_id>, /blocked <agent_id>, /approve <handoff_id>"
	if err == nil {
		responseText = op.handleCommand(ctx, cmd, *message.From)
	}
	_ = client.sendMessage(ctx, cfg.Token, telegramSendMessageRequest{ChatID: message.Chat.ID, Text: responseText, ReplyToMessageID: &replyTo})
}

func (o *operator) handleExecutionResult(ctx context.Context, text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if !strings.Contains(trimmed, "clawside.result") {
		return "", false
	}
	var envelope struct {
		Type           string                      `json:"type"`
		CorrelationID  string                      `json:"correlation_id"`
		Status         string                      `json:"status"`
		Summary        string                      `json:"summary"`
		ArtifactCount  int                         `json:"artifact_count"`
		ReviewDecision orchestrator.ReviewDecision `json:"review_decision"`
	}
	if err := json.Unmarshal([]byte(trimmed), &envelope); err != nil {
		return "invalid execution result", true
	}
	if envelope.Type != "clawside.result" {
		return "", false
	}
	if o.executionResultSink == nil {
		return "invalid execution result", true
	}
	if err := o.executionResultSink.SaveExecutionResult(ctx, swarmdriver.ExecutionResult{
		CorrelationID:  envelope.CorrelationID,
		Status:         swarmdriver.AdapterStatus(envelope.Status),
		Summary:        envelope.Summary,
		ArtifactCount:  envelope.ArtifactCount,
		ReviewDecision: envelope.ReviewDecision,
	}); err != nil {
		return "invalid execution result", true
	}
	return "execution result recorded", true
}

func (o *operator) handleInboundAgentText(ctx context.Context, cfg operatorConfig, text string, message telegramMessage) (string, bool) {
	if o.inboundAgentBridge == nil || strings.TrimSpace(cfg.BotName) == "" || message.From == nil {
		return "", false
	}
	response, err := o.inboundAgentBridge.Run(ctx, telegramInboundAgentInput{
		TargetAgent:      cfg.BotName,
		Text:             text,
		ChatID:           message.Chat.ID,
		FromUserID:       message.From.ID,
		ReplyToMessageID: message.MessageID,
	})
	if err != nil {
		return "operation failed", true
	}
	response = strings.TrimSpace(response)
	if response == "" {
		return "agent returned empty response", true
	}
	return response, true
}

func (o *operator) handleCommand(ctx context.Context, cmd operatorCommand, from telegramUser) string {
	switch cmd.Kind {
	case commandHealth:
		return "ok"
	case commandStatus:
		view, err := o.handlers.HandleWorkflowStatus(ctx, toolserver.WorkflowStatusInput{WorkflowID: cmd.Arg})
		if err != nil {
			return "operation failed"
		}
		return formatWorkflowStatus(view)
	case commandNext:
		out, err := o.handlers.HandleNextWork(ctx, toolserver.WorkQueryInput{AgentID: cmd.Arg, Limit: 5})
		if err != nil {
			return "operation failed"
		}
		return formatNextWork(cmd.Arg, out)
	case commandBlocked:
		out, err := o.handlers.HandleBlockedWork(ctx, toolserver.WorkQueryInput{AgentID: cmd.Arg, Limit: 5})
		if err != nil {
			return "operation failed"
		}
		return formatBlockedWork(cmd.Arg, out)
	case commandApprove:
		out, err := o.handlers.HandleHandoffProgress(ctx, toolserver.HandoffProgressInput{
			Action:    "approve",
			HandoffID: cmd.Arg,
			Actor:     toolserver.ActorRefInput{Type: "user", ID: fmt.Sprintf("telegram:%d", from.ID)},
		})
		if err != nil {
			return "operation failed"
		}
		return fmt.Sprintf("approved %s: state=%s decision=%s", out.Handoff.ID, out.Handoff.State, out.Handoff.ReviewDecision)
	default:
		return "operation failed"
	}
}

func formatWorkflowStatus(view orchestrator.WorkflowView) string {
	return fmt.Sprintf("workflow %s: %s\nhandoffs: %d\ncurrent: %s", view.Workflow.ID, view.Workflow.Status, len(view.Handoffs), view.Workflow.CurrentHandoffID)
}

func formatNextWork(agentID string, out toolserver.NextWorkOutput) string {
	if len(out.Items) == 0 {
		return fmt.Sprintf("next work for %s: none", agentID)
	}
	var builder strings.Builder
	_, _ = fmt.Fprintf(&builder, "next work for %s:", agentID)
	for i, item := range out.Items {
		_, _ = fmt.Fprintf(&builder, "\n%d. %s workflow=%s state=%s kind=%s", i+1, item.Handoff.ID, item.Workflow.ID, item.Handoff.State, item.Handoff.TaskKind)
	}
	return builder.String()
}

func formatBlockedWork(agentID string, out toolserver.BlockedWorkOutput) string {
	if len(out.Items) == 0 {
		return fmt.Sprintf("blocked work for %s: none", agentID)
	}
	var builder strings.Builder
	_, _ = fmt.Fprintf(&builder, "blocked work for %s:", agentID)
	for i, item := range out.Items {
		reason := "unknown"
		dependency := ""
		if len(item.Reasons) > 0 {
			reason = item.Reasons[0].Code
			dependency = item.Reasons[0].DependencyHandoffID
		}
		_, _ = fmt.Fprintf(&builder, "\n%d. %s workflow=%s reason=%s", i+1, item.Handoff.ID, item.Workflow.ID, reason)
		if dependency != "" {
			_, _ = fmt.Fprintf(&builder, " dependency=%s", dependency)
		}
	}
	return builder.String()
}

func parseOperatorCommand(text string) (operatorCommand, error) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return operatorCommand{}, unsupportedOperatorCommandError()
	}
	commandToken := fields[0]
	if !strings.HasPrefix(commandToken, "/") {
		return operatorCommand{}, unsupportedOperatorCommandError()
	}
	if containsForbiddenCommandVocabulary(commandToken) {
		return operatorCommand{}, unsupportedOperatorCommandError()
	}
	for _, field := range fields[1:] {
		if containsForbiddenArgument(field) || strings.Contains(field, "=") {
			return operatorCommand{}, unsupportedOperatorCommandError()
		}
	}

	commandName := strings.TrimPrefix(commandToken, "/")
	if before, _, found := strings.Cut(commandName, "@"); found {
		commandName = before
	}

	switch commandName {
	case "health":
		if len(fields) != 1 {
			return operatorCommand{}, unsupportedOperatorCommandError()
		}
		return operatorCommand{Kind: commandHealth}, nil
	case "status":
		return parseSingleArgCommand(fields, commandStatus)
	case "next":
		return parseSingleArgCommand(fields, commandNext)
	case "blocked":
		return parseSingleArgCommand(fields, commandBlocked)
	case "approve":
		return parseSingleArgCommand(fields, commandApprove)
	default:
		return operatorCommand{}, unsupportedOperatorCommandError()
	}
}

func parseSingleArgCommand(fields []string, kind operatorCommandKind) (operatorCommand, error) {
	if len(fields) != 2 || strings.TrimSpace(fields[1]) == "" {
		return operatorCommand{}, unsupportedOperatorCommandError()
	}
	arg := strings.TrimSpace(fields[1])
	if !isSafeOperatorIdentifier(arg) {
		return operatorCommand{}, unsupportedOperatorCommandError()
	}
	return operatorCommand{Kind: kind, Arg: arg}, nil
}

func isSafeOperatorIdentifier(value string) bool {
	if value == "" || strings.Contains(value, "..") || strings.HasPrefix(value, "~") {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.' || r == ':':
		default:
			return false
		}
	}
	return true
}

func unsupportedOperatorCommandError() error {
	return fmt.Errorf("unsupported command; allowed: /health, /status <workflow_id>, /next <agent_id>, /blocked <agent_id>, /approve <handoff_id>")
}

func containsForbiddenCommandVocabulary(commandToken string) bool {
	commandName := strings.TrimPrefix(strings.ToLower(commandToken), "/")
	if before, _, found := strings.Cut(commandName, "@"); found {
		commandName = before
	}
	for _, segment := range strings.FieldsFunc(commandName, func(r rune) bool {
		return r == '/' || r == '-' || r == '_' || r == '.'
	}) {
		if _, forbidden := forbiddenOperatorVocabulary[segment]; forbidden {
			return true
		}
	}
	return false
}

func containsForbiddenArgument(arg string) bool {
	key := strings.ToLower(strings.TrimSpace(arg))
	if before, _, found := strings.Cut(key, "="); found {
		key = before
	}
	key = strings.Trim(key, " \t\n\r\"'`.,;:()[]{}")
	_, forbidden := forbiddenOperatorVocabulary[key]
	return forbidden
}
