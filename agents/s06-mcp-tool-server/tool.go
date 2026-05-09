package main

import (
	"context"
	"encoding/json"
)

// Category names a coarse-grained group of tools that a user can switch
// off in one shot via the OMC_DISABLE_TOOLS env var. The upstream
// TypeScript port models this as a string-literal union (lsp / ast /
// python / trace / state / notepad / memory / skills / interop / codex /
// gemini / shared-memory / deepinit / wiki — see
// `src/mcp/omc-tools-server.ts` L21–L29 and the `ToolCategory` import
// from `../constants/index.js`).
//
// We deliberately keep Category as a `type Category string` *named
// alias* rather than an enum-like int so:
//
//   1. JSON env-var values map straight to Category values without a
//      lookup table.
//   2. `grep -rn 'Category("lsp")' .` finds every call site, which is
//      the cheapest possible cross-reference for a teaching repo.
//   3. New categories require zero registry changes — adding "trace"
//      is one constant line, no enum drift.
//
// The cost is that any string literal silently becomes a Category;
// we mitigate that with `Validate` checks at registration time and a
// short list of canonical constants below.
type Category string

// Canonical category constants. Mirrors the upstream
// TOOL_CATEGORIES record (referenced from `omc-tools-server.ts` L21).
// Not exhaustive; tools may carry their own ad-hoc Category strings —
// we surface the common ones so callers benefit from compile-time
// spell-checking when they want it.
const (
	CategoryLSP     Category = "lsp"
	CategoryAST     Category = "ast"
	CategoryPython  Category = "python"
	CategoryTrace   Category = "trace"
	CategoryState   Category = "state"
	CategoryNotepad Category = "notepad"
	CategoryMemory  Category = "memory"
	CategorySkills  Category = "skills"
)

// Tool is the in-process equivalent of the upstream `ToolDef`
// interface (`src/mcp/omc-tools-server.ts` L21–L29). Three things to
// notice when reading this struct against its TypeScript twin:
//
//   1. `Handler` is a *first-class struct field of function type*. This
//      is the chapter's standout pattern: a tool is data plus a closure,
//      stored together in a single value, dispatched dynamically from a
//      map. It is the building block s10 will reuse for worker dispatch.
//
//   2. We drop the `schema Record<string, unknown>` field entirely.
//      Schema validation belongs to whichever transport wraps the
//      registry (real MCP, gRPC, HTTP); the registry itself is
//      transport-agnostic. The handler receives `json.RawMessage` so it
//      can defer parsing to its own typed unmarshal.
//
//   3. The handler returns `(string, error)` instead of the upstream's
//      `Promise<{ content: [...]; isError?: boolean }>`. Go's
//      idiomatic `(value, error)` pair carries the same information
//      with less ceremony — the caller decides whether to wrap the
//      error into a transport-specific shape.
type Tool struct {
	Name        string
	Description string
	Category    Category
	Handler     func(ctx context.Context, args json.RawMessage) (string, error)
}
