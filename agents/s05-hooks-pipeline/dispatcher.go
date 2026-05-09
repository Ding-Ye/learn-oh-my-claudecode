package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// defaultHookTimeout is applied when a hook entry omits the timeout field
// (or sets it to 0). Five seconds is the upstream default for short
// notify-style hooks like keyword-detector.mjs; long jobs (session-end,
// setup-init) declare their own larger value in the manifest.
const defaultHookTimeout = 5 * time.Second

// Result is one hook's outcome. The dispatcher does not abort siblings on
// failure: a Result with a non-nil Err is returned alongside the
// successful results, and the caller decides what to do. This mirrors the
// upstream `Promise.allSettled` posture — a flapping post-tool hook must
// not block a clean session-end hook from running.
type Result struct {
	Event    string // event name the hook was fired for ("UserPromptSubmit")
	Matcher  string // entry matcher that selected this hook ("*" or "init")
	Command  string // exact command string (verbatim from manifest)
	ExitCode int    // process exit code; -1 when the process did not start
	Stdout   string // captured stdout
	Stderr   string // captured stderr
	Err      error  // non-nil if the timeout fired or sh -c returned non-zero
}

// Dispatcher is the manifest plus a configurable shell. Tests can swap
// the shell ("bash") or fall through to the default ("sh") without
// touching the dispatch logic. We keep no other state — every Dispatch
// call is independent, so the same dispatcher can handle concurrent
// events from different goroutines.
type Dispatcher struct {
	Manifest Manifest
	// Shell is the program used to run each hook command, defaulting to
	// "sh". Override only for tests or platform-specific shells.
	Shell string
}

// Dispatch runs every hook in every entry whose Matcher accepts the
// event, piping payloadJSON to each hook's stdin. Errors from individual
// hooks do NOT abort siblings — the slice we return contains one Result
// per attempted hook, in declaration order, with the per-hook error (if
// any) carried in Result.Err.
//
// The ctx parameter bounds the entire batch; per-hook timeouts are
// derived via context.WithTimeout(ctx, hook.Timeout). If the parent ctx
// is canceled, every still-running hook is canceled with it.
func (d *Dispatcher) Dispatch(ctx context.Context, event string, payloadJSON []byte) []Result {
	entries := d.Manifest[event]
	if len(entries) == 0 {
		return nil
	}

	var results []Result
	for _, entry := range entries {
		if !matches(entry.Matcher, payloadJSON) {
			continue
		}
		for _, h := range entry.Hooks {
			results = append(results, d.runHook(ctx, event, entry.Matcher, h, payloadJSON))
		}
	}
	return results
}

// runHook executes a single hook and packages its outcome into a Result.
// Each hook gets its own derived context with the per-hook timeout, so
// one slow hook can't burn the next one's budget.
func (d *Dispatcher) runHook(ctx context.Context, event, matcher string, h Hook, payload []byte) Result {
	timeout := h.Timeout
	if timeout <= 0 {
		timeout = defaultHookTimeout
	}
	hookCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	shell := d.Shell
	if shell == "" {
		shell = "sh"
	}

	cmd := exec.CommandContext(hookCtx, shell, "-c", h.Command)
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Put the hook into its own process group so we can SIGKILL the
	// whole subtree on timeout. Without Setpgid, exec.CommandContext
	// only kills the immediate `sh -c` child — a `sh -c "sh script.sh"`
	// command then leaks the inner `sh` and any `sleep` it forked, and
	// cmd.Wait blocks on the inherited stdio pipes until those orphans
	// close. With our own group, the Cancel below targets the group
	// (-pgid) and everything dies together.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Cancel overrides the default "kill the leader" behavior with a
	// group-wide signal. WaitDelay gives a second-chance window to
	// reap any straggler — Go releases the inherited pipes after the
	// delay even if the descendants haven't died yet. (Go 1.20+.)
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 500 * time.Millisecond

	res := Result{
		Event:    event,
		Matcher:  matcher,
		Command:  h.Command,
		ExitCode: -1,
	}

	err := cmd.Run()
	res.Stdout = stdout.String()
	res.Stderr = stderr.String()

	// hookCtx.Err() is the canonical signal that we hit the timeout —
	// cmd.Run's error wraps the kill signal but doesn't always mention
	// DeadlineExceeded directly, so we check the context first.
	if hookCtx.Err() != nil && errors.Is(hookCtx.Err(), context.DeadlineExceeded) {
		res.Err = fmt.Errorf("hook %q timed out after %s: %w", h.Command, timeout, context.DeadlineExceeded)
		return res
	}

	if err != nil {
		// exec.ExitError carries the exit code; everything else means
		// the process never started or was killed for some other reason.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
		}
		res.Err = err
		return res
	}

	res.ExitCode = 0
	return res
}

// matches decides whether an entry's Matcher accepts the given event
// payload. "*" wildcards everything (the common case in the upstream
// manifest); any other Matcher value is treated as a substring against
// the JSON payload — that is enough to honor matchers like "init" or
// "Bash" without requiring a regex engine. Upstream's `keyword-detector`
// scripts do the same kind of textual sniffing inside Node, so this
// faithful-to-upstream simplification is on purpose.
func matches(matcher string, payload []byte) bool {
	if matcher == "" || matcher == "*" {
		return true
	}
	return strings.Contains(string(payload), matcher)
}
