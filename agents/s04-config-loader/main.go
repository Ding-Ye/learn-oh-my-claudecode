package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// main is the chapter's runnable demo. It walks through the four merge
// stages and, after each one, prints the four fields most useful for
// inspection — keeping the captured stdout tight so testdata/expected.txt
// is a real drift detector instead of a 200-line wall.
//
// Stages:
//
//  1. defaults  → DefaultConfig()
//  2. + user    → testdata/user.json
//  3. + project → testdata/project.json
//  4. + env     → OMC_MODEL_HIGH=opus-test
//
// Probes used at each stage:
//
//   - agents.executor.model   — overridden by both files, demonstrates
//     "project beats user".
//   - agents.architect.model  — never touched by files, demonstrates the
//     env tier overlay.
//   - features.parallelExecution — toggled by project.
//   - mcpServers.exa.enabled  — toggled by user.
func main() {
	probe := func(label string, cfg Config) {
		fmt.Printf("== %s ==\n", label)
		fmt.Printf("  agents.executor.model    = %s\n", cfg.Agents["executor"].Model)
		fmt.Printf("  agents.architect.model   = %s\n", cfg.Agents["architect"].Model)
		fmt.Printf("  features.parallelExec    = %t\n", cfg.Features.ParallelExecution)
		fmt.Printf("  mcpServers.exa.enabled   = %t\n", cfg.MCPServers["exa"].Enabled)
		fmt.Println()
	}

	// Stage 1 — pristine defaults.
	probe("defaults", DefaultConfig())

	// Stage 2 — defaults + user.json.
	dst, _ := configToMap(DefaultConfig())
	user, err := readLayer(filepath.Join("testdata", "user.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	dst = deepMerge(dst, user)
	cfgUser, _ := mapToConfig(dst)
	probe("defaults + user", cfgUser)

	// Stage 3 — + project.json (project beats user on conflict).
	project, err := readLayer(filepath.Join("testdata", "project.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	dst = deepMerge(dst, project)
	cfgProject, _ := mapToConfig(dst)
	probe("defaults + user + project", cfgProject)

	// Stage 4 — + env. Only HIGH-tier defaults still on the fallback
	// string get rewritten. architect was never touched by the files, so
	// it flips to opus-test. executor stays pinned at its project value.
	_ = os.Setenv("OMC_MODEL_HIGH", "opus-test")
	defer os.Unsetenv("OMC_MODEL_HIGH")
	cfgEnv := cfgProject
	applyEnvOverlay(&cfgEnv)
	probe("defaults + user + project + env(OMC_MODEL_HIGH=opus-test)", cfgEnv)
}
