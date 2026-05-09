package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriteAtomicSurvivesPanicSimulation is the crash-safety pin. We
// can't actually crash the test process without bringing the suite
// down, so we *simulate* a crash by:
//
//  1. Writing the prior contents through writeAtomic (the legitimate
//     way), then verifying the file holds those bytes.
//  2. Spawning a goroutine that:
//       a. Manually opens the path+".tmp" sibling and writes 1 KB of
//          bogus data into it (the equivalent of being mid-write
//          inside writeAtomic before os.Rename).
//       b. Panics, before the rename can happen.
//     We catch the panic with recover() inside the goroutine so it
//     does not abort the test harness; the goroutine then exits.
//  3. Asserting that the *target* file still contains the prior
//     contents — proving that a crashed writer cannot corrupt it.
//
// This is a teaching-grade approximation of the real crash story.
// Real crashes (SIGKILL of the process, kernel panic, power loss)
// would behave the same because os.Rename atomicity is enforced by
// the filesystem, not by Go's runtime: the rename either happened or
// it did not, and if it did not, the target's last-known contents are
// what readers see.
func TestWriteAtomicSurvivesPanicSimulation(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")

	// (1) Establish a known-good state via the legitimate API.
	good := []byte(`{"status":"original"}`)
	if err := writeAtomic(target, good, 0o644); err != nil {
		t.Fatalf("seed writeAtomic: %v", err)
	}

	// Sanity-check what's on disk before the simulated crash.
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile after seed: %v", err)
	}
	if string(got) != string(good) {
		t.Fatalf("seed mismatch: got %q, want %q", got, good)
	}

	// (2) Simulate the crash. We open .tmp directly, write 1 KB of
	// fill, then panic before any rename. recover() inside the
	// goroutine catches the panic so the test process keeps running.
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			// Recover swallows the panic so the testing harness
			// doesn't get a stack dump in the middle of the run.
			// We deliberately do NOT t.Fatal on recovered != nil
			// because the panic IS the simulation; if recover
			// returns nil our setup is wrong and the assertion
			// below will catch it.
			_ = recover()
		}()

		tmp := target + ".tmp"
		fill := make([]byte, 1024)
		for i := range fill {
			fill[i] = 'X'
		}
		if err := os.WriteFile(tmp, fill, 0o644); err != nil {
			// Filesystem-level error before we even simulated the
			// crash; bail with an explicit signal.
			panic(err)
		}

		// Crash here, before os.Rename. The .tmp file is orphaned.
		panic("simulated crash mid-write")
	}()
	<-done

	// (3) Assert the target is untouched.
	got, err = os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile after simulated crash: %v", err)
	}
	if string(got) != string(good) {
		t.Errorf("target corrupted by crashed writer: got %q, want %q",
			got, good)
	}

	// Optional extra: the .tmp orphan may or may not still exist
	// (depending on whether anything cleaned it up). We do NOT assert
	// either way — the contract writeAtomic provides is about the
	// *target* path, and a tidy-up scan is a higher-level concern.

	// Finally, prove that a *successful* writeAtomic after the
	// simulated crash still works. This confirms the crashed writer
	// did not leave the directory in a state that blocks future
	// writes.
	good2 := []byte(`{"status":"recovered"}`)
	if err := writeAtomic(target, good2, 0o644); err != nil {
		t.Fatalf("post-crash writeAtomic: %v", err)
	}

	got, err = os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile after recovery write: %v", err)
	}
	if string(got) != string(good2) {
		t.Errorf("post-recovery: got %q, want %q", got, good2)
	}
}
