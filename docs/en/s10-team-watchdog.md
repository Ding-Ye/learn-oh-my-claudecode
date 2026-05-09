---
title: "s10 · Team Runtime & Watchdog (Goroutine Pool)"
chapter: 10
slug: s10-team-watchdog
est_read_min: 14
---

# Chapter 10 — Team Runtime & Watchdog (Goroutine Pool)

> Tenth and final chapter of `learn-oh-my-claudecode`. **The capstone.**
> Where s09 made the filesystem the coordination primitive, s10
> makes goroutines + channels the **execution primitive**: a
> three-goroutine architecture (workers + watchdog + main loop)
> replaces upstream's tmux + `done.json` polling, collapsing the
> 1,034-line `runtime.ts` into ~810 LOC of runtime core (with the
> rest of the chapter coming in at ~1,290 total). This is the
> chapter where Go-the-language pays its rent.

## Problem

s09 left the team-orchestration question half-answered: tasks live
on disk with claim tokens, leases, and atomic writes — but **who
runs them?** Upstream's answer is `src/team/runtime.ts`: a tmux
session, one pane per worker, each pane running a CLI agent that
writes `done.json` when finished, and a watchdog that polls those
signal files every 1 second. The implementation is 1,034 lines and
spans four parallel runtimes (`runtime.ts`, `runtime-v2.ts`,
`runtime-cli.ts`, `worker-bootstrap.ts`).

Five concrete problems the runtime must solve, all of which the
upstream tackles via filesystem signals + tmux:

1. **Spawn N workers.** Upstream: `tmux split-window` × N, `tmux send-keys`
   to launch the agent inside each pane. Heavy.
2. **Dispatch tasks to free workers.** Upstream: each worker reads a
   per-pane task envelope file `<paneId>.task.json`.
3. **Detect a finished task.** Upstream: poll `done.json` every 1s.
4. **Detect a stalled worker.** Upstream: poll `heartbeat.json`
   mtime; gap > 60s → strike; 3 strikes → kill pane.
5. **Recover after process restart.** Upstream: re-read tmux pane
   list, re-scan task files, rebuild `activeWorkers` map.

The Go port replaces all five with primitives that are first-class
in the language: goroutine, `chan`, `time.Ticker`, `context.CancelFunc`,
`sync.Mutex`. The chapter's central teaching point is that the
1,034-line TypeScript collapse to ~810 lines of Go — and the
collapse is not in clever compression but in **removed concepts**:
no tmux, no `done.json`, no per-pane heartbeat file, no mtime
arithmetic, no `tmux has-session` checks on resume.

## Solution

Three goroutines per Pool:

- **N workers** (`worker.go`): each runs `workerLoop`, selecting on
  a shared `tasks <-chan Task`. On task pickup it sets `currentTask`,
  calls `runTask` (which sleeps for `t.WorkSeconds` to simulate
  work and panics if `t.Panic` is set, with a defer-recover converting
  the panic to an error), then sends a `WorkerResult` on the
  `done <-chan WorkerResult`.

- **1 watchdog** (`watchdog.go`): a `for { select { case <-ctx.Done():
  return; case <-ticker.C: watchdogTick(p) } }` loop. Each tick
  scans the heartbeat map; if a worker's gap exceeds 60s, increment
  its strike count; at 3 strikes call `pool.killWorker(name)` —
  which cancels the worker's `context.Context` and spawns a
  replacement under the same name.

- **1 main loop** (`pool.go`'s `Run`): selects on the results channel.
  Each result invokes `handleResult`: success → `status=done`,
  decrement `pendingCount`; failure with `Retries < maxRetries` →
  `Retries++`, re-queue with `status=pending`; failure at the cap
  → `status=failed`, decrement `pendingCount`. Run returns when
  pendingCount hits zero or ctx fires.

`Resume` (`resume.go`) is a one-shot: read every task file under
`<root>/tasks/*.json`, push pending+orphaned IDs onto the dispatch
channel, return a `Pool` ready for `.Run()`. ~25 LOC because there
is no tmux session to verify and no panes to enumerate.

## How It Works

### A tmux pane is a goroutine

```go
// worker.go: the entire "pane lifecycle" is one for-select
func workerLoop(ctx context.Context, name string, tasks <-chan Task,
                done chan<- WorkerResult, beat *time.Time, beatMu *sync.Mutex,
                currentTask *string, currentTaskMu *sync.Mutex) {
    updateBeat(beat, beatMu)
    for {
        select {
        case <-ctx.Done():
            return
        case t, ok := <-tasks:
            if !ok { return }
            currentTaskMu.Lock(); *currentTask = t.ID; currentTaskMu.Unlock()
            err := runTask(ctx, t, beat, beatMu)
            currentTaskMu.Lock(); *currentTask = ""; currentTaskMu.Unlock()
            done <- WorkerResult{Worker: name, TaskID: t.ID, Err: err}
        }
    }
}
```

Compare with `runtime.ts` L582-L770 (`spawnWorkerForTask`): tmux
split-window, build CLI argv, send-keys for prompt, attach
heartbeat writer, register pane in lookup table. Same behavior,
~190 lines vs. 25.

### `time.Ticker` + `context.CancelFunc` is the watchdog

```go
// watchdog.go: heartbeat staleness → strike → respawn
func watchdog(ctx context.Context, pool *Pool, tick time.Duration) {
    t := time.NewTicker(tick); defer t.Stop()
    for {
        select {
        case <-ctx.Done(): return
        case <-t.C: watchdogTick(pool)
        }
    }
}
// watchdogTick: scan beats; if gap > 60s, strike++; at 3, killWorker.
// killWorker cancels the context (worker exits via ctx.Done) and
// spawns a replacement goroutine under the same name.
```

Upstream's `watchdogCliWorkers` (L466-L580, ~115 lines) does the
same arithmetic but layered on three sub-loops: done.json poll,
`isWorkerAlive(paneId)` for dead-pane, heartbeat for stalls.
The Go port deletes the first two: channels eliminate done.json,
defer-recover turns panics into channel results so dead-pane
detection becomes the same code path as natural failure.

### Channels eliminate the `done.json` round-trip

```go
// pool.go: one select replaces an entire polling loop
case res := <-p.results:
    p.handleResult(res)
    if pendingCount == 0 { return nil }
```

Upstream needs this on every tick:

```ts
// runtime.ts L481-L504 (paraphrased)
for (const [name, state] of activeWorkers) {
    const donePath = path.join(state.taskDir, 'done.json');
    if (await fs.exists(donePath)) {
        const done = JSON.parse(await fs.readFile(donePath));
        await markTaskFromDone(...);
        await fs.unlink(donePath);
        await tmux.killPane(state.paneId);
        await spawnNextPendingTask(...);
    }
}
```

That entire block — file existence check, JSON parse, mark,
unlink, kill, respawn — collapses to the Go select statement plus
`handleResult`. The clean-up (`fs.unlink`) vanishes because there
is no signal file to remove. The `tmux.killPane` vanishes because
the worker reuses its goroutine for the next task. The poll loop
(`for ... await fs.exists`) vanishes because `select` is push-driven.

## What Changed (vs. s09)

s09 was the first chapter where coordination crossed a process
boundary: filesystem-backed CAS, file locks, atomic rename. **The
filesystem was the coordination primitive.** s10 keeps that on-disk
state (Resume reads from it) but introduces a fundamentally
different primitive: **the goroutine pool is the execution primitive**.

| Concern | s09 | s10 |
|---|---|---|
| Coordination primitive | `flock` + `os.Rename` + `crypto/rand` token | `chan Task` + `chan WorkerResult` + `time.Ticker` + `context.CancelFunc` |
| First-class data | `Task`, `Claim`, `LeaseDuration` | `WorkerResult`, `Worker`, `Pool`, plus `Task` (re-declared) |
| Process model | Single process, but coordination across processes via flock | Single process, N+2 goroutines, channels for everything |
| Crash recovery | The next worker reads stale Claim, sees lease expired, re-claims | `Resume()` reads pending+orphaned task files, re-pushes onto channel |
| External deps | `github.com/gofrs/flock` (one) | **stdlib only** |
| Where state lives | On disk (durable, slow) | In RAM (fast, ephemeral) — disk is the audit log |

The biggest single conceptual delta in the curriculum: where s09
was "files + locks talking to each other," s10 is "goroutines
talking to each other with files as the audit trail." Resume is
the bridge — it reads the audit trail to repopulate the in-memory
state. Once Resume returns, the on-disk store is purely an
observer; the channels run the show.

This is also the chapter that **deletes** the upstream architecture
rather than translating it. Tmux: gone. `done.json`: gone.
`heartbeat.json` mtime polling: gone. The phrase "1,034 lines →
~810 lines" understates the cognitive shrink: many of those 1,034
lines are *concepts that no longer apply* in a single-process
goroutine model.

## Try It

```bash
cd agents/s10-team-watchdog

GOWORK=off go vet ./...                # silent
GOWORK=off go build ./...              # silent
GOWORK=off go test -v -count=1 -timeout=30s ./...   # 6 tests, ~1s
GOWORK=off go run .                    # output matches testdata/expected.txt
```

Expected output:

```
== Setup ==
workers=3 tasks=7 store=<tmpdir>

== Submitting 7 tasks ==
submitted id=task-0 work=0.10s
submitted id=task-1 work=0.20s
submitted id=task-2 work=0.15s
submitted id=task-3 work=0.30s
submitted id=task-4 work=0.20s
submitted id=task-5 work=0.25s
submitted id=task-6 work=0.15s

== Completions ==
id=task-0 status=done
id=task-1 status=done
id=task-2 status=done
id=task-3 status=done
id=task-4 status=done
id=task-5 status=done
id=task-6 status=done

== Final tallies ==
done=7 failed=0 total=7
```

Note that the demo deliberately suppresses per-task `Retries` from
its output — which task the watchdog respawned at the 150ms mark
varies with scheduler timing, so the count is non-deterministic.
The status convergence (every task reaches `done`) IS deterministic;
that's the stable assertion.

Further exercises:

- Replace `runTask`'s sleep with an `exec.CommandContext` that
  invokes a real CLI binary. The defer-recover should keep working;
  any non-zero exit becomes a `WorkerResult{Err: ...}`.
- Add a `worktree string` field to `Task` and have the worker
  `git worktree add` before the run, `git worktree remove` after —
  one worktree per concurrent worker, the upstream's
  `TEAM-WORKTREE-MODE.md` translated to Go.
- Add a `tmux` build tag for an optional tmux-pane backend, proving
  the Worker abstraction is plug-replaceable.

## Upstream Source Reading

Excerpt from `src/team/runtime.ts` L466-L580 (`watchdogCliWorkers`),
the function the Go port most directly replaces. Full annotated copy
at `upstream-readings/s10-runtime.ts`:

```ts
async function watchdogCliWorkers(runtime: TeamRuntime): Promise<void> {
  // (a) done.json poll. ~30 lines. Deleted in Go.
  for (const [name, state] of runtime.activeWorkers) {
    const donePath = path.join(state.taskDir, 'done.json');
    if (await fs.exists(donePath)) {
      const done = JSON.parse(await fs.readFile(donePath, 'utf8'));
      await markTaskFromDone(runtime.teamName, state.taskId, runtime.cwd, done);
      await fs.unlink(donePath);
      await tmux.killPane(state.paneId);
      await spawnNextPendingTask(runtime, name);
    }
  }
  // (b) dead-pane detection via isWorkerAlive(paneId).
  for (const [name, state] of runtime.activeWorkers) {
    if (!await isWorkerAlive(state.paneId)) {
      await applyDeadPaneTransition(runtime, name, state);
      await spawnNextPendingTask(runtime, name);
    }
  }
  // (c) heartbeat stale check. The Go watchdog mirrors only this branch.
  const now = Date.now();
  for (const [name, state] of runtime.activeWorkers) {
    const heartbeat = await readHeartbeat(state.heartbeatPath);
    if (now - new Date(heartbeat.updatedAt).getTime() > 60_000) {
      state.unresponsiveCount = (state.unresponsiveCount || 0) + 1;
      if (state.unresponsiveCount >= 3) {
        await tmux.killPane(state.paneId);
        await applyDeadPaneTransition(runtime, name, state);
        await spawnNextPendingTask(runtime, name);
      }
    } else {
      state.unresponsiveCount = 0;
    }
  }
}
```

Reading notes (Go-port comparisons):

1. **Sub-loop (a) is deleted.** Worker → orchestrator signaling is
   a buffered `chan WorkerResult` write in Go, not a `done.json`
   filesystem rendezvous. The orchestrator reads via `<-p.results`
   in the Pool's main loop. No file existence check, no JSON parse,
   no `fs.unlink`. ~30 lines of TypeScript become 4 lines of Go.
2. **Sub-loop (b) is folded into worker self-recovery.** The
   `isWorkerAlive(paneId)` check exists upstream because a tmux
   pane can die without writing done.json (process killed by user,
   OOM, segfault). Goroutines have an analogous failure: a panic.
   `worker.go`'s `runTask` wraps the work in `defer func() { if r
   := recover(); r != nil { err = ... } }()`. The recovered error
   becomes a normal `WorkerResult{Err: ...}` — same channel, same
   handler, same retry path. Dead-pane detection vanishes.
3. **Sub-loop (c) is the surviving branch.** This IS what
   `watchdog.go`'s `watchdogTick` mirrors: read heartbeat, compute
   gap, count strikes, kill+respawn at threshold. The constants
   `HeartbeatStaleThreshold = 60s` and `UnresponsiveStrikeMax = 3`
   are exposed for tunability. The Go version uses `*time.Time`
   cells under `beatsMu` instead of mtime on `heartbeat.json`.
4. **`tmux.killPane(state.paneId)` becomes `worker.cancel()`.**
   Both terminate the unit of execution. The cancel propagates via
   the worker's `context.Context`; the worker's `runTask` returns
   `ctx.Err()`; the worker exits at the next select. The
   replacement is spawned by `spawnWorker` under the same name.
5. **`spawnNextPendingTask` is implicit.** Upstream picks the next
   pending task and assigns it to the freshly-respawned worker.
   Go's respawned worker just reads from the shared `tasks`
   channel — whichever task arrives next is what it runs. The
   "assignment" step disappears because the channel IS the
   assignment.
6. **`watchdog-failed.json` fallback (L552-L564) is dropped.**
   Upstream tracks consecutive errors in the watchdog itself; at
   3, it gives up and writes a marker file. The Go watchdog reads
   in-memory state, so the only failure mode is a deadlocked Pool
   — caught by `Shutdown`'s grace timeout, not a marker file.

The 1,034-line shrink is real, but the line count is a proxy for
something deeper: **conceptual surface area**. Upstream's runtime
maintains tmux session state, pane id tables, heartbeat files,
done.json files, retry-marker files, watchdog-failed files. The Go
runtime maintains in-memory maps + buffered channels. The student
who reads both side-by-side comes away understanding why
"goroutine + channel" is consistently the Go answer to "how do I
build a worker pool?"
