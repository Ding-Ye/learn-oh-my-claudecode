package main

import (
	"context"
	"time"
)

// Watchdog tunables. Promoted from magic numbers so tests can reach
// for them and so a future operator can tune without recompiling
// internal logic.
//
// HeartbeatStaleThreshold mirrors upstream's `60_000` ms constant
// (runtime.ts L530). UnresponsiveStrikeMax mirrors the "kill after 3
// consecutive checks" rule (runtime.ts L538-L545). WatchdogTickInterval
// mirrors the `setInterval(... , 1000)` from `startTeam` (runtime.ts
// L369).
const (
	// HeartbeatStaleThreshold is the maximum permitted gap between
	// successive heartbeats. A worker whose last heartbeat is older
	// than this is considered stalled and accumulates a strike on
	// each subsequent tick.
	HeartbeatStaleThreshold = 60 * time.Second

	// UnresponsiveStrikeMax is how many consecutive stalled ticks a
	// worker survives before the watchdog kills + respawns it. Three
	// matches upstream; the value is a tradeoff between false
	// positives (network blips) and recovery latency.
	UnresponsiveStrikeMax = 3

	// WatchdogTickInterval is how often the watchdog runs its checks.
	// One second matches upstream and is short enough that recovery
	// latency stays under (HeartbeatStaleThreshold +
	// UnresponsiveStrikeMax * WatchdogTickInterval) ≈ 63 seconds.
	WatchdogTickInterval = 1 * time.Second
)

// watchdog is the goroutine that watches every worker's heartbeat.
// Each tick:
//
//  1. Snapshot the current set of worker names. The set can change
//     under us (a respawn during the previous tick) so we re-read
//     each tick rather than caching.
//  2. For each worker, compute time-since-last-beat. If it exceeds
//     HeartbeatStaleThreshold, increment the strike count; otherwise
//     reset to zero.
//  3. If any worker's strikes reach UnresponsiveStrikeMax, kill +
//     respawn it via Pool.killWorker. The pool handles re-queuing the
//     in-flight task; we just trigger the respawn.
//
// The function exits when ctx is cancelled, which happens when the
// Pool's Shutdown calls cancel() on the parent context.
//
// Mirrors upstream `watchdogCliWorkers` (runtime.ts L466-L580). The
// Go version drops three pieces:
//
//   - Done-signal polling. Workers send WorkerResult over a channel,
//     so there is no `done.json` file to scan. This shrinks ~30 lines
//     of upstream into zero.
//   - Dead-pane detection via `isWorkerAlive(paneId)`. Goroutines
//     don't have PIDs to check; if a goroutine panics, the worker
//     loop's defer-recover converts it to a result (with Err) which
//     the Pool's main loop processes. The watchdog only handles the
//     "alive but stuck" case.
//   - `watchdog-failed.json` fallback. Three consecutive watchdog
//     errors → upstream writes a marker file and stops. Here the
//     watchdog has no errors to count: it reads in-memory state, and
//     the only failure mode is a deadlocked Pool (which would block
//     on the lock and is caught by Shutdown's grace timeout).
func watchdog(ctx context.Context, pool *Pool, tick time.Duration) {
	if tick <= 0 {
		tick = WatchdogTickInterval
	}

	t := time.NewTicker(tick)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			watchdogTick(pool)
		}
	}
}

// watchdogTick is one iteration of the watchdog's main loop, broken
// out so tests can drive it deterministically without sleeping for a
// real second.
func watchdogTick(pool *Pool) {
	now := time.Now()

	// Snapshot worker names + their heartbeats. We hold both locks
	// briefly to get a consistent view, then release them so the
	// kill+respawn path doesn't deadlock against worker goroutines
	// trying to update their own beat.
	pool.workersMu.Lock()
	names := make([]string, 0, len(pool.workers))
	for n := range pool.workers {
		names = append(names, n)
	}
	pool.workersMu.Unlock()

	type beatSnapshot struct {
		name string
		last time.Time
	}
	snapshots := make([]beatSnapshot, 0, len(names))

	pool.beatsMu.Lock()
	for _, n := range names {
		beat, ok := pool.beats[n]
		if !ok || beat == nil {
			continue
		}
		snapshots = append(snapshots, beatSnapshot{name: n, last: *beat})
	}
	pool.beatsMu.Unlock()

	// Decide who to kill. We collect the names first so we can call
	// killWorker outside any lock.
	var toKill []string

	pool.workersMu.Lock()
	for _, s := range snapshots {
		gap := now.Sub(s.last)
		if gap > HeartbeatStaleThreshold {
			pool.strikes[s.name]++
			if pool.strikes[s.name] >= UnresponsiveStrikeMax {
				toKill = append(toKill, s.name)
			}
		} else {
			// Reset on healthy ticks. A flaky worker that recovers
			// gets its strike count cleared so future stalls start
			// from zero.
			if pool.strikes[s.name] != 0 {
				pool.strikes[s.name] = 0
			}
		}
	}
	pool.workersMu.Unlock()

	for _, name := range toKill {
		_ = pool.killWorker(name)
	}
}
