package main

// reservedKeys are the three names deepMerge MUST refuse to copy from
// `src` into `dst`. Mirrors upstream `src/agents/utils.ts` L376:
//
//	if (key === '__proto__' || key === 'constructor' || key === 'prototype') continue;
//
// In JavaScript these names are special: a payload like
//
//	{ "__proto__": { "polluted": true } }
//
// merged into Object.prototype taints every object in the runtime — the
// classic prototype-pollution CVE class. Go has no prototype chain, so the
// attack does not transfer literally; we still skip the keys for two
// reasons:
//
//  1. **Defense in depth.** If a downstream consumer unmarshals the merged
//     map back into JavaScript (via a shell exec, a JSON-typed shell var,
//     etc.), the attack reappears at the boundary.
//  2. **Behavioral parity.** The Go port is a teaching mirror — keeping
//     the guard makes the line-for-line correspondence with upstream
//     legible.
var reservedKeys = map[string]struct{}{
	"__proto__":   {},
	"constructor": {},
	"prototype":   {},
}

// deepMerge layers `src` onto a copy of `dst` and returns the result.
//
// Semantics (matching upstream `deepMerge<T>(target, source)`):
//
//   - Keys in `src` overwrite keys in `dst`.
//   - When both sides hold a `map[string]any` for the same key, recurse.
//   - When `src` holds nil, the dst value is preserved (nil is treated as
//     "not provided").
//   - Reserved keys (__proto__ / constructor / prototype) are silently
//     dropped from `src`.
//
// The function operates on `map[string]any` because that is what
// `encoding/json` produces for arbitrary JSON. Typed merging happens at
// the Load boundary by re-marshaling.
func deepMerge(dst, src map[string]any) map[string]any {
	out := make(map[string]any, len(dst))
	for k, v := range dst {
		out[k] = v
	}

	for k, sv := range src {
		if _, banned := reservedKeys[k]; banned {
			// Security-critical: never let __proto__ / constructor /
			// prototype keys flow into the merged map. See reservedKeys
			// doc comment.
			continue
		}
		if sv == nil {
			continue
		}

		dv, hasDst := out[k]
		srcMap, srcIsMap := sv.(map[string]any)
		dstMap, dstIsMap := dv.(map[string]any)

		if hasDst && srcIsMap && dstIsMap {
			out[k] = deepMerge(dstMap, srcMap)
		} else {
			out[k] = sv
		}
	}
	return out
}
