package main

import (
	"testing"
	"time"
)

func TestRuntimeStateTracksWorkerStateAndTimestamps(t *testing.T) {
	state := NewRuntimeState()
	loc := time.FixedZone("UTC+8", 8*60*60)
	startedAt := time.Date(2026, 3, 26, 18, 0, 0, 0, loc)
	loopAt := startedAt.Add(2 * time.Second)
	claimAt := startedAt.Add(3 * time.Second)
	successAt := startedAt.Add(4 * time.Second)
	failureAt := startedAt.Add(5 * time.Second)

	state.MarkWorkerStarted(startedAt)
	state.MarkWorkerLoop(loopAt)
	state.MarkJobClaimed(claimAt)
	state.MarkJobSucceeded(successAt)
	state.MarkJobFailed(failureAt)

	snapshot := state.Snapshot()
	if !snapshot.WorkerRunning {
		t.Fatalf("expected worker to be running")
	}
	if !snapshot.StartedAt.Equal(startedAt.UTC()) {
		t.Fatalf("expected started at %s, got %s", startedAt.UTC(), snapshot.StartedAt)
	}
	if !snapshot.LastLoopAt.Equal(loopAt.UTC()) {
		t.Fatalf("expected last loop at %s, got %s", loopAt.UTC(), snapshot.LastLoopAt)
	}
	if !snapshot.LastJobClaimAt.Equal(claimAt.UTC()) {
		t.Fatalf("expected last job claim at %s, got %s", claimAt.UTC(), snapshot.LastJobClaimAt)
	}
	if !snapshot.LastSuccessAt.Equal(successAt.UTC()) {
		t.Fatalf("expected last success at %s, got %s", successAt.UTC(), snapshot.LastSuccessAt)
	}
	if !snapshot.LastFailureAt.Equal(failureAt.UTC()) {
		t.Fatalf("expected last failure at %s, got %s", failureAt.UTC(), snapshot.LastFailureAt)
	}

	state.MarkWorkerStopped()

	snapshot = state.Snapshot()
	if snapshot.WorkerRunning {
		t.Fatalf("expected worker to be stopped")
	}
	if !snapshot.StartedAt.Equal(startedAt.UTC()) {
		t.Fatalf("expected started at to be retained as %s, got %s", startedAt.UTC(), snapshot.StartedAt)
	}
	if !snapshot.LastLoopAt.Equal(loopAt.UTC()) {
		t.Fatalf("expected last loop at to be retained as %s, got %s", loopAt.UTC(), snapshot.LastLoopAt)
	}
	if !snapshot.LastJobClaimAt.Equal(claimAt.UTC()) {
		t.Fatalf("expected last job claim at to be retained as %s, got %s", claimAt.UTC(), snapshot.LastJobClaimAt)
	}
	if !snapshot.LastSuccessAt.Equal(successAt.UTC()) {
		t.Fatalf("expected last success at to be retained as %s, got %s", successAt.UTC(), snapshot.LastSuccessAt)
	}
	if !snapshot.LastFailureAt.Equal(failureAt.UTC()) {
		t.Fatalf("expected last failure at to be retained as %s, got %s", failureAt.UTC(), snapshot.LastFailureAt)
	}
}

func TestRuntimeStateSnapshotOnNilState(t *testing.T) {
	var state *RuntimeState

	snapshot := state.Snapshot()
	if snapshot.WorkerRunning {
		t.Fatalf("expected worker to be stopped for nil state")
	}
	if !snapshot.StartedAt.IsZero() || !snapshot.LastLoopAt.IsZero() || !snapshot.LastJobClaimAt.IsZero() || !snapshot.LastSuccessAt.IsZero() || !snapshot.LastFailureAt.IsZero() {
		t.Fatalf("expected zero timestamps for nil state snapshot, got %+v", snapshot)
	}
}
