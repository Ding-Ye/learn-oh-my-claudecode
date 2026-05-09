# s08 — Background Task Heuristics / 后台任务启发式调度

> Eighth chapter of `learn-oh-my-claudecode`. We pivot from s07's pure
> regex classifier to **two layered APIs in one chapter**: a pure
> `Decide` recommender that returns `{Background, Reason, Confidence}`
> with no I/O, paired with an impure `Executor` that actually spawns
> processes via `os/exec`. This is the first chapter where the
> "deterministic core, side-effecting shell" split is laid out as the
> teaching point.

## Scope (one line)

Two regex slices (`LongRunningPatterns`, `BlockingPatterns`), one cap
constant (`DefaultMaxBackgroundTasks = 5`), a pure `Decide(cmd string,
running, max int) Decision`, and an `Executor.Run(ctx, cmd) (*Handle,
error)` that uses `exec.CommandContext` plus `Setpgid` so context
cancel propagates to grandchildren — ported from
`src/features/background-tasks.ts` L1–L100 in ~250 stdlib-only Go
lines.

## Files

| Path | Role |
|---|---|
| `patterns.go` | The two `[]*regexp.Regexp` slices plus `DefaultMaxBackgroundTasks`. Each pattern cites the upstream line it ports. Compiled at init via `MustCompile` so a typo is caught on first import. |
| `decide.go` | `Decision` struct and `Decide(cmd, running, max) Decision`. Pure: same input always yields the same verdict, no I/O, no goroutines. Four-branch priority order: cap → long-running → blocking → default. |
| `executor.go` | `Handle`, `Executor`, `Executor.Run`. Uses `exec.CommandContext("sh", "-c", cmd)` so callers can pass any shell expression; sets `Setpgid` on Unix so SIGKILL propagates through `sh` to grandchildren. Returns a `*Handle` with a memoized `Wait` closure. |
| `main.go` | Demo: print Decide verdicts for seven sample commands, then spawn a real `sleep 0.3`, Wait, print Pid-positivity and ExitCode. |
| `decide_test.go` | Three tests — npm install → bg, git status → fg, cap-reached → fg. |
| `executor_test.go` | Two tests — `exit 7` returns ExitCode=7, `sleep 30` plus 100ms cancel returns within deadline. |
| `testdata/expected.txt` | Captured `go run .` stdout for drift detection. |
| `go.mod` | `go 1.21`, stdlib only. |

## Run

```bash
cd agents/s08-background-tasks

GOWORK=off go vet ./...                # silent
GOWORK=off go build ./...              # silent
GOWORK=off go test -v -count=1 ./...   # 5 tests pass, sub-second
GOWORK=off go run .                    # output matches testdata/expected.txt
```

## Three teaching points

1. **Pure recommender + impure executor in one chapter.** `Decide` is
   the first half: it touches no `syscall`, allocates one `Decision`,
   and is fully testable with table-driven cases. `Executor` is the
   second half: it forks a process, sets a process-group id, and
   returns a `Wait` closure. The split keeps the regex layer
   independent of `os/exec` — a test can exercise either without the
   other.
2. **`Setpgid` is the difference between killing a wrapper and
   killing the work.** Without it, `exec.CommandContext` cancel
   reaches `sh` but leaves the orphaned child running. The
   `TestExecutorCancelsOnContext` case is designed to fail loudly if
   anyone removes the `SysProcAttr` line.
3. **Concurrency cap as a hard limit, not a hint.** `Decide` checks
   `runningCount >= max` *before* the regex slices, so even
   `npm install` returns foreground when five other long-running
   commands are already in flight. The reason field encodes WHY so
   the runtime can distinguish "queue this for later" from "run it
   inline because it's cheap".

## Anti-pattern callout

Plan §"Anti-pattern #3" reads "Multiple parallel runtimes." Upstream
has a parallel "actually launch" path bolted onto the recommender
(`shouldRunInBackground` calls into a separate `BackgroundTaskManager`
class). The Go port keeps `Decide` (pure) and `Executor` (impure) in
distinct files so a test can use one without the other. Single
canonical surface per concern:

```go
// Pure: no I/O, deterministic, free of side effects.
d := Decide("npm install", 0, DefaultMaxBackgroundTasks)

// Impure: forks a process. Touches the OS.
h, _ := (&Executor{}).Run(ctx, "npm install")
```

The two functions never know about each other. A future composer
could glue them — `if Decide(cmd, ...).Background { exe.Run(...) }` —
without adding a third file.

## Upstream lineage

- `src/features/background-tasks.ts` L1–L100 — the heuristic core. We
  port `LONG_RUNNING_PATTERNS` (L29–L70), `BLOCKING_PATTERNS`
  (L74–L100), and `DEFAULT_MAX_BACKGROUND_TASKS` (L24). The
  `BackgroundTaskManager` lifecycle class (L114+) is *not* ported —
  the chapter teaches the recommender + spawn primitive, and a real
  runtime would compose them with s09's task store.
- See `upstream-readings/s08-background-tasks.ts` for the annotated
  excerpt that mirrors what we ported and what we deliberately left.
