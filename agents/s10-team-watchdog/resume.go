package main

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// Resume reconstructs a Pool from on-disk task state. Re-enqueues any
// tasks that are pending or were claimed by a worker that's no longer
// running (orphaned, i.e. status="in_progress" with no live owner —
// since the new Pool starts with zero live workers, every prior
// in_progress task is an orphan). Tasks marked done or failed are
// skipped: they have nothing to recover.
//
// The Pool returned by Resume is NOT yet running. The caller must
// invoke pool.Run() to start the worker goroutines + watchdog. This
// matches the upstream `resumeTeam` (runtime.ts L922-L990) shape:
// resume reconstructs state, then a separate `restartWatchdog` call
// starts the polling loop.
//
// Two simplifications vs. upstream:
//
//   - No tmux session liveness check. There is no tmux. We simply
//     read the task directory and re-queue.
//   - No pane-to-task mapping. Workers in the new Pool are fresh
//     goroutines; they pick up re-queued tasks via the dispatch
//     channel without needing to know which prior pane owned them.
//
// Mirrors upstream `resumeTeam` (runtime.ts L922-L990) but at ~25 LOC
// vs. ~70 — most of upstream's bulk is tmux/pane/lookup-table
// recovery that the Go port deletes by construction.
func Resume(ctx context.Context, root string, n int) (*Pool, error) {
	store := NewStore(root)
	pool := New(ctx, n, store)

	// List every persisted task. An empty/missing root yields a
	// nil-safe empty list, so a fresh start path is also legal.
	ids, err := store.List()
	if err != nil {
		return nil, fmt.Errorf("Resume: list: %w", err)
	}

	for _, id := range ids {
		t, err := store.Read(id)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				// Race: file removed between List and Read. Skip.
				continue
			}
			return nil, fmt.Errorf("Resume: read %s: %w", id, err)
		}

		// Skip terminal tasks — nothing to recover. The on-disk file
		// remains as audit history.
		if t.Status == "done" || t.Status == "failed" {
			continue
		}

		// Re-queue: pending tasks resume directly; in_progress tasks
		// are orphaned (no live worker owns them) and must restart.
		// In both cases we reset Status to pending so a new worker
		// can pick the task up cleanly.
		t.Status = "pending"
		// Don't reset Retries — a task that has already failed twice
		// shouldn't get a fresh budget on resume.
		if err := store.Write(t); err != nil {
			return nil, fmt.Errorf("Resume: persist %s: %w", id, err)
		}

		// pendingCount bookkeeping mirrors Submit's pattern. We also
		// push directly to the channel; the buffered size is N*4
		// which is sufficient for any realistic resume backlog.
		pool.pendingCountMu.Lock()
		pool.pendingCount++
		pool.pendingCountMu.Unlock()

		select {
		case pool.tasks <- *t:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return pool, nil
}
