package main

import (
	"sync"
	"time"
)

type RuntimeState struct {
	mu             sync.RWMutex
	workerRunning  bool
	startedAt      time.Time
	lastLoopAt     time.Time
	lastJobClaimAt time.Time
	lastSuccessAt  time.Time
	lastFailureAt  time.Time
}

type RuntimeSnapshot struct {
	WorkerRunning  bool
	StartedAt      time.Time
	LastLoopAt     time.Time
	LastJobClaimAt time.Time
	LastSuccessAt  time.Time
	LastFailureAt  time.Time
}

func NewRuntimeState() *RuntimeState {
	return &RuntimeState{}
}

func (s *RuntimeState) MarkWorkerStarted(at time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workerRunning = true
	s.startedAt = at.UTC()
}

func (s *RuntimeState) MarkWorkerLoop(at time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastLoopAt = at.UTC()
}

func (s *RuntimeState) MarkJobClaimed(at time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastJobClaimAt = at.UTC()
}

func (s *RuntimeState) MarkJobSucceeded(at time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSuccessAt = at.UTC()
}

func (s *RuntimeState) MarkJobFailed(at time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastFailureAt = at.UTC()
}

func (s *RuntimeState) MarkWorkerStopped() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workerRunning = false
}

func (s *RuntimeState) Snapshot() RuntimeSnapshot {
	if s == nil {
		return RuntimeSnapshot{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return RuntimeSnapshot{
		WorkerRunning:  s.workerRunning,
		StartedAt:      s.startedAt,
		LastLoopAt:     s.lastLoopAt,
		LastJobClaimAt: s.lastJobClaimAt,
		LastSuccessAt:  s.lastSuccessAt,
		LastFailureAt:  s.lastFailureAt,
	}
}
