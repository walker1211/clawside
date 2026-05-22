package a2aserver

import "encoding/json"

const (
	MethodWorkflowList   = "clawside.workflow.list"
	MethodWorkflowStatus = "clawside.workflow.status"
	MethodHandoffGet     = "clawside.handoff.get"
	MethodAgentList      = "clawside.agent.list"
	MethodNextWork       = "clawside.work.next"
	MethodBlockedWork    = "clawside.work.blocked"
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
