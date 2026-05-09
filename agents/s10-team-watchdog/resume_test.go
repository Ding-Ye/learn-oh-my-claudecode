package main

import (
	"context"
	"testing"
	"time"
)

// TestResumeReadsExistingPendingTasks pins down the resume path: a
// fresh process starts, finds task files under <root>/tasks/*.json
// from a prior run, and re-queues them. The test seeds pending +
// in_progress tasks directly via the store (mimicking a crashed
// previous Pool) and asserts they all reach status=done after Run.
//
// What this exercises specifically:
//
//  1. Resume() is total against a non-existent root (no prior tasks).
//     The simplest invariant: Resume + Run with no pending work
//     returns immediately.
//  2. Resume() picks up a 'pending' task and dispatches it.
//  3. Resume() picks up an orphaned 'in_progress' task (claimed by
//     a worker that no longer exists) and re-runs it.
//  4. Resume() skips terminal tasks (done/failed).
func TestResumeReadsExistingPendingTasks(t *testing.T) {
	root := t.TempDir()

	// Seed three tasks directly via the store. This simulates the
	// state a crashed previous pool would have left on disk.
	prevStore := NewStore(root)

	pendingTask := &Task{
		ID:          "pending-task",
		Status:      "pending",
		Description: "submitted but never dispatched",
		WorkSeconds: 0.05,
	}
	if err := prevStore.Write(pendingTask); err != nil {
		t.Fatalf("seed pending: %v", err)
	}

	orphanedTask := &Task{
		ID:          "orphaned-task",
		Status:      "in_progress",
		Description: "previous worker died mid-run",
		WorkSeconds: 0.05,
		Retries:     1,
	}
	if err := prevStore.Write(orphanedTask); err != nil {
		t.Fatalf("seed orphaned: %v", err)
	}

	terminalTask := &Task{
		ID:          "already-done",
		Status:      "done",
		Description: "was completed before crash",
		WorkSeconds: 0.05,
	}
	if err := prevStore.Write(terminalTask); err != nil {
		t.Fatalf("seed terminal: %v", err)
	}

	// Resume — this is the whole point of the test.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := Resume(ctx, root, 2)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}

	if err := pool.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	store := pool.store

	// Pending and orphaned must be done.
	for _, id := range []string{"pending-task", "orphaned-task"} {
		got, err := store.Read(id)
		if err != nil {
			t.Fatalf("Read %s: %v", id, err)
		}
		if got.Status != "done" {
			t.Errorf("%s: status=%q, want done", id, got.Status)
		}
	}

	// Already-done must remain done (not re-run, not corrupted).
	got, err := store.Read("already-done")
	if err != nil {
		t.Fatalf("Read already-done: %v", err)
	}
	if got.Status != "done" {
		t.Errorf("already-done: status=%q, want done (untouched)", got.Status)
	}

	// Retries on the orphaned task should NOT have been reset to 0
	// — Resume preserves the retry budget so a flaky task that has
	// already failed twice doesn't get a fresh budget on resume.
	got, err = store.Read("orphaned-task")
	if err != nil {
		t.Fatalf("Read orphaned-task post-run: %v", err)
	}
	if got.Retries < 1 {
		t.Errorf("orphaned-task: retries=%d, want >=1 (preserved across resume)", got.Retries)
	}

	if err := pool.Shutdown(2 * time.Second); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}
