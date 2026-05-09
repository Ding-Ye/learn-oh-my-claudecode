// Source: src/config/loader.ts (Yeachan-Heo/oh-my-claudecode, MIT)
// Source: src/agents/utils.ts L367–L393 (deepMerge)
// License: MIT, Copyright (c) 2025 Yeachan Heo
//
// Annotated excerpt for s04. The Go port keeps the four-layer order
// (defaults → user → project → env) verbatim but drops JSONC for strict
// JSON, and replaces typed-but-loosely-merged TS objects with a
// `map[string]any` fold inside Load. The prototype-pollution guard at
// utils.ts L376 is preserved exactly — see merge.go::reservedKeys.

// (1) Load-order docblock (loader.ts L1–L9) — mirrored in Load.
/**
 * Configuration Loader
 * - User config:    ~/.config/claude-omc/config.jsonc
 * - Project config: .claude/omc.jsonc
 * - Environment variables
 */

// (2) buildDefaultConfig (loader.ts L41–L72) — ported into DefaultConfig().
// Tier resolution is eager: getDefaultTierModels() reads env at call time, so
// the Go port also reads env on every call (t.Setenv visible without rebuild).
export function buildDefaultConfig(): PluginConfig {
  const defaultTierModels = getDefaultTierModels();
  return {
    agents: {
      omc:              { model: defaultTierModels.HIGH },
      explore:          { model: defaultTierModels.LOW },
      analyst:          { model: defaultTierModels.HIGH },
      planner:          { model: defaultTierModels.HIGH },
      architect:        { model: defaultTierModels.HIGH },
      executor:         { model: defaultTierModels.MEDIUM },
      // ... 14 more agents elided
    },
    features: { parallelExecution: true, lspTools: true },
    mcpServers: { exa: { enabled: true }, context7: { enabled: true } },
  };
}

// (3) ⭐ deepMerge (utils.ts L367–L393) — THE SECURITY CHAPTER.
// L376 is the prototype-pollution guard. A payload like
//   { "__proto__": { "polluted": true } }
// merged into Object.prototype taints every object in the JS runtime.
// Go has no prototype chain, but the Go port mirrors the guard so a
// downstream `node script.js < merged.json` consumer stays safe.
export function deepMerge<T extends Record<string, unknown>>(target: T, source: Partial<T>): T {
  const result = { ...target };
  for (const key of Object.keys(source)) {
    if (key === '__proto__' || key === 'constructor' || key === 'prototype') continue; // ⭐ L376
    const sv = source[key as keyof T], tv = target[key as keyof T];
    if (sv && typeof sv === 'object' && !Array.isArray(sv)
        && tv && typeof tv === 'object' && !Array.isArray(tv)) {
      (result as any)[key] = deepMerge(tv as any, sv as any);
    } else if (sv !== undefined) {
      (result as any)[key] = sv;
    }
  }
  return result;
}

// Reading map (Go-port comparisons):
//
// 1. **L1–L9 docblock → loader.go::Load.** Same four-layer order. Missing
//    files skipped silently; malformed JSON returns an error.
// 2. **L41–L72 buildDefaultConfig → defaults.go::DefaultConfig.** Direct port.
//    Tier IDs come from OMC_MODEL_HIGH/_MEDIUM/_LOW via envOr.
// 3. **⭐ L376 reserved-key skip → merge.go::reservedKeys.** THE load-bearing
//    teaching point. Even without a prototype chain, the Go port preserves
//    the guard as a *boundary* defense: downstream JS consumers of the
//    merged map stay safe without re-validation.
// 4. **L378–L391 merge body → merge.go::deepMerge.** Array.isArray → Go type
//    assertion (only when both sides are map[string]any do we recurse).
//    `sv !== undefined` → `sv != nil`.
// 5. **JSONC vs JSON.** Upstream uses parseJsonc for inline `// comments`. Go
//    port drops it (plan §"Risks #2"): encoding/json is stdlib; comments
//    belong in README docs.
// 6. **Env overlay (env.go)** runs post-merge so env wins. Restricted to
//    agents still on a tier-fallback string — user-pinned IDs survive,
//    mirroring upstream's OMC_ROUTING_FORCE_INHERIT-off-by-default intent.
