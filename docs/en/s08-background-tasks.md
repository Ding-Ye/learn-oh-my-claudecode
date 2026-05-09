---
title: "s08 · Background Task Heuristics"
chapter: 8
slug: s08-background-tasks
est_read_min: 9
---

# Chapter 8 — Background Task Heuristics

> Eighth chapter of `learn-oh-my-claudecode`. We pivot from s07's pure
> regex classifier to **two layered APIs in one chapter**: a pure
> `Decide` recommender that returns `{Background, Reason, Confidence}`
> with no I/O, paired with an impure `Executor` that actually spawns
> processes via `os/exec`. This is the first chapter where the
> "deterministic core, side-effecting shell" split is laid out as the
> teaching point.

## Problem

Long-running shell commands break interactive flow. Three failure
modes recur in production traces:

1. **Blocked terminal on `npm install`.** A package install can take
   90 seconds; the model issues the command, the runtime waits
   synchronously, and for those 90 seconds the user sees a frozen
   prompt. Worse, while the install runs, the model cannot accept new
   instructions — a single `npm install` halts the entire session.
2. **Backgrounded `git status`.** The opposite mistake: a recommender
   that's too eager to background "every shell command" will spawn
   `git status` asynchronously, making the model wait on a
   filesystem-poll loop for output that should have been inline. The
   user types a question, the answer arrives 200ms late, and nothing
   feels right.
3. **Forty parallel `cargo build`s.** Without a soft cap, an
   over-enthusiastic agent can fork a dozen long-running processes at
   once and saturate the developer's machine. The recommender needs to
   know *how many are already in flight* and refuse to start the
   next when the budget is full.

Upstream solves this in `src/features/background-tasks.ts` L1–L100
with three primitives: a cap constant (`DEFAULT_MAX_BACKGROUND_TASKS
= 5`, L24), an array of long-running regex patterns (L29–L70), and an
array of always-blocking regex patterns (L74–L100). We port all
three plus a working spawn primitive in ~250 stdlib-only Go lines.

## Solution

Two layered APIs in one chapter:

- **Pure `Decide(cmd, running, max) Decision`** in `decide.go`. No
  I/O, no goroutines, no time-of-day. Four-branch priority order: cap
  → long-running → blocking → default. The function allocates one
  `Decision` and walks at most ~45 regex patterns; safe to call on
  every command without a regret budget.
- **Impure `Executor.Run(ctx, cmd) (*Handle, error)`** in
  `executor.go`. Uses `exec.CommandContext("sh", "-c", cmd)` so the
  caller can pass any shell expression. Sets `Setpgid` on Unix so
  SIGKILL propagates to grandchildren — without it, a canceled
  `sh -c "sleep 30"` would kill `sh` but leave the orphaned `sleep`
  running for thirty seconds.

The split keeps the regex layer independent of `os/exec` — a test can
exercise either without the other. Two `[]*regexp.Regexp` slices
encode the policy, one `Decision` struct carries the verdict, one
`Handle` struct carries the spawned-process state. Total: ~250 lines.

## How It Works

### Two regex slices, each citing upstream

```go
var LongRunningPatterns = []*regexp.Regexp{
    regexp.MustCompile(`(?i)\b(npm|yarn|pnpm|bun)\s+(install|ci|update|upgrade)\b`),
    regexp.MustCompile(`(?i)\b(pip|pip3)\s+install\b`),
    regexp.MustCompile(`(?i)\bcargo\s+(build|install|test)\b`),
    // ... 21 more, each citing the upstream line it ports
}
var BlockingPatterns = []*regexp.Regexp{
    regexp.MustCompile(`(?i)\bgit\s+(status|diff|log|branch)\b`),
    regexp.MustCompile(`(?i)\bls\b`),
    // ... 18 more
}
```

`Decide` is one for-loop per slice, no switch statements. Adding a
new long-running family is a one-line append. A malformed regex is
caught at first import via `MustCompile`'s panic — same posture s07
used for completion regexes.

### Pure recommender, four-branch priority

```go
func Decide(cmd string, runningCount, max int) Decision {
    if runningCount >= max { return Decision{Background: false,
        Reason: "concurrency cap reached", Confidence: "high"} }
    for _, p := range LongRunningPatterns { if p.MatchString(cmd) {
        return Decision{Background: true, ...} } }
    for _, p := range BlockingPatterns { if p.MatchString(cmd) {
        return Decision{Background: false, ...} } }
    return Decision{Background: false,
        Reason: "no pattern matched; defaulting to foreground",
        Confidence: "low"}
}
```

The cap check is *first* because it is a hard limit — even
`npm install` returns foreground when five other long-running
commands are in flight. The reason field encodes WHY foreground was
chosen so the runtime can distinguish "queue this for later" from
"run it inline because it's cheap."

### Setpgid is the difference between killing a wrapper and killing the work

```go
cmd := exec.CommandContext(ctx, "sh", "-c", command)
cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
if err := cmd.Start(); err != nil { return nil, err }
```

Without `Setpgid`, `exec.CommandContext` cancel reaches `sh` but
leaves the orphaned child running. A canceled `sh -c "sleep 30"`
would let `sleep` keep ticking for the full 30 seconds with no parent
to reap it. The `Setpgid: true` line puts the child in its own process
group, so the kernel signal propagates through `sh` to `sleep` —
`TestExecutorCancelsOnContext` is designed to fail loudly if anyone
removes the `SysProcAttr` line.

## What Changed (vs. s07)

s07 was a **string → Signal classifier** — one input, one verdict, no
side effects, no concurrency primitives. s08 introduces something
fundamentally new: **two layered APIs in one chapter**, where a pure
recommender (`Decide`) is paired with an impure executor that actually
forks processes. For the first time the chapter cleanly separates
"deterministic, free of side effects" code from "touches the OS"
code, in two files that never know about each other.

| Concern | s07 | s08 |
|---|---|---|
| First-class data | `Signal` (Claimed + Confidence + Reason) | `Decision` (Background + Reason + Confidence) and `Handle` (Cmd + Wait) |
| Embed shape | `string` for prompt, `[]byte` for JSON pool | (none — pure regex slices) |
| Behavior driver | classify by regex match | classify by regex match + spawn by `exec.CommandContext` |
| Side effects | none | `os/exec` fork + `Setpgid` + `Wait` |
| Output | `Signal` verdict for runtime use | `Decision` verdict OR `*Handle` to a live process |

This is also the first chapter where `context.WithCancel` controls a
real OS resource — a process — instead of just shaping in-memory
control flow. That muscle memory carries directly into s10's
goroutine-pool watchdog, where the same pattern is used to terminate
workers on shutdown.

## Try It

```bash
cd agents/s08-background-tasks

GOWORK=off go vet ./...                # silent
GOWORK=off go build ./...              # silent
GOWORK=off go test -v -count=1 ./...   # 5 tests pass, sub-second
GOWORK=off go run .                    # output matches testdata/expected.txt
```

Expected output:

```
== Decide: seven sample commands ==
[long-running] cmd="npm install" running=0 max=5
  -> background=true confidence="high" reason="matches long-running pattern"
[long-running] cmd="cargo build --release" running=0 max=5
  -> background=true confidence="high" reason="matches long-running pattern"
[long-running] cmd="docker pull alpine" running=0 max=5
  -> background=true confidence="high" reason="matches long-running pattern"
[blocking] cmd="git status" running=0 max=5
  -> background=false confidence="high" reason="matches blocking pattern"
[blocking] cmd="ls -la" running=0 max=5
  -> background=false confidence="high" reason="matches blocking pattern"
[default] cmd="hello-world" running=0 max=5
  -> background=false confidence="low" reason="no pattern matched; defaulting to foreground"
[cap-reached] cmd="npm install" running=5 max=5
  -> background=false confidence="high" reason="concurrency cap reached"

== Executor: spawn `sleep 0.3` and Wait ==
started: pid>0=true
exited: code=0
```

Seven samples cover every branch of `Decide` (long-running, blocking,
default, cap-reached). The Executor demo runs `sleep 0.3` because it's
long enough to demonstrate that `Run` returns before the process exits
but short enough to keep `go run .` under one second.

Further exercises:

- Add a new long-running family to `patterns.go` (e.g.,
  `pip-tools compile`) and re-run the tests. The append is one line.
- Compose with s05's hooks pipeline: register a `PreToolUse` hook
  that calls `Decide` on the proposed command and blocks the tool
  invocation if the verdict is `Background=false, Confidence="low"`
  while a `Force-Background` user flag is set.

## Upstream Source Reading

Excerpt from `src/features/background-tasks.ts` L1–L100 (full
annotated copy at `upstream-readings/s08-background-tasks.ts`):

```ts
// L24 — the cap constant
export const DEFAULT_MAX_BACKGROUND_TASKS = 5;

// L29-L70 — long-running patterns (24 regexes covering 5 families)
export const LONG_RUNNING_PATTERNS = [
  /\b(npm|yarn|pnpm|bun)\s+(install|ci|update|upgrade)\b/i,
  /\b(pip|pip3)\s+install\b/i,
  /\bcargo\s+(build|install|test)\b/i,
  /\bgo\s+(build|install|test)\b/i,
  // ... package managers, builds, test suites, docker, db, lint, slow git
];

// L74-L100 — blocking patterns (20 regexes)
export const BLOCKING_PATTERNS = [
  /\bgit\s+(status|diff|log|branch)\b/i,
  /\bls\b/i,
  /\bpwd\b/i,
  // ... quick status checks, file ops, env checks
];
```

Reading notes (Go-port comparisons):

1. **L24 (cap constant) → `DefaultMaxBackgroundTasks`.** Same value
   (5), same advisory semantics. The Go constant is exported so
   callers can compare or override at construction time. The cap is
   the *first* check in `Decide` because it is a hard limit; the
   regex verdict is irrelevant when the budget is full.
2. **L29–L70 (long-running patterns) → `LongRunningPatterns`.**
   Verbatim port of all 24 regexes. Two minor adjustments forced by
   Go's RE2 engine: TS's `/.../i` flag suffix becomes Go's `(?i)`
   inline flag, and the `make` regex's trailing `$` anchor is
   preserved because that anchor is what catches `make` at end of
   line versus `make ./target` (which falls through to the default
   branch).
3. **L74–L100 (blocking patterns) → `BlockingPatterns`.** Verbatim
   port of all 20 regexes. Note that `\bls\b` does not look at
   flags: `ls -la` and `ls --color=never` both stay foreground. The
   runtime trusts the user's `ls` even if it streams 100k lines —
   piping into a pager is the user's choice, not the recommender's.
4. **L150–L210 (`shouldRunInBackground`, paraphrased) → `Decide`.**
   The decision order is identical: cap → long-running → blocking →
   default. The ONE behavioral difference is the default branch.
   Upstream returns `ok=true, confidence='low'` ("when in doubt,
   background it"). The Go port intentionally inverts this to
   `Background=false, Confidence="low"` ("when in doubt, keep it
   visible to the user"). The plan calls this out as a deliberate
   teaching choice — backgrounding the unknown is the wrong default
   for an interactive CLI agent.
5. **`BackgroundTaskManager` class (L114+) — NOT ported.** Upstream
   bundles a stateful task tracker (running task list, per-task
   timeout, completion callbacks) into the same file. The Go chapter
   intentionally leaves this surface for s09 (filesystem-CAS task
   store) plus s10 (goroutine pool with watchdog) to compose. Keeping
   the recommender pure means a test can exercise it without standing
   up the rest of the runtime.
6. **The `Executor` layer is a Go-side addition.** Upstream relies on
   Claude Code's built-in `run_in_background` Bash-tool flag to do
   the actual fork; there is no `BackgroundTaskManager.spawn` method
   in the source. The Go chapter adds a tiny `Executor` so the
   spawn-and-cancel mechanics are visible in this repo — concretely,
   so `Setpgid` is a thing the reader sees rather than a footnote.
