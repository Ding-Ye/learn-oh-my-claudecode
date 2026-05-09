---
title: "s06 · MCP Tool Registry with Categories"
chapter: 6
slug: s06-mcp-tool-server
est_read_min: 10
---

# Chapter 6 — MCP Tool Registry with Categories

> Sixth chapter of `learn-oh-my-claudecode`. We pivot from process
> management (s05) to **first-class function values stored as data**:
> a tool is a struct whose `Handler` field is a closure of type
> `func(ctx, args) (string, error)`, registered into a
> `map[string]Tool`, and dispatched at runtime through a
> category-based disable filter parsed from the
> `OMC_DISABLE_TOOLS=lsp,python,memory` env-var convention.

## Problem

OMC ships ~40 in-process tools to its agents — twelve LSP probes, two
AST-search variants, a python REPL, several state/notepad/memory
helpers, plus a long tail of skill / interop / codex / gemini / wiki
adapters. Two operational facts make the naive "one factory function
per tool" approach break down:

1. **Some users need to switch off a whole category in one shot.** A
   user on a machine without a working language server wants
   `OMC_DISABLE_TOOLS=lsp` to silence all twelve LSP tools at startup
   without combing through the manifest. Same for `python-repl` on
   sandboxed machines, `memory` on stateless CI runners, etc.
2. **The dispatcher has to be transport-agnostic.** Real MCP servers
   speak JSON-RPC over stdio; tests want to call handlers as plain Go
   functions; a future chapter might wrap the same tools in HTTP. The
   registry layer cannot know which transport is alive — it just
   accepts a name plus raw JSON args, finds the matching tool, and
   returns whatever the handler emits.

Upstream solves this in `src/mcp/omc-tools-server.ts` L1–L100 with a
`ToolDef` interface (L21–L29), a `DISABLE_TOOLS_GROUP_MAP`
whitelist (L41–L59), and a `parseDisabledGroups(env)` helper
(L73–L87) whose Set is captured by a dispatcher closure. We port
the in-process half here in ~280 stdlib-only Go lines.

## Solution

Three files compose the public surface:

- `tool.go` — `type Category string` (a *named alias*, not an enum,
  for grep-ability), a half-dozen canonical category constants
  (`CategoryLSP`, `CategoryPython`, `CategoryAST`, …), and the `Tool`
  struct with a `Handler func(ctx, args) (string, error)` field.
- `registry.go` — `New() *Registry`, `Register(t)`,
  `WithDisabled(map[Category]bool) *Registry` returning a *filtered
  view*, `Invoke(ctx, name, args) (string, error)`, and `Names()`
  returning a sorted slice. Two sentinel errors: `ErrUnknownTool` and
  `ErrCategoryDisabled`.
- `env.go` — `ParseDisabled(env string) map[Category]bool` parses the
  upstream comma-separated env-var format with whitespace trimming and
  empty-token skipping. Empty input returns a non-nil empty map.

A `Tool` value carries its closure inside the struct. `Register` puts
it in the map; `Invoke` looks it up by name, gates on category, then
calls the closure. The whole loop is ~10 lines of dispatch code.

## How It Works

### A tool is a value plus a closure

```go
type Tool struct {
    Name        string
    Description string
    Category    Category
    Handler     func(ctx context.Context, args json.RawMessage) (string, error)
}
```

`Handler` is a struct field whose type is a function. That single line
is the chapter's whole point: a tool is *data plus behavior*, packaged
in one value, stored in a map, and dispatched without a switch
statement. The same trick scales: s10 will store a goroutine entry
function the same way for worker dispatch.

### WithDisabled returns a view, not a copy

```go
view := r.WithDisabled(ParseDisabled("lsp,python"))
view.Invoke(ctx, "lsp.def", args)   // → "", ErrCategoryDisabled
r.Invoke(ctx, "lsp.def", args)      // → handler runs (the source registry is untouched)
```

`WithDisabled` returns a fresh `*Registry` whose `tools` map is
*shared* with the receiver but whose `disabled` set is a fresh map
seeded from `cats`. This mirrors the upstream "parse env once, capture
in dispatcher closure" idiom — and lets a test build a clean view per
case without duplicating the tool list. Disabled tools are still
listed by `Names()`; disabling is a runtime gate, not a removal.

### Sentinels make error paths greppable

```go
out, err := view.Invoke(ctx, "lsp.def", args)
switch {
case errors.Is(err, ErrUnknownTool):       // typo, plugin missing, …
case errors.Is(err, ErrCategoryDisabled):  // OMC_DISABLE_TOOLS includes this category
case err != nil:                           // handler failure
}
```

Two exported sentinels carry the registry's two distinguished failure
modes; `errors.Is` matches them through any wrapping. Compare s05's
`Result.Err` field — same posture (errors are values, not panics),
different shape (per-row vs. single-error return).

## What Changed (vs. s05)

s05 introduced shelling out via context-aware `os/exec` — first time
the chapter series produced side effects across process boundaries.
s06 introduces something subtler but equally load-bearing: **`func` as
a struct field, plus a registry pattern that stores closures as data**.

| Concern | s05 | s06 |
|---|---|---|
| Side effects | `os/exec` spawns child processes | none — handlers are in-process closures |
| Dispatch shape | declarative manifest → `[]Result` slice | name + args → `(string, error)` |
| Failure model | `[]Result` with per-row `Err` | sentinel errors via `errors.Is` |
| Filtering | matcher predicate per hook | category gate per registry view |
| First-class data | `Hook` (matcher + command + timeout) | `Tool` (name + category + **closure**) |

This is the chapter where Go's "functions are values" becomes
*structurally load-bearing*: the entire registry pattern collapses if
you cannot put a `func` inside a `struct`. s10's worker pool will
reuse exactly this trick.

## Try It

```bash
cd agents/s06-mcp-tool-server

GOWORK=off go vet ./...                # silent
GOWORK=off go build ./...              # silent
GOWORK=off go test -v -count=1 ./...   # 5 tests + 4 subtests pass, sub-second
GOWORK=off go run .                    # output matches testdata/expected.txt
```

Expected output:

```
== invoke python.repl (enabled) ==
out="python.repl evaluated args={\"code\":\"print(1+1)\"}" err=<nil>

== invoke lsp.find_definition (disabled via OMC_DISABLE_TOOLS=lsp) ==
out="" err=category-disabled

== Names() (disabled tools annotated) ==
- lsp.find_definition (category=lsp) [disabled]
- python.repl (category=python)
```

Two cases exercise the central abstractions: (a) the python tool runs
because its category is enabled; (b) the lsp tool short-circuits with
`ErrCategoryDisabled` because `ParseDisabled("lsp")` put it on the
view's disabled set. `Names()` lists both because disabling is a
*runtime gate*, not a *removal*.

Further exercises:

- Add a third tool in a third category (say `CategoryAST`) and rerun
  with `OMC_DISABLE_TOOLS=lsp,ast`. Notice the `Names()` output gains
  a second `[disabled]` row but the demo printer needs zero changes.
- Replace the in-memory `Invoke` with a stdio MCP server using
  `github.com/modelcontextprotocol/go-sdk`. The registry stays exactly
  the same; only the transport wrapper is new. (This is Appendix B
  exercise #3.)

## Upstream Source Reading

Excerpt from `src/mcp/omc-tools-server.ts` L21–L87 + `src/mcp/servers.ts`
L20–L75 (full annotated copy at `upstream-readings/s06-mcp.ts`):

```ts
// omc-tools-server.ts L21-L29 — the row type every tool fills in
interface ToolDef {
  name: string;
  description: string;
  category?: ToolCategory;                     // lsp / ast / python / …
  schema: Record<string, unknown>;             // dropped in the Go port
  handler: (args: unknown) => Promise<{ content: ...; isError?: boolean }>;
}

// omc-tools-server.ts L73-L87 — the env-var parser
export function parseDisabledGroups(envValue?: string): Set<ToolCategory> {
  const disabled = new Set<ToolCategory>();
  const value = envValue ?? process.env.OMC_DISABLE_TOOLS;
  if (!value || !value.trim()) return disabled;
  for (const name of value.split(',')) {
    const trimmed = name.trim().toLowerCase();
    if (!trimmed) continue;
    const category = DISABLE_TOOLS_GROUP_MAP[trimmed];
    if (category !== undefined) disabled.add(category);
  }
  return disabled;
}

// servers.ts L20-L75 — the OTHER half of the upstream MCP story
// (out-of-process factories spawned via npx, NOT ported here)
export function createExaServer(apiKey?: string) { /* {command, args, env} */ }
export function createMemoryServer() { /* {command, args} */ }
```

Reading notes (Go-port comparisons):

1. **L21–L29 (`ToolDef`) → `tool.go::Tool`.** Field-for-field
   mirror, with two simplifications: `schema` is dropped (validation
   is a transport concern; handlers `json.Unmarshal` from RawMessage
   themselves) and the Promise envelope `{ content, isError }`
   collapses to Go's idiomatic `(string, error)` pair.
2. **L21 (`type ToolCategory`) → `tool.go::type Category string`.**
   Upstream models categories as a string-literal union (~15 names);
   we use a `type Category string` named alias plus a handful of
   exported constants (`CategoryLSP`, `CategoryPython`, …). The string
   alias keeps env-var parsing direct (`Category(token)` is one cast,
   no lookup table) and lets `grep -rn 'Category("lsp")' .` find every
   call site — the cheapest cross-reference for a teaching repo.
3. **L41–L59 (`DISABLE_TOOLS_GROUP_MAP`).** Upstream validates env
   tokens against a whitelist; aliases like `python-repl` collapse to
   `python`. The Go port has *no* whitelist — the registry is
   permissive about ad-hoc Category strings, and a typo simply ends
   up disabling no tool at dispatch. Trade-off: typos fail open, not
   loud. For teaching we prefer the simpler dispatcher.
4. **L73–L87 (`parseDisabledGroups`) → `env.go::ParseDisabled`.**
   Same shape (split-comma, trim, skip-empty), but no `process.env`
   fallback (Go callers pass `os.Getenv("OMC_DISABLE_TOOLS")`
   themselves) and no `.toLowerCase()` (we preserve casing for
   custom Category values).
5. **`servers.ts` L20–L75 — the OTHER half.** Five external-MCP
   factories produce `{ command, args, env }` records spawned via npx
   at runtime. They are NOT in-process and we do NOT port them — the
   in-process registry is one teachable concern, out-of-process
   supervision is another. A future chapter could pair `os/exec` (s05
   muscle memory) with a stdio framing helper to cover that half; the
   in-process registry would be untouched.
6. **In-process vs. out-of-process is the single biggest distinction**
   in the upstream MCP layer. omc-tools-server.ts is closures stored
   as data; servers.ts is config records consumed by an external
   supervisor. Knowing which half a tool lives in tells you where to
   add a new capability without rebuilding the dispatcher.
