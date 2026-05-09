package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// scriptsDir resolves the testdata/scripts path relative to the package
// dir. Go runs each test with the package dir as cwd, so a relative path
// works — we just spell it explicitly so a future move of testdata is
// caught at one place rather than scattered across five tests.
func scriptsDir(t *testing.T) string {
	t.Helper()
	return filepath.Join("testdata", "scripts")
}

// TestDispatchRunsMatchingHook checks the happy path: a single "*"
// matcher with one echo hook produces one Result, exit=0, no error.
func TestDispatchRunsMatchingHook(t *testing.T) {
	mf := Manifest{
		"UserPromptSubmit": []Entry{
			{
				Matcher: "*",
				Hooks: []Hook{
					{Type: "command", Command: "sh " + filepath.Join(scriptsDir(t), "echo.sh"), Timeout: 5 * time.Second},
				},
			},
		},
	}
	d := &Dispatcher{Manifest: mf}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	results := d.Dispatch(ctx, "UserPromptSubmit", []byte(`{"prompt":"hello"}`))
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Err != nil {
		t.Errorf("unexpected error: %v", r.Err)
	}
	if r.ExitCode != 0 {
		t.Errorf("exit code: got %d, want 0", r.ExitCode)
	}
	if r.Event != "UserPromptSubmit" {
		t.Errorf("event: got %q", r.Event)
	}
	if r.Matcher != "*" {
		t.Errorf("matcher: got %q, want *", r.Matcher)
	}
}

// TestDispatchSkipsNonMatchingHook checks that a matcher that is NOT a
// substring of the payload silently filters its hooks out — no Result
// emitted, no error returned. Two entries are in the manifest: one with
// matcher "*" (always fires) and one with matcher "init" (only fires if
// the payload mentions "init"). With a payload that does not mention
// "init", we should see exactly one Result, from the "*" entry.
func TestDispatchSkipsNonMatchingHook(t *testing.T) {
	echoCmd := "sh " + filepath.Join(scriptsDir(t), "echo.sh")
	mf := Manifest{
		"SessionStart": []Entry{
			{Matcher: "*", Hooks: []Hook{{Type: "command", Command: echoCmd, Timeout: 5 * time.Second}}},
			{Matcher: "init", Hooks: []Hook{{Type: "command", Command: echoCmd, Timeout: 5 * time.Second}}},
		},
	}
	d := &Dispatcher{Manifest: mf}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	results := d.Dispatch(ctx, "SessionStart", []byte(`{"prompt":"hello"}`))
	if len(results) != 1 {
		t.Fatalf("want 1 result (only the * matcher fires), got %d", len(results))
	}
	if results[0].Matcher != "*" {
		t.Errorf("matcher: got %q, want *", results[0].Matcher)
	}

	// Now flip the payload so it contains "init" — both entries should fire.
	results = d.Dispatch(ctx, "SessionStart", []byte(`{"reason":"init"}`))
	if len(results) != 2 {
		t.Fatalf("want 2 results when payload mentions 'init', got %d", len(results))
	}
}

// TestDispatchEnforcesTimeout uses sleep_too_long.sh (sleep 30) with a
// 1-second timeout. The hook context must fire DeadlineExceeded; the
// dispatcher must wrap it in Result.Err and the exit code must NOT be 0.
//
// On macOS, exec.CommandContext sends SIGKILL when the context expires,
// so cmd.Run returns "signal: killed" — we don't assert on that exact
// string, only on the wrapped DeadlineExceeded sentinel and the
// non-success exit code.
func TestDispatchEnforcesTimeout(t *testing.T) {
	mf := Manifest{
		"UserPromptSubmit": []Entry{
			{
				Matcher: "*",
				Hooks: []Hook{
					{Type: "command", Command: "sh " + filepath.Join(scriptsDir(t), "sleep_too_long.sh"), Timeout: 1 * time.Second},
				},
			},
		},
	}
	d := &Dispatcher{Manifest: mf}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	results := d.Dispatch(ctx, "UserPromptSubmit", []byte(`{}`))
	elapsed := time.Since(start)

	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	r := results[0]

	if r.Err == nil {
		t.Fatalf("expected error from timed-out hook, got nil")
	}
	if !errors.Is(r.Err, context.DeadlineExceeded) {
		t.Errorf("error should wrap context.DeadlineExceeded, got: %v", r.Err)
	}
	if r.ExitCode == 0 {
		t.Errorf("exit code should be non-zero on timeout, got 0")
	}
	if elapsed > 4*time.Second {
		t.Errorf("dispatch took %v; timeout (1s) should have killed the sleep before then", elapsed)
	}
}

// TestDispatchPassesPayloadOnStdin checks that the JSON payload is piped
// to the hook's stdin. echo.sh is `cat`, so its stdout is whatever stdin
// it received; we assert the payload bytes appear in Result.Stdout.
func TestDispatchPassesPayloadOnStdin(t *testing.T) {
	mf := Manifest{
		"UserPromptSubmit": []Entry{
			{
				Matcher: "*",
				Hooks: []Hook{
					{Type: "command", Command: "sh " + filepath.Join(scriptsDir(t), "echo.sh"), Timeout: 5 * time.Second},
				},
			},
		},
	}
	d := &Dispatcher{Manifest: mf}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	payload := `{"prompt":"the boulder never stops"}`
	results := d.Dispatch(ctx, "UserPromptSubmit", []byte(payload))
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	if !strings.Contains(results[0].Stdout, payload) {
		t.Errorf("stdout should contain payload; got %q", results[0].Stdout)
	}
}

// TestDispatchCollectsAllResultsEvenWhenOneFails wires two hooks under
// the same matcher: the first calls a missing command (sh's "command not
// found" exits 127), the second is the well-behaved echo. We expect
// len(results)==2 and results[0].Err non-nil but results[1].Err nil —
// proving siblings do not abort each other.
func TestDispatchCollectsAllResultsEvenWhenOneFails(t *testing.T) {
	mf := Manifest{
		"UserPromptSubmit": []Entry{
			{
				Matcher: "*",
				Hooks: []Hook{
					{Type: "command", Command: "definitely-not-a-real-binary-9f7a1c", Timeout: 5 * time.Second},
					{Type: "command", Command: "sh " + filepath.Join(scriptsDir(t), "echo.sh"), Timeout: 5 * time.Second},
				},
			},
		},
	}
	d := &Dispatcher{Manifest: mf}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	results := d.Dispatch(ctx, "UserPromptSubmit", []byte(`{}`))
	if len(results) != 2 {
		t.Fatalf("want 2 results (failed + ok), got %d", len(results))
	}
	if results[0].Err == nil {
		t.Errorf("first hook (bad command) should have an error")
	}
	if results[0].ExitCode == 0 {
		t.Errorf("first hook exit code should be non-zero, got 0")
	}
	if results[1].Err != nil {
		t.Errorf("second hook (echo) should succeed, got %v", results[1].Err)
	}
	if results[1].ExitCode != 0 {
		t.Errorf("second hook exit code should be 0, got %d", results[1].ExitCode)
	}
}

// TestLoadManifestParsesTimeoutSeconds is a small sanity test for the
// custom UnmarshalJSON: a JSON literal with `"timeout": 5` must come back
// as 5*time.Second, not 5 nanoseconds (which is what the stdlib would do
// for a naive time.Duration field).
func TestLoadManifestParsesTimeoutSeconds(t *testing.T) {
	mf, err := LoadManifest(filepath.Join("testdata", "hooks.json"))
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	entries, ok := mf["UserPromptSubmit"]
	if !ok || len(entries) == 0 {
		t.Fatalf("UserPromptSubmit should have at least one entry")
	}
	first := entries[0].Hooks[0]
	if first.Timeout < time.Second {
		t.Errorf("timeout: parsed as %v, want at least 1s — UnmarshalJSON likely treated value as nanoseconds", first.Timeout)
	}
}
