package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"time"
)

// Sentinel errors returned by ClaimTask, TransitionTask, and
// RenewClaim. Tests assert against these via errors.Is. Each one
// corresponds to a distinct upstream error code from
// `ClaimTaskResult` / `TransitionTaskResult` (src/team/state/tasks.ts):
//
//   - ErrTaskNotFound       ↔ "task_not_found"
//   - ErrLeaseStillValid    ↔ "claim_conflict" (with active lease)
//   - ErrTokenMismatch      ↔ "claim_mismatch"
//   - ErrIllegalTransition  ↔ "invalid_transition"
//
// The Go names lean toward describing the *condition* rather than the
// upstream wire string; readers familiar with the TS code will spot
// the correspondence, and readers new to the codebase get
// self-documenting names.
var (
	ErrTaskNotFound      = errors.New("task not found")
	ErrLeaseStillValid   = errors.New("task already claimed (lease valid)")
	ErrTokenMismatch     = errors.New("claim token does not match")
	ErrIllegalTransition = errors.New("illegal status transition")
)

// randomUUID returns a random hex-encoded 16-byte token, formatted
// as a v4-style UUID without dashes (32 hex chars). Using crypto/rand
// rather than math/rand is non-negotiable: the token is the entire
// security boundary on a claim — anyone who guesses it can transition
// the task. crypto/rand.Read fails only on broken systems and we
// surface that as a fatal error rather than a falsifiable token.
//
// We deliberately do NOT pull in github.com/google/uuid for one
// 16-byte read. The chapter's teaching point is the state-machine
// discipline; a transitive UUID dep would obscure that. Hex output is
// indistinguishable from a UUIDv4 in token-space: 128 bits of entropy.
func randomUUID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("randomUUID: read entropy: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}

// ClaimTask attempts to atomically claim a pending task for `worker`.
// Returns the freshly minted claim token on success. The token must be
// retained by the caller and presented to TransitionTask / RenewClaim;
// it is the only proof of ownership the store accepts.
//
// Mirrors upstream's `claimTask` (src/team/state/tasks.ts L50-L99)
// with three teaching-driven simplifications:
//
//   - We do NOT check team configuration. The upstream rejects
//     unknown worker names; for a learning module the verification
//     belongs higher up the stack.
//   - We do NOT compute task readiness (dependency graph). Tasks
//     are flat in this chapter; depends_on is a runtime concern.
//   - We do NOT distinguish "blocked" from "pending". Either
//     non-terminal, non-in_progress status is claimable when the
//     lease is dead.
//
// The flow inside the file lock is straight out of the TS code:
//
//  1. Read the on-disk Task.
//  2. If status is terminal (done/failed) → ErrIllegalTransition.
//     A claimed-then-completed task should not be re-claimed.
//  3. If status is in_progress AND the existing claim's lease is
//     still in the future → ErrLeaseStillValid.
//  4. Otherwise mint a token, set Owner / Claim / Status / bump
//     Version, refresh UpdatedAt, atomic-write.
//
// Two callers racing here will serialize via withFileLock; the loser
// will see the winner's update and return ErrLeaseStillValid.
func (s *Store) ClaimTask(team, taskID, worker string) (string, error) {
	var token string

	err := withFileLock(s.taskPath(team, taskID), func() error {
		t, err := s.Read(team, taskID)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return ErrTaskNotFound
			}
			return err
		}

		// (2) Terminal task: claiming a done/failed task is a misuse;
		// surface as illegal transition rather than overwriting state.
		if t.Status == "done" || t.Status == "failed" {
			return fmt.Errorf("%w: cannot claim %s task", ErrIllegalTransition, t.Status)
		}

		// (3) Active lease check. The upstream test (tasks.ts L80-L83)
		// is "if v.claim || (v.owner && v.owner !== workerName) → conflict";
		// the Go version refines that with a wall-clock comparison so a
		// dead lease can be reaped (TestSecondClaimSucceedsAfterLeaseExpires).
		if t.Claim != nil && time.Now().Before(t.Claim.LeasedUntil) {
			return ErrLeaseStillValid
		}

		// (4) Mint a fresh token. We mint INSIDE the lock so two
		// concurrent claimers cannot both believe they hold a valid
		// token; the loser's call to randomUUID is wasted work, but
		// the winner's token is the only one written to disk.
		newToken, err := randomUUID()
		if err != nil {
			return err
		}

		now := time.Now()
		t.Status = "in_progress"
		t.Owner = worker
		t.Claim = &Claim{
			Token:       newToken,
			Owner:       worker,
			LeasedUntil: now.Add(LeaseDuration),
		}
		t.Version++
		t.UpdatedAt = now

		if err := s.Write(team, t); err != nil {
			return err
		}

		token = newToken
		return nil
	})

	if err != nil {
		return "", err
	}
	return token, nil
}

// TransitionTask moves a task from fromStatus to toStatus, with the
// caller proving ownership via worker+token. Three checks fail the
// transition; all of them under the file lock:
//
//  1. Task on disk has Status != fromStatus → ErrIllegalTransition.
//     The state machine refuses to transition off a status the caller
//     wasn't last observing. This is a CAS check on the status field.
//  2. Task has no Claim or Claim.Token != token → ErrTokenMismatch.
//     The caller is not the current lease holder.
//  3. Lease has expired (now >= LeasedUntil) → ErrLeaseStillValid.
//     We use the same sentinel as ClaimTask so callers can branch on
//     a single error: "the lease situation is wrong, recover and
//     retry". The transition refuses because allowing it would let a
//     dead worker overwrite state another worker has already taken.
//
// On success Status is updated, Version is bumped, UpdatedAt is
// refreshed, and Claim is cleared if the new status is terminal
// (done/failed). The latter is what allows a successor task or a
// completion-aware verifier to read final state without seeing a
// dangling claim record.
func (s *Store) TransitionTask(team, taskID, worker, token, fromStatus, toStatus string) error {
	return withFileLock(s.taskPath(team, taskID), func() error {
		t, err := s.Read(team, taskID)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return ErrTaskNotFound
			}
			return err
		}

		// (1) Status CAS. The caller's expectation of fromStatus is the
		// optimistic-concurrency guard; we don't trust the version int
		// here because the upstream protocol doesn't either — claim
		// token plus status equality is the canonical pair.
		if t.Status != fromStatus {
			return fmt.Errorf("%w: %s -> %s but task is in %s",
				ErrIllegalTransition, fromStatus, toStatus, t.Status)
		}

		// (2) Claim presence + token equality. We compare both Owner
		// and Token. Upstream relies on Token alone but checking Owner
		// catches the case where a malformed Task on disk has a token
		// that happens to collide with another worker's mint — that's
		// astronomically unlikely with 128 bits of entropy, but the
		// extra check is free in code and clarifies intent.
		if t.Claim == nil || t.Claim.Token != token || t.Claim.Owner != worker {
			return ErrTokenMismatch
		}

		// (3) Lease freshness. A token whose lease has expired is
		// equivalent to no token; the worker has implicitly forfeited.
		// We use Before (strict) rather than !After so that the exact
		// LeasedUntil instant is treated as "still valid" — the lease
		// is a half-open interval [claim_time, LeasedUntil).
		if !time.Now().Before(t.Claim.LeasedUntil) {
			return ErrLeaseStillValid
		}

		now := time.Now()
		t.Status = toStatus
		t.Version++
		t.UpdatedAt = now

		// Terminal states clear the claim. A done task has no owner
		// any more; a failed task is up for re-claim by a retry layer
		// that may or may not exist (s10's watchdog will, eventually,
		// be that layer). Non-terminal transitions preserve the claim
		// because the worker continues to hold the lease.
		if toStatus == "done" || toStatus == "failed" {
			t.Claim = nil
			t.Owner = ""
		}

		return s.Write(team, t)
	})
}

// RenewClaim extends the lease by LeaseDuration starting from now.
// The token is required and must match — a worker cannot extend a
// claim it does not hold. Used by long-running workers to signal
// "still alive" without changing the task's status.
//
// Not currently exercised by main.go (the demo finishes inside one
// LeaseDuration window) but tested implicitly through the
// "successful claim" path. A future s10 watchdog will call this on a
// 5-minute timer to keep claims fresh.
func (s *Store) RenewClaim(team, taskID, worker, token string) error {
	return withFileLock(s.taskPath(team, taskID), func() error {
		t, err := s.Read(team, taskID)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return ErrTaskNotFound
			}
			return err
		}

		if t.Claim == nil || t.Claim.Token != token || t.Claim.Owner != worker {
			return ErrTokenMismatch
		}

		// Renewing an expired lease is *allowed* here — it is the
		// only legitimate way for a worker to recover from a brief
		// pause that pushed it past LeasedUntil without any other
		// worker stealing the task. The protective check is the
		// token equality above: a stolen task already has a different
		// token on disk, so the original worker's renew will fail
		// with ErrTokenMismatch.
		now := time.Now()
		t.Claim.LeasedUntil = now.Add(LeaseDuration)
		t.Version++
		t.UpdatedAt = now

		return s.Write(team, t)
	})
}
