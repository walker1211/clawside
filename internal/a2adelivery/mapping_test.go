package a2adelivery

import "testing"

func TestResolveBotForTargetAgentKnownMappings(t *testing.T) {
	tests := []struct {
		name        string
		targetAgent string
		wantBot     string
	}{
		{name: "main maps 1:1", targetAgent: "main", wantBot: "main"},
		{name: "planner maps 1:1", targetAgent: "planner", wantBot: "planner"},
		{name: "engineer maps 1:1", targetAgent: "engineer", wantBot: "engineer"},
		{name: "researcher maps 1:1", targetAgent: "researcher", wantBot: "researcher"},
		{name: "archivist maps 1:1", targetAgent: "archivist", wantBot: "archivist"},
		{name: "guardian maps 1:1", targetAgent: "guardian", wantBot: "guardian"},
		{name: "closer maps 1:1", targetAgent: "closer", wantBot: "closer"},
		{name: "agent worker maps to main", targetAgent: "agent:worker", wantBot: "main"},
		{name: "agent downstream maps to main", targetAgent: "agent:downstream", wantBot: "main"},
		{name: "downstream maps to main", targetAgent: "downstream", wantBot: "main"},
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

func TestNewTargetAgentBotResolverUsesBuiltInFallback(t *testing.T) {
	resolver, err := NewTargetAgentBotResolver("")
	if err != nil {
		t.Fatalf("NewTargetAgentBotResolver: %v", err)
	}

	got, err := resolver.ResolveBotForTargetAgent(" Planner ")
	if err != nil {
		t.Fatalf("expected built-in mapping, got error: %v", err)
	}
	if got != "planner" {
		t.Fatalf("expected bot planner, got %q", got)
	}
}

func TestNewTargetAgentBotResolverAddsConfiguredMapping(t *testing.T) {
	resolver, err := NewTargetAgentBotResolver("qa=guardian")
	if err != nil {
		t.Fatalf("NewTargetAgentBotResolver: %v", err)
	}

	got, err := resolver.ResolveBotForTargetAgent("qa")
	if err != nil {
		t.Fatalf("expected configured mapping, got error: %v", err)
	}
	if got != "guardian" {
		t.Fatalf("expected bot guardian, got %q", got)
	}
}

func TestNewTargetAgentBotResolverOverridesBuiltInMapping(t *testing.T) {
	resolver, err := NewTargetAgentBotResolver("planner=guardian")
	if err != nil {
		t.Fatalf("NewTargetAgentBotResolver: %v", err)
	}

	got, err := resolver.ResolveBotForTargetAgent("planner")
	if err != nil {
		t.Fatalf("expected configured override, got error: %v", err)
	}
	if got != "guardian" {
		t.Fatalf("expected bot guardian, got %q", got)
	}
}

func TestNewTargetAgentBotResolverOverridesOpenClawAgentAlias(t *testing.T) {
	resolver, err := NewTargetAgentBotResolver("agent:worker=guardian")
	if err != nil {
		t.Fatalf("NewTargetAgentBotResolver: %v", err)
	}

	for _, targetAgent := range []string{"worker", "agent:worker"} {
		got, err := resolver.ResolveBotForTargetAgent(targetAgent)
		if err != nil {
			t.Fatalf("expected configured override for %q, got error: %v", targetAgent, err)
		}
		if got != "guardian" {
			t.Fatalf("expected bot guardian for %q, got %q", targetAgent, got)
		}
	}
}

func TestNewTargetAgentBotResolverRejectsInvalidPairs(t *testing.T) {
	for _, raw := range []string{"qa", "qa=guardian=extra"} {
		t.Run(raw, func(t *testing.T) {
			_, err := NewTargetAgentBotResolver(raw)
			if err == nil {
				t.Fatalf("expected invalid mapping %q to fail", raw)
			}
		})
	}
}

func TestNewTargetAgentBotResolverRejectsBlankSides(t *testing.T) {
	for _, raw := range []string{"=guardian", "qa= ", " =guardian"} {
		t.Run(raw, func(t *testing.T) {
			_, err := NewTargetAgentBotResolver(raw)
			if err == nil {
				t.Fatalf("expected blank-sided mapping %q to fail", raw)
			}
		})
	}
}

func TestConfiguredTargetAgentBotResolverRejectsUnknownTarget(t *testing.T) {
	resolver, err := NewTargetAgentBotResolver("qa=guardian")
	if err != nil {
		t.Fatalf("NewTargetAgentBotResolver: %v", err)
	}

	_, err = resolver.ResolveBotForTargetAgent("unknown")
	if err == nil {
		t.Fatalf("expected unknown target_agent to fail")
	}
}
