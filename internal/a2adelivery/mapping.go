package a2adelivery

import (
	"fmt"
	"maps"
	"strings"

	"github.com/walker1211/clawside/internal/deliveryrules"
)

var firstVersionTargetAgentToBot = map[string]string{
	"main":       "main",
	"planner":    "planner",
	"engineer":   "engineer",
	"researcher": "researcher",
	"archivist":  "archivist",
	"guardian":   "guardian",
	"closer":     "closer",
	"worker":     "main",
	"upstream":   "main",
	"downstream": "main",
	"reviewer":   "main",
}

type TargetAgentBotResolver struct {
	targetAgentToBot map[string]string
}

func NewTargetAgentBotResolver(config string) (*TargetAgentBotResolver, error) {
	mapping := make(map[string]string, len(firstVersionTargetAgentToBot))
	maps.Copy(mapping, firstVersionTargetAgentToBot)

	if strings.TrimSpace(config) == "" {
		return &TargetAgentBotResolver{targetAgentToBot: mapping}, nil
	}
	for rawPair := range strings.SplitSeq(config, ",") {
		pair := strings.TrimSpace(rawPair)
		if pair == "" {
			return nil, fmt.Errorf("target_agent mapping pair must not be blank")
		}
		parts := strings.Split(pair, "=")
		if len(parts) != 2 {
			return nil, fmt.Errorf("target_agent mapping %q must use target=bot", pair)
		}
		targetAgent := normalizeTargetAgentName(parts[0])
		bot := deliveryrules.NormalizeBotName(parts[1])
		if targetAgent == "" || bot == "" {
			return nil, fmt.Errorf("target_agent mapping %q must include non-blank target and bot", pair)
		}
		mapping[targetAgent] = bot
	}
	return &TargetAgentBotResolver{targetAgentToBot: mapping}, nil
}

func (r *TargetAgentBotResolver) ResolveBotForTargetAgent(targetAgent string) (string, error) {
	normalized := normalizeTargetAgentName(targetAgent)
	bot, ok := r.targetAgentToBot[normalized]
	if !ok {
		return "", fmt.Errorf("unknown target_agent %q", targetAgent)
	}
	return bot, nil
}

func normalizeTargetAgentName(value string) string {
	return strings.TrimPrefix(deliveryrules.NormalizeBotName(value), "agent:")
}

func ResolveBotForTargetAgent(targetAgent string) (string, error) {
	resolver, err := NewTargetAgentBotResolver("")
	if err != nil {
		return "", err
	}
	return resolver.ResolveBotForTargetAgent(targetAgent)
}
