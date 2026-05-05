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

func writeOpenClawToolCallChecklist(w interface{ Write([]byte) (int, error) }, checklist []OpenClawToolCallChecklistEntry) error {
	if len(checklist) == 0 {
		return nil
	}
	if err := writeLine(w, "OpenClaw read-only tool call checklist:"); err != nil {
		return err
	}
	for _, entry := range checklist {
		if err := writeLine(w, "- %s: call with {}; expect %s", entry.Tool, openClawChecklistTextExpected(entry)); err != nil {
			return err
		}
	}
	return nil
}

func openClawChecklistTextExpected(entry OpenClawToolCallChecklistEntry) string {
	switch entry.Tool {
	case "sender_health", "sender_ready":
		return "status=ok"
	case "sender_stats":
		return "worker_running=true and queue counters"
	default:
		return entry.Expected
	}
}
