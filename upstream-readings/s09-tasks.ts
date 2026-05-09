// Source: https://raw.githubusercontent.com/Yeachan-Heo/oh-my-claudecode/main/src/team/state/tasks.ts
// Lines:  L1-L120 (claimTask + withTaskClaimLock context); read alongside
//         src/team/runtime.ts L80-L140 (writeTask / readTask / markTask*).
// License: MIT, Copyright (c) 2025 Yeachan Heo
//
// Annotated excerpt for s09. The upstream surface has FOUR pieces:
//   (1) computeTaskReadiness — dependency-graph readiness predicate
//   (2) withTaskClaimLock    — generic concurrent-access coordinator
//   (3) claimTask            — atomic claim with token + lease mint
//   (4) writeAtomic          — temp-then-rename persistence helper
//
// We port (2)-(4). Readiness (1) is intentionally deferred — flat
// tasks are sufficient for this chapter, and depends_on can be a
// later exercise without changing the lock or claim layers.

import { randomUUID } from 'crypto';
import type { TeamTaskV2, ClaimTaskResult } from '../types.js';

// ─── (1) computeTaskReadiness (L20-L37, NOT ported) ───
//
// Walks task.depends_on (or .blocked_by), checks each dep's status,
// returns ready=true iff all deps are 'completed'. Pure data; touches
// no locks. The Go port collapses this to "any non-terminal task is
// claimable" — see the design note in claim.go's docstring.

// ─── (2) withTaskClaimLock (L43, dependency-injected; impl elsewhere) ───
//
// Signature in upstream:
//   withTaskClaimLock<T>(teamName, taskId, cwd, fn) =>
//     Promise<{ok:true, value:T} | {ok:false}>
//
// The Go equivalent (lock.go) flattens the result: lock-acquired-then-
// fn-returns-error returns the error directly, lock-acquire-failed
// returns a wrapped flock error. There is no timed "{ok:false}" path
// because flock blocks; if the OS can't grant the lock, that is a
// real bug and we want it loud, not silently absorbed.

// ─── (3) claimTask (L50-L99) ───
//
// THE function this chapter ports. Flow inside the lock (L66-L96):
//   read current → normalize → version CAS → readiness recheck →
//   terminal/conflict checks → mint token → bump version → write.
//
// Two upstream invariants the Go port preserves verbatim:
//   - 15-minute lease (L90: `15 * 60 * 1000`). Constant LeaseDuration.
//   - Version bump on every successful mutation (L91). Field Task.Version.
//
// Two we skip (with rationale in claim.go):
//   - Worker-name validation against TeamConfig (L57). A higher layer
//     concern; teaching scope keeps this surface flat.
//   - readinessAfterLock recheck (L72-L75). No deps, no readiness.

export async function claimTask(
  taskId: string,
  workerName: string,
  expectedVersion: number | null,
  deps: ClaimTaskDeps,
): Promise<ClaimTaskResult> {
  // ... existence + worker check elided for brevity ...

  const lock = await deps.withTaskClaimLock(deps.teamName, taskId, deps.cwd, async () => {
    const current = await deps.readTask(deps.teamName, taskId, deps.cwd);
    if (!current) return { ok: false as const, error: 'task_not_found' as const };

    const v = deps.normalizeTask(current);
    if (expectedVersion !== null && v.version !== expectedVersion)
      return { ok: false as const, error: 'claim_conflict' as const };

    // Status guards. Terminal (done/failed) is rejected; in_progress
    // with active claim is rejected. Pending/blocked with no claim
    // and matching owner is the only path through.
    if (deps.isTerminalTaskStatus(v.status))
      return { ok: false as const, error: 'already_terminal' as const };
    if (v.status === 'in_progress')
      return { ok: false as const, error: 'claim_conflict' as const };
    if ((v.status === 'pending' || v.status === 'blocked') && v.claim)
      return { ok: false as const, error: 'claim_conflict' as const };

    // ─── The mint. 15-minute lease window. crypto.randomUUID().
    const claimToken = randomUUID();
    const updated: TeamTaskV2 = {
      ...v,
      status: 'in_progress',
      owner: workerName,
      claim: { owner: workerName, token: claimToken,
               leased_until: new Date(Date.now() + 15 * 60 * 1000).toISOString() },
      version: v.version + 1,
    };

    // writeAtomic = write to <path>.tmp, then rename. Survives crash.
    await deps.writeAtomic(deps.taskFilePath(deps.teamName, taskId, deps.cwd),
                           JSON.stringify(updated, null, 2));
    return { ok: true as const, task: updated, claimToken };
  });

  if (!lock.ok) return { ok: false, error: 'claim_conflict' };
  return lock.value;
}

// ─── (4) writeAtomic (impl in src/team/runtime.ts) ───
//
//   await fs.writeFile(path + '.tmp', data);
//   await fs.rename(path + '.tmp', path);
//
// Atomic with respect to readers because POSIX guarantees rename(2)
// either points the directory entry at the new inode or doesn't. A
// half-written .tmp never becomes the target. The Go port (atomic.go)
// uses the same two-line shape with an extra MkdirAll for the parent.
