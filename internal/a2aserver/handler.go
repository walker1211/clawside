package a2aserver

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/walker1211/clawside/internal/toolserver"
)

const rpcBodyLimitBytes = 64 << 10

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
	switch r.URL.Path {
	case "/healthz":
		h.handleHealthz(w, r)
	case "/.well-known/agent-card.json":
		h.handleAgentCard(w, r)
	case "/a2a/rpc":
		h.handleRPC(w, r)
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
		writeRPCError(w, request.ID, rpcErr.Code, rpcErr.Message)
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
			return nil, &RPCError{Code: rpcInternalError, Message: "internal error"}
		}
		return result, nil
	case MethodHandoffGet:
		var input toolserver.HandoffGetInput
		if err := decodeRPCParams(params, &input); err != nil {
			return nil, &RPCError{Code: rpcInvalidParams, Message: "invalid params"}
		}
		result, err := h.handlers.HandleHandoffGet(r.Context(), input)
		if err != nil {
			return nil, &RPCError{Code: rpcInternalError, Message: "internal error"}
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
	default:
		return nil, &RPCError{Code: rpcMethodNotFound, Message: "method not found"}
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
		Description:     "Read-only Clawside coordination and workflow status queries.",
		URL:             rpcURL,
		Provider: AgentProvider{
			Organization: "clawside",
		},
		Capabilities: AgentCapabilities{
			Streaming:         false,
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
		},
		SecuritySchemes: map[string]AgentCardSecurity{
			"bearer": {Type: "http", Scheme: "bearer"},
		},
		Security: []map[string][]string{{"bearer": {}}},
	}
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

func writeRPCResult(w http.ResponseWriter, id json.RawMessage, result any) {
	writeJSON(w, http.StatusOK, RPCResponse{JSONRPC: "2.0", ID: normalizeRPCID(id), Result: result})
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	writeJSON(w, http.StatusOK, RPCResponse{JSONRPC: "2.0", ID: normalizeRPCID(id), Error: &RPCError{Code: code, Message: message}})
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
