// Package main implements s04 — the layered configuration loader.
//
// The Config struct is the on-the-wire shape we accept in JSON files and
// produce after merging. It deliberately covers only the three sub-objects
// the upstream `buildDefaultConfig()` populates at L41–L72 of
// `src/config/loader.ts`: agents (model tier per role), features (boolean
// flags), and mcpServers (enable / disable per server). Real OMC has many
// more keys; the curriculum scope is the merge mechanism, not the schema.
package main

// Config is the resolved, merged configuration the caller works with.
//
// All three top-level fields are addressable independently so a partial
// override file (one that mentions only `features`, say) can layer cleanly
// without nuking the other two — that property is what makes deepMerge
// useful in the first place.
type Config struct {
	Agents     map[string]AgentRef `json:"agents"`
	Features   Features            `json:"features"`
	MCPServers map[string]MCPRef   `json:"mcpServers"`
}

// AgentRef pins the model tier for a named agent. Mirrors upstream's
// `{ omc: { model: defaultTierModels.HIGH } }` literal.
type AgentRef struct {
	Model string `json:"model"`
}

// Features carries the boolean flags OMC reads at runtime. Two flags are
// enough to demonstrate that nested structs (vs. flat maps) merge by
// field, not by key string.
type Features struct {
	ParallelExecution bool `json:"parallelExecution"`
	LSPTools          bool `json:"lspTools"`
}

// MCPRef enables or disables a single MCP server entry. Maps from server
// name (e.g. "exa", "context7") to this struct give the merge a per-key
// granularity that scalar bool flags cannot provide.
type MCPRef struct {
	Enabled bool `json:"enabled"`
}
