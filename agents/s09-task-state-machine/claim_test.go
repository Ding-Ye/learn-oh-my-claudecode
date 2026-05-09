package main

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// freshStore returns a new Store rooted in t.TempDir() and seeds a
// single pending task named `<team>/<id>`. Used by every test in this
// file because the claim/transition flow has the same prelude
// everywhere — read a task, mutate, assert on the result.
//
// The helper is private to this file (not store.go) because it's a
// test fixture, not a production primitive: it bakes in choices
// (status=pending, version=1) that real seeders may want to vary.
func freshStore(t *testing.T, team, id string) *Store {
	t.Helper()

	root := filepath.Join(t.TempDir(), ".omc", "state", "team")
	s := NewStore(root)

	now := time.Now()
	seed := &Task{
		ID:          id,
		Status:      "pending",
		Version:     1,
		Description: "test task " + id,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.Write(team, seed); err != nil {
		t.Fatalf("freshStore: seed Write: %v", err)
	}
	return s
}

// TestClaimTaskMintsToken pins the success path: a worker claims a
// pending task, the call returns a non-empty token, and the on-disk
// task reflects the new state (status=in_progress, owner set, claim
// non-nil with matching token, version bumped). If any one of these
// invariants drifts the state machine has been broken; tests would
// otherwise pass while real workers see corrupted task files.
func TestClaimTaskMintsToken(t *testing.T) {
	const team, id = "alpha", "task-1"
	s := freshStore(t, team, id)

	token, err := s.ClaimTask(team, id, "worker-1")
	if err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}

	// Token shape: 32 hex chars (16 bytes encoded as hex). Anything
	// shorter means randomUUID truncated; longer means it changed
	// encoding without us noticing.
	if len(token) != 32 {
		t.Errorf("token length: got %d, want 32 (hex of 16 bytes)", len(token))
	}

	// Re-read from disk. The token returned by ClaimTask must equal
	// the one persisted; otherwise a worker has no way to transition
	// later because the on-disk record will reject its token.
	got, err := s.Read(team, id)
	if err != nil {
		t.Fatalf("Read after claim: %v", err)
	}
	if got.Status != "in_progress" {
		t.Errorf("Status: got %q, want %q", got.Status, "in_progress")
	}
	if got.Owner != "worker-1" {
		t.Errorf("Owner: got %q, want %q", got.Owner, "worker-1")
	}
	if got.Claim == nil {
		t.Fatalf("Claim: got nil, want populated")
	}
	if got.Claim.Token != token {
		t.Errorf("on-disk token: got %q, want %q", got.Claim.Token, token)
	}
	if got.Claim.Owner != "worker-1" {
		t.Errorf("Claim.Owner: got %q, want %q", got.Claim.Owner, "worker-1")
	}
	// Lease must be in the future. Using >0 instead of ==15m allows
	// a slow CI box to drift a few microseconds without flaking.
	if remaining := time.Until(got.Claim.LeasedUntil); remaining <= 0 {
		t.Errorf("LeasedUntil: %v in the past, want future", got.Claim.LeasedUntil)
	}
	if got.Version != 2 {
		t.Errorf("Version: got %d, want 2 (bumped from seed=1)", got.Version)
	}
}

// TestSecondClaimFailsWhileLeaseValid is the contention test: a second
// worker attempting to claim a freshly-claimed task must receive
// ErrLeaseStillValid. The error type matters — a runtime that
// distinguishes "lease still valid, retry later" from "task was
// terminal, give up" needs the sentinel.
func TestSecondClaimFailsWhileLeaseValid(t *testing.T) {
	const team, id = "alpha", "task-2"
	s := freshStore(t, team, id)

	if _, err := s.ClaimTask(team, id, "worker-A"); err != nil {
		t.Fatalf("first ClaimTask: %v", err)
	}

	_, err := s.ClaimTask(team, id, "worker-B")
	if err == nil {
		t.Fatalf("second ClaimTask: nil error, want ErrLeaseStillValid")
	}
	if !errors.Is(err, ErrLeaseStillValid) {
		t.Errorf("error type: got %v, want ErrLeaseStillValid", err)
	}

	// Double-check that the stored task is still owned by worker-A —
	// a buggy implementation might overwrite ownership and *also*
	// return an error, leaving the on-disk state silently broken.
	got, err := s.Read(team, id)
	if err != nil {
		t.Fatalf("Read after failed claim: %v", err)
	}
	if got.Owner != "worker-A" {
		t.Errorf("Owner after failed steal: got %q, want %q", got.Owner, "worker-A")
	}
}

// TestSecondClaimSucceedsAfterLeaseExpires forces lease expiry by
// reading the task, manually rewinding LeasedUntil into the past,
// writing the result back, and then having a second worker claim.
// This is the "dead worker recovery" scenario: a worker claims, then
// the process dies, and a new worker eventually picks the task back
// up after the lease window passes. The test simulates the time
// passage rather than actually waiting 15 minutes.
//
// The mutation goes through Store.Write, NOT through a special
// back-door, so the test exercises the same write path production
// uses. If a future change adds invariant checks to Write, this test
// will catch any that break the recovery story.
func TestSecondClaimSucceedsAfterLeaseExpires(t *testing.T) {
	const team, id = "alpha", "task-3"
	s := freshStore(t, team, id)

	tokenA, err := s.ClaimTask(team, id, "worker-A")
	if err != nil {
		t.Fatalf("first ClaimTask: %v", err)
	}

	// Rewind the lease to one minute in the past. Reading from disk
	// then writing back via the same Store keeps the modification
	// honest — we are exercising the public API, not poking an
	// internal field.
	stale, err := s.Read(team, id)
	if err != nil {
		t.Fatalf("Read for rewind: %v", err)
	}
	if stale.Claim == nil {
		t.Fatalf("Claim: got nil after first claim, want populated")
	}
	stale.Claim.LeasedUntil = time.Now().Add(-time.Minute)
	if err := s.Write(team, stale); err != nil {
		t.Fatalf("Write rewound task: %v", err)
	}

	tokenB, err := s.ClaimTask(team, id, "worker-B")
	if err != nil {
		t.Fatalf("second ClaimTask after expiry: %v", err)
	}
	if tokenB == tokenA {
		t.Errorf("token: B reused A's value (got=%q); each claim must mint fresh", tokenB)
	}

	got, err := s.Read(team, id)
	if err != nil {
		t.Fatalf("Read after re-claim: %v", err)
	}
	if got.Owner != "worker-B" {
		t.Errorf("Owner: got %q, want worker-B (took over)", got.Owner)
	}
	if got.Claim.Token != tokenB {
		t.Errorf("Claim.Token: got %q, want %q (B's token)", got.Claim.Token, tokenB)
	}
}

// TestTransitionRequiresMatchingToken pins the security boundary:
// presenting the wrong token to TransitionTask must fail with
// ErrTokenMismatch, even when fromStatus and toStatus are otherwise
// legal. Without this check anyone with read access to the task file
// could see Status=in_progress and "complete" the task by guessing.
func TestTransitionRequiresMatchingToken(t *testing.T) {
	const team, id = "alpha", "task-4"
	s := freshStore(t, team, id)

	tokenA, err := s.ClaimTask(team, id, "worker-A")
	if err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}

	// First, the negative case: wrong token, must fail.
	bogus := "deadbeefdeadbeefdeadbeefdeadbeef"
	err = s.TransitionTask(team, id, "worker-A", bogus, "in_progress", "done")
	if err == nil {
		t.Fatalf("TransitionTask with bogus token: nil error, want ErrTokenMismatch")
	}
	if !errors.Is(err, ErrTokenMismatch) {
		t.Errorf("error type: got %v, want ErrTokenMismatch", err)
	}

	// Defense-in-depth: confirm the on-disk task is still
	// in_progress, not silently transitioned to done.
	got, err := s.Read(team, id)
	if err != nil {
		t.Fatalf("Read after failed transition: %v", err)
	}
	if got.Status != "in_progress" {
		t.Errorf("Status: got %q, want %q (transition should have rejected)",
			got.Status, "in_progress")
	}

	// Then the positive case: with the real token, the same
	// transition succeeds. This proves the failure was about the
	// token specifically, not about the from/to pair being illegal.
	if err := s.TransitionTask(team, id, "worker-A", tokenA,
		"in_progress", "done"); err != nil {
		t.Fatalf("TransitionTask with real token: %v", err)
	}

	got, err = s.Read(team, id)
	if err != nil {
		t.Fatalf("Read after real transition: %v", err)
	}
	if got.Status != "done" {
		t.Errorf("Status: got %q, want %q", got.Status, "done")
	}
	if got.Claim != nil {
		t.Errorf("Claim: got %+v, want nil after terminal transition", got.Claim)
	}
}
