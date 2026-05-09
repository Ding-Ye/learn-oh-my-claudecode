package main

import "testing"

// TestDecideRecommendsBackgroundForNpmInstall pins the canonical
// long-running case. `npm install` is the first regex in
// LongRunningPatterns and the most common real-world command users
// expect to be backgrounded; if this test ever fails, the pattern
// slice has been edited or the priority order in Decide has flipped.
func TestDecideRecommendsBackgroundForNpmInstall(t *testing.T) {
	got := Decide("npm install", 0, DefaultMaxBackgroundTasks)

	if !got.Background {
		t.Fatalf("Background: got false, want true for `npm install`")
	}
	if got.Confidence != "high" {
		t.Errorf("Confidence: got %q, want %q", got.Confidence, "high")
	}
	if got.Reason == "" {
		t.Errorf("Reason: want non-empty for a high-confidence recommendation")
	}
}

// TestDecideKeepsForegroundForGitStatus pins the canonical blocking
// case. `git status` is in BlockingPatterns; the runtime should never
// background it because the user is staring at the terminal expecting
// the diff inline. Confidence stays high because a regex DID fire —
// "low" is reserved for the no-pattern-matched default branch.
func TestDecideKeepsForegroundForGitStatus(t *testing.T) {
	got := Decide("git status", 0, DefaultMaxBackgroundTasks)

	if got.Background {
		t.Fatalf("Background: got true, want false for `git status`")
	}
	if got.Confidence != "high" {
		t.Errorf("Confidence: got %q, want %q", got.Confidence, "high")
	}
	if got.Reason == "" {
		t.Errorf("Reason: want non-empty when a blocking pattern fired")
	}
}

// TestDecideRespectsConcurrencyCap proves the cap branch fires before
// the regex branches. With runningCount == max the function must return
// foreground regardless of the command — even an obvious long-running
// one like `npm install`. The reason field encodes WHY foreground was
// chosen so the runtime can distinguish "cap full, queue this" from
// "command is fast, run it inline".
func TestDecideRespectsConcurrencyCap(t *testing.T) {
	const max = 5
	got := Decide("npm install", max, max)

	if got.Background {
		t.Fatalf("Background: got true, want false when running==max==%d", max)
	}
	if got.Reason != "concurrency cap reached" {
		t.Errorf("Reason: got %q, want %q", got.Reason, "concurrency cap reached")
	}
	if got.Confidence != "high" {
		t.Errorf("Confidence: got %q, want %q (the cap is a hard limit)",
			got.Confidence, "high")
	}
}
