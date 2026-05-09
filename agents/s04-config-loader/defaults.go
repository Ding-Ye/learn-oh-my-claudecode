package main

import "os"

// DefaultConfig returns the baseline configuration that ships in the
// binary. Mirrors `buildDefaultConfig()` from upstream `src/config/loader.ts`
// L41–L72 — same agent set, same feature flags, same two MCP servers.
//
// Model tier IDs come from the environment so the same binary can target a
// different routing matrix in CI vs. dev vs. prod without rebuilding:
//
//	OMC_MODEL_HIGH    → analyst, planner, architect, code-reviewer, omc, …
//	OMC_MODEL_MEDIUM  → executor, debugger, designer, …
//	OMC_MODEL_LOW     → explore, writer
//
// If a tier env var is unset we fall back to a stable string. The
// fallbacks are deliberately *names*, not version pins — they would be
// wrong in production but make tests deterministic without OS env access.
func DefaultConfig() Config {
	high := envOr("OMC_MODEL_HIGH", tierHighFallback)
	medium := envOr("OMC_MODEL_MEDIUM", tierMediumFallback)
	low := envOr("OMC_MODEL_LOW", tierLowFallback)

	return Config{
		Agents: map[string]AgentRef{
			"omc":              {Model: high},
			"explore":          {Model: low},
			"analyst":          {Model: high},
			"planner":          {Model: high},
			"architect":        {Model: high},
			"debugger":         {Model: medium},
			"executor":         {Model: medium},
			"verifier":         {Model: medium},
			"securityReviewer": {Model: medium},
			"codeReviewer":     {Model: high},
			"testEngineer":     {Model: medium},
			"designer":         {Model: medium},
			"writer":           {Model: low},
			"qaTester":         {Model: medium},
			"scientist":        {Model: medium},
			"tracer":           {Model: medium},
			"gitMaster":        {Model: medium},
			"codeSimplifier":   {Model: high},
			"critic":           {Model: high},
		},
		Features: Features{
			ParallelExecution: true,
			LSPTools:          true,
		},
		MCPServers: map[string]MCPRef{
			"exa":      {Enabled: true},
			"context7": {Enabled: true},
		},
	}
}

// envOr returns the env var if set and non-empty, otherwise the fallback.
// Kept private — the public surface is DefaultConfig.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
