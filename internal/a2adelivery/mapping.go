package a2adelivery

import (
	"fmt"
	"strings"
)

var firstVersionTargetAgentToBot = map[string]string{
	"planner":    "planner",
	"engineer":   "engineer",
	"researcher": "researcher",
	"archivist":  "archivist",
	"guardian":   "guardian",
	"closer":     "closer",
}

func ResolveBotForTargetAgent(targetAgent string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(targetAgent))
	bot, ok := firstVersionTargetAgentToBot[normalized]
	if !ok {
		return "", fmt.Errorf("unknown target_agent %q", targetAgent)
	}
	return bot, nil
}
