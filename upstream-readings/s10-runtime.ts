// Source: https://raw.githubusercontent.com/Yeachan-Heo/oh-my-claudecode/main/src/team/runtime.ts
// Lines:  L289-L390 (startTeam), L466-L580 (watchdogCliWorkers), L922-L990 (resumeTeam).
// License: MIT, Copyright (c) 2025 Yeachan Heo
//
// Annotated excerpt for s10. The full file is 1,034 lines: tmux session
// setup, pane management, MCP comm, retry policy, heartbeat plumbing,
// crash markers, resume logic. The Go port reuses the BEHAVIOR but
// none of the IMPLEMENTATION — channels and goroutines replace tmux
// and `done.json`, collapsing ~700 lines of orchestration into ~810.

import * as tmux from './tmux-session.js';

// ─── (1) startTeam (L289-L390) — pool initialization ───
//
// Upstream sequence: validate CLI binaries → write task files
// (status=pending) → open tmux session, attach leader pane → spawn
// initial worker panes via `spawnWorkerForTask` → start a 1-second
// `setInterval(watchdogCliWorkers)`. The Go port (pool.go's New +
// Run) collapses all five steps: constructor + N goroutines + 1
// watchdog goroutine. The tmux session vanishes; the leader pane is
// the Pool struct itself.

export async function startTeam(opts: StartTeamOpts): Promise<TeamRuntime> {
  const session = await tmux.createSession(opts.sessionName, opts.cwd);
  const activeWorkers = new Map<string, ActiveWorkerState>();
  for (let i = 0; i < opts.config.maxConcurrentWorkers; i++) {
    await spawnWorkerForTask(`worker-${i}`, opts.teamName, session.firstPaneId,
                             opts.cwd, activeWorkers, /*...*/);
  }
  const stopWatchdog = setInterval(() => watchdogCliWorkers(runtime), 1000);
  return { teamName, sessionName, ...activeWorkers, stopWatchdog };
}

// ─── (2) watchdogCliWorkers (L466-L580) — the loop the Go port subsumes ───
//
// THIS is the function s10 most clearly improves on. Three sub-loops
// per tick: done.json poll, dead-pane detection, heartbeat stale.
// The Go port deletes (a) entirely (channels), folds (b) into
// defer-recover inside workerLoop, and keeps (c) as watchdog.go.

async function watchdogCliWorkers(runtime: TeamRuntime): Promise<void> {
  // (a) done.json poll. ~30 lines upstream. Deleted in Go: workers
  // send WorkerResult on a channel; the Pool's main loop selects.
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

  // (b) dead-pane detection via isWorkerAlive(paneId). Replaced in
  // Go by defer-recover in workerLoop.runTask: a panic becomes a
  // WorkerResult{Err}, and handleResult re-queues with retries++.
  for (const [name, state] of runtime.activeWorkers) {
    if (!await isWorkerAlive(state.paneId)) {
      await applyDeadPaneTransition(runtime, name, state);
      await spawnNextPendingTask(runtime, name);
    }
  }

  // (c) heartbeat stale check. The Go watchdog.go mirrors this:
  // gap > 60s → strike++; strike >= 3 → cancel context, respawn.
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

// ─── (3) resumeTeam (L922-L990) — recovery after process restart ───
//
// Upstream: read config → verify tmux session lives → list panes →
// scan task files for in_progress + map to panes → return runtime.
// Go's Resume (resume.go) does ~25 LOC of step 4 only: list files,
// re-queue pending/orphaned, return Pool. No tmux session, no panes
// to enumerate, no lookup table.

export async function resumeTeam(teamName: string, cwd: string): Promise<TeamRuntime | null> {
  const config = await readTeamConfig(teamName, cwd);
  if (!config || !await tmux.hasSession(config.sessionName)) return null;

  const paneIds = await tmux.listPanes(config.sessionName);
  const activeWorkers = new Map<string, ActiveWorkerState>();
  for (const task of await listTasks(teamName, cwd)) {
    if (task.status === 'in_progress' && task.owner) {
      const paneId = await lookupWorkerPane(task.owner, paneIds.slice(1));
      if (paneId) activeWorkers.set(task.owner, { taskId: task.id, paneId });
    }
  }
  return { teamName, sessionName: config.sessionName, activeWorkers /*...*/ };
}
