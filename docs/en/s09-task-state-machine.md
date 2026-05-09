---
title: "s09 · File-backed Task State Machine"
chapter: 9
slug: s09-task-state-machine
est_read_min: 11
---

# Chapter 9 — File-backed Task State Machine (CAS + Lease)

> Ninth chapter of `learn-oh-my-claudecode`. We pivot from s08's pure
> regex + light `os/exec` into the chapter where the **filesystem
> itself becomes the coordination primitive**: a JSON-backed task
> store, one `flock` per task, atomic-rename writes, and claim tokens
> with 15-minute leases. s10's goroutine pool will be built on top of
> this layer.

## Problem

Multiple workers staring at the same pending-task list need to pick up
work without stepping on each other. Production traces show at least
four failure modes:

1. **Two workers both claim task #7.** Without mutual exclusion, A's
   read-modify-write and B's interleave: both see status=pending,
   both flip it to in_progress, the on-disk record reflects whoever
   wrote second. Both processes start working; their outputs collide.
2. **A claims, then crashes; #7 is stuck in_progress forever.** A
   bare `owner` field is the "claim it forever" model — once A is
   gone, no successor can take over. Some expiry mechanism is
   required.
3. **A process is killed mid-write; on-disk task.json is half-baked.**
   A reader could see `{"status":"in_pr…` and json.Unmarshal explodes.
   The store must guarantee readers see either the prior file or the
   new file — never a partial.
4. **B forges a token and pushes a transition.** Without token
   validation, any reader can pretend to be the owner and mark a
   task done — the state machine becomes an open read-write database.

Upstream solves these in `src/team/state/tasks.ts` L1-L120 with three
primitives: a per-task `flock` (`withTaskClaimLock`, L43), an atomic-
rename `writeAtomic` (called from `src/team/runtime.ts` L80-L140), and
`claimTask` (L50-L99) that bakes token + leased_until into the JSON.
We port all three plus `crypto/rand` tokens — about 470 Go lines.

## Solution

Four files, each owning one concern, none crossing the others:

- **`atomic.go`**: `writeAtomic(path, data, perm)`. Writes
  `path.tmp`, then `os.Rename`. POSIX guarantees rename is atomic
  for readers — a partial `.tmp` never becomes the target; a
  crashed writer simply leaves the prior file untouched.
- **`lock.go`**: `withFileLock(lockPath, fn)`. Takes an exclusive
  `flock` on `lockPath+".lock"`, runs fn, defers the release. We
  use `github.com/gofrs/flock` rather than hand-rolling
  `syscall.Flock` because the latter is Linux-specific; this single
  dep is paid once and unlocks Darwin / Linux / FreeBSD / Windows.
- **`store.go`**: `Store{ root }` with the trio `Read` / `Write` /
  `List`. No in-memory cache; every read goes to disk. Layout is
  `<root>/<team>/tasks/<id>.json`, byte-for-byte aligned with
  upstream's `taskFilePath`.
- **`claim.go`**: the state machine. `ClaimTask` reads inside the
  lock → checks terminal status → checks the lease → mints a token
  → bumps Version → atomic-writes. `TransitionTask` checks status
  CAS, token equality, and lease freshness — all three must pass
  before the write. `crypto/rand` produces a 16-byte hex token —
  128 bits of entropy, on par with UUIDv4.

Mutual exclusion comes from `flock`, durability from `os.Rename`,
identity from `crypto/rand`. There is not a single `sync.Mutex` in
the chapter.

## How It Works

### One flock + one atomic rename = the entire crash story

```go
// claim.go: the heart of ClaimTask, error-handling elided
err := withFileLock(s.taskPath(team, taskID), func() error {
    t, _ := s.Read(team, taskID)
    if t.Claim != nil && time.Now().Before(t.Claim.LeasedUntil) {
        return ErrLeaseStillValid
    }
    newToken, _ := randomUUID()
    t.Status, t.Owner = "in_progress", worker
    t.Claim = &Claim{Token: newToken, Owner: worker,
                     LeasedUntil: time.Now().Add(LeaseDuration)}
    t.Version++
    return s.Write(team, t)  // internally calls writeAtomic
})
```

After the lock releases, the on-disk task.json is either A's version
(success) or untouched (any failure). Two workers calling ClaimTask
simultaneously: the first acquires the lock, mints, writes; the
second blocks at Lock, enters fn after release, sees the live Claim,
returns ErrLeaseStillValid. No half-states, no double-owner.

### The token is the line between "you think you are" and "you actually are"

```go
// claim.go: TransitionTask's three-step check
if t.Status != fromStatus { return ErrIllegalTransition }
if t.Claim == nil || t.Claim.Token != token || t.Claim.Owner != worker {
    return ErrTokenMismatch
}
if !time.Now().Before(t.Claim.LeasedUntil) { return ErrLeaseStillValid }
```

The token is generated via `crypto/rand` and returned by ClaimTask.
It is the only proof a worker has that it is the legitimate holder.
Even if another worker reads the on-disk task.json and sees the
token field, it has no legal path forward — it never invoked the
ClaimTask that produced that mint. `TestTransitionRequiresMatchingToken`
verifies this by trying to push a transition with a forged 32-char
hex string `deadbeef…` and asserting the failure.

### The lease keeps a dead worker from blocking the queue forever

```go
// claim_test.go simulates expiry by rewinding LeasedUntil one minute
stale.Claim.LeasedUntil = time.Now().Add(-time.Minute)
s.Write(team, stale)
// the second worker can now claim
tokenB, _ := s.ClaimTask(team, id, "worker-B")
```

LeaseDuration = 15 minutes (`time.Now().Add(LeaseDuration)`). Once
wall-clock crosses LeasedUntil, the next worker calling ClaimTask
sees the dead lease and proceeds to mint a new token, overwriting
owner. The original worker's token still lives in whatever
"previous Write" was last on disk — but the next atomic-write
clobbers it. If the dead worker miraculously revives and tries to
push a TransitionTask with its old token, the equality check fails
and it gets ErrTokenMismatch.

## What Changed (vs. s08)

s08 was a **string → Decision recommender + an Executor that actually
forks** — two layered APIs, but everything lived inside a single
process. s09 introduces something fundamentally new: **the filesystem
as a coordination primitive**. For the first time we use real
concurrency primitives (`flock`), real crash-safe I/O (`os.Rename`),
and a state machine where optimistic concurrency is enforced via
tokens rather than an owner field.

| Concern | s08 | s09 |
|---|---|---|
| First-class data | `Decision`, `Handle` | `Task`, `Claim`, `LeaseDuration` |
| Coordination primitive | (none — single process) | `flock` mutex + `os.Rename` atomic write + crypto/rand token |
| Crash model | (untouched — `Setpgid` guarded against orphans) | readers see old or new, never partial; tokens block impersonation |
| Side-effect range | one fork, one process lifetime | a directory of JSON + `.lock` + `.tmp` siblings, durable across processes |
| External deps | none (stdlib only) | `github.com/gofrs/flock` (one) |

This is also the first chapter that requires a **lease semantics**
discussion — "having claimed it does not mean keeping it; any
successor can take over after LeasedUntil." That muscle memory
carries directly into s10's watchdog: there, a per-second heartbeat
plus a 60-second silence threshold marks workers dead.

## Try It

```bash
cd agents/s09-task-state-machine

GOWORK=off go vet ./...                # silent
GOWORK=off go build ./...              # silent
GOWORK=off go test -v -count=1 ./...   # 6 tests pass, sub-second
GOWORK=off go run .                    # output matches testdata/expected.txt
```

Expected output:

```
== Seed: pending task ==
team=demo id=fix-login status=pending version=1

== Worker A: ClaimTask ==
worker=worker-A token_len=32

== Worker B: ClaimTask (must fail) ==
worker=worker-B err=ErrLeaseStillValid (expected)

== Worker A: TransitionTask in_progress -> done ==
transition=ok

== Final task on disk ==
{
  "id": "fix-login",
  "status": "done",
  "version": 3,
  "description": "Fix the broken login flow in src/auth/login.go",
  "created_at": "0001-01-01T00:00:00Z",
  "updated_at": "0001-01-01T00:00:00Z"
}
```

The final line prints the token *length* (not the value) because the
token is random per run; 32 hex chars is the stable assertion — that's
16 bytes of entropy in hex encoding. Timestamps are zeroed for
reproducible fixtures; the on-disk write path is unchanged.

Further exercises:

- Add an end-to-end test for `RenewClaim`: worker A claims, the
  test rewinds LeasedUntil one minute into the past, Renew pulls
  it back to the future; concurrently, attempt a `ClaimTask` from
  worker B and confirm it bounces with ErrLeaseStillValid.
- Add a `DependsOn []string` field to `Task` and a readiness
  predicate modeled on upstream's `computeTaskReadiness`
  (tasks.ts L20-L37); have ClaimTask re-check readiness *after*
  acquiring the lock so a dependency can't slip while you wait.

## Upstream Source Reading

Excerpt from `src/team/state/tasks.ts` L1-L120 (full annotated copy
at `upstream-readings/s09-tasks.ts`):

```ts
// L43 — withTaskClaimLock signature (implementation lives elsewhere)
withTaskClaimLock: <T>(
  teamName, taskId, cwd, fn
) => Promise<{ ok: true; value: T } | { ok: false }>;

// L50-L99 — claimTask, the chapter's main course
export async function claimTask(taskId, workerName, expectedVersion, deps) {
  const lock = await deps.withTaskClaimLock(deps.teamName, taskId, deps.cwd,
    async () => {
      const current = await deps.readTask(...);
      if (!current) return { ok: false, error: 'task_not_found' };
      const v = deps.normalizeTask(current);
      if (expectedVersion !== null && v.version !== expectedVersion)
        return { ok: false, error: 'claim_conflict' };
      if (deps.isTerminalTaskStatus(v.status))
        return { ok: false, error: 'already_terminal' };
      if (v.status === 'in_progress')
        return { ok: false, error: 'claim_conflict' };

      // L85-L92 — mint token + 15-minute lease + version bump
      const claimToken = randomUUID();
      const updated = { ...v, status: 'in_progress', owner: workerName,
        claim: { owner: workerName, token: claimToken,
                 leased_until: new Date(Date.now() + 15 * 60 * 1000).toISOString() },
        version: v.version + 1 };
      await deps.writeAtomic(deps.taskFilePath(...), JSON.stringify(updated, null, 2));
      return { ok: true, task: updated, claimToken };
    });
  if (!lock.ok) return { ok: false, error: 'claim_conflict' };
  return lock.value;
}
```

Reading notes (Go-port comparisons):

1. **L43 (`withTaskClaimLock` injected) → `withFileLock`.** Upstream
   injects the lock primitive because four call sites share it
   (claim, transition, release, renew). The Go port uses a top-level
   function because Go does not need a mocked lock for tests
   (`flock` is real, t.TempDir() spins up an isolated mount instantly).
   The `{ok, value}` wrapper degenerates in Go to `(value, error)` —
   no separate "lock acquired but fn signaled failure" middle state
   to disambiguate.
2. **L66-L67 (re-read `current` inside the lock) → single read inside
   our lock.** This is the critical "double-read" pattern: the
   pre-lock `existing` was a fast-fail (skip the lock if the task
   doesn't exist), but the source-of-truth read must happen *inside*
   the lock. The Go port keeps only the inside-lock read because
   `flock` is cheap (one syscall); trading one microsecond for
   one fewer branch is the right call.
3. **L70-L71 (version CAS) → Task.Version field.** The Go port keeps
   the Version field and bumps it on every Write, but **does not
   currently expose `expectedVersion` in ClaimTask's signature**.
   The reason: claim + transition is closable using just status
   equality + token; Version is a hook for a future exercise (the
   README asks the reader to wire it into TransitionTask).
4. **L77-L78 (terminal + in_progress rejection) → split into two
   branches in claim.go.** Go uses `Status == "done"||"failed"` for
   ErrIllegalTransition, then `t.Claim != nil && Now().Before(...)`
   for ErrLeaseStillValid. The split is intentional — recovery
   strategies differ: terminal means give up; live lease means
   retry later. Upstream collapses both to `claim_conflict`; Go
   refines.
5. **L85 (`randomUUID()`) → `crypto/rand` + `hex.EncodeToString`.**
   Sixteen random bytes is 128 bits of entropy, equivalent to a
   UUIDv4 in token-space. We deliberately do *not* import
   `github.com/google/uuid` — the chapter already pulls in
   `gofrs/flock`, and a second dep would dilute the teaching focus
   (the state-machine discipline).
6. **L90 (`15 * 60 * 1000` ms) → `LeaseDuration = 15 * time.Minute`.**
   Promoted to a named constant so `TestSecondClaimSucceedsAfterLeaseExpires`
   doesn't fight a magic number. Fifteen minutes covers ~900
   heartbeat seconds — generous enough to avoid premature kills,
   short enough that crashed workers are reclaimed within a coffee
   break.
7. **L94 (`writeAtomic`) → `atomic.go`.** Upstream's implementation
   is two lines (write tmp + rename). The Go port adds an MkdirAll
   for first-team-write, plus a best-effort `os.Remove(tmp)` on
   rename failure (so an orphan doesn't accumulate without masking
   the original error). `TestWriteAtomicSurvivesPanicSimulation`
   exists to prove this two-line primitive really does block the
   "writer dies mid-write" failure.
