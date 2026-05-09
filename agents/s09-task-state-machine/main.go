package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// main is the chapter's runnable demo. It walks through the canonical
// claim/transition flow without any goroutines:
//
//  1. Set up a fresh tempdir as the store root.
//  2. Seed one pending task on disk (the equivalent of an upstream
//     `TaskCreate` MCP call).
//  3. Worker A claims it → success, prints token characters >0.
//  4. Worker B tries to claim → fails with ErrLeaseStillValid.
//  5. Worker A transitions in_progress → done.
//  6. Print the final task JSON so the captured fixture pins down the
//     terminal shape (status=done, claim cleared, version=3).
//
// The output format follows the s07/s08 "== section ==" banner so a
// reader flipping between chapters sees the same shape. The fixture
// captures the *number of token chars* (32) rather than the actual
// hex value — tokens are random, so the value would change on every
// run. The character count is stable and proves the mint succeeded.
func main() {
	// (1) Tempdir + store. Using os.MkdirTemp instead of a fixed path
	// keeps the demo idempotent: re-running `go run .` never collides
	// with a prior invocation, and CI's working tree stays clean. The
	// dir is removed on success; a failure leaves it for inspection.
	tmpRoot, err := os.MkdirTemp("", "s09-demo-")
	if err != nil {
		fmt.Printf("MkdirTemp: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpRoot)

	// The store root mimics the upstream layout's `<cwd>/.omc/state/team`
	// segment. Tasks for team `demo` will live at
	// <root>/demo/tasks/<id>.json.
	storeRoot := filepath.Join(tmpRoot, ".omc", "state", "team")
	store := NewStore(storeRoot)

	const team = "demo"
	const taskID = "fix-login"

	// (2) Seed a pending task. In production this would be the
	// `TaskCreate` MCP tool call from the lead agent; here we open-
	// code the equivalent because we are below that abstraction layer.
	now := time.Now()
	seed := &Task{
		ID:          taskID,
		Status:      "pending",
		Version:     1,
		Description: "Fix the broken login flow in src/auth/login.go",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.Write(team, seed); err != nil {
		fmt.Printf("seed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("== Seed: pending task ==")
	fmt.Printf("team=%s id=%s status=%s version=%d\n",
		team, seed.ID, seed.Status, seed.Version)
	fmt.Println()

	// (3) Worker A claims. This is the success path: the task is
	// pending, no prior claim, lease is moot. ClaimTask returns the
	// minted token; Worker A must hold onto this for any future
	// transition.
	fmt.Println("== Worker A: ClaimTask ==")
	tokenA, err := store.ClaimTask(team, taskID, "worker-A")
	if err != nil {
		fmt.Printf("ClaimTask: %v\n", err)
		os.Exit(1)
	}
	// We print the token character count, not the value, because the
	// value is freshly random per run. Stable: 32 hex chars (16 bytes
	// of entropy from randomUUID). The expected fixture asserts on
	// exactly that.
	fmt.Printf("worker=worker-A token_len=%d\n", len(tokenA))
	fmt.Println()

	// (4) Worker B tries to claim. The lease is fresh (Worker A just
	// minted it), so the contention check fires and we get
	// ErrLeaseStillValid. The demo exits the error path normally — a
	// runtime would log this and pick up a different task.
	fmt.Println("== Worker B: ClaimTask (must fail) ==")
	_, errB := store.ClaimTask(team, taskID, "worker-B")
	if errB == nil {
		fmt.Println("UNEXPECTED: worker-B claim succeeded")
		os.Exit(1)
	}
	if !errors.Is(errB, ErrLeaseStillValid) {
		fmt.Printf("UNEXPECTED error type: %v\n", errB)
		os.Exit(1)
	}
	fmt.Printf("worker=worker-B err=ErrLeaseStillValid (expected)\n")
	fmt.Println()

	// (5) Worker A transitions in_progress → done. The token from
	// step 3 is the proof of ownership; without it the call would
	// return ErrTokenMismatch.
	fmt.Println("== Worker A: TransitionTask in_progress -> done ==")
	if err := store.TransitionTask(team, taskID, "worker-A", tokenA,
		"in_progress", "done"); err != nil {
		fmt.Printf("TransitionTask: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("transition=ok")
	fmt.Println()

	// (6) Final state. Read back from disk (no in-memory cache means
	// this is a real round-trip) and print as canonical JSON. We zero
	// the timestamps so the captured fixture is reproducible across
	// runs — wall-clock time would otherwise change every invocation.
	final, err := store.Read(team, taskID)
	if err != nil {
		fmt.Printf("Read final: %v\n", err)
		os.Exit(1)
	}
	final.CreatedAt = time.Time{}
	final.UpdatedAt = time.Time{}

	out, err := json.MarshalIndent(final, "", "  ")
	if err != nil {
		fmt.Printf("Marshal final: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("== Final task on disk ==")
	fmt.Println(string(out))
}
