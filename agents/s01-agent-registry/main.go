package main

import "fmt"

func main() {
	r := New()
	r.Register(Agent{
		Name:        "architect",
		Description: "Code-analysis lead: analyzes code, debugs, verifies.",
		Prompt:      "You are an architect...",
		Model:       "claude-opus-4-7",
	})
	r.Register(Agent{
		Name:         "executor",
		Description:  "Implementation lane: applies edits and runs builds.",
		Prompt:       "You are an executor...",
		Model:        "claude-sonnet-4-6",
		DefaultModel: "claude-sonnet-4-6",
	})

	// Look up
	arch, ok := r.Get("architect")
	fmt.Printf("architect found=%v model=%s\n", ok, arch.Model)

	// Tier resolution: override beats agent.Model
	resolved := ResolveModel(arch, "claude-haiku-4-5", "", "")
	fmt.Printf("with override: %s\n", resolved)

	// Tier resolution: agent.Model is the fallback
	resolved = ResolveModel(arch, "", "", "")
	fmt.Printf("without override: %s\n", resolved)

	// Names() sorted
	fmt.Printf("registered: %v\n", r.Names())
}
