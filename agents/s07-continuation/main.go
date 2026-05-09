package main

import (
	"fmt"
	"strings"
)

// main is the chapter's runnable demo. It does three things, each
// keeping the captured fixture small:
//
//  1. Print the first five lines of the embedded Sisyphus prompt
//     addendum so a reader sees `//go:embed -> string` in action
//     without dumping the whole 50-line file into the fixture.
//  2. Classify three sample responses against DetectCompletion to
//     show the high / low / no-claim verdicts side by side.
//  3. Print one reminder via RandomReminder() with a deterministic
//     seed so the captured fixture is reproducible.
//
// The output format mirrors s06's "== section ==" banners so a reader
// flipping between chapters sees the same shape.
func main() {
	// (1) Show that //go:embed produces a string by truncating to its
	// first five lines. The full text is in prompt_addition.md; this
	// snippet just proves "yes, the binary carries it." A real runtime
	// would prepend the whole string to the model's system prompt.
	fmt.Println("== prompt addendum (first 5 lines, embedded via //go:embed) ==")
	fmt.Println(firstNLines(SystemPromptAddition, 5))
	fmt.Println()

	// (2) Walk three responses that exercise every branch of
	// DetectCompletion: high-confidence claim, low-confidence (hedged)
	// claim, and no claim at all. The fixture captures all three so a
	// future change that breaks any branch is visible in the diff.
	fmt.Println("== DetectCompletion: three sample responses ==")
	samples := []struct {
		label    string
		response string
	}{
		{"high confidence", "I have completed all tasks."},
		{"low confidence", "I think this should be working now."},
		{"no claim", "Still investigating the bug."},
	}
	for _, s := range samples {
		sig := DetectCompletion(s.response)
		fmt.Printf("[%s] response=%q\n", s.label, s.response)
		fmt.Printf("  -> claimed=%t confidence=%q reason=%q\n",
			sig.Claimed, sig.Confidence, sig.Reason)
	}
	fmt.Println()

	// (3) Pick a reminder. The seed is hardcoded so `go run .` is
	// deterministic; the real runtime would pull from time.Now().
	SeedReminderRNG(42)
	fmt.Println("== one reminder (seeded for fixture stability) ==")
	fmt.Println(RandomReminder())
}

// firstNLines returns the first n lines of s joined by "\n". If s has
// fewer than n lines, the whole string is returned unchanged. Used to
// trim the prompt addendum to a digestible preview without an external
// dependency.
func firstNLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n")
}
