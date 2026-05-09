# s01 — Agent Registry & Model Tiers / 智能体注册表与模型分层

> First chapter of `learn-oh-my-claudecode`. Establishes the `Agent` shape and
> the four-fold model-tier resolution chain that subsequent chapters reuse.

## Scope (一句话)

A typed registry (`map[string]Agent`) plus a five-input priority chain
(`override → envInherit → configured → agent.Model → agent.DefaultModel`)
that resolves which model handles which named role. Pure data, no I/O.

## Files

| Path | Role |
|---|---|
| `agent.go` | The `Agent` struct — canonical shape inherited (in subsets) by every later chapter. |
| `registry.go` | `Registry` type (`New`, `Register`, `Get`, `Names`) and the `ResolveModel` priority-chain helper. |
| `main.go` | 30-line demo: register two agents, look up, exercise overrides, print sorted names. |
| `registry_test.go` | Five table-driven tests: get-roundtrip, missing-key, override-precedence, fallback-chain, duplicate-overwrite. |
| `testdata/expected.txt` | Captured stdout of `go run .` — used as a fixture for the doc's "Try It" section. |
| `go.mod` | Module declaration. Stdlib only — zero external deps for s01. |

## Run

```bash
go run .              # prints the four-line demo
go test -v ./...      # all 5 tests pass (one is table-driven with 5 sub-cases)
go vet ./...          # silent — no warnings
```

Expected `go run .` output:

```
architect found=true model=claude-opus-4-7
with override: claude-haiku-4-5
without override: claude-opus-4-7
registered: [architect executor]
```

## Three teaching points

1. **The `Agent` shape is the contract.** Subsequent chapters import none of
   this code (each chapter is independently buildable), but they reuse a
   subset of `Agent`'s fields conceptually: s02 uses `Name + Prompt`, s04
   uses `Name + Model`, s10 uses `Name + Model`. Lock the shape here once.
2. **Model tier is resolved at lookup time, not at register time.** Upstream
   `definitions.ts` L289-L298 recomputes the tier on every `getAgentDefinitions`
   call, allowing per-call overrides without mutating the stored agent. We
   follow the same discipline: `ResolveModel` is a pure function, the
   registry never stores the resolved value.
3. **We deliberately drop `AGENT_CONFIG_KEY_MAP`.** The upstream maintains
   two name conventions for every agent (kebab-case in the registry,
   camelCase in user config) and a translation map between them. Go uses
   one casing everywhere, so the map evaporates. This is anti-pattern #4 in
   the curriculum's "do-not-repeat" list.

## Upstream lineage

- `src/agents/types.ts` L64–L83 — the `AgentConfig` interface.
- `src/agents/definitions.ts` L210–L298 — the `getAgentDefinitions` registry
  plus the four-fold resolution chain at L289.
- See `upstream-readings/s01-definitions.ts` for an annotated copy.
