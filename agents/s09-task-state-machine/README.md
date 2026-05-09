# s09 — Task State Machine / 文件态任务状态机

> Ninth chapter of `learn-oh-my-claudecode`. We pivot from s08's pure
> regex + light `os/exec` into the chapter where the filesystem
> *itself* is the coordination primitive: a tiny task store backed by
> JSON files, an exclusive `flock` per task, atomic-rename writes, and
> claim tokens with 15-minute leases. This is the foundation s10's
> goroutine pool will plug into.

## Scope (one line)

A `Store` rooted at `<workdir>/.omc/state/team` that exposes
`ClaimTask`, `TransitionTask`, `RenewClaim` over a per-task `flock`,
with atomic `os.Rename`-based persistence and `crypto/rand` claim
tokens — ported from `src/team/state/tasks.ts` L1–L120 and
`src/team/runtime.ts` L80–L140 in ~470 Go lines.

## External dependencies

This chapter pulls in **one** library:

- `github.com/gofrs/flock` v0.12.1 (MIT). Cross-platform advisory
  file locking. Hand-rolling `syscall.Flock` would tie us to Linux;
  this dep is paid once and unlocks Darwin / Linux / FreeBSD /
  Windows. No transitive runtime deps beyond `golang.org/x/sys`.

The rest of the chapter is stdlib only (`crypto/rand`, `encoding/json`,
`os`, `path/filepath`, `sort`, `strings`, `sync`, `time`).

## Files

| Path | Role |
|---|---|
| `task.go` | `Task`, `Claim`, and the `LeaseDuration = 15 * time.Minute` constant. JSON tags align with upstream's `TeamTaskV2` shape; field set is shrunk to the four-state machine (pending / in_progress / done / failed). |
| `store.go` | `Store{ root }` with `Read`, `Write`, `List`. No in-memory cache. Layout `<root>/<team>/tasks/<id>.json`. List filters to `.json` exact suffix so `.lock` and `.tmp` siblings don't leak into task IDs. |
| `atomic.go` | `writeAtomic(path, data, perm)`. Writes to `path.tmp`, then `os.Rename`. Crash-safe by construction: a partial `.tmp` never promotes; readers see either the prior file or the new file. |
| `lock.go` | `withFileLock(path, fn)`. Wraps `gofrs/flock` on `path+".lock"`. Blocks indefinitely; release is deferred so a panic inside fn cannot deadlock the team. |
| `claim.go` | The state machine: `ClaimTask`, `TransitionTask`, `RenewClaim`, plus the four `Err*` sentinels. Tokens are 32-hex-char crypto/rand outputs. |
| `main.go` | Demo: tempdir → seed pending → worker-A claim → worker-B claim fails → worker-A transitions to done → print final task. Output is captured at `testdata/expected.txt`. |
| `claim_test.go` | The four spec'd tests: `TestClaimTaskMintsToken`, `TestSecondClaimFailsWhileLeaseValid`, `TestSecondClaimSucceedsAfterLeaseExpires`, `TestTransitionRequiresMatchingToken`. |
| `lock_test.go` | `TestWithFileLockSerializesConcurrentCallers` — 8 goroutines race on the same lock target; an interleave check trips the test if `flock` is somehow ignored. |
| `atomic_test.go` | `TestWriteAtomicSurvivesPanicSimulation` — open `.tmp`, write 1 KB, panic before rename; assert the target's prior contents are intact. |
| `testdata/expected.txt` | Captured `go run .` stdout for drift detection. |
| `go.mod` / `go.sum` | One dep (`github.com/gofrs/flock`); `go.sum` carries the dep + `golang.org/x/sys` + the test dep. |

## Run

```bash
cd agents/s09-task-state-machine

GOWORK=off go vet ./...                # silent
GOWORK=off go build ./...              # silent
GOWORK=off go test -v -count=1 ./...   # 6 tests pass, sub-second
GOWORK=off go run .                    # output matches testdata/expected.txt
```

## Three teaching points

1. **Filesystem as coordination primitive.** The whole chapter has
   zero `sync.Mutex`. Mutual exclusion comes from `flock`; durability
   from `os.Rename`; identity from `crypto/rand`. A working
   distributed task queue, on a single laptop or across an NFS mount.
2. **Lease + token, not just token.** A token alone reuses the
   "owner forever until explicit release" model — fine until the
   owner's process dies. Adding `LeasedUntil` lets a successor claim
   without operator intervention; the test
   `TestSecondClaimSucceedsAfterLeaseExpires` is the recovery story.
3. **Atomic rename is the entire crash story.** `writeAtomic` writes
   to `<target>.tmp` and renames. A partial `.tmp` never becomes the
   target; a crashed writer leaves the prior contents intact. The
   simulation in `atomic_test.go` proves this without actually
   crashing the test process.

## Anti-pattern callout

Plan §"Anti-pattern #6" reads "Tmux as worker dispatch." This chapter
is the *separation* point: task state and worker lifecycle are two
distinct concerns. Solving claim/transition/lease here, on the
filesystem alone, lets s10 plug in any worker model — goroutines,
`os/exec`, or yes, tmux — without touching this code. That separation
is why the upstream's 1,034-line `runtime.ts` collapses into ~250
idiomatic Go lines once you put the state machine where it belongs.

## Upstream lineage

- `src/team/state/tasks.ts` L1–L120 — `claimTask` and the
  `withTaskClaimLock` wrapper. Ported in `claim.go` with three
  shrinks: no team-config check, no readiness graph, no
  blocked-vs-pending distinction. The lease + token discipline is
  preserved verbatim.
- `src/team/runtime.ts` L80–L140 — `writeTask`, `readTask`,
  `markTaskInProgress`, `markTaskFromDone`. Ported in `store.go` and
  `claim.go`. The `withTaskLock` ↔ `withFileLock` rename is
  cosmetic; behavior is unchanged.
- See `upstream-readings/s09-tasks.ts` for the annotated excerpt.
