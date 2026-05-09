package main

import "time"

// Task is one row in the file-backed task store. Mirrors upstream's
// `TeamTaskV2` shape (src/team/state/tasks.ts L86-L92) with three
// intentional shrinks for teaching scope:
//
//   - We carry only the fields needed for the claim+transition state
//     machine. Upstream TeamTaskV2 also tracks delegation evidence,
//     dependency lists, terminal-data envelopes, etc. Those are runtime
//     concerns layered on top of this core, and they obscure the lesson.
//   - `Owner` is a free-form string (a worker name) instead of a struct.
//     The s09 layer only needs to know "who currently holds the lease"
//     for transition checks; richer worker identity belongs to s10.
//   - `Version` is a plain int. Upstream uses a number, but the storage
//     posture is the same: increment on every successful transition,
//     and let optimistic readers compare-and-set against it.
//
// One Task = one JSON file under
// `<root>/<team>/tasks/<id>.json`. The file is the single source of
// truth; there is no in-memory cache. Every read goes through the
// filesystem, every write is an atomic rename. This is *the* teaching
// point of the chapter.
type Task struct {
	// ID is the canonical task identifier. Forms half of the file
	// path, so the caller is responsible for keeping it filesystem-safe
	// (alphanumeric + dashes is the upstream convention). The store
	// does not validate or sanitize it.
	ID string `json:"id"`

	// Status is the task's lifecycle state. Valid values for the
	// teaching subset:
	//
	//   - "pending"     — not yet claimed; eligible for ClaimTask
	//   - "in_progress" — claimed by a worker, lease active
	//   - "done"        — terminal success
	//   - "failed"      — terminal failure
	//
	// TransitionTask enforces legal transitions. Upstream has a richer
	// machine (blocked, completed, cancelled, …); we strip to four so
	// the chapter focuses on the locking discipline, not the graph.
	Status string `json:"status"`

	// Owner is the worker name that currently owns the task, set
	// alongside Claim during ClaimTask. Cleared (left blank) when the
	// task transitions back to pending. Distinct from Claim.Owner only
	// in defensive-coding terms: both should agree, and TransitionTask
	// double-checks via Claim.Token.
	Owner string `json:"owner,omitempty"`

	// Claim is the optimistic-concurrency token + lease. Nil when the
	// task is pending and no worker has touched it. Non-nil from the
	// moment ClaimTask succeeds until the task either transitions to
	// terminal state or has its claim explicitly cleared.
	Claim *Claim `json:"claim,omitempty"`

	// Version is bumped on every successful mutating operation. A
	// caller can read a Task, do some logic off-store, then call a
	// transition expecting the version to still match — the store will
	// reject the write if anyone else has bumped it. The teaching
	// version of TransitionTask does NOT yet expose this expected-
	// version check (claim-token is enough), but the field is present
	// so a future exercise can layer it on without a schema change.
	Version int `json:"version"`

	// Description is human-readable context for the task; carried
	// purely so the demo and tests have something to print. Not
	// inspected by any state-machine logic.
	Description string `json:"description"`

	// CreatedAt is set once when the task is first written.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is rewritten on every store mutation.
	UpdatedAt time.Time `json:"updated_at"`
}

// Claim is the optimistic-concurrency record attached to an
// in-progress task. Mirrors upstream's `claim` field on TeamTaskV2
// (src/team/state/tasks.ts L90):
//
//	claim: { owner, token, leased_until }
//
// Three fields, all required when the struct exists:
//
//   - Token is a freshly minted UUID. Anyone wishing to mutate the
//     task must present this exact token; the only legal way to obtain
//     one is to call ClaimTask while the task is pending (or while a
//     prior lease has expired).
//   - Owner is the worker name. Redundant with Task.Owner; both must
//     agree. The duplication is intentional: it lets a stale on-disk
//     task be sanity-checked without trusting either field alone.
//   - LeasedUntil is now+LeaseDuration at claim time. After this
//     instant the lease is considered dead and a new worker may steal
//     the task. The store reads this field on every mutating operation;
//     callers do NOT need to renew leases for short tasks (< 15 min).
type Claim struct {
	Token       string    `json:"token"`
	Owner       string    `json:"owner"`
	LeasedUntil time.Time `json:"leased_until"`
}

// LeaseDuration is the lifetime of a claim from the moment it is
// minted. Mirrors upstream's hardcoded 15-minute window
// (src/team/state/tasks.ts L90: `15 * 60 * 1000` ms). The constant is
// exported so a test can construct a deliberately-expired Claim
// without re-encoding the magic number.
//
// Why fifteen minutes specifically: the upstream worker spawn protocol
// writes a heartbeat once per second, so a 15-minute lease covers
// ~900 missed heartbeats — generous enough that a stalled-but-alive
// worker isn't preempted, yet short enough that a crashed worker's
// task is recoverable in a coffee break.
const LeaseDuration = 15 * time.Minute
