package main

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"
)

// Handle is what Executor.Run returns. It bundles the *exec.Cmd (so
// callers can read Pid, Stdout, etc.) with a Wait closure that blocks
// until the process exits. Splitting Wait into a separate field keeps
// the API symmetric with Go's `errgroup` style: start, then wait, then
// check the error.
//
// The closure form (rather than a method) is deliberate — it captures
// any state the executor wants to clean up after Wait returns (e.g.,
// re-parenting reaping logic) without exposing those internals on the
// struct. Tests can mock Handle by constructing one directly.
type Handle struct {
	// Cmd is the underlying *exec.Cmd. Cmd.Process is non-nil after a
	// successful Run; Cmd.ProcessState is non-nil after Wait returns.
	// Callers may inspect Cmd.Process.Pid or assign Cmd.Stdout BEFORE
	// they call Wait — but doing so AFTER Run has already been called
	// is a race against the spawned process.
	Cmd *exec.Cmd

	// Wait blocks until the process exits and returns the exit error,
	// or nil on success. Calling Wait more than once is allowed and
	// returns the cached result — exec.Cmd.Wait is single-shot
	// internally, so we wrap it in a sync.Once-style memoizer below.
	Wait func() error
}

// Executor is the impure layer that actually spawns processes. It is
// stateless — every Executor instance behaves the same — but kept as a
// struct so future enhancements (logging hook, max-concurrency tracker,
// custom env injection) can land without changing the call site.
//
// Composition note: Decide is the "should we?" function and Executor
// is the "do it" function. The chapter intentionally keeps them in
// separate files so a test can use Decide without ever touching
// os/exec, and a different test can use Executor without ever touching
// the regex slices. This is the first session where the pattern
// "pure recommender + impure side-effecter" is laid out explicitly.
type Executor struct{}

// Run starts cmd in the background and returns a *Handle whose Wait
// closure blocks until the process exits. The command is parsed by
// `sh -c "<command>"` so the caller can pass any shell expression
// (`exit 7`, `sleep 30`, `npm install && echo done`); we don't try
// to tokenize ourselves because the upstream feature accepts shell
// strings for the same reason.
//
// Cancellation is via the passed context. exec.CommandContext arranges
// for SIGKILL when ctx is canceled; on Unix we additionally call
// Setpgid so the kill propagates to the entire process group, not just
// the immediate `sh` wrapper. Without Setpgid, a `sh -c "sleep 30"`
// that gets canceled would kill `sh` but leave `sleep` orphaned —
// which is the exact failure mode the chapter test
// TestExecutorCancelsOnContext is designed to catch.
//
// The function does NOT call cmd.Wait — that is the caller's job via
// Handle.Wait. Calling Run and discarding the handle leaks the child;
// the chapter is careful to always Wait, even in the demo.
func (e *Executor) Run(ctx context.Context, command string) (*Handle, error) {
	if command == "" {
		return nil, fmt.Errorf("Executor.Run: empty command")
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", command)

	// Setpgid (Unix only) puts the child in its own process group so
	// SIGKILL on context cancel propagates to grandchildren. The
	// alternative — leaving the child in our group — would mean a
	// canceled `sh -c "sleep 30"` leaves `sleep` running on its own.
	// On Windows the field is silently ignored at compile time on
	// non-Unix builds; this chapter targets Unix-likes (macOS, Linux)
	// per the repo's stated audience.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Start (NOT Run): Start returns once the child is launched, Run
	// would block until exit. The whole point of the executor is to
	// hand back a Handle the caller can interact with while the
	// process is alive.
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("Executor.Run: start %q: %w", command, err)
	}

	// Memoize Wait so the caller can call it more than once safely.
	// exec.Cmd.Wait itself returns ErrAlreadyClosed on the second
	// call; we cache the first verdict and serve it forever.
	var (
		waitDone bool
		waitErr  error
	)
	wait := func() error {
		if waitDone {
			return waitErr
		}
		waitErr = cmd.Wait()
		waitDone = true
		return waitErr
	}

	return &Handle{Cmd: cmd, Wait: wait}, nil
}
