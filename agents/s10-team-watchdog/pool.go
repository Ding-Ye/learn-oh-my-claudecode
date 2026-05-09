package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Pool is the goroutine-pool replacement for upstream's tmux-backed
// `TeamRuntime` (runtime.ts L42-L57). The fields map to upstream
// concepts, but the implementations differ entirely:
//
//	upstream                              go port
//	─────────────────────────────────     ─────────────────────────────────
//	leaderPaneId, sessionName             (none — the Pool struct itself)
//	workerPaneIds []string                workers map[string]*Worker
//	activeWorkers Map<name, state>        currentTasks map[string]string
//	watchdog poll loop on done.json       watchdog goroutine + done channel
//	stopWatchdog: () => void              cancel context.CancelFunc
//	resumeTeam → list panes               Resume() → Store.List()
//
// This is the chapter's central teaching point: a tmux + filesystem-
// signal architecture (1,034 lines in runtime.ts) collapses into one
// struct + three goroutines per pool by switching to channels +
// goroutines + context cancellation.
//
// Lifecycle:
//
//   1. New(ctx, n, store) constructs the Pool with N worker slots.
//   2. Submit(t) enqueues a task on the buffered channel.
//   3. Run() spawns N worker goroutines + 1 watchdog goroutine and
//      blocks until either ctx is cancelled or all submitted tasks
//      have terminated (done or failed).
//   4. Shutdown(grace) signals stop, waits for in-flight goroutines.
type Pool struct {
	// store is the on-disk task record. Used for status transitions
	// and for Resume() to reconstruct state. Local to s10 — see
	// store.go for why we don't import s09's.
	store *Store

	// n is the configured worker count. The Pool may at any moment
	// have fewer than n live workers (during a respawn window) but
	// never more.
	n int

	// maxRetries is how many times the watchdog will re-queue a task
	// after a worker crash before marking the task failed. Mirrors
	// upstream's `MAX_DEAD_PANE_RETRIES = 3` (runtime.ts L515).
	maxRetries int

	// workers is the registry of live goroutines, keyed by name. The
	// watchdog mutates this map (replacing crashed workers); the Pool
	// reads it during Shutdown.
	workers   map[string]*Worker
	workersMu sync.Mutex

	// tasks is the dispatch channel. Submit() pushes onto it; workers
	// receive from it. Buffered to N*2 so a reasonable burst of
	// Submit calls doesn't block the caller.
	tasks chan Task

	// results is the completion channel. Workers send WorkerResult
	// values here; the Pool's main loop in Run() processes them.
	// Watchdog never reads from this channel.
	results chan WorkerResult

	// beats is the heartbeat map: each worker's last-heartbeat time.
	// The watchdog scans this every tick to detect stalls.
	// beatsMu serializes mutations.
	beats   map[string]*time.Time
	beatsMu sync.Mutex

	// currentTasks tracks the in-flight task per worker so the
	// watchdog can re-queue when it decides to kill a stalled worker.
	// Updated by workerLoop, read by the watchdog.
	currentTasks   map[string]*string
	currentTasksMu sync.Mutex

	// strikes counts consecutive ticks during which each worker has
	// failed the heartbeat check. At UnresponsiveStrikeMax the
	// watchdog respawns the worker. Mirrors upstream's
	// `unresponsiveCount` field on ActiveWorkerState (runtime.ts L70).
	strikes map[string]int

	// pendingCount is the number of tasks Submit() has accepted but
	// the Pool has not yet seen reach a terminal state. Run() returns
	// when this hits zero (or ctx fires). Bumped on Submit, decremented
	// by the result loop on done/failed.
	pendingCount   int
	pendingCountMu sync.Mutex

	// ctx and cancel are the Pool-wide cancellation. cancel() fires
	// during Shutdown and propagates to every worker via their
	// per-worker contexts (which are derived from ctx).
	ctx    context.Context
	cancel context.CancelFunc

	// wg waits for the worker goroutines + watchdog to finish during
	// Shutdown.
	wg sync.WaitGroup

	// finalStatusBus is closed by Run() when its loop exits, so test
	// helpers (and Shutdown) can wait for the result drain to settle.
	doneOnce sync.Once
	doneCh   chan struct{}
}

// Pool defaults — exposed so tests can rebuild a Pool without
// re-encoding magic numbers.
const (
	defaultMaxRetries = 3
)

// New constructs a Pool. The ctx becomes the parent of every worker's
// context, so cancelling it (or the Pool's internal cancel via
// Shutdown) cleanly tears down the entire pool.
func New(ctx context.Context, n int, store *Store) *Pool {
	if n < 1 {
		n = 1
	}
	derivedCtx, cancel := context.WithCancel(ctx)

	return &Pool{
		store:        store,
		n:            n,
		maxRetries:   defaultMaxRetries,
		workers:      make(map[string]*Worker, n),
		tasks:        make(chan Task, n*4),
		results:      make(chan WorkerResult, n*2),
		beats:        make(map[string]*time.Time, n),
		currentTasks: make(map[string]*string, n),
		strikes:      make(map[string]int, n),
		ctx:          derivedCtx,
		cancel:       cancel,
		doneCh:       make(chan struct{}),
	}
}

// SetMaxRetries lets tests override the retry budget without
// constructing a fresh Pool. Default is 3, matching upstream.
func (p *Pool) SetMaxRetries(n int) {
	p.maxRetries = n
}

// Submit enqueues a task. Persists the pending status to disk so a
// process crash here can be recovered by Resume() at next start.
//
// Returns immediately unless the dispatch channel is full (which
// only happens with very high burst rates against a small N — the
// channel is sized for N*4 backlog).
func (p *Pool) Submit(t Task) error {
	if t.ID == "" {
		t.ID = NewTaskID()
	}
	t.Status = "pending"
	t.Retries = 0

	if err := p.store.Write(&t); err != nil {
		return fmt.Errorf("Pool.Submit: persist: %w", err)
	}

	p.pendingCountMu.Lock()
	p.pendingCount++
	p.pendingCountMu.Unlock()

	select {
	case p.tasks <- t:
		return nil
	case <-p.ctx.Done():
		return p.ctx.Err()
	}
}

// Run spawns the worker goroutines and the watchdog, then blocks on
// the result-processing loop until either:
//
//   - ctx is cancelled (Shutdown or external cancel), OR
//   - pendingCount hits zero (every submitted task is terminal).
//
// Returns nil on natural completion, ctx.Err() on cancellation. The
// caller may call Shutdown after Run returns to release any lingering
// internal goroutines (the watchdog).
func (p *Pool) Run() error {
	// Spawn N workers.
	for i := 0; i < p.n; i++ {
		name := fmt.Sprintf("worker-%d", i)
		p.spawnWorker(name)
	}

	// Spawn the watchdog. It runs until ctx is cancelled.
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		watchdog(p.ctx, p, WatchdogTickInterval)
	}()

	// Result-processing loop. Each WorkerResult is the signal that
	// some task finished or failed; we transition the on-disk state
	// and decrement pendingCount accordingly.
	for {
		select {
		case <-p.ctx.Done():
			p.signalDone()
			return p.ctx.Err()

		case res := <-p.results:
			p.handleResult(res)

			// All submitted tasks are terminal — exit. The
			// per-worker goroutines stay alive (waiting on the tasks
			// channel) until Shutdown drains them.
			p.pendingCountMu.Lock()
			done := p.pendingCount == 0
			p.pendingCountMu.Unlock()
			if done {
				p.signalDone()
				return nil
			}
		}
	}
}

// signalDone closes the doneCh exactly once. Test helpers and
// Shutdown can wait on it to observe Run's exit without re-deriving
// state from goroutine introspection.
func (p *Pool) signalDone() {
	p.doneOnce.Do(func() {
		close(p.doneCh)
	})
}

// Done returns a channel that is closed when Run() exits. Mostly
// used by tests; production callers can just use Run's return value.
func (p *Pool) Done() <-chan struct{} {
	return p.doneCh
}

// handleResult applies a worker's WorkerResult to the on-disk task.
// Three branches:
//
//  1. Err == nil: success → status=done, decrement pendingCount.
//  2. Err != nil and Retries < maxRetries: increment Retries,
//     re-queue with status=pending. pendingCount unchanged.
//  3. Err != nil and Retries >= maxRetries: status=failed, decrement
//     pendingCount.
//
// The retry branch is the upstream `applyDeadPaneTransition` shape
// (runtime.ts L515-L545) but expressed in-process: no need to write
// `retry-marker.json`, no need to re-spawn a tmux pane.
func (p *Pool) handleResult(res WorkerResult) {
	t, err := p.store.Read(res.TaskID)
	if err != nil {
		// Should not happen in practice — Submit always persists
		// before queuing. Log the situation by failing the result
		// silently (the watchdog will retry orphaned tasks via
		// Resume's logic if the pool is restarted).
		return
	}

	if res.Err == nil {
		t.Status = "done"
		_ = p.store.Write(t)
		p.pendingCountMu.Lock()
		p.pendingCount--
		p.pendingCountMu.Unlock()
		return
	}

	// Failure path. Bump retries; decide whether to re-queue or fail.
	t.Retries++
	if t.Retries > p.maxRetries {
		t.Status = "failed"
		_ = p.store.Write(t)
		p.pendingCountMu.Lock()
		p.pendingCount--
		p.pendingCountMu.Unlock()
		return
	}

	// Re-queue with the same ID. The status flips back to pending so
	// any reader (or Resume) sees a coherent on-disk picture.
	t.Status = "pending"
	_ = p.store.Write(t)
	requeue := *t
	// Only the *first* failure ID gets the panic flag stripped so
	// retries can succeed; otherwise the test would never make
	// progress. For real CLI workers the panic field is always false
	// to begin with so this branch is a no-op there.
	requeue.Panic = false

	select {
	case p.tasks <- requeue:
	case <-p.ctx.Done():
		// Pool is shutting down; abandon the requeue. Resume() at
		// next start will recover the task because its on-disk
		// status is pending.
	}
}

// spawnWorker creates a goroutine for the given name. Used both at
// Run() startup and from the watchdog when a worker is killed and
// must be replaced. The name is reused across respawns so log output
// stays continuous.
//
// The heap-allocated `beat` and `currentTask` cells are shared
// between the goroutine and the Pool's other locks: the worker
// updates them via beatMu / currentTasksMu, and the watchdog reads
// them via the same mutexes. Sharing pointers (not the maps' values)
// keeps the lock ownership symmetric — both sides traverse the maps
// under the lock to *find* the cell, but the cell itself is the
// stable target.
func (p *Pool) spawnWorker(name string) {
	workerCtx, workerCancel := context.WithCancel(p.ctx)

	// Heap-allocate the per-worker cells so the goroutine can read
	// them without re-traversing the maps (and so the maps' lock
	// only protects insertion / iteration, not the field reads).
	beatPtr := new(time.Time)
	*beatPtr = time.Now()
	currentTaskPtr := new(string)

	p.workersMu.Lock()
	p.workers[name] = &Worker{
		Name:     name,
		LastBeat: *beatPtr,
		cancel:   workerCancel,
	}
	p.workersMu.Unlock()

	p.beatsMu.Lock()
	p.beats[name] = beatPtr
	p.beatsMu.Unlock()

	p.currentTasksMu.Lock()
	p.currentTasks[name] = currentTaskPtr
	p.currentTasksMu.Unlock()

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		workerLoop(
			workerCtx,
			name,
			p.tasks,
			p.results,
			beatPtr,
			&p.beatsMu,
			currentTaskPtr,
			&p.currentTasksMu,
		)
	}()
}

// killWorker is the watchdog's "respawn" primitive. It cancels the
// worker's context (which makes the goroutine exit at its next
// select), then spawns a fresh goroutine under the same name. The
// cancelled worker, on its way out, emits a WorkerResult{Err:ctx.Err()}
// for any task it was running — so handleResult sees the failure
// through the same channel it sees natural completions.
//
// Returns the task ID the worker was running at kill time, or "" if
// idle. Caller uses this for logging only; the actual re-queue is
// driven by handleResult.
//
// Why no synthetic WorkerResult here: the worker goroutine itself
// always emits a result for the task it owned at cancel time (see
// workerLoop's post-runTask send). Synthesizing here in addition
// would cause handleResult to process the same task twice — once
// from the killer's synthetic, once from the worker's natural exit
// — and double-charge retries. The worker's exit signal is the
// canonical event; killWorker just triggers it.
func (p *Pool) killWorker(name string) string {
	p.workersMu.Lock()
	w, ok := p.workers[name]
	p.workersMu.Unlock()
	if !ok {
		return ""
	}

	// Snapshot the current task for the return value (logging only).
	p.currentTasksMu.Lock()
	taskIDPtr := p.currentTasks[name]
	taskID := ""
	if taskIDPtr != nil {
		taskID = *taskIDPtr
	}
	p.currentTasksMu.Unlock()

	// Cancel the goroutine. The worker's runTask will return
	// ctx.Err(); the worker's post-runTask code then sends a
	// WorkerResult into p.results, which handleResult treats as a
	// re-queue trigger.
	w.cancel()

	// Reset strikes so the replacement worker starts fresh.
	p.workersMu.Lock()
	p.strikes[name] = 0
	p.workersMu.Unlock()

	// Spawn replacement under the same name. The fresh goroutine
	// inherits an empty currentTask + new heartbeat.
	p.spawnWorker(name)

	return taskID
}

// Shutdown signals every worker to stop, then waits up to `grace` for
// in-flight tasks to drain. After the grace period the function
// returns even if some goroutines are still running — context
// cancellation will eventually unblock them.
//
// Mirrors upstream's `shutdownTeam` (runtime.ts L850-L920) but in a
// single function: no tmux session to kill, no panes to enumerate, no
// `done.json` cleanup pass. The cancel() + wg.Wait() pair is the
// entire teardown sequence.
func (p *Pool) Shutdown(grace time.Duration) error {
	p.cancel()
	close(p.tasks)

	doneCh := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(doneCh)
	}()

	select {
	case <-doneCh:
		return nil
	case <-time.After(grace):
		return fmt.Errorf("Pool.Shutdown: graceful drain timed out after %s", grace)
	}
}
