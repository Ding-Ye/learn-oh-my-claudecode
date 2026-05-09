// Package main implements the Chapter 1 agent registry: the typed source of
// truth that maps a role name (e.g. "architect") to its prompt, tool list, and
// model tier. It mirrors the upstream `getAgentDefinitions()` registry in
// `src/agents/definitions.ts` and the `AgentConfig` shape in
// `src/agents/types.ts`, deliberately dropped to a minimum viable subset:
// no I/O, no concurrency primitives, no kebab-vs-camelCase translation map.
// Subsequent chapters reuse subsets of the Agent shape declared here.
package main

// Agent is the canonical shape every other chapter inherits a subset of.
//
// Field-by-field correspondence to upstream `AgentConfig`
// (src/agents/types.ts L64-L83):
//
//   - Name          ↔ name              (canonical kebab-case role identifier)
//   - Description   ↔ description       (one-line summary used for delegation)
//   - Prompt        ↔ prompt            (system prompt body; loaded in s02)
//   - Tools         ↔ tools             (allowlist; nil/empty means "all")
//   - Model         ↔ model             (tier-resolved string e.g. "claude-opus-4-7")
//   - DefaultModel  ↔ defaultModel      (final fallback for ResolveModel)
//
// Upstream also carries `disallowedTools` and `metadata` fields. Both are out
// of scope for s01: disallowedTools is purely a runtime concern (s05 hooks
// pipeline territory) and metadata drives auto-generated delegation tables we
// haven't yet introduced.
type Agent struct {
	Name         string
	Description  string
	Prompt       string
	Tools        []string
	Model        string // tier-resolved string like "claude-opus-4-7"
	DefaultModel string // fallback if Model is empty
}
