package a2aserver

import "encoding/json"

const (
	MethodWorkflowList   = "clawside.workflow.list"
	MethodWorkflowStatus = "clawside.workflow.status"
	MethodHandoffGet     = "clawside.handoff.get"
	MethodAgentList      = "clawside.agent.list"
	MethodNextWork       = "clawside.work.next"
	MethodBlockedWork    = "clawside.work.blocked"
	MethodTaskCreate     = "clawside.task.create"
	MethodTasksGet       = "tasks/get"
)

const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
	rpcInternalError  = -32603
)

type Config struct {
	PublicURL string
	AuthKey   string
}

type AgentCard struct {
	ProtocolVersion    string                       `json:"protocolVersion"`
	Name               string                       `json:"name"`
	Description        string                       `json:"description"`
	URL                string                       `json:"url"`
	Provider           AgentProvider                `json:"provider"`
	Capabilities       AgentCapabilities            `json:"capabilities"`
	DefaultInputModes  []string                     `json:"defaultInputModes"`
	DefaultOutputModes []string                     `json:"defaultOutputModes"`
	Skills             []AgentSkill                 `json:"skills"`
	SecuritySchemes    map[string]AgentCardSecurity `json:"securitySchemes,omitempty"`
	Security           []map[string][]string        `json:"security,omitempty"`
}

type AgentProvider struct {
	Organization string `json:"organization"`
}

type AgentCapabilities struct {
	Streaming         bool `json:"streaming"`
	PushNotifications bool `json:"pushNotifications"`
}

type AgentSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	InputModes  []string `json:"inputModes"`
	OutputModes []string `json:"outputModes"`
}

type AgentCardSecurity struct {
	Type   string `json:"type"`
	Scheme string `json:"scheme"`
}

type RPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type TasksGetInput struct {
	ID            string `json:"id"`
	HistoryLength *int   `json:"historyLength,omitempty"`
}

type TaskCreateInput struct {
	IdempotencyKey string               `json:"idempotency_key"`
	Intent         string               `json:"intent"`
	Receiver       TaskCreateActorInput `json:"receiver"`
	ProjectRef     string               `json:"project_ref,omitempty"`
	ArtifactRefs   []TaskArtifactRef    `json:"artifact_refs,omitempty"`
}

type TaskCreateActorInput struct {
	ID string `json:"id"`
}

type TaskArtifactRef struct {
	URI      string `json:"uri"`
	Type     string `json:"type,omitempty"`
	Version  string `json:"version,omitempty"`
	Checksum string `json:"checksum,omitempty"`
}

type TaskCreateOutput struct {
	Task                A2ATask `json:"task"`
	WorkflowID          string  `json:"workflowId"`
	HandoffID           string  `json:"handoffId"`
	IdempotencyReplayed bool    `json:"idempotencyReplayed,omitempty"`
}

type A2ATask struct {
	ID        string         `json:"id"`
	ContextID string         `json:"contextId,omitempty"`
	Status    A2ATaskStatus  `json:"status"`
	History   []A2ATaskEvent `json:"history,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type A2ATaskStatus struct {
	State     string `json:"state"`
	Timestamp string `json:"timestamp,omitempty"`
}

type A2ATaskEvent struct {
	Kind      string `json:"kind"`
	EventID   string `json:"eventId,omitempty"`
	Type      string `json:"type"`
	Timestamp string `json:"timestamp,omitempty"`
	Accepted  bool   `json:"accepted"`
}
