package main

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// Task is the unit of work scheduled by the Pool. The shape mirrors
// s09's Task with three deliberate shrinks:
//
//   - We carry no Owner / Claim / Version fields. s09 already proved
//     the file-backed CAS story; s10's job is to demonstrate the
//     goroutine-pool dispatch story without re-running that lesson.
//   - We add WorkSeconds, a simulated work duration. Real production
//     tasks call out to a CLI agent (claude / codex / gemini); the
//     teaching version sleeps for WorkSeconds so a test can assert
//     ordering without invoking external processes.
//   - We add Retries, the per-task retry counter. The watchdog
//     increments it when a worker dies mid-task; once it crosses
//     `maxRetries` (Pool field, default 3) the task is marked failed.
//     Upstream's `applyDeadPaneTransition` (runtime.ts L515-L545)
//     does the same arithmetic in TypeScript.
//
// JSON tags use snake_case to align with s09's on-disk convention so a
// future merge of the two stores is purely additive.
type Task struct {
	// ID identifies the task. NewTaskID() mints a 16-hex-char value;
	// callers may also pass deterministic IDs in tests.
	ID string `json:"id"`

	// Status is the task's lifecycle state, mirroring s09:
	//   - "pending"     — waiting for a worker
	//   - "in_progress" — a worker is currently running this task
	//   - "done"        — terminal success
	//   - "failed"      — terminal failure (after maxRetries panics)
	Status string `json:"status"`

	// Description is human-readable context, printed by the demo.
	Description string `json:"description,omitempty"`

	// WorkSeconds is how long the simulated work should take. Real
	// production tasks would replace this with a `claude --prompt …`
	// exec. We keep the float-second granularity because most demos
	// run in 100-500ms, well below the 1-second watchdog tick.
	WorkSeconds float64 `json:"work_seconds,omitempty"`

	// Retries counts the number of times the task has been re-queued
	// after a worker failure. The watchdog bumps this; the Pool checks
	// it against maxRetries before deciding to fail vs. re-queue.
	Retries int `json:"retries,omitempty"`

	// Panic, when true, instructs the worker to panic instead of
	// finishing normally. This is the test-only knob that drives
	// TestWatchdogRespawnsCrashedWorker and the retry-failure test.
	// In production the field is always false; the watchdog reacts to
	// real crashes the same way it reacts to simulated ones.
	Panic bool `json:"panic,omitempty"`
}

// NewTaskID returns 16 random hex chars (8 bytes of entropy). We do
// not pull in github.com/google/uuid for this — the chapter is meant
// to stay stdlib-only on the dependency front, and 64 bits of entropy
// is plenty for a single Pool's task IDs (collision probability is
// negligible at any realistic queue size). For the canonical UUID
// shape, swap to crypto/rand on a 16-byte buffer.
func NewTaskID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand only fails on broken systems. Falling back to
		// a wall-clock ID would silently weaken the uniqueness
		// guarantee, so we instead emit a deterministic sentinel that
		// will obviously collide if it ever appears in production.
		return "00000000-bad-rand"
	}
	return hex.EncodeToString(buf[:])
}

// nowJSON is a tiny helper used by demo output and store writes when a
// timestamp is needed in a stable format. Kept here (not in main.go)
// because the store writes use it too.
func nowJSON() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
