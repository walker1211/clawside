package a2adelivery

import "testing"

func TestResolveBotForTargetAgentKnownMappings(t *testing.T) {
	tests := []struct {
		name        string
		targetAgent string
		wantBot     string
	}{
		{name: "planner maps 1:1", targetAgent: "planner", wantBot: "planner"},
		{name: "engineer maps 1:1", targetAgent: "engineer", wantBot: "engineer"},
		{name: "researcher maps 1:1", targetAgent: "researcher", wantBot: "researcher"},
		{name: "archivist maps 1:1", targetAgent: "archivist", wantBot: "archivist"},
		{name: "guardian maps 1:1", targetAgent: "guardian", wantBot: "guardian"},
		{name: "closer maps 1:1", targetAgent: "closer", wantBot: "closer"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveBotForTargetAgent(tc.targetAgent)
			if err != nil {
				t.Fatalf("expected mapping for %q, got error: %v", tc.targetAgent, err)
			}
			if got != tc.wantBot {
				t.Fatalf("expected bot %q for target_agent %q, got %q", tc.wantBot, tc.targetAgent, got)
			}
		})
	}
}

func TestResolveBotForTargetAgentUnknownFailsImmediately(t *testing.T) {
	_, err := ResolveBotForTargetAgent("qa")
	if err == nil {
		t.Fatalf("expected unknown target_agent to fail")
	}
}
