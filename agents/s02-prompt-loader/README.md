# s02 — Prompt Loader with embed.FS / 提示词加载器

> Second chapter of `learn-oh-my-claudecode`. Introduces filesystem-aware
> Go: `//go:embed` for build-time bundling, `regexp` for YAML frontmatter
> stripping, and a tight `^[a-z0-9-]+$` name guard that defeats path
> traversal before any I/O happens.

## Scope (one line)

A 60-line `Loader` that reads agent prompts out of an `embed.FS`, validates
the requested name against `^[a-z0-9-]+$`, and strips a leading
YAML-frontmatter block — porting upstream `src/agents/utils.ts` L83–L131
(`loadAgentPrompt`) into a single Go code path that serves both `go run`
and `go build` without the runtime/build-time fork upstream maintains.

## Files

| Path | Role |
|---|---|
| `loader.go` | `Loader` struct, `New`, `Load`. The frontmatter regex and the security comments live here. |
| `validate.go` | `validateName(name)` and the package-level `validNamePattern`. Single responsibility — the security guard. |
| `main.go` | Demo: `go:embed` directive, load `architect`, exercise the path-traversal rejection, print embedded agents. |
| `loader_test.go` | Five tests (one is table-driven across six malformed names) plus a second `embed.FS` rooted at `testdata/`. |
| `agents/architect.md` | Fixture, model=opus, level=3 (READ-ONLY). |
| `agents/executor.md` | Fixture, model=sonnet. |
| `agents/explore.md` | Fixture, model=haiku. |
| `testdata/raw-body.md` | Fixture WITHOUT frontmatter. Pins the regex's "leading fence required" semantic. |
| `testdata/expected.txt` | Captured `go run .` stdout — used as the doc's "Try It" fixture. |
| `go.mod` | `go 1.21`, stdlib only, no external deps. |

## Run

```bash
cd agents/s02-prompt-loader

go vet ./...     # silent
go build ./...   # silent
go test -v ./... # 5 tests pass; one is table-driven across 6 cases
go run .         # output exactly matches testdata/expected.txt
```

Expected `go run .` output:

```
architect prompt (759 bytes), first line: "# Architect"
path traversal rejected: true
unknown agent surfaced as: true
embedded agents: [architect executor explore]
```

## Three teaching points

1. **`//go:embed` collapses upstream's dual-mode loader.** The TypeScript
   original branches between a build-time `__AGENT_PROMPTS__` map (injected
   by esbuild for the CJS bundle) and a runtime `readFileSync` (for dev).
   Go's `embed.FS` serves both contexts from a single code path —
   `go run` reads from disk, `go build` bakes the bytes into the binary,
   and the call site never knows the difference. This deletes ~30 LOC and
   one entire failure mode (bundler/runtime divergence).
2. **Validate first, concatenate paths second, never `filepath.Join`.**
   `^[a-z0-9-]+$` rejects `../` before any path is built. We then use plain
   `+` string concatenation — *not* `filepath.Join` — because Join cleans
   `..` segments and would silently re-introduce traversal if the regex ever
   regressed. Defense in depth: regex catches the symptom, no-Join policy
   prevents recurrence.
3. **Sentinel errors classify, not just signal.** `ErrInvalidName` and
   `ErrAgentNotFound` are exported variables; callers use `errors.Is` to
   tell "user attacked" from "user mistyped". Conflating them as a generic
   "load failed" would erase the security audit signal — the upstream
   `loadAgentPrompt` deliberately keeps the same distinction.

## Upstream lineage

- `src/agents/utils.ts` L83–L131 — `loadAgentPrompt` plus the
  `^---[\s\S]*?---\s*([\s\S]*)$` frontmatter regex.
- `agents/architect.md` L1–L7 — the canonical YAML frontmatter shape we
  mirror in our three fixtures.
- See `upstream-readings/s02-utils.ts` for the annotated upstream excerpt.
