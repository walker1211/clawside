package a2aserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/walker1211/clawside/internal/orchestrator"
	"github.com/walker1211/clawside/internal/toolserver"
)

const rpcBodyLimitBytes = 64 << 10

const (
	rpcErrorDataParseError     = "parse_error"
	rpcErrorDataInvalidRequest = "invalid_request"
	rpcErrorDataMethodNotFound = "method_not_found"
	rpcErrorDataInvalidParams  = "invalid_params"
	rpcErrorDataNotFound       = "not_found"
	rpcErrorDataInternalError  = "internal_error"
)

var errInvalidTasksCancel = errors.New("invalid tasks/cancel")

type Handler struct {
	handlers *toolserver.Handlers
	cfg      Config
}

type httpErrorResponse struct {
	Error string `json:"error"`
}

type healthResponse struct {
	Status string `json:"status"`
}

func NewHandler(handlers *toolserver.Handlers, cfg Config) http.Handler {
	cfg.PublicURL = strings.TrimSpace(cfg.PublicURL)
	cfg.AuthKey = strings.TrimSpace(cfg.AuthKey)
	return &Handler{handlers: handlers, cfg: cfg}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/healthz":
		h.handleHealthz(w, r)
	case r.URL.Path == "/.well-known/agent-card.json":
		h.handleAgentCard(w, r)
	case r.URL.Path == "/a2a/rpc":
		h.handleRPC(w, r)
	case isTaskEventsPath(r.URL.Path):
		h.handleTaskEvents(w, r)
	default:
		writeJSON(w, http.StatusNotFound, httpErrorResponse{Error: "not found"})
	}
}

func (h *Handler) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, httpErrorResponse{Error: "method not allowed"})
		return
	}
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}

func (h *Handler) handleAgentCard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, httpErrorResponse{Error: "method not allowed"})
		return
	}
	writeJSON(w, http.StatusOK, buildAgentCard(h.rpcURL(r)))
}

func (h *Handler) handleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, httpErrorResponse{Error: "method not allowed"})
		return
	}
	if !h.authorized(r.Header.Get("Authorization")) {
		if strings.TrimSpace(r.Header.Get("Authorization")) == "" {
			writeJSON(w, http.StatusUnauthorized, httpErrorResponse{Error: "missing authorization"})
			return
		}
		writeJSON(w, http.StatusForbidden, httpErrorResponse{Error: "invalid authorization"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, rpcBodyLimitBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeRPCError(w, nil, rpcInvalidRequest, "invalid request")
		return
	}
	trimmedBody := bytes.TrimSpace(body)
	if len(trimmedBody) == 0 {
		writeRPCError(w, nil, rpcInvalidRequest, "invalid request")
		return
	}
	if trimmedBody[0] == '[' {
		writeRPCError(w, nil, rpcInvalidRequest, "batch requests are not supported")
		return
	}
	if !json.Valid(trimmedBody) {
		writeRPCError(w, nil, rpcParseError, "parse error")
		return
	}

	idPresent, idValid := inspectRPCID(trimmedBody)
	if !idValid {
		writeRPCError(w, nil, rpcInvalidRequest, "invalid request")
		return
	}

	decoder := json.NewDecoder(bytes.NewReader(trimmedBody))
	decoder.DisallowUnknownFields()
	var request RPCRequest
	if err := decoder.Decode(&request); err != nil {
		if !idPresent {
			writeNoRPCResponse(w)
			return
		}
		writeRPCError(w, nil, rpcInvalidRequest, "invalid request")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if !idPresent {
			writeNoRPCResponse(w)
			return
		}
		writeRPCError(w, request.ID, rpcInvalidRequest, "invalid request")
		return
	}
	if request.JSONRPC != "2.0" || strings.TrimSpace(request.Method) == "" {
		if !idPresent {
			writeNoRPCResponse(w)
			return
		}
		writeRPCError(w, request.ID, rpcInvalidRequest, "invalid request")
		return
	}

	result, rpcErr := h.dispatchRPC(r, request.Method, request.Params)
	if !idPresent {
		writeNoRPCResponse(w)
		return
	}
	if rpcErr != nil {
		writeRPCErrorObject(w, request.ID, rpcErr)
		return
	}
	writeRPCResult(w, request.ID, result)
}

func (h *Handler) dispatchRPC(r *http.Request, method string, params json.RawMessage) (any, *RPCError) {
	switch method {
	case MethodWorkflowList:
		if err := decodeRPCParams(params, &struct{}{}); err != nil {
			return nil, &RPCError{Code: rpcInvalidParams, Message: "invalid params"}
		}
		workflows, err := h.handlers.HandleWorkflowList(r.Context())
		if err != nil {
			return nil, &RPCError{Code: rpcInternalError, Message: "internal error"}
		}
		return toolserver.WorkflowListOutput{Workflows: workflows}, nil
	case MethodWorkflowStatus:
		var input toolserver.WorkflowStatusInput
		if err := decodeRPCParams(params, &input); err != nil {
			return nil, &RPCError{Code: rpcInvalidParams, Message: "invalid params"}
		}
		result, err := h.handlers.HandleWorkflowStatus(r.Context(), input)
		if err != nil {
			return nil, rpcErrorFromLookup(err)
		}
		return result, nil
	case MethodHandoffGet:
		var input toolserver.HandoffGetInput
		if err := decodeRPCParams(params, &input); err != nil {
			return nil, &RPCError{Code: rpcInvalidParams, Message: "invalid params"}
		}
		result, err := h.handlers.HandleHandoffGet(r.Context(), input)
		if err != nil {
			return nil, rpcErrorFromLookup(err)
		}
		return result, nil
	case MethodAgentList:
		var input toolserver.AgentListInput
		if err := decodeRPCParams(params, &input); err != nil {
			return nil, &RPCError{Code: rpcInvalidParams, Message: "invalid params"}
		}
		result, err := h.handlers.HandleAgentList(r.Context(), input)
		if err != nil {
			return nil, &RPCError{Code: rpcInternalError, Message: "internal error"}
		}
		return result, nil
	case MethodNextWork:
		var input toolserver.WorkQueryInput
		if err := decodeRPCParams(params, &input); err != nil {
			return nil, &RPCError{Code: rpcInvalidParams, Message: "invalid params"}
		}
		result, err := h.handlers.HandleNextWork(r.Context(), input)
		if err != nil {
			return nil, &RPCError{Code: rpcInternalError, Message: "internal error"}
		}
		return result, nil
	case MethodBlockedWork:
		var input toolserver.WorkQueryInput
		if err := decodeRPCParams(params, &input); err != nil {
			return nil, &RPCError{Code: rpcInvalidParams, Message: "invalid params"}
		}
		result, err := h.handlers.HandleBlockedWork(r.Context(), input)
		if err != nil {
			return nil, &RPCError{Code: rpcInternalError, Message: "internal error"}
		}
		return result, nil
	case MethodTaskCreate:
		var input TaskCreateInput
		if err := decodeRPCParams(params, &input); err != nil {
			return nil, &RPCError{Code: rpcInvalidParams, Message: "invalid params"}
		}
		normalized, ok := normalizeTaskCreateInput(input)
		if !ok {
			return nil, &RPCError{Code: rpcInvalidParams, Message: "invalid params"}
		}
		result, err := h.handleTaskCreate(r, normalized)
		if err != nil {
			if errors.Is(err, orchestrator.ErrIdempotencyConflict) {
				return nil, &RPCError{Code: rpcInvalidParams, Message: "invalid params"}
			}
			return nil, &RPCError{Code: rpcInternalError, Message: "internal error"}
		}
		return result, nil
	case MethodTasksGet:
		var input TasksGetInput
		if err := decodeRPCParams(params, &input); err != nil {
			return nil, &RPCError{Code: rpcInvalidParams, Message: "invalid params"}
		}
		if !validTasksGetInput(input) {
			return nil, &RPCError{Code: rpcInvalidParams, Message: "invalid params"}
		}
		result, err := h.handleTasksGet(r, input)
		if err != nil {
			return nil, rpcErrorFromLookup(err)
		}
		return result, nil
	case MethodTasksCancel:
		var input TasksCancelInput
		if err := decodeRPCParams(params, &input); err != nil {
			return nil, &RPCError{Code: rpcInvalidParams, Message: "invalid params"}
		}
		if !validTasksCancelInput(input) {
			return nil, &RPCError{Code: rpcInvalidParams, Message: "invalid params"}
		}
		result, err := h.handleTasksCancel(r, input)
		if err != nil {
			if errors.Is(err, errInvalidTasksCancel) {
				return nil, newRPCError(rpcInvalidParams, "invalid params", rpcErrorDataInvalidParams)
			}
			return nil, rpcErrorFromLookup(err)
		}
		return result, nil
	default:
		return nil, &RPCError{Code: rpcMethodNotFound, Message: "method not found"}
	}
}

func (h *Handler) handleTaskCreate(r *http.Request, input TaskCreateInput) (TaskCreateOutput, error) {
	payloadHash, err := taskCreatePayloadHash(input)
	if err != nil {
		return TaskCreateOutput{}, err
	}
	artifactRefs := make([]toolserver.ControlledTaskArtifactRef, 0, len(input.ArtifactRefs))
	for _, artifactRef := range input.ArtifactRefs {
		artifactRefs = append(artifactRefs, toolserver.ControlledTaskArtifactRef{
			URI:      artifactRef.URI,
			Type:     artifactRef.Type,
			Version:  artifactRef.Version,
			Checksum: artifactRef.Checksum,
		})
	}
	result, err := h.handlers.HandleControlledTaskCreate(r.Context(), toolserver.ControlledTaskCreateInput{
		IdempotencyKey: input.IdempotencyKey,
		PayloadHash:    payloadHash,
		Intent:         input.Intent,
		ReceiverID:     input.Receiver.ID,
		ProjectRef:     input.ProjectRef,
		ArtifactRefs:   artifactRefs,
	})
	if err != nil {
		return TaskCreateOutput{}, err
	}
	task := toA2ATask(toolserver.HandoffGetOutput{Handoff: result.Handoff}, nil)
	return TaskCreateOutput{
		Task:                task,
		WorkflowID:          result.Workflow.ID,
		HandoffID:           result.Handoff.ID,
		IdempotencyReplayed: result.Replayed,
	}, nil
}

func (h *Handler) handleTasksGet(r *http.Request, input TasksGetInput) (A2ATask, error) {
	result, err := h.handlers.HandleHandoffGet(r.Context(), toolserver.HandoffGetInput{HandoffID: strings.TrimSpace(input.ID)})
	if err != nil {
		return A2ATask{}, err
	}
	return toA2ATask(result, input.HistoryLength), nil
}

func (h *Handler) handleTasksCancel(r *http.Request, input TasksCancelInput) (A2ATask, error) {
	handoffID := strings.TrimSpace(input.ID)
	current, err := h.handlers.HandleHandoffGet(r.Context(), toolserver.HandoffGetInput{HandoffID: handoffID})
	if err != nil {
		return A2ATask{}, err
	}

	switch current.Handoff.State {
	case orchestrator.StateFailed:
		return toA2ATask(current, nil), nil
	case orchestrator.StateCompleted, orchestrator.StateExpired:
		return A2ATask{}, errInvalidTasksCancel
	}

	if _, err := h.handlers.HandleHandoffProgress(r.Context(), toolserver.HandoffProgressInput{
		Action:     string(orchestrator.ProtocolActionFail),
		WorkflowID: current.Handoff.WorkflowID,
		HandoffID:  handoffID,
		Actor: toolserver.ActorRefInput{
			Type: string(orchestrator.ActorSystem),
			ID:   "workflow-controller",
		},
	}); err != nil {
		return A2ATask{}, err
	}

	updated, err := h.handlers.HandleHandoffGet(r.Context(), toolserver.HandoffGetInput{HandoffID: handoffID})
	if err != nil {
		return A2ATask{}, err
	}
	return toA2ATask(updated, nil), nil
}

const (
	taskEventsPathPrefix           = "/a2a/tasks/"
	taskEventsPathSuffix           = "/events"
	defaultTaskEventsRetryMs       = 3000
	defaultTaskEventsPollMs        = 1000
	minimumTaskEventsPollMs        = 250
	maximumTaskEventsPollMs        = 10000
	maximumTaskEventsHistoryLength = 100
)

func (h *Handler) handleTaskEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, httpErrorResponse{Error: "method not allowed"})
		return
	}
	if !h.authorized(r.Header.Get("Authorization")) {
		if strings.TrimSpace(r.Header.Get("Authorization")) == "" {
			writeJSON(w, http.StatusUnauthorized, httpErrorResponse{Error: "missing authorization"})
			return
		}
		writeJSON(w, http.StatusForbidden, httpErrorResponse{Error: "invalid authorization"})
		return
	}
	if r.ContentLength != 0 {
		writeJSON(w, http.StatusBadRequest, httpErrorResponse{Error: "invalid request"})
		return
	}
	handoffID, ok := parseTaskEventsHandoffID(r.URL.Path)
	if !ok {
		writeJSON(w, http.StatusBadRequest, httpErrorResponse{Error: "invalid request"})
		return
	}
	historyLength, pollInterval, ok := parseTaskEventsQuery(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, httpErrorResponse{Error: "invalid request"})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, httpErrorResponse{Error: "streaming unsupported"})
		return
	}
	snapshot, err := h.taskStreamSnapshot(r.Context(), handoffID, historyLength)
	if err != nil {
		writeJSON(w, http.StatusNotFound, httpErrorResponse{Error: "not found"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if err := writeSSERetry(w, defaultTaskEventsRetryMs); err != nil {
		return
	}
	if err := writeSSEJSON(w, "task", snapshot.EventID, snapshot); err != nil {
		return
	}
	flusher.Flush()
	lastFingerprint := taskStreamFingerprint(snapshot)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			snapshot, err := h.taskStreamSnapshot(r.Context(), handoffID, historyLength)
			if err != nil {
				return
			}
			fingerprint := taskStreamFingerprint(snapshot)
			if fingerprint == lastFingerprint {
				continue
			}
			if err := writeSSEJSON(w, "task", snapshot.EventID, snapshot); err != nil {
				return
			}
			flusher.Flush()
			lastFingerprint = fingerprint
		}
	}
}

func (h *Handler) taskStreamSnapshot(ctx context.Context, handoffID string, historyLength int) (TaskStreamEvent, error) {
	result, err := h.handlers.HandleHandoffGet(ctx, toolserver.HandoffGetInput{HandoffID: handoffID})
	if err != nil {
		return TaskStreamEvent{}, err
	}
	eventID, timestamp := taskStreamCursor(result)
	return TaskStreamEvent{
		Task:       toA2ATask(result, &historyLength),
		HandoffID:  result.Handoff.ID,
		WorkflowID: result.Handoff.WorkflowID,
		EventID:    eventID,
		Timestamp:  timestamp.UTC().Format(time.RFC3339Nano),
	}, nil
}

func taskStreamCursor(result toolserver.HandoffGetOutput) (string, time.Time) {
	eventID := result.Handoff.ID
	timestamp := result.Handoff.UpdatedAt
	if len(result.Timeline) == 0 {
		return eventID, timestamp
	}
	latest := result.Timeline[len(result.Timeline)-1]
	eventID = latest.ID
	timestamp = latest.IngestedAt
	if timestamp.IsZero() {
		timestamp = latest.ProducerEventTime
	}
	return eventID, timestamp
}

func taskStreamFingerprint(event TaskStreamEvent) string {
	return event.EventID + "\x00" + event.Task.Status.State + "\x00" + event.Task.Status.Timestamp
}

func isTaskEventsPath(path string) bool {
	return strings.HasPrefix(path, taskEventsPathPrefix) && strings.HasSuffix(path, taskEventsPathSuffix)
}

func parseTaskEventsHandoffID(path string) (string, bool) {
	if !isTaskEventsPath(path) {
		return "", false
	}
	handoffID := strings.TrimSuffix(strings.TrimPrefix(path, taskEventsPathPrefix), taskEventsPathSuffix)
	handoffID = strings.TrimSpace(handoffID)
	if !validRequiredTaskCreateString(handoffID, maxTaskCreateIDLength) {
		return "", false
	}
	if strings.ContainsAny(handoffID, `/\\`) {
		return "", false
	}
	return handoffID, true
}

func parseTaskEventsQuery(r *http.Request) (int, time.Duration, bool) {
	query := r.URL.Query()
	for key := range query {
		if key != "historyLength" && key != "pollIntervalMs" {
			return 0, 0, false
		}
	}
	historyLength, ok := parseOptionalTaskEventsInt(query["historyLength"], 0)
	if !ok || historyLength < 0 || historyLength > maximumTaskEventsHistoryLength {
		return 0, 0, false
	}
	pollIntervalMs, ok := parseOptionalTaskEventsInt(query["pollIntervalMs"], defaultTaskEventsPollMs)
	if !ok || pollIntervalMs < minimumTaskEventsPollMs || pollIntervalMs > maximumTaskEventsPollMs {
		return 0, 0, false
	}
	return historyLength, time.Duration(pollIntervalMs) * time.Millisecond, true
}

func parseOptionalTaskEventsInt(values []string, defaultValue int) (int, bool) {
	if len(values) == 0 {
		return defaultValue, true
	}
	if len(values) != 1 {
		return 0, false
	}
	value := strings.TrimSpace(values[0])
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func validTasksGetInput(input TasksGetInput) bool {
	if strings.TrimSpace(input.ID) == "" {
		return false
	}
	return input.HistoryLength == nil || *input.HistoryLength >= 0
}

func validTasksCancelInput(input TasksCancelInput) bool {
	return strings.TrimSpace(input.ID) != ""
}

const (
	maxTaskCreateIDLength     = 200
	maxTaskCreateIntentLength = 2000
	maxTaskCreateRefLength    = 1000
	maxTaskCreateArtifacts    = 32
)

func normalizeTaskCreateInput(input TaskCreateInput) (TaskCreateInput, bool) {
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.Intent = strings.TrimSpace(input.Intent)
	input.Receiver.ID = strings.TrimSpace(input.Receiver.ID)
	input.ProjectRef = strings.TrimSpace(input.ProjectRef)
	if !validRequiredTaskCreateString(input.IdempotencyKey, maxTaskCreateIDLength) {
		return TaskCreateInput{}, false
	}
	if !validRequiredTaskCreateString(input.Intent, maxTaskCreateIntentLength) {
		return TaskCreateInput{}, false
	}
	if !validRequiredTaskCreateString(input.Receiver.ID, maxTaskCreateIDLength) {
		return TaskCreateInput{}, false
	}
	if !validOptionalTaskCreateString(input.ProjectRef, maxTaskCreateRefLength) || !safeTaskCreateExternalRef(input.ProjectRef) {
		return TaskCreateInput{}, false
	}
	if len(input.ArtifactRefs) > maxTaskCreateArtifacts {
		return TaskCreateInput{}, false
	}
	for i := range input.ArtifactRefs {
		input.ArtifactRefs[i].URI = strings.TrimSpace(input.ArtifactRefs[i].URI)
		input.ArtifactRefs[i].Type = strings.TrimSpace(input.ArtifactRefs[i].Type)
		input.ArtifactRefs[i].Version = strings.TrimSpace(input.ArtifactRefs[i].Version)
		input.ArtifactRefs[i].Checksum = strings.TrimSpace(input.ArtifactRefs[i].Checksum)
		if !validRequiredTaskCreateString(input.ArtifactRefs[i].URI, maxTaskCreateRefLength) || !safeTaskCreateExternalRef(input.ArtifactRefs[i].URI) {
			return TaskCreateInput{}, false
		}
		if !validOptionalTaskCreateString(input.ArtifactRefs[i].Type, maxTaskCreateIDLength) {
			return TaskCreateInput{}, false
		}
		if !validOptionalTaskCreateString(input.ArtifactRefs[i].Version, maxTaskCreateIDLength) {
			return TaskCreateInput{}, false
		}
		if !validOptionalTaskCreateString(input.ArtifactRefs[i].Checksum, maxTaskCreateIDLength) {
			return TaskCreateInput{}, false
		}
	}
	return input, true
}

func taskCreatePayloadHash(input TaskCreateInput) (string, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("%x", sum), nil
}

func validRequiredTaskCreateString(value string, maxLength int) bool {
	return value != "" && validOptionalTaskCreateString(value, maxLength)
}

func validOptionalTaskCreateString(value string, maxLength int) bool {
	if len(value) > maxLength || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func safeTaskCreateExternalRef(ref string) bool {
	if ref == "" {
		return true
	}
	lower := strings.ToLower(ref)
	return strings.HasPrefix(lower, "project://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://")
}

func toA2ATask(result toolserver.HandoffGetOutput, historyLength *int) A2ATask {
	handoff := result.Handoff
	return A2ATask{
		ID:        handoff.ID,
		ContextID: handoff.WorkflowID,
		Status: A2ATaskStatus{
			State:     toA2ATaskState(handoff.State),
			Timestamp: handoff.UpdatedAt.UTC().Format(time.RFC3339Nano),
		},
		History: toA2ATaskHistory(result.Timeline, historyLength),
		Metadata: map[string]any{
			"workflowId":    handoff.WorkflowID,
			"workflowKind":  handoff.WorkflowKind,
			"internalState": string(handoff.State),
			"taskKind":      string(handoff.TaskKind),
			"intent":        handoff.Intent,
			"receiver":      handoff.ReceiverActor,
			"currentOwner":  handoff.CurrentOwner,
			"needsReview":   handoff.NeedsReview,
			"artifactCount": handoff.ArtifactCount,
		},
	}
}

func toA2ATaskHistory(events []orchestrator.EventRecord, historyLength *int) []A2ATaskEvent {
	if historyLength != nil && *historyLength == 0 {
		return nil
	}
	start := 0
	if historyLength != nil && *historyLength > 0 && len(events) > *historyLength {
		start = len(events) - *historyLength
	}
	history := make([]A2ATaskEvent, 0, len(events)-start)
	for _, event := range events[start:] {
		timestamp := event.IngestedAt
		if timestamp.IsZero() {
			timestamp = event.ProducerEventTime
		}
		history = append(history, A2ATaskEvent{
			Kind:      "event",
			EventID:   event.ID,
			Type:      string(event.Type),
			Timestamp: timestamp.UTC().Format(time.RFC3339Nano),
			Accepted:  event.Accepted,
		})
	}
	return history
}

func toA2ATaskState(state orchestrator.HandoffState) string {
	switch state {
	case orchestrator.StateCreated, orchestrator.StateDispatched, orchestrator.StateSubmitted:
		return "submitted"
	case orchestrator.StateReceived, orchestrator.StateClaimed, orchestrator.StateStarted, orchestrator.StateCheckpointed, orchestrator.StateReviewed:
		return "working"
	case orchestrator.StateCompleted:
		return "completed"
	case orchestrator.StateFailed, orchestrator.StateExpired:
		return "failed"
	default:
		return "unknown"
	}
}

func decodeRPCParams(params json.RawMessage, out any) error {
	trimmed := bytes.TrimSpace(params)
	if len(trimmed) == 0 {
		trimmed = []byte("{}")
	}
	if !json.Valid(trimmed) {
		return errors.New("invalid params")
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("invalid params")
	}
	return nil
}

func inspectRPCID(body []byte) (bool, bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return false, true
	}
	id, ok := fields["id"]
	if !ok {
		return false, true
	}
	return true, validRPCID(id)
}

func validRPCID(id json.RawMessage) bool {
	trimmed := bytes.TrimSpace(id)
	if len(trimmed) == 0 {
		return false
	}
	switch trimmed[0] {
	case '"':
		return true
	case 'n':
		return bytes.Equal(trimmed, []byte("null"))
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		decoder := json.NewDecoder(bytes.NewReader(trimmed))
		decoder.UseNumber()
		var number json.Number
		if err := decoder.Decode(&number); err != nil {
			return false
		}
		return decoder.Decode(&struct{}{}) == io.EOF
	default:
		return false
	}
}

func (h *Handler) authorized(authHeader string) bool {
	authHeader = strings.TrimSpace(authHeader)
	if authHeader == "" || h.cfg.AuthKey == "" {
		return false
	}
	const bearerPrefix = "Bearer "
	if !strings.HasPrefix(authHeader, bearerPrefix) {
		return false
	}
	provided := strings.TrimSpace(strings.TrimPrefix(authHeader, bearerPrefix))
	if provided == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(h.cfg.AuthKey)) == 1
}

func (h *Handler) rpcURL(r *http.Request) string {
	base := strings.TrimRight(h.cfg.PublicURL, "/")
	if base == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	if strings.HasSuffix(base, "/a2a/rpc") {
		return base
	}
	return base + "/a2a/rpc"
}

func buildAgentCard(rpcURL string) AgentCard {
	return AgentCard{
		ProtocolVersion: "0.3.0",
		Name:            "clawside-coordination",
		Description:     "Clawside coordination queries and controlled inbound task creation.",
		URL:             rpcURL,
		Provider: AgentProvider{
			Organization: "clawside",
		},
		Capabilities: AgentCapabilities{
			Streaming:         true,
			PushNotifications: false,
		},
		DefaultInputModes:  []string{"application/json"},
		DefaultOutputModes: []string{"application/json"},
		Skills: []AgentSkill{
			newSkill(MethodWorkflowList, "List workflows", "List workflows with projected handoffs."),
			newSkill(MethodWorkflowStatus, "Workflow status", "Get workflow status and projected handoffs."),
			newSkill(MethodHandoffGet, "Get handoff", "Get current handoff truth and timeline."),
			newSkill(MethodAgentList, "List agents", "List registered agents by coordination filters."),
			newSkill(MethodNextWork, "Next work", "List executable handoffs for an agent or work filter."),
			newSkill(MethodBlockedWork, "Blocked work", "List blocked handoffs with reasons and suggestions."),
			newSkill(MethodTaskCreate, "Create controlled task", "Create an idempotent controlled inbound task without runtime or delivery execution."),
			newSkill(MethodTasksGet, "Get task", "Get A2A-compatible read-only task status for a Clawside handoff."),
			newSkill(MethodTasksCancel, "Cancel task", "Cancel an A2A task by marking the corresponding Clawside handoff failed in the truth-plane only."),
			newStreamingSkill(MethodTasksEvents, "Task events", "Subscribe to read-only A2A task projection events for a Clawside handoff."),
		},
		Metadata: buildAgentCardMetadata(),
		SecuritySchemes: map[string]AgentCardSecurity{
			"bearer": {Type: "http", Scheme: "bearer"},
		},
		Security: []map[string][]string{{"bearer": {}}},
	}
}

func buildAgentCardMetadata() AgentCardMetadata {
	return AgentCardMetadata{
		Endpoints: AgentCardEndpointMetadata{
			JSONRPC:    "/a2a/rpc",
			TaskEvents: "/a2a/tasks/{handoffID}/events",
		},
		Methods: []AgentCardMethodMetadata{
			newAgentCardMethodMetadata(MethodWorkflowList, "json-rpc", "read", "/a2a/rpc"),
			newAgentCardMethodMetadata(MethodWorkflowStatus, "json-rpc", "read", "/a2a/rpc"),
			newAgentCardMethodMetadata(MethodHandoffGet, "json-rpc", "read", "/a2a/rpc"),
			newAgentCardMethodMetadata(MethodAgentList, "json-rpc", "read", "/a2a/rpc"),
			newAgentCardMethodMetadata(MethodNextWork, "json-rpc", "read", "/a2a/rpc"),
			newAgentCardMethodMetadata(MethodBlockedWork, "json-rpc", "read", "/a2a/rpc"),
			newAgentCardMethodMetadata(MethodTaskCreate, "json-rpc", "controlled-write", "/a2a/rpc"),
			newAgentCardMethodMetadata(MethodTasksGet, "json-rpc", "read", "/a2a/rpc"),
			newAgentCardMethodMetadata(MethodTasksCancel, "json-rpc", "controlled-write", "/a2a/rpc"),
			newAgentCardMethodMetadata(MethodTasksEvents, "sse", "stream", "/a2a/tasks/{handoffID}/events"),
		},
		SSE: AgentCardSSEMetadata{
			RetryMilliseconds: defaultTaskEventsRetryMs,
			Reconnect:         "call tasks/get, then resubscribe to the task events stream",
		},
		Safety: []string{
			"bearer_auth_required",
			"no_command_execution",
			"no_worker_launch",
			"no_sender_delivery",
			"no_local_path_access",
			"no_private_prompt_or_secret_echo",
		},
	}
}

func newAgentCardMethodMetadata(id, transport, mode, endpoint string) AgentCardMethodMetadata {
	return AgentCardMethodMetadata{ID: id, Transport: transport, Mode: mode, Endpoint: endpoint}
}

func newSkill(id, name, description string) AgentSkill {
	return AgentSkill{
		ID:          id,
		Name:        name,
		Description: description,
		InputModes:  []string{"application/json"},
		OutputModes: []string{"application/json"},
	}
}

func newStreamingSkill(id, name, description string) AgentSkill {
	skill := newSkill(id, name, description)
	skill.OutputModes = []string{"application/json", "text/event-stream"}
	return skill
}

func writeRPCResult(w http.ResponseWriter, id json.RawMessage, result any) {
	writeJSON(w, http.StatusOK, RPCResponse{JSONRPC: "2.0", ID: normalizeRPCID(id), Result: result})
}

func writeSSERetry(w io.Writer, retryMs int) error {
	_, err := fmt.Fprintf(w, "retry: %d\n", retryMs)
	return err
}

func writeSSEJSON(w io.Writer, eventName, id string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", eventName); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "id: %s\n", id); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	return nil
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	writeRPCErrorObject(w, id, newRPCError(code, message, defaultRPCErrorDataCode(code)))
}

func writeRPCErrorObject(w http.ResponseWriter, id json.RawMessage, rpcErr *RPCError) {
	writeJSON(w, http.StatusOK, RPCResponse{JSONRPC: "2.0", ID: normalizeRPCID(id), Error: normalizeRPCError(rpcErr)})
}

func newRPCError(code int, message, dataCode string) *RPCError {
	return &RPCError{Code: code, Message: message, Data: &RPCErrorData{Code: dataCode}}
}

func normalizeRPCError(rpcErr *RPCError) *RPCError {
	if rpcErr == nil {
		return newRPCError(rpcInternalError, "internal error", rpcErrorDataInternalError)
	}
	if rpcErr.Data != nil && rpcErr.Data.Code != "" {
		return rpcErr
	}
	return newRPCError(rpcErr.Code, rpcErr.Message, defaultRPCErrorDataCode(rpcErr.Code))
}

func rpcErrorFromLookup(err error) *RPCError {
	if errors.Is(err, sql.ErrNoRows) {
		return newRPCError(rpcInvalidParams, "invalid params", rpcErrorDataNotFound)
	}
	return newRPCError(rpcInternalError, "internal error", rpcErrorDataInternalError)
}

func defaultRPCErrorDataCode(code int) string {
	switch code {
	case rpcParseError:
		return rpcErrorDataParseError
	case rpcInvalidRequest:
		return rpcErrorDataInvalidRequest
	case rpcMethodNotFound:
		return rpcErrorDataMethodNotFound
	case rpcInvalidParams:
		return rpcErrorDataInvalidParams
	case rpcInternalError:
		return rpcErrorDataInternalError
	default:
		return rpcErrorDataInternalError
	}
}

func writeNoRPCResponse(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

func normalizeRPCID(id json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(id)) == 0 {
		return json.RawMessage("null")
	}
	return id
}

func writeJSON(w http.ResponseWriter, statusCode int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(body)
}
