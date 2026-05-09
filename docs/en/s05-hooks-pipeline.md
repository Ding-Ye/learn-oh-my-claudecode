---
title: "s05 · Hooks Pipeline via os/exec"
chapter: 5
slug: s05-hooks-pipeline
est_read_min: 10
---

# Chapter 5 — Hooks Pipeline via os/exec

> Fifth chapter of `learn-oh-my-claudecode`. We pivot from the typed
> struct world of s04 to **process management**: a JSON manifest of
> lifecycle hooks, each one shelled out via `os/exec`, with per-hook
> timeouts via `context.WithTimeout`, payload piped on stdin, and
> per-hook errors collected without aborting siblings. **First time the
> chapter series shells out**.

## Problem

Claude Code's lifecycle has ten or so events: `UserPromptSubmit`,
`SessionStart`, `PreToolUse`, `PostToolUse`, `PermissionRequest`, `Stop`,
`SessionEnd`, and friends. For each event, OMC wants to run a *small*
program that inspects the event payload and side-effects something —
inject a skill reminder, log a project-memory snapshot, refuse a
dangerous Bash command. There can be *more than one* program per event,
and a slow program must NOT block the others or hang the whole agent.

That's three sub-problems hiding inside one feature:

1. **Declarative wiring.** The set of hooks per event is data
   (`hooks/hooks.json`), not code. A user (or a plugin) must be able to
   add a new event handler by editing a JSON file — no recompile.
2. **Per-hook timeout.** A buggy script that loops forever cannot wedge
   the dispatcher. Each hook gets its own time budget (3–60s upstream;
   we use 1s in the timeout test so the suite stays fast).
3. **Sibling isolation.** When the third hook of five times out, hooks
   #1, #2, #4, and #5 must still produce results. The caller decides
   what to do with the failure — we do not raise `panic` halfway through.

Upstream solves this in `hooks/hooks.json` L1–L212 (the manifest) plus a
TypeScript dispatcher that uses `child_process.spawn` and
`Promise.allSettled`. This chapter ports the manifest verbatim and
re-implements the dispatcher in ~260 stdlib-only Go lines.

## Solution

Three files compose the public surface:

- `hook.go` — `Hook / Entry / Manifest` plus a custom `UnmarshalJSON`
  that converts the on-disk `"timeout": 5` (seconds, int) into
  `time.Duration`.
- `dispatcher.go` — `Dispatcher.Dispatch(ctx, event, payload) []Result`,
  the matcher predicate, and the per-hook `runHook` helper. Every hook
  runs in its own process group; on timeout we SIGKILL `-pgid` so any
  descendant `sleep` dies with the leader.
- `main.go` — the demo: load `testdata/hooks.json`, fire two
  `UserPromptSubmit` payloads (one quiet, one mentioning `boulder` to
  trigger the 1-second-timeout entry).

Each `Result` carries `Event / Matcher / Command / ExitCode / Stdout /
Stderr / Err`. We never throw; we return a `[]Result` per `Dispatch`
call, and `Err` is always carried per row.

## How It Works

### Pipeline at a glance

```
  Dispatch(ctx, "UserPromptSubmit", payload)
        │
        ▼  Manifest["UserPromptSubmit"] → []Entry
        │
        ▼  for each Entry: matches(entry.Matcher, payload) ? continue
        │
        ▼  for each Hook in Entry.Hooks:
        │       hookCtx, _ := context.WithTimeout(ctx, hook.Timeout)
        │       cmd := exec.CommandContext(hookCtx, "sh", "-c", hook.Command)
        │       cmd.Setpgid + cmd.Cancel = SIGKILL(-pgid) + cmd.WaitDelay
        │       cmd.Stdin = payload  ;  capture stdout/stderr  ;  cmd.Run()
        │       package outcome → Result
        │
        ▼  []Result (one per fired hook, in declaration order)
```

The three knobs in the third box — `Setpgid`, `Cancel`, `WaitDelay` —
are the chapter's standout systems lesson; `## How It Works` zooms in on
them next.

### The process-group trick

`exec.CommandContext` only kills the immediate child. So a manifest
entry like `sh testdata/scripts/sleep_too_long.sh` becomes a `sh -c "sh
script"` invocation: the outer `sh` forks an inner `sh`, which forks
`sleep`. Killing the outer leaves the inner two as orphans, and Go's
`cmd.Wait` blocks on the *inherited* stdout pipe — so a 1-second
context-timeout test takes 30 wall-clock seconds.

Three lines fix it:

```go
cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
cmd.Cancel = func() error {
    return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)  // negative pid → group
}
cmd.WaitDelay = 500 * time.Millisecond
```

`Setpgid: true` puts the hook in its own process group whose ID equals
the leader's PID. `Cancel` is the Go 1.20+ override of "what to do when
the context expires" — we send SIGKILL to `-pgid` so the whole subtree
dies together. `WaitDelay` is the safety net: if descendants still hold
the inherited pipes after kill, Go drains them after 500 ms anyway.

### Errors as first-class siblings

```go
results = append(results, d.runHook(ctx, event, entry.Matcher, h, payloadJSON))
// no break, no return on err — siblings keep running
```

`runHook` packages every outcome — success, exit-non-zero, deadline
exceeded, missing binary — into a single `Result` value. The dispatcher
just appends. Callers iterating the slice see each row's `Err` field,
in declaration order, with no `error` aggregation games.

## What Changed (vs. s04)

s04 was a pure-data layer: structs, JSON, deep merge, no I/O beyond
`os.ReadFile`, no concurrency. s05 introduces four things at once:

| Concern | s04 | s05 |
|---|---|---|
| Side effects | none (pure functions) | `os/exec` spawns child processes |
| Failure model | `(Config, error)` | `[]Result` with per-row `Err` |
| Time | n/a | `time.Duration`, `context.WithTimeout`, `WaitDelay` |
| Resource cleanup | n/a | process groups, SIGKILL, pipe drain |

This is the chapter where Go's "errors are values" stops being a slogan
and starts being a *posture*: the dispatcher returns a slice because
some rows in that slice are *expected* to be failures, and that's fine.

## Try It

```bash
cd agents/s05-hooks-pipeline

GOWORK=off go vet ./...                # silent
GOWORK=off go build ./...              # silent
GOWORK=off go test -v -count=1 ./...   # 6 tests pass, ~2s wall-time
GOWORK=off go run .                    # output matches testdata/expected.txt
```

Expected output:

```
== UserPromptSubmit (payload={"prompt":"hello"}) ==
[UserPromptSubmit/*] sh testdata/scripts/echo.sh -> exit=0 stdout="{\"prompt\":\"hello\"}" err=<nil>

== UserPromptSubmit (payload={"prompt":"push the boulder"}) ==
[UserPromptSubmit/*] sh testdata/scripts/echo.sh -> exit=0 stdout="{\"prompt\":\"push the boulder\"}" err=<nil>
[UserPromptSubmit/boulder] sh testdata/scripts/sleep_too_long.sh -> exit=-1 stdout="" err=deadline-exceeded
```

The first payload only matches `"*"`. The second contains `boulder`, so
the third entry fires too — and its `sleep 30` hits the 1-second budget.

Further exercises:

- Replace the `os/exec` shell-out with the 8-line in-process function
  table from the README anti-pattern callout. Notice how every test
  shrinks; notice how nothing exercises process groups anymore.
- Add a `PostToolUseFailure` event to `testdata/hooks.json` with two
  matchers (one `*`, one `Bash`). What changes in the dispatcher? (Answer:
  nothing — `Manifest` is permissive on event names by design.)

## Upstream Source Reading

Excerpt from `hooks/hooks.json` L1–L62 (full annotated copy at
`upstream-readings/s05-hooks.json`):

```jsonc
{
  "description": "OMC orchestration hooks with async capabilities",
  "hooks": {
    "UserPromptSubmit": [               // L4–L19 — single "*" matcher, 2 hooks
      { "matcher": "*",
        "hooks": [
          { "type": "command",
            "command": "node ... keyword-detector.mjs",
            "timeout": 5 },
          { "type": "command",
            "command": "node ... skill-injector.mjs",
            "timeout": 3 }
        ] }
    ],
    "SessionStart": [                   // L21–L62 — three matchers!
      { "matcher": "*",                 //   always fires (3 hooks, 5s each)
        "hooks": [ /* ... */ ] },
      { "matcher": "init",              //   first run only (1 hook, 30s)
        "hooks": [ /* ... */ ] },
      { "matcher": "maintenance",       //   weekly upkeep (1 hook, 60s)
        "hooks": [ /* ... */ ] }
    ]
  }
}
```

Reading notes (Go-port comparisons):

1. **L4–L19 (UserPromptSubmit) → `Manifest["UserPromptSubmit"]`.** The
   shape is "event: [{matcher, hooks: [...]}, ...]" — exactly what `type
   Manifest map[string][]Entry` encodes. Upstream fires the two hooks
   concurrently via `Promise.allSettled`; the Go port walks them serially
   so test assertions see deterministic ordering.
2. **L21–L62 (multi-matcher SessionStart) → `dispatcher.go::matches`.**
   The chapter's hard case. A SessionStart with payload `{"reason":"init"}`
   should fire entries 1 (`*`) and 2 (`init`) but not 3 (`maintenance`).
   The Go port does substring match against the raw payload bytes — close
   enough for teaching; production would parse the payload and extract
   the matcher field properly.
3. **`"timeout": 5` (seconds) → `time.Duration`.** Upstream relies on the
   JS runtime's "number means milliseconds in setTimeout" convention. Go
   has no such convention; without `hook.go::UnmarshalJSON` the field
   would deserialize as 5 *nanoseconds* and every hook would time out
   immediately.
4. **Anti-pattern #5 ("CLI-shelling-out").** Plan §"Anti-patterns" calls
   the upstream's process-per-hook model out — every hook is a Node boot
   (~30–60 ms). The Go port keeps shelling out *anyway*, because the
   teaching point of this chapter is exactly the process-management
   surface. The 8-line in-process alternative lives in the README as a
   side note, not the canonical implementation.
5. **What we leave out.** Eight more event types follow in the upstream
   file (PreToolUse, PermissionRequest, PostToolUse, PostToolUseFailure,
   SubagentStart/Stop, PreCompact, Stop, SessionEnd) — all the same
   shape. `Manifest` is a permissive `map[string][]Entry`, so any new
   event name "just works" without touching the dispatcher.
6. **Why JSONC for the upstream-readings copy.** The upstream file is
   plain JSON; comments are not legal there. Our annotated copy uses
   JSONC purely as the annotation medium. The teaching repo's actual
   fixture (`testdata/hooks.json`) is plain JSON so `encoding/json`
   parses it without a JSONC dependency.
