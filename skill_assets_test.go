package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestA2ADeliveryTriggerEvalsSeparateOutwardDeliveryFromOrdinaryReplies(t *testing.T) {
	data, err := os.ReadFile(".claude/skills/openclaw-a2a-delivery/trigger-evals.json")
	if err != nil {
		t.Fatalf("read trigger evals: %v", err)
	}
	var evals []struct {
		Query         string `json:"query"`
		ShouldTrigger bool   `json:"should_trigger"`
	}
	if err := json.Unmarshal(data, &evals); err != nil {
		t.Fatalf("unmarshal trigger evals: %v", err)
	}

	var hasOutwardDelivery, hasOrdinaryReply bool
	for _, eval := range evals {
		if eval.ShouldTrigger && strings.Contains(eval.Query, "sender bridge") {
			hasOutwardDelivery = true
		}
		if !eval.ShouldTrigger && strings.Contains(eval.Query, "总结") && strings.Contains(eval.Query, "直接回复") {
			hasOrdinaryReply = true
		}
	}
	if !hasOutwardDelivery {
		t.Fatalf("expected positive sender bridge trigger eval")
	}
	if !hasOrdinaryReply {
		t.Fatalf("expected negative ordinary in-session reply trigger eval")
	}
}
