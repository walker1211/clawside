package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	a2aAuthKeyEnvName = "CLAWSIDE_A2A_AUTH_KEY"
	exampleTimeout    = 5 * time.Second
)

type options struct {
	BaseURL        string
	AuthKey        string
	Timeout        time.Duration
	IdempotencyKey string
	ReceiverID     string
	Cancel         bool
}

type agentCard struct {
	Name         string `json:"name"`
	Capabilities struct {
		Streaming         bool `json:"streaming"`
		PushNotifications bool `json:"pushNotifications"`
	} `json:"capabilities"`
	Skills []struct {
		ID string `json:"id"`
	} `json:"skills"`
	Metadata struct {
		Methods []struct {
			ID string `json:"id"`
		} `json:"methods"`
		Endpoints struct {
			JSONRPC    string `json:"jsonrpc"`
			TaskEvents string `json:"taskEvents"`
		} `json:"endpoints"`
	} `json:"metadata"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int           `json:"code"`
	Message string        `json:"message"`
	Data    *rpcErrorData `json:"data,omitempty"`
}

type rpcErrorData struct {
	Code string `json:"code,omitempty"`
}

type taskCreateOutput struct {
	Task       a2aTask `json:"task"`
	WorkflowID string  `json:"workflowId"`
	HandoffID  string  `json:"handoffId"`
}

type a2aTask struct {
	ID        string        `json:"id"`
	ContextID string        `json:"contextId,omitempty"`
	Status    a2aTaskStatus `json:"status"`
}

type a2aTaskStatus struct {
	State string `json:"state"`
}

type sseEvent struct {
	Name  string
	ID    string
	Retry string
	Data  []byte
}

type taskStreamEvent struct {
	Task       a2aTask `json:"task"`
	HandoffID  string  `json:"handoffId"`
	WorkflowID string  `json:"workflowId"`
	EventID    string  `json:"eventId,omitempty"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if isHelpRequest(args) {
		return printUsage(stdout)
	}
	opts, err := resolveOptions(args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()
	_ = stderr
	return runExample(ctx, opts, stdout)
}

func resolveOptions(args []string) (options, error) {
	opts := options{Timeout: exampleTimeout, ReceiverID: "example-client", Cancel: true}
	fs := flag.NewFlagSet("clawside-a2a-example", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.BaseURL, "base-url", "", "A2A server base URL")
	fs.DurationVar(&opts.Timeout, "timeout", opts.Timeout, "example timeout")
	fs.StringVar(&opts.IdempotencyKey, "idempotency-key", "", "controlled task idempotency key")
	fs.StringVar(&opts.ReceiverID, "receiver", opts.ReceiverID, "controlled task receiver id")
	fs.BoolVar(&opts.Cancel, "cancel", opts.Cancel, "cancel the created task before exiting")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	opts.BaseURL = strings.TrimSpace(opts.BaseURL)
	opts.IdempotencyKey = strings.TrimSpace(opts.IdempotencyKey)
	opts.ReceiverID = strings.TrimSpace(opts.ReceiverID)
	if opts.BaseURL == "" {
		return options{}, fmt.Errorf("base_url check: base-url is required")
	}
	baseURL, err := normalizeBaseURL(opts.BaseURL)
	if err != nil {
		return options{}, fmt.Errorf("base_url check: %w", err)
	}
	opts.BaseURL = baseURL
	if opts.Timeout <= 0 {
		return options{}, fmt.Errorf("timeout must be positive")
	}
	if opts.ReceiverID == "" {
		return options{}, fmt.Errorf("receiver is required")
	}
	opts.AuthKey = strings.TrimSpace(os.Getenv(a2aAuthKeyEnvName))
	if opts.AuthKey == "" {
		return options{}, fmt.Errorf("auth check: %s is required", a2aAuthKeyEnvName)
	}
	if opts.IdempotencyKey == "" {
		opts.IdempotencyKey = fmt.Sprintf("clawside-a2a-example-%d", time.Now().UTC().UnixNano())
	}
	return opts, nil
}

func runExample(ctx context.Context, opts options, stdout io.Writer) error {
	client := &http.Client{}
	var card agentCard
	if err := getJSON(ctx, client, endpoint(opts.BaseURL, "/.well-known/agent-card.json"), "", &card); err != nil {
		return err
	}
	if err := validateAgentCard(card); err != nil {
		return err
	}
	if err := writeLine(stdout, "agent_card ok methods=%d streaming=%t", len(card.Metadata.Methods), card.Capabilities.Streaming); err != nil {
		return err
	}

	var created taskCreateOutput
	if err := rpc(ctx, client, opts, "clawside.task.create", map[string]any{
		"idempotency_key": opts.IdempotencyKey,
		"intent":          "clawside A2A external example",
		"receiver":        map[string]any{"id": opts.ReceiverID},
		"project_ref":     "project://a2a-example",
	}, &created); err != nil {
		return err
	}
	if created.WorkflowID == "" || created.HandoffID == "" || created.Task.ID != created.HandoffID || created.Task.ContextID != created.WorkflowID || created.Task.Status.State != "submitted" {
		return fmt.Errorf("task_create check: unexpected task projection")
	}
	if err := writeLine(stdout, "task_create ok workflow_id=%s handoff_id=%s state=%s", created.WorkflowID, created.HandoffID, created.Task.Status.State); err != nil {
		return err
	}

	var task a2aTask
	if err := rpc(ctx, client, opts, "tasks/get", map[string]any{"id": created.HandoffID}, &task); err != nil {
		return err
	}
	if task.ID != created.HandoffID || task.ContextID != created.WorkflowID || task.Status.State != "submitted" {
		return fmt.Errorf("tasks_get check: unexpected task projection")
	}
	if err := writeLine(stdout, "tasks_get ok handoff_id=%s state=%s", task.ID, task.Status.State); err != nil {
		return err
	}

	reader, closeSSE, err := openSSE(ctx, client, opts, created.HandoffID)
	if err != nil {
		return err
	}
	defer closeSSE()
	initial, err := readLabeledSSEEvent(ctx, reader, closeSSE, "initial")
	if err != nil {
		return err
	}
	initialPayload, err := validateTaskEvent(initial, created.WorkflowID, created.HandoffID, "submitted", true)
	if err != nil {
		return err
	}
	if err := writeLine(stdout, "tasks_events ok handoff_id=%s state=%s retry=%s", created.HandoffID, initialPayload.Task.Status.State, initial.Retry); err != nil {
		return err
	}
	if !opts.Cancel {
		return writeLine(stdout, "example ok")
	}

	var canceled a2aTask
	if err := rpc(ctx, client, opts, "tasks/cancel", map[string]any{"id": created.HandoffID}, &canceled); err != nil {
		return err
	}
	if canceled.ID != created.HandoffID || canceled.Status.State != "failed" {
		return fmt.Errorf("tasks_cancel check: unexpected task projection")
	}
	if err := writeLine(stdout, "tasks_cancel ok handoff_id=%s state=%s", canceled.ID, canceled.Status.State); err != nil {
		return err
	}

	changed, err := readLabeledSSEEvent(ctx, reader, closeSSE, "changed")
	if err != nil {
		return err
	}
	changedPayload, err := validateTaskEvent(changed, created.WorkflowID, created.HandoffID, "failed", false)
	if err != nil {
		return err
	}
	if initialPayload.EventID == changedPayload.EventID {
		return fmt.Errorf("sse check: malformed event: expected changed event cursor")
	}
	if err := writeLine(stdout, "tasks_events ok handoff_id=%s state=%s", created.HandoffID, changedPayload.Task.Status.State); err != nil {
		return err
	}
	return writeLine(stdout, "example ok")
}

func getJSON(ctx context.Context, client *http.Client, rawURL string, authKey string, output any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("base_url/connectivity check: agent_card request build failed: %w", err)
	}
	if authKey != "" {
		request.Header.Set("Authorization", "Bearer "+authKey)
	}
	response, err := client.Do(request)
	if err != nil {
		return connectivityError(ctx, "agent_card request", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return httpStatusError("agent_card", response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(output); err != nil {
		return fmt.Errorf("agent_card check: malformed JSON response: %w", err)
	}
	return nil
}

func rpc(ctx context.Context, client *http.Client, opts options, method string, params any, output any) error {
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      "example",
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return fmt.Errorf("rpc check: %s request encode failed: %w", method, err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(opts.BaseURL, "/a2a/rpc"), bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("rpc check: %s request build failed: %w", method, err)
	}
	request.Header.Set("Authorization", "Bearer "+opts.AuthKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return connectivityError(ctx, "rpc request", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return rpcHTTPStatusError(method, response.StatusCode)
	}
	var rpcResponse rpcResponse
	if err := json.NewDecoder(response.Body).Decode(&rpcResponse); err != nil {
		return fmt.Errorf("rpc check: %s returned malformed JSON response: %w", method, err)
	}
	if rpcResponse.Error != nil {
		return rpcRemoteError(method, rpcResponse.Error)
	}
	if len(rpcResponse.Result) == 0 {
		return fmt.Errorf("rpc check: %s missing result", method)
	}
	if err := json.Unmarshal(rpcResponse.Result, output); err != nil {
		return fmt.Errorf("rpc check: %s returned malformed result: %w", method, err)
	}
	return nil
}

func openSSE(ctx context.Context, client *http.Client, opts options, handoffID string) (*bufio.Reader, func(), error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint(opts.BaseURL, "/a2a/tasks/"+url.PathEscape(handoffID)+"/events?historyLength=1&pollIntervalMs=250"), nil)
	if err != nil {
		return nil, nil, fmt.Errorf("sse check: request build failed: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+opts.AuthKey)
	request.Header.Set("Accept", "text/event-stream")
	response, err := client.Do(request)
	if err != nil {
		return nil, nil, connectivityError(ctx, "sse request", err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, nil, httpStatusError("sse", response.StatusCode)
	}
	if !strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		response.Body.Close()
		return nil, nil, fmt.Errorf("sse check: unexpected content type")
	}
	return bufio.NewReader(response.Body), func() { _ = response.Body.Close() }, nil
}

func readLabeledSSEEvent(ctx context.Context, reader *bufio.Reader, closeSSE func(), label string) (sseEvent, error) {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			closeSSE()
		case <-done:
		}
	}()
	defer close(done)

	event, err := readSSEEvent(reader)
	if err != nil {
		if ctx.Err() != nil {
			return sseEvent{}, fmt.Errorf("sse check: timeout waiting for %s task event", label)
		}
		return sseEvent{}, fmt.Errorf("sse check: read %s task event failed: %w", label, err)
	}
	return event, nil
}

func readSSEEvent(reader *bufio.Reader) (sseEvent, error) {
	var event sseEvent
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return sseEvent{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if event.Name != "" || event.ID != "" || len(event.Data) > 0 {
				return event, nil
			}
			continue
		}
		if value, ok := strings.CutPrefix(line, "event: "); ok {
			event.Name = value
			continue
		}
		if value, ok := strings.CutPrefix(line, "id: "); ok {
			event.ID = value
			continue
		}
		if value, ok := strings.CutPrefix(line, "retry: "); ok {
			event.Retry = value
			continue
		}
		if value, ok := strings.CutPrefix(line, "data: "); ok {
			event.Data = []byte(value)
		}
	}
}

func validateTaskEvent(event sseEvent, workflowID, handoffID, state string, requireRetry bool) (taskStreamEvent, error) {
	if event.Name != "task" {
		return taskStreamEvent{}, fmt.Errorf("sse check: malformed event: expected event name task")
	}
	if event.ID == "" {
		return taskStreamEvent{}, fmt.Errorf("sse check: malformed event: missing event id")
	}
	if len(event.Data) == 0 {
		return taskStreamEvent{}, fmt.Errorf("sse check: malformed event: missing data")
	}
	if requireRetry && event.Retry != "3000" {
		return taskStreamEvent{}, fmt.Errorf("sse check: malformed event: unexpected retry")
	}
	var payload taskStreamEvent
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return taskStreamEvent{}, fmt.Errorf("sse check: malformed event: data is not valid JSON: %w", err)
	}
	if payload.HandoffID != handoffID || payload.WorkflowID != workflowID || payload.EventID != event.ID {
		return taskStreamEvent{}, fmt.Errorf("sse check: malformed event: unexpected ids")
	}
	if payload.Task.ID != handoffID || payload.Task.ContextID != workflowID || payload.Task.Status.State != state {
		return taskStreamEvent{}, fmt.Errorf("sse check: malformed event: unexpected task projection")
	}
	return payload, nil
}

func validateAgentCard(card agentCard) error {
	if card.Name != "clawside-coordination" {
		return fmt.Errorf("agent_card check: unsupported metadata: unexpected name")
	}
	if card.Metadata.Endpoints.JSONRPC != "/a2a/rpc" {
		return fmt.Errorf("agent_card check: unsupported metadata: jsonrpc endpoint must be /a2a/rpc")
	}
	if card.Metadata.Endpoints.TaskEvents != "/a2a/tasks/{handoffID}/events" {
		return fmt.Errorf("agent_card check: unsupported metadata: taskEvents endpoint must be /a2a/tasks/{handoffID}/events")
	}
	if !card.Capabilities.Streaming {
		return fmt.Errorf("agent_card check: unsupported metadata: streaming capability is required")
	}
	if card.Capabilities.PushNotifications {
		return fmt.Errorf("agent_card check: unsupported metadata: push notifications are not supported")
	}
	skills := map[string]bool{}
	for _, skill := range card.Skills {
		skills[skill.ID] = true
	}
	methods := map[string]bool{}
	for _, method := range card.Metadata.Methods {
		methods[method.ID] = true
	}
	for _, method := range requiredMethods() {
		if !skills[method] {
			return fmt.Errorf("agent_card check: unsupported metadata: missing skill %s", method)
		}
		if !methods[method] {
			return fmt.Errorf("agent_card check: unsupported metadata: missing method metadata %s", method)
		}
	}
	allowedMethods := supportedMethods()
	for method := range skills {
		if !allowedMethods[method] {
			return fmt.Errorf("agent_card check: unsupported metadata: advertised unsupported method %s", method)
		}
	}
	for method := range methods {
		if !allowedMethods[method] {
			return fmt.Errorf("agent_card check: unsupported metadata: advertised unsupported method %s", method)
		}
	}
	return nil
}

func connectivityError(ctx context.Context, operation string, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("base_url/connectivity check: %s timed out", operation)
	}
	return fmt.Errorf("base_url/connectivity check: %s failed: %w", operation, err)
}

func httpStatusError(scope string, statusCode int) error {
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		return fmt.Errorf("auth check: server rejected bearer auth with HTTP %d", statusCode)
	}
	return fmt.Errorf("%s check: unexpected HTTP status %d", scope, statusCode)
}

func rpcHTTPStatusError(method string, statusCode int) error {
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		return fmt.Errorf("auth check: server rejected bearer auth with HTTP %d while calling %s", statusCode, method)
	}
	return fmt.Errorf("rpc check: %s returned unexpected HTTP status %d", method, statusCode)
}

func rpcRemoteError(method string, rpcErr *rpcError) error {
	if rpcErr.Data != nil && rpcErr.Data.Code != "" {
		return fmt.Errorf("rpc check: %s returned JSON-RPC error code=%d data=%s", method, rpcErr.Code, rpcErr.Data.Code)
	}
	return fmt.Errorf("rpc check: %s returned JSON-RPC error code=%d", method, rpcErr.Code)
}

func supportedMethods() map[string]bool {
	methods := map[string]bool{}
	for _, method := range []string{
		"clawside.workflow.list",
		"clawside.workflow.status",
		"clawside.handoff.get",
		"clawside.agent.list",
		"clawside.work.next",
		"clawside.work.blocked",
		"clawside.task.create",
		"tasks/get",
		"tasks/cancel",
		"tasks/events",
	} {
		methods[method] = true
	}
	return methods
}

func requiredMethods() []string {
	return []string{"clawside.task.create", "tasks/get", "tasks/cancel", "tasks/events"}
}

func normalizeBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid base-url")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" {
		return "", fmt.Errorf("invalid base-url")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func endpoint(baseURL, path string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return baseURL + path
	}
	endpoint, err := url.Parse(path)
	if err != nil {
		parsed.Path = strings.TrimRight(parsed.Path, "/") + path
		return parsed.String()
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + endpoint.Path
	parsed.RawQuery = endpoint.RawQuery
	return parsed.String()
}

func writeLine(stdout io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(stdout, format+"\n", args...)
	return err
}

func isHelpRequest(args []string) bool {
	return len(args) == 1 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h")
}

func printUsage(stdout io.Writer) error {
	_, err := fmt.Fprintf(stdout, `usage: clawside-a2a-example [options]

Runs a minimal external-client-shaped example against a Clawside A2A endpoint.
The example creates one controlled truth-plane task, reads it, subscribes to SSE, and cancels it by default.

Options:
  --base-url URL          A2A server base URL, for example http://127.0.0.1:8789 (required)
  --timeout DURATION      example timeout (default: %s)
  --idempotency-key KEY   controlled task idempotency key (default: generated unique key)
  --receiver ID           controlled task receiver id (default: example-client)
  --cancel                cancel the created task before exiting (default: true)
  help, --help, -h        show this help

Environment:
  %s      A2A bearer auth key (required)
`, exampleTimeout, a2aAuthKeyEnvName)
	return err
}
