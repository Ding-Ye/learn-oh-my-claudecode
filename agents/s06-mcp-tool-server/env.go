package main

import "strings"

// ParseDisabled reads a comma-separated string in the upstream
// `OMC_DISABLE_TOOLS=lsp,python,memory` shape and returns a
// map[Category]bool whose entries are `true` for every category the
// user wants disabled.
//
// Three small invariants the function must honor:
//
//   1. **Empty input → empty map, never nil panic.** Callers should be
//      able to write `r.WithDisabled(ParseDisabled(os.Getenv(...)))`
//      without an `if envValue == "" { ... }` precheck — both the
//      empty-string and unset-env cases must round-trip cleanly.
//
//   2. **Whitespace is trimmed.** Upstream's
//      `parseDisabledGroups` calls `.trim().toLowerCase()` on each
//      token (`omc-tools-server.ts` L78–L80). Users writing
//      `OMC_DISABLE_TOOLS="lsp, ast , python"` with stray spaces around
//      commas should still get the expected three categories. We trim
//      but DO NOT lowercase — the canonical Category constants in
//      tool.go are already lowercase, and case-folding here would
//      silently alter custom categories the user may have invented.
//
//   3. **Unknown categories pass through unchanged.** Upstream filters
//      against a known whitelist (DISABLE_TOOLS_GROUP_MAP). We do not,
//      because the registry is permissive about ad-hoc categories — a
//      misspelled `"lzp"` token simply ends up disabling no tools, and
//      the registry will happily run the LSP tool. This matches the
//      "unknown names are silently ignored" upstream comment but
//      relocates the check from parse-time to dispatch-time.
//
// Returning a `map` rather than a `Set`-like type keeps the API
// stdlib-only and lets callers cheaply test membership with
// `disabled[CategoryLSP]`.
func ParseDisabled(env string) map[Category]bool {
	out := make(map[Category]bool)
	if strings.TrimSpace(env) == "" {
		return out
	}

	// We split on `,` rather than allowing whitespace as a separator
	// because the upstream env-var contract uses comma exclusively;
	// spaces inside a token (like `"shared memory"`) would be a
	// genuinely invalid category name we should not normalize away.
	for _, raw := range strings.Split(env, ",") {
		token := strings.TrimSpace(raw)
		if token == "" {
			continue
		}
		out[Category(token)] = true
	}
	return out
}
