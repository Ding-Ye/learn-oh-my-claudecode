package main

import "sort"

// Registry maps agent names to their canonical Agent definitions.
// Concurrent reads are safe; concurrent registers are NOT (callers should
// populate the registry once at init and treat it as read-only thereafter).
type Registry struct {
	agents map[string]Agent
}

// New returns an empty registry ready for Register calls.
func New() *Registry {
	return &Registry{agents: make(map[string]Agent)}
}

// Register inserts an agent into the registry. If an agent with the same Name
// already exists, it is overwritten. We deliberately choose "overwrite" over
// "error on duplicate" to match the upstream Object-spread semantics in
// `src/agents/definitions.ts` L211-L260, where later assignments win.
func (r *Registry) Register(a Agent) {
	r.agents[a.Name] = a
}

// Get returns the agent registered under name and a boolean indicating
// whether it was present. The bool follows the standard Go map-lookup idiom:
// callers MUST check it before using the returned Agent (the zero-value is
// otherwise indistinguishable from a registered agent with empty fields).
func (r *Registry) Get(name string) (Agent, bool) {
	a, ok := r.agents[name]
	return a, ok
}

// Names returns all registered agent names in lexicographic order.
// Sorting makes the demo output deterministic and tests stable across map
// iteration orderings.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.agents))
	for n := range r.agents {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ResolveModel implements the four-fold priority chain from upstream
// src/agents/definitions.ts L289-L298:
//
//	override.model ?? envInheritModel ?? configuredModel ?? agent.Model ?? agent.DefaultModel
//
// Returns the first non-empty value; returns "" if all five are empty.
//
// Why five inputs and not one: each layer carries different intent.
//
//   - override:    a per-call request the caller knows wins over everything
//                  (e.g. CLI flag --model=haiku for one specific run).
//   - envInherit:  the OMC_ROUTING_FORCE_INHERIT escape hatch — when set, the
//                  parent process forces every child to share its tier.
//   - configured:  whatever the user set in `~/.config/claude-omc/config.jsonc`
//                  (loaded in s04).
//   - agent.Model: the agent author's recommended tier (declared in
//                  agents/<name>.md frontmatter, read in s02).
//   - agent.DefaultModel: the explicit fallback tier — usually equal to
//                  agent.Model, but lets an agent declare "if my preferred
//                  tier is gone, use this instead" without losing the hint.
//
// Upstream stops at four (it does not consult `defaultModel` here; that field
// is exposed separately to the Claude Agent SDK as a sibling to `model`). We
// fold it in as a fifth fallback so a single-call lookup yields the most
// useful non-empty answer; if you need the upstream behavior verbatim, pass
// agent.DefaultModel as "" and read it from agent yourself.
func ResolveModel(a Agent, override, envInherit, configured string) string {
	for _, candidate := range []string{override, envInherit, configured, a.Model, a.DefaultModel} {
		if candidate != "" {
			return candidate
		}
	}
	return ""
}
