package main

import (
	"strings"
	"testing"
)

// TestDetectsHighConfidenceClaim verifies that an unambiguous
// completion claim with no hedge words returns {Claimed: true,
// Confidence: "high"}. This is the response shape the runtime *trusts*
// — no further verification reminder is injected.
func TestDetectsHighConfidenceClaim(t *testing.T) {
	got := DetectCompletion("I have completed all tasks.")

	if !got.Claimed {
		t.Fatalf("Claimed: got false, want true")
	}
	if got.Confidence != "high" {
		t.Errorf("Confidence: got %q, want %q", got.Confidence, "high")
	}
	if got.Reason == "" {
		t.Errorf("Reason: want non-empty for high-confidence claims")
	}
}

// TestDetectsLowConfidenceClaim verifies the hedge-word demotion path.
// "I think this should be working" contains BOTH a completion-ish
// claim ("working") and two uncertainty markers ("I think", "should");
// the verdict must be Claimed=true with Confidence="low" so the runtime
// knows to ask for verification.
func TestDetectsLowConfidenceClaim(t *testing.T) {
	got := DetectCompletion("I think this should be working now.")

	if !got.Claimed {
		t.Fatalf("Claimed: got false, want true (hedge words still constitute a claim)")
	}
	if got.Confidence != "low" {
		t.Errorf("Confidence: got %q, want %q", got.Confidence, "low")
	}
}

// TestNoClaimReturnsClaimedFalse covers the "still working" path.
// "Still investigating the bug." is neither a completion claim nor a
// hedge — it is in-progress narration. The function must return
// Claimed=false and an empty Confidence (the field is meaningless when
// no claim is on the table).
func TestNoClaimReturnsClaimedFalse(t *testing.T) {
	got := DetectCompletion("Still investigating the bug.")

	if got.Claimed {
		t.Fatalf("Claimed: got true, want false (response is mid-investigation)")
	}
	if got.Confidence != "" {
		t.Errorf("Confidence: got %q, want empty string when no claim was made",
			got.Confidence)
	}
	if got.Reason == "" {
		t.Errorf("Reason: want non-empty even on the no-claim path")
	}
}

// TestRandomReminderRotates calls RandomReminder repeatedly and asserts
// at least two distinct strings come out — i.e., the function is
// genuinely picking from a pool, not returning a fixed value. We seed
// the RNG explicitly so the rotation is deterministic; without a seed
// reset this test would flake on the off-chance that all picks land on
// the same index.
func TestRandomReminderRotates(t *testing.T) {
	SeedReminderRNG(99) // deterministic for the test process

	const calls = 20
	seen := make(map[string]struct{}, calls)
	for i := 0; i < calls; i++ {
		seen[RandomReminder()] = struct{}{}
	}

	if len(seen) < 2 {
		t.Fatalf("RandomReminder rotation: got %d distinct values across %d calls, want >= 2",
			len(seen), calls)
	}
}

// TestPromptAdditionContainsSisyphus pins the persona name into the
// embedded prompt. The Sisyphus reference is the *whole point* of the
// addendum; if it's gone, somebody edited prompt_addition.md without
// understanding what the chapter teaches.
func TestPromptAdditionContainsSisyphus(t *testing.T) {
	if !strings.Contains(SystemPromptAddition, "Sisyphus") {
		t.Errorf("SystemPromptAddition: 'Sisyphus' not found; the persona name is the point of this chapter")
	}
	// While we're here, sanity-check that the addendum is non-empty —
	// a missing //go:embed file would silently produce "" without this
	// guard, and the Sisyphus check would also fail but for a misleading
	// reason.
	if len(SystemPromptAddition) < 200 {
		t.Errorf("SystemPromptAddition length=%d, want >=200; did the embed target shrink?",
			len(SystemPromptAddition))
	}
}
