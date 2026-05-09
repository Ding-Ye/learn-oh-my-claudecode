package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeJSON is a tiny helper used across the loader tests so each test
// can spell its fixture inline without bringing in a fixtures helper
// package. mkdirAll is forgiving — calling twice for the same dir is a
// no-op.
func writeJSON(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

// isolatedHome redirects $HOME to a temp dir for the duration of the
// test. Without it Load would consult the developer's real
// ~/.config/claude-omc/config.json and the test would be machine-dependent.
func isolatedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Guard against env tier vars leaking from the host shell. Each test
	// that wants OMC_MODEL_* set will Setenv it explicitly.
	t.Setenv("OMC_MODEL_HIGH", "")
	t.Setenv("OMC_MODEL_MEDIUM", "")
	t.Setenv("OMC_MODEL_LOW", "")
	t.Setenv("OMC_DISABLE_TOOLS", "")
	return home
}

func TestLoadDefaultsWhenNoFiles(t *testing.T) {
	_ = isolatedHome(t)
	work := t.TempDir() // empty — no .claude/omc.json

	cfg, err := Load(work)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := DefaultConfig()
	if got := cfg.Agents["architect"].Model; got != want.Agents["architect"].Model {
		t.Errorf("architect model: got %q, want %q", got, want.Agents["architect"].Model)
	}
	if !cfg.Features.ParallelExecution {
		t.Errorf("Features.ParallelExecution: got false, want true (default)")
	}
	if !cfg.MCPServers["exa"].Enabled {
		t.Errorf("mcpServers.exa.enabled: got false, want true (default)")
	}
}

func TestLoadProjectOverridesUser(t *testing.T) {
	home := isolatedHome(t)
	work := t.TempDir()

	writeJSON(t, filepath.Join(home, ".config", "claude-omc"), "config.json", `{
		"agents": {
			"executor": { "model": "user-executor" },
			"writer":   { "model": "user-writer" }
		},
		"features": { "lspTools": false }
	}`)

	writeJSON(t, filepath.Join(work, ".claude"), "omc.json", `{
		"agents": {
			"executor": { "model": "project-executor" }
		},
		"features": { "parallelExecution": false }
	}`)

	cfg, err := Load(work)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// project wins on conflict
	if got := cfg.Agents["executor"].Model; got != "project-executor" {
		t.Errorf("executor model: got %q, want project-executor", got)
	}
	// user wins where project is silent
	if got := cfg.Agents["writer"].Model; got != "user-writer" {
		t.Errorf("writer model: got %q, want user-writer", got)
	}
	// both layers contribute to features
	if cfg.Features.LSPTools {
		t.Errorf("Features.LSPTools: got true, want false (user)")
	}
	if cfg.Features.ParallelExecution {
		t.Errorf("Features.ParallelExecution: got true, want false (project)")
	}
}

func TestEnvOverridesAll(t *testing.T) {
	home := isolatedHome(t)
	work := t.TempDir()

	// Project file pins one HIGH-tier agent ("planner") to the
	// fallback string explicitly — env overlay should still rewrite it
	// because it's on the tier default. Another agent ("executor") is
	// pinned to a non-tier string; env should leave it alone.
	writeJSON(t, filepath.Join(work, ".claude"), "omc.json", `{
		"agents": {
			"executor": { "model": "manually-pinned-executor" }
		}
	}`)
	// User file is empty / absent — implicit by not writing it.
	_ = home

	t.Setenv("OMC_MODEL_HIGH", "opus-test")

	cfg, err := Load(work)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := cfg.Agents["planner"].Model; got != "opus-test" {
		t.Errorf("planner (HIGH tier default) should be rewritten: got %q, want opus-test", got)
	}
	if got := cfg.Agents["architect"].Model; got != "opus-test" {
		t.Errorf("architect (HIGH tier default) should be rewritten: got %q, want opus-test", got)
	}
	if got := cfg.Agents["executor"].Model; got != "manually-pinned-executor" {
		t.Errorf("executor (manually pinned) should NOT be rewritten: got %q", got)
	}
}

func TestLoadReturnsErrorOnMalformedJSON(t *testing.T) {
	_ = isolatedHome(t)
	work := t.TempDir()

	// Project file with a stray comma — invalid JSON.
	writeJSON(t, filepath.Join(work, ".claude"), "omc.json", `{
		"agents": { "executor": { "model": "x", } }
	}`)

	_, err := Load(work)
	if err == nil {
		t.Fatalf("Load: expected error on malformed JSON, got nil")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("error should mention parse failure, got: %v", err)
	}
}
