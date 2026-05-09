# s06 — MCP Tool Registry / MCP 工具注册表

> Sixth chapter of `learn-oh-my-claudecode`. We pivot from process
> management (s05) to **first-class function values stored as data**: a
> tool is a struct whose `Handler` field is `func(ctx, args) (string,
> error)`, registered into a `map[string]Tool`, and dispatched at
> runtime under a category-based disable filter. **First time the
> chapter series stores a closure inside a struct field**.

## Scope (one line)

A `Registry` whose `Register(t Tool)` accumulates tools, whose
`WithDisabled(map[Category]bool)` returns a filtered view, and whose
`Invoke(ctx, name, args) (string, error)` dispatches to the matching
handler — gated by two sentinels (`ErrUnknownTool`,
`ErrCategoryDisabled`) and fed by a `ParseDisabled(env)` helper that
parses the upstream `OMC_DISABLE_TOOLS=lsp,python,memory` env-var
shape, ported from `src/mcp/omc-tools-server.ts` L1–L100 plus
`src/mcp/servers.ts` L20–L75 in ~280 stdlib-only Go lines.

## Files

| Path | Role |
|---|---|
| `tool.go` | `type Category string` + canonical `CategoryLSP / Python / AST / …` constants; `Tool` struct with `Handler func(ctx, args) (string, error)` field. |
| `registry.go` | `Registry`, `New`, `Register`, `WithDisabled` (returns a *view*, not a clone), `Invoke`, `Names`, `IsDisabled`. Sentinels `ErrUnknownTool`, `ErrCategoryDisabled`. |
| `env.go` | `ParseDisabled(env string) map[Category]bool` — comma-split, whitespace-trim, empty-token-skip; nil-safe on empty input. |
| `main.go` | Demo: register lsp + python tools, disable lsp via `ParseDisabled("lsp")`, invoke each, print sorted Names() with `[disabled]` annotation. |
| `registry_test.go` | Five tests (one with four subtests): register/invoke happy path, unknown-tool sentinel, ParseDisabled corner cases, WithDisabled view semantics, disabled-tool short-circuit. |
| `testdata/expected.txt` | Captured `go run .` stdout for drift detection. |
| `go.mod` | `go 1.21`, stdlib only. |

## Run

```bash
cd agents/s06-mcp-tool-server

GOWORK=off go vet ./...                # silent
GOWORK=off go build ./...              # silent
GOWORK=off go test -v -count=1 ./...   # 5 tests + 4 subtests pass, sub-second
GOWORK=off go run .                    # output matches testdata/expected.txt
```

## Three teaching points

1. **A tool is data plus a closure.** `Tool.Handler` is a struct field
   of type `func(ctx, args) (string, error)`. Storing closures as data
   is what unlocks `Register` → `Invoke` dispatch without a switch
   statement, and it's the same trick s10 will use for worker bodies.
2. **WithDisabled returns a view, not a deep copy.** The view shares
   the underlying tools map but keeps its own `disabled` set. This
   mirrors the upstream "parse env once, capture in dispatcher
   closure" idiom (`omc-tools-server.ts` L73–L87) and lets a test
   construct a clean view per case without rebuilding the tool list.
3. **Sentinels make error paths greppable.** `errors.Is(err,
   ErrCategoryDisabled)` is one-line documentation: the caller knows
   exactly which path triggered without parsing strings. Compare s05's
   `Result.Err` field — same posture, different shape.

## Anti-pattern callout

Plan §"Anti-pattern #10" reads "Global side-effect imports / barrel
files." The TS upstream registers tools by importing them at the top
of `omc-tools-server.ts` and spreading the resulting arrays; testing
that file in isolation is hard because every import has side effects.
Our Go port requires an explicit `Register(t)` per tool, so a test
constructs a Registry with exactly the tools it needs:

```go
r := New()
r.Register(Tool{Name: "lsp.def", Category: CategoryLSP, Handler: ...})
// no init() side effects, no global; the test owns the registry.
```

This is the same lesson as Go's `init()`-vs-explicit-construction
debate. The cost is one line per tool at startup; the win is testable
modules.

## Upstream lineage

- `src/mcp/omc-tools-server.ts` L1–L100 — the `ToolDef` interface
  (L21–L29), `DISABLE_TOOLS_GROUP_MAP` (L41–L59), and
  `parseDisabledGroups` (L73–L87). The shape every Tool field maps to.
- `src/mcp/servers.ts` L20–L75 — five external-MCP-server factories
  (`createExaServer`, `createContext7Server`, `createPlaywrightServer`,
  `createFilesystemServer`, `createMemoryServer`). Out of scope for
  the in-process registry pattern, but worth seeing as the *other*
  half of the MCP story; we touch it in §"Upstream Source Reading" of
  the chapter docs.
- See `upstream-readings/s06-mcp.ts` for the annotated excerpt that
  combines both files.
