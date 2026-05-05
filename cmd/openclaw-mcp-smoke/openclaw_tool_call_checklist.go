package main

type OpenClawToolCallChecklistEntry struct {
	Tool      string         `json:"tool"`
	Purpose   string         `json:"purpose"`
	Arguments map[string]any `json:"arguments"`
	Expected  string         `json:"expected"`
	Safe      bool           `json:"safe"`
}

func buildOpenClawToolCallChecklist() []OpenClawToolCallChecklistEntry {
	return []OpenClawToolCallChecklistEntry{
		{
			Tool:      "sender_health",
			Purpose:   "Confirm OpenClaw can call the registered clawside MCP sender health tool.",
			Arguments: map[string]any{},
			Expected:  "successful tool result with status=ok",
			Safe:      true,
		},
		{
			Tool:      "sender_ready",
			Purpose:   "Confirm OpenClaw can call the registered clawside MCP sender readiness tool.",
			Arguments: map[string]any{},
			Expected:  "successful tool result with status=ok",
			Safe:      true,
		},
		{
			Tool:      "sender_stats",
			Purpose:   "Confirm OpenClaw can call the registered clawside MCP sender stats tool and observe queue counters.",
			Arguments: map[string]any{},
			Expected:  "successful tool result containing worker_running=true and queue counters such as pending, retry, sending, sent, and failed counts",
			Safe:      true,
		},
	}
}
