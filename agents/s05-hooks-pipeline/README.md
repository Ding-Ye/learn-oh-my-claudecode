# s05 — Hooks Pipeline / 钩子流水线

> Fifth chapter of `learn-oh-my-claudecode`. We pivot from typed structs
> and JSON merging (s04) to **process management**: a JSON manifest of
> lifecycle hooks, each one shelled out via `os/exec` with its own
> `context.WithTimeout` budget, payload piped over stdin, and per-hook
> errors collected without aborting siblings. **First time the chapter
> series shells out**.

## Scope (one line)

A `Dispatcher.Dispatch(ctx, event, payload) []Result` that reads
`hooks.json`, runs each accepting hook via `sh -c`, enforces per-hook
timeouts via `context.WithTimeout` plus a process-group SIGKILL, and
returns one `Result` per hook (with `Err` carried inline, never
aborting siblings) — ported from upstream `hooks/hooks.json` L1–L212
into ~260 Go lines, stdlib only.

## Files

| Path | Role |
|---|---|
| `hook.go` | `Hook / Entry / Manifest` plus the custom `UnmarshalJSON` that converts on-disk `"timeout": 5` (seconds, int) into `time.Duration`. |
| `dispatcher.go` | `Dispatcher`, `Dispatch`, `runHook`, `matches`. Every hook runs in its own process group; `cmd.Cancel` SIGKILLs `-pgid` on timeout so descendant `sleep`s die with the leader. |
| `main.go` | Loads `testdata/hooks.json`, fires `UserPromptSubmit` for two payloads (one quiet, one with `boulder` to trigger the 1-second-timeout entry). |
| `dispatcher_test.go` | Six tests: happy path, matcher skip, timeout, stdin payload, sibling isolation, plus a parser sanity test. |
| `testdata/hooks.json` | Three matchers + mixed timeouts in the upstream shape. |
| `testdata/scripts/echo.sh` | `cat` — copies stdin to stdout. |
| `testdata/scripts/sleep_too_long.sh` | `sleep 30` — exercises the timeout path. |
| `testdata/expected.txt` | Captured `go run .` stdout for drift detection. |
| `go.mod` | `go 1.21`, stdlib only. |

## Run

```bash
cd agents/s05-hooks-pipeline

GOWORK=off go vet ./...                # silent
GOWORK=off go build ./...              # silent
GOWORK=off go test -v -count=1 ./...   # 6 tests pass, ~2s wall-time
GOWORK=off go run .                    # matches testdata/expected.txt
```

## Three teaching points

1. **`exec.CommandContext` alone leaks descendants.** A `sh -c "sh
   inner.sh"` hook fork-fork-execs `sleep`; killing only the immediate
   `sh` orphans the rest and `cmd.Wait` blocks on inherited pipes. Fix:
   `Setpgid: true` + `cmd.Cancel = SIGKILL(-pgid)` + `WaitDelay`.
2. **Errors are values, even mid-batch.** Upstream uses
   `Promise.allSettled` — flapping hooks cannot block clean ones. The
   Go port returns `[]Result` with `Err` carried per row.
3. **On-disk seconds → in-process `time.Duration`.** A six-line custom
   `UnmarshalJSON` keeps the JSON file stable and the runtime API
   ergonomic. Drop it and `5` deserializes as 5 nanoseconds.

## Anti-pattern callout

Plan §"Anti-pattern #5" reads "CLI-shelling-out for hooks." We
deliberately shell out anyway because the *teaching point* is exactly
the process-management surface: timeouts, stdin, error collection,
process-group kill. The pure-Go alternative is short:

```go
// 8-line in-process alternative — side-note only.
type HookFn func(ctx context.Context, payload []byte) Result
type Registry map[string][]HookFn
func (r Registry) Dispatch(ctx context.Context, event string, payload []byte) []Result {
    var out []Result
    for _, fn := range r[event] { out = append(out, fn(ctx, payload)) }
    return out
}
```

No `sh -c`, no process-group dance. Keep it for the day you own both
the manifest and the runtime.

## Upstream lineage

- `hooks/hooks.json` L1–L212 — the declarative manifest.
- See `upstream-readings/s05-hooks.json` for the annotated excerpt.
