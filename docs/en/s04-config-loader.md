---
title: "s04 · Layered Config & deepMerge"
chapter: 4
slug: s04-config-loader
est_read_min: 9
---

# Chapter 4 — Layered Config & deepMerge

> Fourth chapter of `learn-oh-my-claudecode`. We pivot from the
> string-only world of s03 to typed structs with nested maps, four
> layered overlays, and one security-critical line that mirrors a CVE
> class from the upstream JavaScript runtime.

## Problem

A real OMC user has *opinions*. They want to keep most of the shipped
defaults but pin `executor` to a specific Sonnet build, disable the
LSP-tools feature on a low-memory laptop, and ratchet a single
project's `analyst` to Opus regardless of the host machine's tier
vars. The same user, on a different project, wants project-local
overrides that don't bleed into other repos.

That is four layers of configuration — each one with veto power over
the one below — collapsed into a single `Config` struct:

```
1. defaults                          (in-binary baseline)
2. ~/.config/claude-omc/config.json  (user-wide overlay)
3. <workingDir>/.claude/omc.json     (project overlay)
4. OMC_MODEL_HIGH / _MEDIUM / _LOW   (env overlay, last word)
```

Three sub-problems lurk inside that picture:

1. **Partial overlays.** A project file naming only `features.lspTools`
   must not nuke the rest of `features` or any other top-level key. The
   merge has to be *deep* — fields, not files.
2. **Prototype pollution.** Upstream runs in JavaScript, where a
   payload like `{"__proto__": {"polluted": true}}` merged into
   `Object.prototype` taints every object in the runtime. The Go port
   has no prototype chain, but configs cross language boundaries —
   defense in depth requires we strip those keys here.
3. **Env vs. user pin.** When the user manually pinned `executor` to
   `claude-sonnet-4-5`, an `OMC_MODEL_MEDIUM=foo` env var must NOT
   undo their choice. Env wins over *defaults*, not over explicit user
   selections.

Upstream solves all three at `src/config/loader.ts` L1–L80 plus the
`deepMerge` helper at `src/agents/utils.ts` L367–L393. This chapter
ports that into ~250 Go lines, stdlib only.

## Solution

Five files compose the public surface:

- `config.go` — the `Config / AgentRef / Features / MCPRef` struct
  family that defines the JSON shape.
- `defaults.go` — `DefaultConfig() Config` returning 19 agents over
  three tiers, plus the tier-fallback string constants used for env
  reasoning.
- `merge.go` — `deepMerge(dst, src map[string]any) map[string]any`
  with the `__proto__ / constructor / prototype` skip.
- `loader.go` — `Load(workingDir string) (Config, error)` running the
  four-layer fold.
- `env.go` — `applyEnvOverlay(*Config)` rewriting tier-default agent
  models from `OMC_MODEL_*`.

The merge runs over `map[string]any` so nested partial overlays "just
work" — `encoding/json.Unmarshal` into `map[string]any` is exactly the
representation `deepMerge` operates on, and a final round-trip through
`mapToConfig` re-types the result into the `Config` struct.

## How It Works

### Layering pipeline

```
        DefaultConfig()
              │
              ▼ (configToMap)
       map[string]any  ◀── starting state
              │
              ▼ deepMerge(_, user.json)
              │
              ▼ deepMerge(_, project.json)
              │
              ▼ (mapToConfig)
        Config struct
              │
              ▼ applyEnvOverlay(&cfg)
              │
              ▼
         resolved Config
```

Each `deepMerge` call returns a fresh map, so layers are pure functions
of the layers below. There is no in-place mutation, no shared state.

### The reserved-key guard

```go
// merge.go
var reservedKeys = map[string]struct{}{
    "__proto__":   {},
    "constructor": {},
    "prototype":   {},
}

for k, sv := range src {
    if _, banned := reservedKeys[k]; banned {
        continue   // ⭐ security-critical mirror of utils.ts L376
    }
    // ... normal merge logic
}
```

Three names, one `for ... continue`. That is the whole CVE mitigation.
The `_test.go` companion (`TestDeepMergeIgnoresProtoPollutionKey`)
feeds in the canonical exploit payload and asserts the key is absent
from the merged output.

### Env overlay nuance

```go
// env.go — only rewrite agents still on a tier-fallback string.
switch ref.Model {
case tierHighFallback:    // "claude-opus-4-7"
    if high != "" { ref.Model = high; cfg.Agents[name] = ref }
case tierMediumFallback:  // "claude-sonnet-4-7"
    if medium != "" { ref.Model = medium; cfg.Agents[name] = ref }
case tierLowFallback:     // "claude-haiku-4-7"
    if low != "" { ref.Model = low; cfg.Agents[name] = ref }
}
```

If a user wrote `"executor": {"model": "claude-sonnet-4-5"}` in their
project file, `executor.Model == "claude-sonnet-4-5"` — none of the
three tier fallbacks. The switch falls through, the env var is
ignored. This mirrors upstream's
`OMC_ROUTING_FORCE_INHERIT off by default` intent without the extra
flag.

## What Changed (vs. s03)

s03 was a pure-string transform: every function was `string → string`,
no I/O, no errors, no structs. s04 introduces three things at once:

| Concern | s03 | s04 |
|---|---|---|
| State shape | none (strings only) | `Config { Agents map[…]; Features; MCPServers map[…] }` |
| I/O | none | `os.ReadFile`, `os.UserHomeDir`, `os.Getenv` |
| Errors | impossible | typed `(Config, error)` returns; missing files OK, malformed = error |
| Security thinking | n/a | reserved-key guard against prototype pollution |

The `(string, error)` posture from s02 returns. Reserved keys vs. user
keys is a new mental category — until s04, no chapter has had to
*refuse* an input in the name of safety.

## Try It

```bash
cd agents/s04-config-loader

GOWORK=off go vet ./...                # silent, no output
GOWORK=off go build ./...              # silent, no output
GOWORK=off go test -v -count=1 ./...   # 8 tests pass
GOWORK=off go run .                    # output matches testdata/expected.txt
```

Expected output:

```
== defaults ==
  agents.executor.model    = claude-sonnet-4-7
  agents.architect.model   = claude-opus-4-7
  features.parallelExec    = true
  mcpServers.exa.enabled   = true

== defaults + user ==
  agents.executor.model    = claude-sonnet-4-5
  agents.architect.model   = claude-opus-4-7
  features.parallelExec    = true
  mcpServers.exa.enabled   = false

== defaults + user + project ==
  agents.executor.model    = project-pinned-executor
  agents.architect.model   = claude-opus-4-7
  features.parallelExec    = false
  mcpServers.exa.enabled   = false

== defaults + user + project + env(OMC_MODEL_HIGH=opus-test) ==
  agents.executor.model    = project-pinned-executor
  agents.architect.model   = opus-test
  features.parallelExec    = false
  mcpServers.exa.enabled   = false
```

Further exercises:

- Add a fifth layer between project and env: a `--config <path>` CLI
  flag that loads one more JSON file. Where in `Load` does the slice
  append go?
- Replace `encoding/json` with a JSONC parser of your choice (e.g.
  `github.com/tailscale/hujson`). Compare the dependency footprint
  against the value of inline comments.

## Upstream Source Reading

Excerpt from `src/config/loader.ts` L41–L72 + `src/agents/utils.ts`
L367–L393 (full annotated copy at `upstream-readings/s04-loader.ts`):

```typescript
// loader.ts L41–L72 — the default-config literal we ported into DefaultConfig().
export function buildDefaultConfig(): PluginConfig {
  const defaultTierModels = getDefaultTierModels();
  return {
    agents: {
      omc:       { model: defaultTierModels.HIGH },
      explore:   { model: defaultTierModels.LOW },
      analyst:   { model: defaultTierModels.HIGH },
      planner:   { model: defaultTierModels.HIGH },
      architect: { model: defaultTierModels.HIGH },
      executor:  { model: defaultTierModels.MEDIUM },
      // ... 14 more
    },
    features: { parallelExecution: true, lspTools: true },
    mcpServers: { exa: { enabled: true }, context7: { enabled: true } },
  };
}

// utils.ts L367–L393 — deepMerge.
export function deepMerge<T>(target: T, source: Partial<T>): T {
  const result = { ...target };
  for (const key of Object.keys(source)) {
    if (key === '__proto__' || key === 'constructor' || key === 'prototype') continue; // ⭐ L376
    // ... merge body
  }
  return result;
}
```

Reading notes (Go-port comparisons):

1. **L1–L9 (load-order docblock) → `loader.go::Load`.** Four-layer
   order preserved verbatim. Missing files are silently skipped (a
   fresh user has none of them); malformed JSON is a hard error
   because silent parse-swallowing hides typos.
2. **L41–L72 (`buildDefaultConfig`) → `defaults.go::DefaultConfig`.**
   Direct port of the agents map. Tier IDs come from
   `OMC_MODEL_{HIGH,MEDIUM,LOW}` via the small `envOr` helper — the
   Go spelling of `getDefaultTierModels`.
3. **⭐ L376 (reserved-key skip) → `merge.go::reservedKeys`.** THE
   load-bearing line. Even without a prototype chain, the guard is
   preserved as a *boundary* defense: downstream JS consumers of the
   merged map stay safe without having to re-validate.
4. **L378–L391 (recursive merge body) → `merge.go::deepMerge`.** The
   `Array.isArray` guard becomes a Go type assertion — only when both
   `dst[k]` and `src[k]` are `map[string]any` do we recurse. The
   `sourceValue !== undefined` branch becomes `sv != nil`.
5. **JSONC vs JSON.** Upstream uses `parseJsonc` for `// comments`.
   The Go port drops this (plan §"Risks #2"): `encoding/json` is
   stdlib, comments belong in README docs.
6. **Env overlay** runs post-merge, so env always wins over defaults
   — but the Go port restricts env-driven rewrites to agents still on
   a tier-fallback string. User-pinned IDs survive untouched.
