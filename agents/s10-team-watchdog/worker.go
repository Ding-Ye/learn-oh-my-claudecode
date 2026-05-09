package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// WorkerResult is what a worker writes back to the Pool when a task
// finishes (or fails). Mirrors upstream's `done.json` payload from
// runtime.ts L488-L499 — there the worker writes `{taskId, exitCode,
// error}` to disk and the watchdog polls; here we send a struct over
// a channel and the Pool's main loop selects on it.
//
// The two-line collapse:
//
//	upstream:  fs.writeFile("done.json", {taskId, error}); ... watchdog reads
//	go ver.:   done <- WorkerResult{Worker, TaskID, Err}
//
// Channels eliminate the file-system round-trip, the directory polling,
// and the `done.json` cleanup step that the upstream's `unlinkSignal`
// path has to manage.
type WorkerResult struct {
	// Worker is the human-readable name of the worker that produced
	// this result (e.g. "worker-0"). The Pool uses this to look up
	// the Worker struct so it can clean up the goroutine's context
	// cancel function.
	Worker string

	// TaskID identifies which task finished. The Pool uses this to
	// transition the task's on-disk status from in_progress to done
	// (or failed) and to decide whether to re-queue.
	TaskID string

	// Err is non-nil if the worker died, panicked, or was cancelled.
	// nil means the task completed normally. The watchdog inspects
	// this to decide between "transition to done" (Err == nil) and
	// "increment Retries, re-queue or fail" (Err != nil).
	Err error
}

// Worker is the in-memory record the Pool keeps for each goroutine.
// Compare with upstream's `ActiveWorkerState` (runtime.ts L60-L72)
// which carries `paneId`, `taskId`, `pid`, `lastBeat`, `unresponsiveCount`.
// The Go shape drops paneId / pid (we have neither tmux nor child
// process) and folds `lastBeat` into a separate map on the Pool so the
// watchdog can take its lock without needing every Worker pointer.
type Worker struct {
	// Name is the human-readable label, e.g. "worker-0". Stable
	// across respawns: when the watchdog kills worker-0 and creates
	// a fresh goroutine, the new goroutine adopts the same name so
	// log output stays continuous.
	Name string

	// LastBeat is when the worker most recently signaled liveness.
	// Updated INSIDE workerLoop (under beatMu) so the watchdog can
	// read it racelessly. A zero LastBeat means "no heartbeat yet"
	// and the watchdog grants a one-tick grace period before
	// considering the worker stalled.
	LastBeat time.Time

	// cancel is the per-worker context cancel function. The Pool calls
	// this to terminate the goroutine — either during Shutdown or when
	// the watchdog decides to respawn a stalled worker. After cancel
	// the worker exits at the next select; pending tasks held by the
	// channel are unaffected (they simply route to a different worker).
	cancel context.CancelFunc

	// CurrentTask is the ID of the task this worker is presently
	// running, or "" if idle. The watchdog reads this so that when it
	// kills a stalled worker, it knows which task to re-queue.
	CurrentTask string
}

// workerLoop is the goroutine each worker runs. It selects on the
// shared tasks channel, runs the task by sleeping for WorkSeconds
// (with optional panic simulation), updates the heartbeat, and reports
// the result on the done channel.
//
// The contract:
//
//   - ctx cancellation: returns immediately. No pending result is sent
//     for the in-flight task; the watchdog/respawn logic in pool.go
//     re-queues based on the worker's CurrentTask field.
//   - panic recovery: a defer-recover loop converts panics into a
//     WorkerResult{Err: ...}. This is the "simulated crash" path; in
//     production the same recovery handles real CLI agent panics.
//   - heartbeat cadence: written before each task pickup AND every
//     250ms during a long sleep, so the watchdog's 60-second
//     stale threshold is never tripped by a long-but-healthy worker.
//
// Mirrors upstream `spawnWorkerForTask` (runtime.ts L582-L770) plus
// the heartbeat plumbing from `writeWorkerHeartbeat` (L420-L465). The
// heartbeat-on-disk file becomes a struct field on Worker; the tmux
// pane becomes the goroutine itself.
func workerLoop(
	ctx context.Context,
	name string,
	tasks <-chan Task,
	done chan<- WorkerResult,
	beat *time.Time,
	beatMu *sync.Mutex,
	currentTask *string,
	currentTaskMu *sync.Mutex,
) {
	// Heartbeat once at startup so the watchdog doesn't immediately
	// flag a brand-new worker as stalled.
	updateBeat(beat, beatMu)

	for {
		select {
		case <-ctx.Done():
			// The cancel signal fired. We exit without sending a
			// result — the Pool's respawn logic uses CurrentTask to
			// re-queue any in-flight task. This is the only branch
			// where the worker leaves a task "in_progress" on disk;
			// the watchdog corrects that next tick.
			return

		case t, ok := <-tasks:
			if !ok {
				// Channel closed → all submissions are done. Exit
				// cleanly; the Pool will collect this via wg.Done.
				return
			}

			// Record the task we're now running. The watchdog reads
			// this when it decides to kill us so the right task gets
			// re-queued.
			currentTaskMu.Lock()
			*currentTask = t.ID
			currentTaskMu.Unlock()

			updateBeat(beat, beatMu)
			err := runTask(ctx, t, beat, beatMu)

			// Clear CurrentTask BEFORE sending the result. The Pool's
			// main loop will read this immediately after dequeuing the
			// result; we must not leave a stale ID for the watchdog
			// to act on.
			currentTaskMu.Lock()
			*currentTask = ""
			currentTaskMu.Unlock()

			updateBeat(beat, beatMu)

			// Send the result. We DELIBERATELY do not race this
			// against ctx.Done — the result must reach the Pool so
			// handleResult can mark the task done (or re-queue it).
			// The done channel is buffered to N*2 by the Pool; a
			// worker that fills it has wedged the entire pool, which
			// is a bug at a higher layer.
			//
			// The one degenerate case: if the Pool's Shutdown closed
			// the results channel before us, this would panic. The
			// Pool deliberately does NOT close results — only the
			// tasks channel — so this send is safe even during
			// shutdown.
			done <- WorkerResult{Worker: name, TaskID: t.ID, Err: err}
		}
	}
}

// runTask is the actual "do the work" step. In the teaching version it
// is a sleep of duration t.WorkSeconds; if t.Panic is set it panics
// half-way through so we can exercise the watchdog's respawn path.
//
// A panic is recovered and converted to an error so the Pool's main
// loop never has to handle a goroutine death. This is the "wrap the
// CLI in a recover" idiom — production code would call exec.Cmd here
// and treat any non-zero exit code as the same kind of error.
func runTask(ctx context.Context, t Task, beat *time.Time, beatMu *sync.Mutex) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("worker panic: %v", r)
		}
	}()

	// Heartbeat tick during long-running work. We loop in 100ms
	// chunks so the watchdog sees a fresh beat well before its 60s
	// stale threshold. The chunk count is ceil(work*10) so a
	// WorkSeconds=0.05 task still gets at least one chunk.
	totalDuration := time.Duration(t.WorkSeconds * float64(time.Second))
	chunkSize := 100 * time.Millisecond
	chunks := int(totalDuration / chunkSize)
	if chunks < 1 {
		chunks = 1
	}
	chunkDuration := totalDuration / time.Duration(chunks)
	if chunkDuration <= 0 {
		chunkDuration = time.Millisecond
	}

	for i := 0; i < chunks; i++ {
		// Inject the panic at the half-way mark so the test fixture
		// has a stable failure point: a panic at the start would
		// barely exercise the heartbeat path.
		if t.Panic && i == chunks/2 {
			panic("simulated worker crash")
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(chunkDuration):
			updateBeat(beat, beatMu)
		}
	}

	// Edge case: if t.Panic is set with WorkSeconds=0 (one chunk), the
	// half-way index was 0, so the loop above already panicked. If
	// Panic is set and chunks > 0 but the half-way slot is past the
	// end of the loop somehow, fall through to a deterministic panic.
	if t.Panic {
		panic("simulated worker crash (post-loop)")
	}

	return nil
}

// updateBeat records the current wall-clock time as the worker's last
// heartbeat. Called from workerLoop and runTask; the watchdog reads
// the same map to detect stalls.
func updateBeat(beat *time.Time, mu *sync.Mutex) {
	mu.Lock()
	*beat = time.Now()
	mu.Unlock()
}
