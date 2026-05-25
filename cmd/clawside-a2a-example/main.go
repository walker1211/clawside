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
	Code    int    `json:"code"`
	Message string `json:"message"`
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
		return options{}, fmt.Errorf("base-url is required")
	}
	baseURL, err := normalizeBaseURL(opts.BaseURL)
	if err != nil {
		return options{}, err
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
		return options{}, fmt.Errorf("A2A auth key is required")
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
		return fmt.Errorf("agent_card check: %w", err)
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
	initial, err := readSSEEvent(reader)
	if err != nil {
		return fmt.Errorf("tasks_events check: read initial event: %w", err)
	}
	initialPayload, err := validateTaskEvent(initial, created.WorkflowID, created.HandoffID, "submitted", true)
	if err != nil {
		return fmt.Errorf("tasks_events check: %w", err)
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

	changed, err := readSSEEvent(reader)
	if err != nil {
		return fmt.Errorf("tasks_events check: read changed event: %w", err)
	}
	changedPayload, err := validateTaskEvent(changed, created.WorkflowID, created.HandoffID, "failed", false)
	if err != nil {
		return fmt.Errorf("tasks_events check: %w", err)
	}
	if initialPayload.EventID == changedPayload.EventID {
		return fmt.Errorf("tasks_events check: expected changed event cursor")
	}
	if err := writeLine(stdout, "tasks_events ok handoff_id=%s state=%s", created.HandoffID, changedPayload.Task.Status.State); err != nil {
		return err
	}
	return writeLine(stdout, "example ok")
}

func getJSON(ctx context.Context, client *http.Client, url string, authKey string, output any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if authKey != "" {
		request.Header.Set("Authorization", "Bearer "+authKey)
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status %d", response.StatusCode)
	}
	return json.NewDecoder(response.Body).Decode(output)
}

func rpc(ctx context.Context, client *http.Client, opts options, method string, params any, output any) error {
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      "example",
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(opts.BaseURL, "/a2a/rpc"), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+opts.AuthKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s check: unexpected HTTP status %d", method, response.StatusCode)
	}
	var rpcResponse rpcResponse
	if err := json.NewDecoder(response.Body).Decode(&rpcResponse); err != nil {
		return err
	}
	if rpcResponse.Error != nil {
		return fmt.Errorf("%s check: JSON-RPC error %d %s", method, rpcResponse.Error.Code, rpcResponse.Error.Message)
	}
	if len(rpcResponse.Result) == 0 {
		return fmt.Errorf("%s check: missing result", method)
	}
	return json.Unmarshal(rpcResponse.Result, output)
}

func openSSE(ctx context.Context, client *http.Client, opts options, handoffID string) (*bufio.Reader, func(), error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint(opts.BaseURL, "/a2a/tasks/"+url.PathEscape(handoffID)+"/events?historyLength=1&pollIntervalMs=250"), nil)
	if err != nil {
		return nil, nil, err
	}
	request.Header.Set("Authorization", "Bearer "+opts.AuthKey)
	request.Header.Set("Accept", "text/event-stream")
	response, err := client.Do(request)
	if err != nil {
		return nil, nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, nil, fmt.Errorf("tasks_events check: unexpected HTTP status %d", response.StatusCode)
	}
	if !strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		response.Body.Close()
		return nil, nil, fmt.Errorf("tasks_events check: unexpected content type")
	}
	return bufio.NewReader(response.Body), func() { _ = response.Body.Close() }, nil
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
	if event.Name != "task" || event.ID == "" {
		return taskStreamEvent{}, fmt.Errorf("unexpected SSE event")
	}
	if requireRetry && event.Retry != "3000" {
		return taskStreamEvent{}, fmt.Errorf("unexpected SSE retry")
	}
	var payload taskStreamEvent
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return taskStreamEvent{}, err
	}
	if payload.HandoffID != handoffID || payload.WorkflowID != workflowID || payload.EventID != event.ID {
		return taskStreamEvent{}, fmt.Errorf("unexpected SSE ids")
	}
	if payload.Task.ID != handoffID || payload.Task.ContextID != workflowID || payload.Task.Status.State != state {
		return taskStreamEvent{}, fmt.Errorf("unexpected SSE task projection")
	}
	return payload, nil
}

func validateAgentCard(card agentCard) error {
	if card.Name != "clawside-coordination" || card.Metadata.Endpoints.JSONRPC != "/a2a/rpc" || card.Metadata.Endpoints.TaskEvents != "/a2a/tasks/{handoffID}/events" {
		return fmt.Errorf("agent_card check: unexpected metadata")
	}
	if !card.Capabilities.Streaming || card.Capabilities.PushNotifications {
		return fmt.Errorf("agent_card check: unexpected capabilities")
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
		if !skills[method] || !methods[method] {
			return fmt.Errorf("agent_card check: missing method %s", method)
		}
	}
	for _, method := range []string{"message/send", "message/stream", "tasks/pushNotification/set", "tasks/pushNotification/get", "handoff_create"} {
		if skills[method] || methods[method] {
			return fmt.Errorf("agent_card check: advertised unsupported method %s", method)
		}
	}
	return nil
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
