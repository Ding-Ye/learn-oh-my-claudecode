package main

import (
	"context"
	"fmt"
	"os/exec"
)

// main is the chapter's runnable demo. It does two things, each
// keeping the captured fixture small and reproducible:
//
//  1. Run Decide over a fixed table of seven sample commands and print
//     each verdict in a banner-tagged line. The seven samples are
//     chosen so every branch of Decide fires (long-running, blocking,
//     default, cap-reached) at least once.
//  2. Spawn one real background command via Executor (`sleep 0.3`),
//     Wait, and print the Pid + ExitCode. We use a 0.3-second sleep
//     because (a) it's long enough to demonstrate that Run returns
//     before the process exits, and (b) it's short enough to not
//     stretch `go run .` past one second.
//
// The output format follows s07's "== section ==" banners so a reader
// flipping between chapters sees the same shape.
func main() {
	// (1) Pure-Decide table. The seven entries below cover:
	//   - npm install      → background (long-running)
	//   - cargo build      → background (long-running)
	//   - docker pull      → background (long-running)
	//   - git status       → foreground (blocking)
	//   - ls -la           → foreground (blocking)
	//   - hello-world      → foreground (default)
	//   - npm install (cap)→ foreground (cap reached)
	fmt.Println("== Decide: seven sample commands ==")
	type sample struct {
		cmd          string
		runningCount int
		max          int
		label        string
	}
	samples := []sample{
		{"npm install", 0, DefaultMaxBackgroundTasks, "long-running"},
		{"cargo build --release", 0, DefaultMaxBackgroundTasks, "long-running"},
		{"docker pull alpine", 0, DefaultMaxBackgroundTasks, "long-running"},
		{"git status", 0, DefaultMaxBackgroundTasks, "blocking"},
		{"ls -la", 0, DefaultMaxBackgroundTasks, "blocking"},
		{"hello-world", 0, DefaultMaxBackgroundTasks, "default"},
		{"npm install", DefaultMaxBackgroundTasks, DefaultMaxBackgroundTasks, "cap-reached"},
	}
	for _, s := range samples {
		d := Decide(s.cmd, s.runningCount, s.max)
		fmt.Printf("[%s] cmd=%q running=%d max=%d\n",
			s.label, s.cmd, s.runningCount, s.max)
		fmt.Printf("  -> background=%t confidence=%q reason=%q\n",
			d.Background, d.Confidence, d.Reason)
	}
	fmt.Println()

	// (2) Real Executor.Run. We invoke `sleep 0.3` because the goal is
	// to *prove the plumbing*, not to demonstrate any particular long
	// command. The fixture captures Pid as ">0" rather than the actual
	// integer (Pid is process-table dependent and varies per run).
	fmt.Println("== Executor: spawn `sleep 0.3` and Wait ==")
	exe := &Executor{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h, err := exe.Run(ctx, "sleep 0.3")
	if err != nil {
		fmt.Printf("Executor.Run failed: %v\n", err)
		return
	}

	pidPositive := h.Cmd.Process != nil && h.Cmd.Process.Pid > 0
	fmt.Printf("started: pid>0=%t\n", pidPositive)

	if err := h.Wait(); err != nil {
		fmt.Printf("Wait returned error: %v\n", err)
		return
	}

	// ExitCode reads ProcessState — populated only after Wait returns
	// successfully. For `sleep 0.3` the expected exit code is 0.
	exitCode := exitCodeOf(h.Cmd)
	fmt.Printf("exited: code=%d\n", exitCode)
}

// exitCodeOf returns the integer exit code for a finished *exec.Cmd.
// On Unix this reads ProcessState.ExitCode(); the helper exists so the
// demo doesn't have to deal with the (*os.ProcessState).Sys() type
// switch on platforms where the field shape differs.
func exitCodeOf(cmd *exec.Cmd) int {
	if cmd == nil || cmd.ProcessState == nil {
		return -1
	}
	return cmd.ProcessState.ExitCode()
}
