package main

import (
	"os"
	"strings"
)

// Tier fallback constants used both by DefaultConfig (when env is unset)
// and by applyEnvOverlay (to decide which agents are on which tier). The
// env overlay treats any agent currently pinned to a fallback string as
// "still on its default tier" and rewrites it to the env override.
const (
	tierHighFallback   = "claude-opus-4-7"
	tierMediumFallback = "claude-sonnet-4-7"
	tierLowFallback    = "claude-haiku-4-7"
)

// applyEnvOverlay is the last layer of Load. It runs after defaults +
// user + project files have all been merged, so env vars get the final
// say. Two concerns:
//
//  1. **Tier overrides.** OMC_MODEL_HIGH / _MEDIUM / _LOW. Strategy: for
//     every agent the merged config still has on a *tier default* model
//     string (the fallback constants above), rewrite to the env value.
//     Agents the user manually pinned to a non-default ID are left
//     alone, mirroring upstream's "OMC_ROUTING_FORCE_INHERIT off by
//     default" intent.
//  2. **Tool disable list.** OMC_DISABLE_TOOLS is a comma-separated list
//     of categories — handled in s06's mcp-tool-server. Here we use it as
//     a feature signal: any non-empty entry flips ParallelExecution off,
//     because the typical reason to disable tools is a constrained env
//     where launching parallel agents is also unwise.
//
// Concern #2 is a teaching simplification — the real OMC keeps the two
// concepts separate. Calling them out together lets us show that env
// overlays touch nested structs as easily as scalars.
func applyEnvOverlay(cfg *Config) {
	high := os.Getenv("OMC_MODEL_HIGH")
	medium := os.Getenv("OMC_MODEL_MEDIUM")
	low := os.Getenv("OMC_MODEL_LOW")

	for name, ref := range cfg.Agents {
		switch ref.Model {
		case tierHighFallback:
			if high != "" {
				ref.Model = high
				cfg.Agents[name] = ref
			}
		case tierMediumFallback:
			if medium != "" {
				ref.Model = medium
				cfg.Agents[name] = ref
			}
		case tierLowFallback:
			if low != "" {
				ref.Model = low
				cfg.Agents[name] = ref
			}
		}
	}

	if disabled := os.Getenv("OMC_DISABLE_TOOLS"); strings.TrimSpace(disabled) != "" {
		cfg.Features.ParallelExecution = false
	}
}
