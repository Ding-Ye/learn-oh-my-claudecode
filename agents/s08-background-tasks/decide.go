package main

// Decision is the verdict Decide returns about a single command line.
// Mirrors upstream's `TaskExecutionDecision` shape (background-tasks.ts
// L105-L114) with two intentional renames:
//
//   - `runInBackground` → `Background`. The TS field reads as a verb
//     phrase; the Go field reads as a state, which is more idiomatic.
//   - `confidence` stays a free-form string so callers can grep for
//     "high" / "medium" / "low" without an extra import. The values
//     this chapter ships are the same three the upstream uses.
//
// The struct is deliberately schema-free at the package level — there
// is no `Decide` interface, no plugin point. Adding a new heuristic is
// a one-line append in patterns.go plus an extra branch in Decide.
type Decision struct {
	// Background is true when the recommendation is to spawn the
	// command in the background. False means run in the foreground —
	// either because it's quick, because it produces output the
	// caller wants inline, or because the concurrency cap is full.
	Background bool

	// Reason is a short human-readable explanation of why this verdict
	// was chosen. Useful for logs and the demo table; never used for
	// control flow. Non-empty for every verdict.
	Reason string

	// Confidence is "high" when a regex slice fired, "low" when the
	// default branch was taken (no pattern matched in either slice).
	// "medium" is reserved for a future refinement (e.g., a fuzzy
	// match below the regex threshold). Callers should treat the
	// field as informational; the Background bool is the actual
	// dispatch instruction.
	Confidence string
}

// Decide is a pure recommender: same input always yields the same
// Decision, no I/O, no goroutines, no time-of-day. It is safe to call
// on every command without a regret budget — the function allocates
// one Decision and at most a handful of regex test calls.
//
// Logic in priority order (the TS upstream uses the same order, see
// `shouldRunInBackground` around L150-L210):
//
//  1. If runningCount >= max, return foreground with reason
//     "concurrency cap reached". This is the *only* branch that
//     ignores the regex verdict — the cap is a hard limit.
//  2. If any LongRunningPattern matches, return background with
//     Confidence="high".
//  3. If any BlockingPattern matches, return foreground with
//     Confidence="high".
//  4. Otherwise, default to foreground with Confidence="low" — the
//     classifier had no opinion, and the safe default is to keep
//     things visible.
//
// runningCount and max are passed in instead of read from a global so
// the function stays pure. A caller that wants to ignore the cap can
// pass max=0 and runningCount=0 — the first comparison short-circuits
// (0 >= 0 is true), which is the documented behavior: a max of zero
// means "do not run anything in the background", and Decide enforces
// that without any special-case code.
func Decide(cmd string, runningCount, max int) Decision {
	// (1) Concurrency cap. Checked first because it is a hard limit:
	// even npm install must wait if five other long-running commands
	// are already in flight. A real runtime would queue the request
	// and re-check; the recommender just reports "fg, cap reached".
	if runningCount >= max {
		return Decision{
			Background: false,
			Reason:     "concurrency cap reached",
			Confidence: "high",
		}
	}

	// (2) Long-running patterns. The first match wins; we don't try
	// to identify which family (package manager vs. build vs. docker)
	// because the runtime treats them all the same — "spawn it in the
	// background and check on it later". A future refinement could
	// return the matching family in Reason for better UX.
	for _, p := range LongRunningPatterns {
		if p.MatchString(cmd) {
			return Decision{
				Background: true,
				Reason:     "matches long-running pattern",
				Confidence: "high",
			}
		}
	}

	// (3) Blocking patterns. Same loop, opposite verdict. Note that
	// `git pull` would have matched both LongRunning (L67) and
	// Blocking (`git status|diff|log|branch` doesn't include `pull`),
	// so step 2 wins for git operations the user expects to take a
	// while. The pattern sets are designed to be disjoint; this
	// ordering is a defense in depth.
	for _, p := range BlockingPatterns {
		if p.MatchString(cmd) {
			return Decision{
				Background: false,
				Reason:     "matches blocking pattern",
				Confidence: "high",
			}
		}
	}

	// (4) Default. The recommender has no signal either way, so we
	// keep things visible to the user. Confidence="low" tells the
	// runtime that this verdict is a heuristic floor — it could be
	// overridden by user preference without losing information.
	return Decision{
		Background: false,
		Reason:     "no pattern matched; defaulting to foreground",
		Confidence: "low",
	}
}
