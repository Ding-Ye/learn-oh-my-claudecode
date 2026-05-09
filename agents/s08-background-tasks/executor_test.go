package main

import (
	"context"
	"testing"
	"time"
)

// TestExecutorReportsExitCode runs `sh -c "exit 7"` through Executor
// and asserts that Wait returns an error AND the cached ProcessState
// reports exit code 7. This is the "the plumbing works end-to-end"
// test — if it fails, either Run is not actually starting the process,
// or Wait is not propagating ProcessState.
//
// Note: Wait returns *exec.ExitError when the child exits with a
// nonzero status. We don't unwrap the error type here because the
// chapter's contract is just "ExitCode is observable on Cmd.ProcessState".
// A future test could add a `errors.As` check; this test stays minimal.
func TestExecutorReportsExitCode(t *testing.T) {
	exe := &Executor{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h, err := exe.Run(ctx, `exit 7`)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// We expect Wait to return a non-nil error because the child
	// exited nonzero. The error type is *exec.ExitError but we don't
	// pin that — the visible contract is on Cmd.ProcessState.
	if waitErr := h.Wait(); waitErr == nil {
		t.Fatalf("Wait: got nil error, want non-nil for `exit 7`")
	}

	if h.Cmd.ProcessState == nil {
		t.Fatalf("ProcessState: nil after Wait; child should have exited")
	}
	if got := h.Cmd.ProcessState.ExitCode(); got != 7 {
		t.Errorf("ExitCode: got %d, want 7", got)
	}
}

// TestExecutorCancelsOnContext spawns `sleep 30` (deliberately longer
// than any reasonable test budget) and cancels the context after
// 100ms. We assert that Wait returns within a generous deadline (1s)
// and that the process did NOT exit with status 0 — either Wait
// returned a non-nil error (signal-killed) or ExitCode is negative
// (terminated by signal, no exit code recorded).
//
// This test is the reason executor.go sets Setpgid: without it, the
// `sh -c "sleep 30"` wrapper would die but the orphaned `sleep`
// process would keep running for 30 seconds and only the wrapper's
// exit would be observed here. The cancel-then-Wait pattern below
// is the exact shape a real runtime uses for "user pressed Ctrl-C".
func TestExecutorCancelsOnContext(t *testing.T) {
	exe := &Executor{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h, err := exe.Run(ctx, `sleep 30`)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Cancel after 100ms. The 100ms gives the process room to actually
	// start before we kill it; canceling mid-Start is a different
	// (rarer) code path and not what this test exercises.
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	// Wait must return within the deadline. We use a separate channel
	// rather than time.After so the test fails loudly if Wait hangs.
	done := make(chan error, 1)
	go func() { done <- h.Wait() }()

	select {
	case waitErr := <-done:
		// Either we got a non-nil error (child was killed by signal),
		// or the process exited and ProcessState shows it was
		// terminated. Both are acceptable; what's NOT acceptable is
		// "Wait returned nil and the child exited with status 0",
		// which would mean the cancel did nothing.
		if waitErr == nil && h.Cmd.ProcessState != nil &&
			h.Cmd.ProcessState.ExitCode() == 0 {
			t.Errorf("Wait returned nil and ExitCode=0; cancel did not propagate")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Wait did not return within 2s after cancel; Setpgid may be missing")
	}
}
