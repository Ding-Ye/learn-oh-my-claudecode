// Source: https://raw.githubusercontent.com/Yeachan-Heo/oh-my-claudecode/main/src/mcp/omc-tools-server.ts
//         https://raw.githubusercontent.com/Yeachan-Heo/oh-my-claudecode/main/src/mcp/servers.ts
// Lines: omc-tools-server.ts L21-L87 + servers.ts L20-L75
// License: MIT, Copyright (c) 2025 Yeachan Heo
//
// Annotated excerpt for s06. The upstream MCP layer has two halves:
// (1) in-process tools — the registry pattern this chapter ports; and
// (2) external MCP server factories — config-only spawners. We faithfully
// port half (1); half (2) is shown for completeness, not implemented.
// Read alongside Section 6 of the s06 chapter docs.

import { TOOL_CATEGORIES, type ToolCategory } from "../constants/index.js";

// ─── (1) The in-process tool definition (omc-tools-server.ts L21-L29) ───
//
// `ToolDef` is upstream's row type. Every tool — LSP probes, AST search,
// python_repl, skill helpers — is produced as a value of this shape and
// aggregated into a single `allTools` array. The Go `Tool` struct mirrors
// it field-for-field, dropping `schema` (a transport concern) and
// collapsing the Promise envelope to `(string, error)`.
interface ToolDef {
  name: string;
  description: string;
  category?: ToolCategory;                     // optional — lsp / ast / python / …
  schema: Record<string, unknown>;             // JSON-schema for arg validation
  handler: (args: unknown) => Promise<{        // ← the closure stored as data
    content: Array<{ type: 'text'; text: string }>;
    isError?: boolean;
  }>;
}

// ─── (2) DISABLE_TOOLS_GROUP_MAP (omc-tools-server.ts L41-L59, abbreviated) ───
export const DISABLE_TOOLS_GROUP_MAP: Record<string, ToolCategory> = {
  'lsp': TOOL_CATEGORIES.LSP,
  'ast': TOOL_CATEGORIES.AST,
  'python': TOOL_CATEGORIES.PYTHON,
  'python-repl': TOOL_CATEGORIES.PYTHON,        // alias
  'memory': TOOL_CATEGORIES.MEMORY,
  'project-memory': TOOL_CATEGORIES.MEMORY,     // alias
  // ... 9 more entries elided
};

// ─── (3) parseDisabledGroups (omc-tools-server.ts L73-L87) ───
export function parseDisabledGroups(envValue?: string): Set<ToolCategory> {
  const disabled = new Set<ToolCategory>();
  const value = envValue ?? process.env.OMC_DISABLE_TOOLS;
  if (!value || !value.trim()) return disabled;        // ← empty-input guard
  for (const name of value.split(',')) {
    const trimmed = name.trim().toLowerCase();         // ← trim + lowercase
    if (!trimmed) continue;                            // ← skip empty tokens
    const category = DISABLE_TOOLS_GROUP_MAP[trimmed];
    if (category !== undefined) disabled.add(category);
  }
  return disabled;
}

// ─── (4) External MCP factories (servers.ts L20-L75, signatures only) ───
// NOT ported by s06 — listed so the reader sees the full upstream MCP
// surface. These produce {command, args, env} records spawned via npx.
export function createExaServer(apiKey?: string) {                 /* L24-L30 */ }
export function createContext7Server() {                           /* L36-L41 */ }
export function createPlaywrightServer() {                         /* L47-L52 */ }
export function createFilesystemServer(allowedPaths: string[]) {   /* L58-L63 */ }
export function createMemoryServer() {                             /* L69-L74 */ }

// ─── Reading notes (Go-port comparisons) ─────────────────────────────────
//
// 1. **L21-L29 (ToolDef) → tool.go::Tool.** Field-for-field mirror; the
//    Go `Handler` field is `func(ctx, args json.RawMessage) (string, error)`,
//    storing a closure as data — the chapter's standout pattern.
// 2. **`schema: Record<string, unknown>` dropped.** Schema validation is
//    a transport concern; handlers `json.Unmarshal` from RawMessage
//    themselves. Trades enforcement for stdlib-only portability.
// 3. **L41-L59 (DISABLE_TOOLS_GROUP_MAP).** Upstream validates env tokens
//    against a whitelist; aliases like `python-repl` collapse to
//    `python`. The Go port has no whitelist — registry is permissive
//    about ad-hoc Category strings; a typo fails open.
// 4. **L73-L87 (parseDisabledGroups) → env.go::ParseDisabled.** Same
//    shape (split-comma, trim, skip-empty) but no `process.env` fallback
//    (Go callers pass `os.Getenv` themselves) and no `.toLowerCase()`
//    (we preserve casing for custom Category values).
// 5. **servers.ts L20-L75 (external factories).** The OTHER half of the
//    upstream MCP story — config-only `{command, args, env}` records the
//    Agent SDK spawns via npx. Out of scope: in-process dispatch and
//    out-of-process supervision are separable concerns. A future chapter
//    could port these using s05's `os/exec` muscle memory plus a tiny
//    stdio framing helper; the in-process registry would be untouched.
// 6. **In-process vs. out-of-process is the single biggest distinction**
//    in the upstream MCP layer. omc-tools-server.ts is closures; servers.ts
//    is config records. Knowing which half a tool lives in tells you
//    where to add a new capability without rebuilding the dispatcher.
