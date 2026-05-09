# s10 — Team Runtime & Watchdog / 团队 Runtime 与 Watchdog（goroutine 池）

> Tenth and final chapter of `learn-oh-my-claudecode`. The capstone:
> we replace upstream's tmux-based worker pool + filesystem `done.json`
> polling with idiomatic Go goroutines + channels + `time.Ticker` +
> `context`. **The chapter where Go-the-language pays its rent.**

## Scope (one line)

A self-contained goroutine pool that dispatches tasks via a buffered
channel, watches per-worker heartbeats with a `time.Ticker`, respawns
stalled goroutines via `context.CancelFunc`, and resumes from on-disk
state — porting the *behavior* of `src/team/runtime.ts` L289-L990
(1,034 lines of TypeScript) into ~1,290 Go lines, with the runtime
core (worker.go + pool.go + watchdog.go) coming in at ~810 LOC. **A
~250-line shrink of the load-bearing concurrency code.**

## The Go-vs-tmux contrast (the takeaway)

| Concern | Upstream (`runtime.ts` 1,034 lines) | s10 (Go ~1,290 lines) |
|---|---|---|
| Worker = ? | A tmux pane spawned by `tmux split-window`. | A goroutine reading from a `chan Task`. |
| Worker → orchestrator signal | Worker writes `done.json`; watchdog polls every 1s. | Worker sends `WorkerResult` on a `chan WorkerResult`. |
| Orchestrator → worker signal | `tmux send-keys` to the pane's keyboard buffer. | Push `Task` onto the shared dispatch channel. |
| Heartbeat | Worker writes `heartbeat.json` once per second; watchdog reads + compares mtime. | Worker mutates a `*time.Time` cell under `beatsMu`; watchdog reads under same mutex. |
| Stall detection | mtime > 60s old → strike; 3 strikes → kill pane. | `time.Ticker` ticks; if `now.Sub(*beat) > 60s` → strike; 3 strikes → cancel `context.CancelFunc`. |
| Respawn | `tmux kill-pane` + `tmux split-window` + re-init CLI agent. | `cancel()` the worker context; spawn a fresh goroutine under same name. |
| Pool shutdown | `tmux kill-session` + `done.json` cleanup. | `cancel()` parent context; `wg.Wait()`. |
| Resume after crash | Re-list tmux panes, re-scan task files, rebuild lookup table. | `Resume()` reads task files and re-pushes pending IDs onto the channel. |

The biggest single line-count delta in the curriculum: tmux + signals
+ pane management + `done.json` plumbing collapse into a `select`
statement and a `time.Ticker`.

## Files

| Path | LOC | Role |
|---|---|---|
| `task.go` | 84 | `Task` shape (re-declared, NOT imported from s09); `NewTaskID` for stdlib-only ID minting. |
| `store.go` | 131 | Inlined task-store: `Read` / `Write` / `List` over JSON files. No flock here — the channels do the serialization. |
| `worker.go` | 229 | `WorkerResult`, `Worker`, `workerLoop` goroutine. `runTask` simulates work via `time.Sleep` + heartbeat tick + optional panic injection. |
| `pool.go` | 433 | The Pool: dispatch channel, results channel, the `New` / `Submit` / `Run` / `Shutdown` lifecycle, `spawnWorker` / `killWorker` respawn helpers, `handleResult` retry-vs-fail decision. |
| `watchdog.go` | 146 | `watchdog(ctx, pool, tick)` goroutine + `watchdogTick(pool)` unit. Constants `HeartbeatStaleThreshold = 60s`, `UnresponsiveStrikeMax = 3`, `WatchdogTickInterval = 1s`. |
| `resume.go` | 87 | `Resume(ctx, root, n)` reads task files, re-queues pending+orphaned, skips terminal. |
| `main.go` | 179 | The runnable demo: 3 workers × 7 tasks, 150ms-mark forced kill of worker-2, watch the respawn drain everything to done. |
| `pool_test.go` | 106 | `TestPoolDispatchesAllTasks`, `TestShutdownDrainsInflightTasks`. |
| `watchdog_test.go` | 170 | `TestWatchdogRespawnsCrashedWorker`, `TestWatchdogMarksTaskFailedAfter3Retries`, `TestWatchdogTickResetsHealthyStrike`. |
| `resume_test.go` | 111 | `TestResumeReadsExistingPendingTasks`. |
| `testdata/expected.txt` | – | Captured `go run .` stdout for drift detection. |
| `go.mod` | – | **stdlib only** — no UUID dep, no flock dep. The chapter is teaching the goroutine model, and a transitive UUID library would obscure the focus. |

## Run

```bash
cd agents/s10-team-watchdog

GOWORK=off go vet ./...                # silent
GOWORK=off go build ./...              # silent
GOWORK=off go test -v -count=1 -timeout=30s ./...   # 6 tests, ~1s
GOWORK=off go run .                    # output matches testdata/expected.txt
```

The demo finishes in roughly **1.5 seconds**, well under the 15s
budget. With `-race`, the test suite passes in under 2 seconds — a
deliberate target so every race-detector caveat surfaces in CI.

## Three teaching points

1. **A tmux pane is a goroutine.** The single most distinctive
   architectural shift in the curriculum: where upstream needs a
   subshell, a pane id, a kill-signal protocol, and a `done.json`
   filesystem rendezvous, we need a function literal and a channel.
   The `for { select { case t := <-tasks: ...; case <-ctx.Done(): } }`
   skeleton is the entire "pane lifecycle." Read `worker.go`
   alongside `runtime.ts` L582-L770 to feel the collapse.

2. **`time.Ticker` + `context.CancelFunc` is the watchdog.** The
   stall-detect → kill → respawn loop is a plain `for {select}` over
   a ticker. No `setInterval`, no `clearInterval`, no fallback
   `watchdog-failed.json` marker file. When ctx is cancelled, the
   ticker stops; when the loop returns, the goroutine ends. The
   teaching ratio: ~110 lines of TS (`watchdogCliWorkers` L466-L580)
   becomes ~75 lines of Go (`watchdog.go`'s `watchdog` + `watchdogTick`).

3. **Channels eliminate the `done.json` round-trip.** Upstream's
   workers write `done.json`, the watchdog reads it every 1 second,
   the watchdog deletes it after consumption. The Go version: worker
   sends `WorkerResult` on a buffered channel, the Pool's main loop
   `select`s on it, the result is consumed by the natural channel
   semantics. No filesystem, no polling, no cleanup. This is the
   same lesson Go's net/http handlers teach about `http.ResponseWriter`
   — the language's primitives obviate the protocol-on-the-side.

## Anti-pattern callouts

Plan §"Anti-patterns to NOT repeat" lists tmux-as-worker-dispatch
(#6) and multiple parallel runtimes (#3). This chapter is the
*resolution* of both:

- **#6 — Tmux as worker dispatch.** Replaced wholesale by goroutines.
  The Worker abstraction in this chapter has a `cancel
  context.CancelFunc`, a `LastBeat time.Time`, and a `Name string` —
  no `paneId`, no `pid`, no `tmuxSocket`. An optional "tmux mode"
  appendix exercise is left to the reader (see Plan Appendix B
  exercise #4); the canonical version stays Go-native.

- **#3 — Multiple parallel runtimes.** Upstream ships
  `runtime.ts` (1,034 lines), `runtime-v2.ts`, `runtime-cli.ts`, and
  `worker-bootstrap.ts` — four implementations of the same idea,
  layered for backward compat. The Go port ships *one* `Pool` type.
  If a future requirement demands an `os/exec`-backed worker, that
  becomes a second implementation of an interface, not a fork of the
  whole runtime.

## Why no external deps

Unlike s09 (which pulls in `gofrs/flock` for cross-platform file
locking), s10 stays stdlib-only. Two reasons:

1. **Channels are the lock.** Goroutines synchronize through channels
   and explicit `sync.Mutex` fields on the Pool — no advisory file
   locking is needed because the Pool owns its task-store calls
   in-process.
2. **Teaching focus.** A UUID library or a goroutine-pool framework
   would dilute the chapter's central message: *the standard library
   is enough*. We mint task IDs from `crypto/rand` (16 hex chars,
   inline in `task.go`), build the pool from `chan` + `sync.Mutex`
   + `context.Context`, run the watchdog from `time.Ticker`. Every
   primitive is in the stdlib chapter index.

## Upstream lineage

- `src/team/runtime.ts` L289-L390 (`startTeam`) → `Pool.Run` +
  `spawnWorker`. Upstream initializes a tmux session, creates the
  leader pane, splits initial worker panes, attaches CLI binaries,
  starts the watchdog. Go port: spawn N goroutines, spawn 1 watchdog
  goroutine, block on `results` channel.

- `src/team/runtime.ts` L466-L580 (`watchdogCliWorkers`) →
  `watchdog.go`. Upstream polls `done.json`, polls `isWorkerAlive`,
  reads `heartbeat.json`, increments `unresponsiveCount`, kills+
  respawns at threshold, writes `watchdog-failed.json` on persistent
  errors. Go port: keep the heartbeat-stale + 3-strike rule;
  drop done-polling (channels), drop pid-liveness checks (panics
  are recovered into channel results), drop watchdog-failed
  (no error mode to record — the Pool's `Shutdown` grace timeout
  is the equivalent escape valve).

- `src/team/runtime.ts` L922-L990 (`resumeTeam`) → `resume.go`.
  Upstream lists tmux panes, reads task files, builds a worker→
  task lookup table, returns a `TeamRuntime` ready for re-attach.
  Go port: read task files, re-push pending+orphaned IDs onto the
  dispatch channel, return a `Pool` ready for `.Run()`.

See `docs/{en,zh}/s10-team-watchdog.md` for the bilingual chapter
narrative and `upstream-readings/s10-runtime.ts` for the annotated
TypeScript excerpt.
