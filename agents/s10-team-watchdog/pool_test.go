package main

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestPoolDispatchesAllTasks is the happy-path test: submit N tasks
// to a Pool, run, observe all reach status=done. The simplest
// possible verification that the goroutine + channel architecture
// actually delivers tasks.
//
// Mirrors the upstream "no failures, no respawns, all panes finish"
// flow — but checked via store.Read instead of pane introspection.
func TestPoolDispatchesAllTasks(t *testing.T) {
	store := NewStore(t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := New(ctx, 2, store)

	const numTasks = 5
	for i := 0; i < numTasks; i++ {
		err := pool.Submit(Task{
			ID:          fmt.Sprintf("t%d", i),
			Description: "happy-path task",
			WorkSeconds: 0.05,
		})
		if err != nil {
			t.Fatalf("Submit %d: %v", i, err)
		}
	}

	if err := pool.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Every task must be terminal=done.
	for i := 0; i < numTasks; i++ {
		id := fmt.Sprintf("t%d", i)
		got, err := store.Read(id)
		if err != nil {
			t.Fatalf("Read %s: %v", id, err)
		}
		if got.Status != "done" {
			t.Errorf("task %s: status=%q, want done", id, got.Status)
		}
	}

	if err := pool.Shutdown(2 * time.Second); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

// TestShutdownDrainsInflightTasks pins down the Pool.Shutdown
// contract: after Shutdown returns, every in-flight task either
// completed naturally or was cancelled, and every goroutine has
// exited. The test verifies this by:
//
//  1. Submitting 4 short tasks against 2 workers.
//  2. Running until completion via Run.
//  3. Calling Shutdown with a 1-second grace.
//  4. Asserting all tasks are done AND Shutdown returns nil.
//
// A leaked worker goroutine would manifest as Shutdown timing out
// (returning a non-nil error). The test fails fast on either symptom.
func TestShutdownDrainsInflightTasks(t *testing.T) {
	store := NewStore(t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := New(ctx, 2, store)

	for i := 0; i < 4; i++ {
		err := pool.Submit(Task{
			ID:          fmt.Sprintf("drain-%d", i),
			WorkSeconds: 0.05,
		})
		if err != nil {
			t.Fatalf("Submit %d: %v", i, err)
		}
	}

	if err := pool.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Shutdown should return nil — no goroutine is held up.
	if err := pool.Shutdown(1 * time.Second); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// Verify final state: all tasks done.
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("drain-%d", i)
		got, err := store.Read(id)
		if err != nil {
			t.Fatalf("Read %s: %v", id, err)
		}
		if got.Status != "done" {
			t.Errorf("task %s: status=%q, want done", id, got.Status)
		}
	}
}
