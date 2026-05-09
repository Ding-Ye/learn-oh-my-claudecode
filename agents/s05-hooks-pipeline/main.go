package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// main is the chapter's runnable demo. It loads testdata/hooks.json,
// fires a single UserPromptSubmit event with a tiny JSON payload, and
// prints each Result line so testdata/expected.txt is a real drift
// detector instead of a wall of process output.
//
// The output format is intentionally compact:
//
//	[event/matcher] command -> exit=N stdout="..." err=<nil|...>
//
// This keeps the captured fixture small and lets the doc render it in a
// fenced block without horizontal scroll.
func main() {
	mf, err := LoadManifest(filepath.Join("testdata", "hooks.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	d := &Dispatcher{Manifest: mf}

	// Parent context bounds the entire batch. We use a generous 10s
	// because one of the demo entries deliberately demonstrates a
	// per-hook timeout (sleep_too_long.sh with timeout: 1).
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Two payloads exercise three behaviors at once: (1) the "*" entry
	// always fires; (2) only the second payload contains "boulder", so
	// the 1-second timeout path is dormant for the first payload and
	// firing for the second; (3) "ultrawork" is in neither payload, so
	// that entry stays silent both times — proof the matcher filter
	// works.
	for _, p := range []map[string]string{
		{"prompt": "hello"},
		{"prompt": "push the boulder"},
	} {
		payload, _ := json.Marshal(p)
		fmt.Printf("== UserPromptSubmit (payload=%s) ==\n", string(payload))
		for _, r := range d.Dispatch(ctx, "UserPromptSubmit", payload) {
			fmt.Printf("[%s/%s] %s -> exit=%d stdout=%q err=%s\n",
				r.Event,
				r.Matcher,
				r.Command,
				clampExit(r.ExitCode),
				singleLine(r.Stdout),
				summarizeErr(r.Err),
			)
		}
		fmt.Println()
	}
}

// clampExit normalizes the timeout-kill exit code. exec.ExitError reports
// -1 when the process was signal-killed before the kernel attached an
// exit status; some Go releases report a positive signal number instead.
// Either way, we want the captured fixture to read "non-zero" without
// baking in a specific OS code.
func clampExit(code int) int {
	if code == 0 {
		return 0
	}
	return -1
}

// singleLine flattens captured stdout onto one line so the printed table
// stays aligned. Hooks that emit multi-line output stay readable in the
// raw stdout field; only this demo printer collapses them.
func singleLine(s string) string {
	out := ""
	for _, r := range s {
		switch r {
		case '\n', '\r':
			out += " "
		default:
			out += string(r)
		}
	}
	if len(out) > 0 && out[len(out)-1] == ' ' {
		out = out[:len(out)-1]
	}
	return out
}

// summarizeErr trims the timeout error to its sentinel name so the
// captured expected.txt does not bake in OS-specific signal phrasing
// (e.g. "signal: killed" vs. "exit status -1").
func summarizeErr(err error) string {
	if err == nil {
		return "<nil>"
	}
	// We classify by whether the message mentions DeadlineExceeded —
	// the runtime wrap of context.DeadlineExceeded varies across Go
	// versions, but the substring is stable.
	msg := err.Error()
	if contains(msg, "context deadline exceeded") {
		return "deadline-exceeded"
	}
	if contains(msg, "exit status") {
		return "exit-status"
	}
	return msg
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && indexOf(s, sub) >= 0
}

// indexOf is a tiny strings.Index stand-in. We use it instead of
// importing strings because main.go already imports encoding/json and
// keeping the import set short makes the demo easier to skim.
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
