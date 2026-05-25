package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/walker1211/clawside/internal/a2aserver"
	"github.com/walker1211/clawside/internal/orchestrator"
	"github.com/walker1211/clawside/internal/toolserver"

	_ "modernc.org/sqlite"
)

const (
	defaultA2AAddr       = "127.0.0.1:8789"
	a2aAddrEnvName       = "CLAWSIDE_A2A_ADDR"
	a2aPublicURLEnvName  = "CLAWSIDE_A2A_PUBLIC_URL"
	a2aAuthKeyEnvName    = "CLAWSIDE_A2A_AUTH_KEY"
	serverShutdownPeriod = 5 * time.Second
	selfTestCommand      = "self-test"
	selfTestTimeout      = 5 * time.Second
)

type options struct {
	DBPath    string
	Addr      string
	PublicURL string
	AuthKey   string
}

type selfTestOptions struct {
	BaseURL        string
	AuthKey        string
	Timeout        time.Duration
	IdempotencyKey string
	ReceiverID     string
}

type selfTestHealthResponse struct {
	Status string `json:"status"`
}

type selfTestRPCResponse struct {
	JSONRPC string              `json:"jsonrpc"`
	ID      json.RawMessage     `json:"id"`
	Result  json.RawMessage     `json:"result,omitempty"`
	Error   *a2aserver.RPCError `json:"error,omitempty"`
}

type selfTestSSEEvent struct {
	Name  string
	ID    string
	Retry string
	Data  []byte
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 && args[0] == selfTestCommand {
		return runSelfTest(args[1:], stdout, stderr)
	}
	if isHelpRequest(args) {
		return printUsage(stdout)
	}
	return runServer(args, stdout, stderr)
}

func runServer(args []string, stdout, stderr io.Writer) error {
	opts, err := resolveOptions(args)
	if err != nil {
		return err
	}
	if err := ensureDatabaseDirectory(opts.DBPath); err != nil {
		return err
	}

	db, err := sql.Open("sqlite", opts.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store, err := orchestrator.NewStore(ctx, db)
	if err != nil {
		return err
	}
	svc := orchestrator.NewService(store, nil)
	handlers := toolserver.NewHandlers(svc, store, nil)
	server := &http.Server{
		Addr: opts.Addr,
		Handler: a2aserver.NewHandler(handlers, a2aserver.Config{
			PublicURL: opts.PublicURL,
			AuthKey:   opts.AuthKey,
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), serverShutdownPeriod)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	if _, err := fmt.Fprintf(stdout, "clawside A2A compatibility server listening on %s\n", opts.Addr); err != nil {
		return err
	}
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	_ = stderr
	return nil
}

func runSelfTest(args []string, stdout, stderr io.Writer) error {
	if isHelpRequest(args) {
		return printSelfTestUsage(stdout)
	}
	opts, err := resolveSelfTestOptions(args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()
	_ = stderr
	return runA2ASelfTest(ctx, opts, stdout)
}

func resolveOptions(args []string) (options, error) {
	opts := options{
		Addr:      envOrDefault(a2aAddrEnvName, defaultA2AAddr),
		PublicURL: strings.TrimSpace(os.Getenv(a2aPublicURLEnvName)),
	}
	fs := flag.NewFlagSet("clawside-a2a", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.DBPath, "db", "", "sqlite db path")
	fs.StringVar(&opts.Addr, "addr", opts.Addr, "listen address")
	fs.StringVar(&opts.PublicURL, "public-url", opts.PublicURL, "public base URL used in the Agent Card")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	opts.DBPath = strings.TrimSpace(opts.DBPath)
	opts.Addr = strings.TrimSpace(opts.Addr)
	opts.PublicURL = strings.TrimSpace(opts.PublicURL)
	opts.AuthKey = strings.TrimSpace(os.Getenv(a2aAuthKeyEnvName))
	if opts.DBPath == "" {
		return options{}, fmt.Errorf("missing db")
	}
	if opts.Addr == "" {
		return options{}, fmt.Errorf("addr is required")
	}
	if opts.AuthKey == "" {
		return options{}, fmt.Errorf("A2A auth key is required")
	}
	return opts, nil
}

func resolveSelfTestOptions(args []string) (selfTestOptions, error) {
	opts := selfTestOptions{Timeout: selfTestTimeout, ReceiverID: "self-test"}
	fs := flag.NewFlagSet("clawside-a2a self-test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.BaseURL, "base-url", "", "A2A server base URL")
	fs.DurationVar(&opts.Timeout, "timeout", opts.Timeout, "self-test timeout")
	fs.StringVar(&opts.IdempotencyKey, "idempotency-key", "", "controlled task idempotency key")
	fs.StringVar(&opts.ReceiverID, "receiver", opts.ReceiverID, "controlled task receiver id")
	if err := fs.Parse(args); err != nil {
		return selfTestOptions{}, err
	}
	opts.BaseURL = strings.TrimSpace(opts.BaseURL)
	opts.IdempotencyKey = strings.TrimSpace(opts.IdempotencyKey)
	opts.ReceiverID = strings.TrimSpace(opts.ReceiverID)
	if opts.BaseURL == "" {
		return selfTestOptions{}, fmt.Errorf("base-url is required")
	}
	baseURL, err := normalizeSelfTestBaseURL(opts.BaseURL)
	if err != nil {
		return selfTestOptions{}, err
	}
	opts.BaseURL = baseURL
	if opts.Timeout <= 0 {
		return selfTestOptions{}, fmt.Errorf("timeout must be positive")
	}
	if opts.ReceiverID == "" {
		return selfTestOptions{}, fmt.Errorf("receiver is required")
	}
	opts.AuthKey = strings.TrimSpace(os.Getenv(a2aAuthKeyEnvName))
	if opts.AuthKey == "" {
		return selfTestOptions{}, fmt.Errorf("A2A auth key is required")
	}
	if opts.IdempotencyKey == "" {
		opts.IdempotencyKey = fmt.Sprintf("clawside-a2a-self-test-%d", time.Now().UTC().UnixNano())
	}
	return opts, nil
}

func runA2ASelfTest(ctx context.Context, opts selfTestOptions, stdout io.Writer) error {
	client := &http.Client{}
	var health selfTestHealthResponse
	if err := selfTestGetJSON(ctx, client, selfTestEndpoint(opts.BaseURL, "/healthz"), "", &health); err != nil {
		return fmt.Errorf("healthz check: %w", err)
	}
	if health.Status != "ok" {
		return fmt.Errorf("healthz check: unexpected status")
	}
	if err := writeSelfTestLine(stdout, "healthz ok"); err != nil {
		return err
	}

	var card a2aserver.AgentCard
	if err := selfTestGetJSON(ctx, client, selfTestEndpoint(opts.BaseURL, "/.well-known/agent-card.json"), "", &card); err != nil {
		return fmt.Errorf("agent card check: %w", err)
	}
	if err := validateSelfTestAgentCard(card); err != nil {
		return err
	}
	if err := writeSelfTestLine(stdout, "agent_card ok methods=%d", len(card.Metadata.Methods)); err != nil {
		return err
	}

	var created a2aserver.TaskCreateOutput
	if err := selfTestRPC(ctx, client, opts, a2aserver.MethodTaskCreate, a2aserver.TaskCreateInput{
		IdempotencyKey: opts.IdempotencyKey,
		Intent:         "clawside A2A self-test",
		Receiver:       a2aserver.TaskCreateActorInput{ID: opts.ReceiverID},
		ProjectRef:     "project://a2a-self-test",
	}, &created); err != nil {
		return err
	}
	if created.WorkflowID == "" || created.HandoffID == "" || created.Task.ID != created.HandoffID || created.Task.ContextID != created.WorkflowID || created.Task.Status.State != "submitted" {
		return fmt.Errorf("task_create check: unexpected task projection")
	}
	if err := writeSelfTestLine(stdout, "task_create ok workflow_id=%s handoff_id=%s state=%s", created.WorkflowID, created.HandoffID, created.Task.Status.State); err != nil {
		return err
	}

	var task a2aserver.A2ATask
	if err := selfTestRPC(ctx, client, opts, a2aserver.MethodTasksGet, a2aserver.TasksGetInput{ID: created.HandoffID}, &task); err != nil {
		return err
	}
	if task.ID != created.HandoffID || task.ContextID != created.WorkflowID || task.Status.State != "submitted" {
		return fmt.Errorf("tasks_get check: unexpected task projection")
	}
	if err := writeSelfTestLine(stdout, "tasks_get ok handoff_id=%s state=%s", task.ID, task.Status.State); err != nil {
		return err
	}

	reader, closeSSE, err := openSelfTestSSE(ctx, client, opts, created.HandoffID)
	if err != nil {
		return err
	}
	defer closeSSE()
	initial, err := readSelfTestSSEEvent(reader)
	if err != nil {
		return fmt.Errorf("tasks_events check: read initial event: %w", err)
	}
	initialPayload, err := validateSelfTestTaskEvent(initial, created.WorkflowID, created.HandoffID, "submitted", true)
	if err != nil {
		return fmt.Errorf("tasks_events check: %w", err)
	}

	var canceled a2aserver.A2ATask
	if err := selfTestRPC(ctx, client, opts, a2aserver.MethodTasksCancel, a2aserver.TasksCancelInput{ID: created.HandoffID}, &canceled); err != nil {
		return err
	}
	if canceled.ID != created.HandoffID || canceled.Status.State != "failed" {
		return fmt.Errorf("tasks_cancel check: unexpected task projection")
	}
	if err := writeSelfTestLine(stdout, "tasks_cancel ok handoff_id=%s state=%s", canceled.ID, canceled.Status.State); err != nil {
		return err
	}

	changed, err := readSelfTestSSEEvent(reader)
	if err != nil {
		return fmt.Errorf("tasks_events check: read changed event: %w", err)
	}
	changedPayload, err := validateSelfTestTaskEvent(changed, created.WorkflowID, created.HandoffID, "failed", false)
	if err != nil {
		return fmt.Errorf("tasks_events check: %w", err)
	}
	if initialPayload.EventID == changedPayload.EventID {
		return fmt.Errorf("tasks_events check: expected changed event cursor")
	}
	if err := writeSelfTestLine(stdout, "tasks_events ok handoff_id=%s initial=%s changed=%s retry=%s", created.HandoffID, initialPayload.Task.Status.State, changedPayload.Task.Status.State, initial.Retry); err != nil {
		return err
	}
	return writeSelfTestLine(stdout, "self-test ok")
}

func selfTestGetJSON(ctx context.Context, client *http.Client, endpoint, authKey string, output any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
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
	decoder := json.NewDecoder(response.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	return nil
}

func selfTestRPC(ctx context.Context, client *http.Client, opts selfTestOptions, method string, params any, output any) error {
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      "self-test",
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, selfTestEndpoint(opts.BaseURL, "/a2a/rpc"), bytes.NewReader(payload))
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
	var rpcResponse selfTestRPCResponse
	if err := json.NewDecoder(response.Body).Decode(&rpcResponse); err != nil {
		return err
	}
	if rpcResponse.Error != nil {
		return fmt.Errorf("%s check: JSON-RPC error %d %s", method, rpcResponse.Error.Code, rpcResponse.Error.Message)
	}
	if len(rpcResponse.Result) == 0 {
		return fmt.Errorf("%s check: missing result", method)
	}
	if err := json.Unmarshal(rpcResponse.Result, output); err != nil {
		return err
	}
	return nil
}

func openSelfTestSSE(ctx context.Context, client *http.Client, opts selfTestOptions, handoffID string) (*bufio.Reader, func(), error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, selfTestEndpoint(opts.BaseURL, "/a2a/tasks/"+url.PathEscape(handoffID)+"/events?historyLength=1&pollIntervalMs=250"), nil)
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

func readSelfTestSSEEvent(reader *bufio.Reader) (selfTestSSEEvent, error) {
	var event selfTestSSEEvent
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return selfTestSSEEvent{}, err
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

func validateSelfTestTaskEvent(event selfTestSSEEvent, workflowID, handoffID, state string, requireRetry bool) (a2aserver.TaskStreamEvent, error) {
	if event.Name != "task" || event.ID == "" {
		return a2aserver.TaskStreamEvent{}, fmt.Errorf("unexpected SSE event")
	}
	if requireRetry && event.Retry != "3000" {
		return a2aserver.TaskStreamEvent{}, fmt.Errorf("unexpected SSE retry")
	}
	var payload a2aserver.TaskStreamEvent
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return a2aserver.TaskStreamEvent{}, err
	}
	if payload.HandoffID != handoffID || payload.WorkflowID != workflowID || payload.EventID != event.ID {
		return a2aserver.TaskStreamEvent{}, fmt.Errorf("unexpected SSE ids")
	}
	if payload.Task.ID != handoffID || payload.Task.ContextID != workflowID || payload.Task.Status.State != state {
		return a2aserver.TaskStreamEvent{}, fmt.Errorf("unexpected SSE task projection")
	}
	return payload, nil
}

func validateSelfTestAgentCard(card a2aserver.AgentCard) error {
	if card.Name != "clawside-coordination" || card.Metadata.Endpoints.JSONRPC != "/a2a/rpc" || card.Metadata.Endpoints.TaskEvents != "/a2a/tasks/{handoffID}/events" {
		return fmt.Errorf("agent card check: unexpected metadata")
	}
	if !card.Capabilities.Streaming || card.Capabilities.PushNotifications {
		return fmt.Errorf("agent card check: unexpected capabilities")
	}
	skills := map[string]bool{}
	for _, skill := range card.Skills {
		skills[skill.ID] = true
	}
	for _, method := range requiredSelfTestMethods() {
		if !skills[method] {
			return fmt.Errorf("agent card check: missing method %s", method)
		}
	}
	for _, method := range []string{
		"message/send",
		"message/stream",
		"tasks.cancel",
		"tasks/pushNotification/set",
		"tasks/pushNotification/get",
		"handoff_create",
		"runtime/session/start",
		"runtime/session/create",
		"sandbox/launch",
		"worker/launch",
		"sender/deliver",
		"telegram/send",
	} {
		if skills[method] {
			return fmt.Errorf("agent card check: advertised unsupported method %s", method)
		}
	}
	return nil
}

func requiredSelfTestMethods() []string {
	return []string{
		a2aserver.MethodWorkflowList,
		a2aserver.MethodWorkflowStatus,
		a2aserver.MethodHandoffGet,
		a2aserver.MethodAgentList,
		a2aserver.MethodNextWork,
		a2aserver.MethodBlockedWork,
		a2aserver.MethodTaskCreate,
		a2aserver.MethodTasksGet,
		a2aserver.MethodTasksCancel,
		a2aserver.MethodTasksEvents,
	}
}

func normalizeSelfTestBaseURL(raw string) (string, error) {
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

func selfTestEndpoint(baseURL, path string) string {
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

func writeSelfTestLine(stdout io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(stdout, format+"\n", args...)
	return err
}

func envOrDefault(name string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func isHelpRequest(args []string) bool {
	return len(args) == 1 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h")
}

func printUsage(stdout io.Writer) error {
	_, err := fmt.Fprintf(stdout, `usage: clawside-a2a [options]
       clawside-a2a self-test [options]

Starts the experimental Clawside A2A compatibility endpoint.

Options:
  --db PATH             SQLite database path (required)
  --addr ADDR           listen address (default: %s or %s)
  --public-url URL      public base URL used in the Agent Card
  help, --help, -h      show this help

Commands:
  self-test             validate a running A2A endpoint as an external client

Environment:
  %s      A2A bearer auth key (required)
`, defaultA2AAddr, a2aAddrEnvName, a2aAuthKeyEnvName)
	return err
}

func printSelfTestUsage(stdout io.Writer) error {
	_, err := fmt.Fprintf(stdout, `usage: clawside-a2a self-test [options]

Validates a running Clawside A2A compatibility endpoint as an external client.
The check creates and cancels one controlled inbound task in the truth-plane only.

Options:
  --base-url URL          A2A server base URL, for example http://127.0.0.1:8789 (required)
  --timeout DURATION      self-test timeout (default: %s)
  --idempotency-key KEY   controlled task idempotency key (default: generated unique key)
  --receiver ID           controlled task receiver id (default: self-test)
  help, --help, -h        show this help

Environment:
  %s      A2A bearer auth key (required)
`, selfTestTimeout, a2aAuthKeyEnvName)
	return err
}

func ensureDatabaseDirectory(path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	return nil
}
