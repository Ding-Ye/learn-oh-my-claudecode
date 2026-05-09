package main

import "testing"

// TestDeepMergeIgnoresProtoPollutionKey verifies the security-critical
// guard at merge.go::reservedKeys. The input contains a `__proto__`
// payload styled exactly like the JS prototype-pollution exploit; the
// merged map must not contain that key.
func TestDeepMergeIgnoresProtoPollutionKey(t *testing.T) {
	dst := map[string]any{
		"agents": map[string]any{
			"omc": map[string]any{"model": "claude-opus-4-7"},
		},
	}
	src := map[string]any{
		"agents": map[string]any{
			"omc": map[string]any{"model": "user-pick"},
		},
		"__proto__": map[string]any{
			"polluted": true,
		},
		"constructor": map[string]any{
			"polluted": true,
		},
		"prototype": map[string]any{
			"polluted": true,
		},
	}

	out := deepMerge(dst, src)

	// 1. Reserved keys must not appear in the merged map.
	for _, k := range []string{"__proto__", "constructor", "prototype"} {
		if _, ok := out[k]; ok {
			t.Errorf("deepMerge propagated reserved key %q (prototype-pollution risk)", k)
		}
	}

	// 2. Legitimate src wins as usual.
	agents, _ := out["agents"].(map[string]any)
	omc, _ := agents["omc"].(map[string]any)
	if got := omc["model"]; got != "user-pick" {
		t.Errorf("omc model: got %v, want user-pick", got)
	}
}

func TestDeepMergeRecursesIntoNestedMaps(t *testing.T) {
	dst := map[string]any{
		"features": map[string]any{
			"parallelExecution": true,
			"lspTools":          true,
		},
	}
	src := map[string]any{
		"features": map[string]any{
			"lspTools": false,
		},
	}
	out := deepMerge(dst, src)
	feats, _ := out["features"].(map[string]any)

	// dst-only key survives the recursion.
	if got := feats["parallelExecution"]; got != true {
		t.Errorf("parallelExecution: got %v, want true", got)
	}
	// src key overwrites.
	if got := feats["lspTools"]; got != false {
		t.Errorf("lspTools: got %v, want false", got)
	}
}

func TestDeepMergeNilSrcValuePreservesDst(t *testing.T) {
	dst := map[string]any{"keep": "this"}
	src := map[string]any{"keep": nil}

	out := deepMerge(dst, src)
	if got := out["keep"]; got != "this" {
		t.Errorf("nil src should preserve dst: got %v, want this", got)
	}
}
