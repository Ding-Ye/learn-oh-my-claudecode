---
title: "s01 · Agent Registry & Model Tiers"
chapter: 1
slug: s01-agent-registry
est_read_min: 8
---

# Chapter 1 — Agent Registry & Model Tiers

> First chapter of `learn-oh-my-claudecode`. We use the smallest possible
> Go module (~100 lines of implementation, ~100 of tests) to lock in the
> `Agent` shape that runs through every later chapter, and to translate
> upstream's four-fold model-priority chain into a single Go function.

## Problem

When OMC orchestrates Claude Code, it dispatches work across 19 named
sub-roles (`architect`, `planner`, `executor`, `critic`, …). Each role has:

- a distinct system prompt (in `agents/<name>.md`);
- an allow-list of tools it may use;
- a recommended model tier (Haiku / Sonnet / Opus).

**The core tension**: model tier cannot be hard-coded. The same `executor`
role might run on Sonnet (the default), drop to Haiku for a specific
cost-conscious call (`--model=haiku`), or be forced to inherit the parent
process's model when `OMC_ROUTING_FORCE_INHERIT` is set in the environment.
**The registry has to store the static data AND let queries decide the
tier dynamically.**

Upstream solves this with a TypeScript interface, a 19-entry literal, and
a four-fold null-coalescing expression (`src/agents/definitions.ts` L289).
This chapter ports the same idea to a Go struct + map + a pure function
called `ResolveModel`, deliberately dropping upstream's kebab-vs-camelCase
translation table (anti-pattern #4 in the curriculum's "do-not-repeat" list).

## Solution

A `map[string]Agent`, registered once and queried many times. At query
time, a **five-input** `ResolveModel(agent, override, envInherit, configured)`
function picks the final model string. The five candidates are tried in
priority order, returning the first non-empty value:

```
override → envInherit → configured → agent.Model → agent.DefaultModel
```

The registry deliberately does NOT cache the resolved model — this matches
upstream's discipline of recomputing on every `getAgentDefinitions()` call,
which lets dynamic inputs (per-call overrides, env vars) flow through
without invalidation bugs. No I/O, no concurrency, no regex — perfect for
the very first session of a Go-flavoured tour.

## How It Works

### Big picture

```
                   register-time                  query-time
         ┌─────────────────────────┐    ┌──────────────────────────┐
Agent{   │                         │    │   ResolveModel(a, ovr,   │
 Name,   │  Registry.Register(a)   │    │      envInherit, cfg)    │
 Prompt, │   ──> agents[a.Name]=a  │    │       │                  │
 Model,  │                         │    │       ▼                  │
 ...} ──▶│  Registry.Get(name)     │───▶│  override?            ┐  │
         │   ──> (a, ok bool)      │    │  envInherit?          │  │
         │                         │    │  configured?          │ first non-empty
         │  Registry.Names()       │    │  agent.Model?         │  │
         │   ──> sorted []string   │    │  agent.DefaultModel?  ┘  │
         └─────────────────────────┘    └──────────────────────────┘
```

### Core code

```go
// agent.go
type Agent struct {
    Name         string
    Description  string
    Prompt       string
    Tools        []string
    Model        string // tier-resolved string like "claude-opus-4-7"
    DefaultModel string // fallback if Model is empty
}

// registry.go
type Registry struct{ agents map[string]Agent }

func New() *Registry              { return &Registry{agents: make(map[string]Agent)} }
func (r *Registry) Register(a Agent) { r.agents[a.Name] = a }       // last write wins
func (r *Registry) Get(name string) (Agent, bool) {
    a, ok := r.agents[name]
    return a, ok
}
func (r *Registry) Names() []string {
    names := make([]string, 0, len(r.agents))
    for n := range r.agents { names = append(names, n) }
    sort.Strings(names)
    return names
}

// Four-fold (five with DefaultModel) priority chain
func ResolveModel(a Agent, override, envInherit, configured string) string {
    for _, c := range []string{override, envInherit, configured, a.Model, a.DefaultModel} {
        if c != "" { return c }
    }
    return ""
}
```

### Three non-obvious points

1. **Why `Register` overwrites instead of erroring on duplicates.**
   Upstream builds its registry with object literals + spread (`definitions.ts`
   L211-L260), where later assignments naturally overwrite earlier ones. We
   keep the same semantic so a user can append a custom agent in init code
   without first calling `Unregister`. The cost is losing an early signal
   for typo-class duplicates — s01 pins the chosen behavior with the
   `TestRegisterDuplicateNameOverwrites` test as a contract.

2. **Why `ResolveModel` takes 4 positional args, not a single options struct.**
   Bundling the four candidates into an `Options` struct looks tidier, but
   forces every caller to write an extra `Options{Override: x}` literal.
   `ResolveModel` is a hot-path candidate — invoked once per dispatched
   agent — so positional args keep call sites short. All four params are
   `string`, so the compiler cannot help you when you reorder them; that's
   the deliberate trade. We will switch to an options struct in s04 (config
   loader) where the inputs are heterogeneous.

3. **Why the Go port has a fifth fallback (`agent.DefaultModel`) when
   upstream stops at four.** Upstream exposes `defaultModel` to the Claude
   Agent SDK as a sibling of `model` and lets the SDK handle degradation.
   The Go port has no SDK layer — `ResolveModel` is the only seam between
   the registry and downstream consumers — so folding `DefaultModel` in as
   the last-ditch fallback is more ergonomic than asking every caller to
   re-check it. If you need upstream-exact behavior, just leave
   `agent.DefaultModel = ""` and the Go function is then equivalent.

## What Changed

s01 is the very first chapter, so there is no "previous session" to diff
against. What this chapter establishes for the next nine is **the `Agent`
data contract**:

| Chapter | Subset of `Agent` reused |
|---|---|
| s02 prompt loader | `Name + Prompt` |
| s04 config loader | `Name + Model` (indirectly through `Config.Agents`) |
| s10 team watchdog | `Name + Model` (consume-only) |
| s03 / s07 / s08 | None — those chapters are pure string/regex pipelines |

By the end of this session you should be able to answer three things:
(a) what does an agent look like in memory? (b) how does a caller force a
specific model for one call? (c) why do we deliberately skip upstream's
`AGENT_CONFIG_KEY_MAP`?

## Try It

```bash
cd agents/s01-agent-registry

go vet ./...     # silent, no output
go build ./...   # silent, no output
go test -v ./... # all 5 tests pass
go run .         # output exactly matches testdata/expected.txt
```

Expected output:

```
architect found=true model=claude-opus-4-7
with override: claude-haiku-4-5
without override: claude-opus-4-7
registered: [architect executor]
```

Further exercises:

- Set `architect.Model` to `""` in `main.go` and rerun. The
  `without override` line will become empty, demonstrating the
  "all-empty returns empty" contract.
- Change the second `r.Register(...)` to `Name: "architect"` (overwriting
  the first), and check that `Names()` still has just one entry.

## Upstream Source Reading

Excerpted from `src/agents/definitions.ts` L210–L298 (full annotated copy
in `upstream-readings/s01-definitions.ts`):

```typescript
const AGENT_CONFIG_KEY_MAP = {
  explore: 'explore',
  // ... 17 more entries ...
  'security-reviewer': 'securityReviewer',
  'code-reviewer': 'codeReviewer',
  // ⚠ kebab-vs-camelCase translation table — Go version drops it
} as const;

export function getAgentDefinitions(options?: {...}) {
  const agents: Record<string, AgentConfig> = {
    explore: exploreAgent, analyst: analystAgent, /* ... 17 entries ... */
  };

  const resolvedConfig = options?.config ?? loadConfig();
  const inheritModel = resolvedConfig.routing?.forceInherit
    ? resolveInheritedModelFromEnv()
    : undefined;

  for (const [name, agentConfig] of Object.entries(agents)) {
    const override = options?.overrides?.[name];
    const configuredModel = getConfiguredAgentModel(name, resolvedConfig);

    // ⭐ The four-fold priority chain (the load-bearing line of this chapter)
    const resolvedModel =
      override?.model ?? inheritModel ?? configuredModel ?? agentConfig.model;

    result[name] = { /* ...repack result... */ model: resolvedModel };
  }
  return result;
}
```

Reading notes (Go-port comparisons):

1. **L211-L260 (agents literal) → Go's repeated `Register` calls**.
   Upstream constructs everything in one literal; Go uses successive
   `Register` calls so users can append private agents at init — closer to
   Go's preference for "explicit construction" over hidden globals.
2. **L289 (the load-bearing line) → Go's `ResolveModel` body**. One-to-one
   translation, just with one extra `agent.DefaultModel` fallback at the end.
3. **L143-L162 (KEY_MAP) → no Go counterpart**. Upstream's camelCase comes
   from the `omc.jsonc` config file. When Go gains config loading in s04,
   we'll keep kebab-case in the JSON file too, so the translation table
   never needs to exist.
4. **`appendSkininthegamebrosGuidance` (wraps the prompt field) → Go
   keeps the prompt verbatim**. That helper appends a "skin in the game"
   commitment to every prompt; it's decoupled from the registry and
   belongs to the s07 continuation-enforcement chapter.
5. **`resolveInheritedModelFromEnv()` → Go pushes env-var reading to the
   caller**. `ResolveModel` accepts the already-resolved string; reading
   `OMC_*` env vars is `main.go`'s job. Easier to test.
