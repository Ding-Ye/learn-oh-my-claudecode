# s04 — Layered Config & deepMerge / 分层配置与深合并

> Fourth chapter of `learn-oh-my-claudecode`. We pivot from string
> transforms (s03) to a typed configuration loader: a four-layer merge
> (defaults → user → project → env) over `map[string]any`, with a
> security guard against prototype-pollution that the upstream
> JavaScript port treats as load-bearing.

## Scope (one line)

A `Load(workingDir string) (Config, error)` that layers
`DefaultConfig()`, `~/.config/claude-omc/config.json`,
`<workingDir>/.claude/omc.json`, and `OMC_MODEL_*` / `OMC_DISABLE_TOOLS`
env vars, ported from upstream `src/config/loader.ts` L1–L80 plus
`src/agents/utils.ts` L367–L393 (the `deepMerge` with reserved-key
guard) into ~250 Go lines, stdlib only.

## Files

| Path | Role |
|---|---|
| `config.go` | `Config / AgentRef / Features / MCPRef` — the JSON shape. |
| `defaults.go` | `DefaultConfig() Config` — 19 agents over three tiers, plus tier fallback constants used by both `defaults.go` and `env.go`. |
| `merge.go` | `deepMerge(dst, src map[string]any) map[string]any` — recursive merge with the `__proto__ / constructor / prototype` skip. |
| `loader.go` | `Load`, `readLayer`, `configToMap`, `mapToConfig` — the four-stage layering and the strict `encoding/json` parser. |
| `env.go` | `applyEnvOverlay(*Config)` — final stage, rewrites tier-default agent models from `OMC_MODEL_*`. |
| `main.go` | Demo: defaults → +user → +project → +env, four `probe()` lines per stage. |
| `loader_test.go` | Five tests: defaults, user/project precedence, env override, malformed JSON. |
| `merge_test.go` | Three tests: prototype-pollution guard, nested recursion, nil-src preserves dst. |
| `testdata/{defaults,user,project}.json` | Sample layers used by the demo. |
| `testdata/expected.txt` | Captured `go run .` stdout for drift detection. |
| `go.mod` | `go 1.21`, stdlib only. |

## Run

```bash
cd agents/s04-config-loader

GOWORK=off go vet ./...                # silent
GOWORK=off go build ./...              # silent
GOWORK=off go test -v -count=1 ./...   # 8 tests pass
GOWORK=off go run .                    # output matches testdata/expected.txt
```

## Three teaching points

1. **Layered merge as a list, not a tree.** Each layer is a
   `map[string]any`; merging is just a fold over the list with
   `deepMerge` as the reducer. This is much easier to reason about than
   a recursive struct merge — you can drop a fifth layer in one line by
   appending to the slice in `Load`.
2. **Prototype pollution is a *boundary* concern.** Go has no prototype
   chain, so `__proto__` cannot taint the runtime. The guard is still
   there because configs cross language boundaries (shell → JSON → JS),
   and a Go-side strip means downstream JS consumers don't have to
   re-validate. Defense-in-depth without performance cost.
3. **Strict JSON beats JSONC for teaching.** Upstream uses JSONC so
   users can comment their config files. The teaching version requires
   strict JSON: `encoding/json` is stdlib, JSONC is a 2 KLOC dependency,
   and comments belong in README docs, not config files.

## Upstream lineage

- `src/config/loader.ts` L1–L80 — the `buildDefaultConfig` shape and
  the documented "user → project → env" order.
- `src/agents/utils.ts` L367–L393 — the `deepMerge` function. **L376**
  is the security-critical line we mirror in `merge.go::reservedKeys`.
- See `upstream-readings/s04-loader.ts` for the annotated excerpt.
