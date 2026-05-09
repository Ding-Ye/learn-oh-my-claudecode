package main

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
)

// ErrUnknownTool is returned by Invoke when the requested tool name is
// not present in the registry. We expose it as a sentinel so callers
// can `errors.Is(err, ErrUnknownTool)` without string-matching.
var ErrUnknownTool = errors.New("mcp-tool-server: unknown tool")

// ErrCategoryDisabled is returned by Invoke when a tool exists in the
// registry but its Category appears in the disabled set. The tool is
// still discoverable via Names() — disabling is a runtime gate, not a
// removal — so a UI can render disabled tools greyed-out without
// fabricating ghost entries.
var ErrCategoryDisabled = errors.New("mcp-tool-server: category disabled")

// Registry maps tool names to Tool values plus a per-category disabled
// set. Both fields are non-nil after `New()`; callers must not mutate
// the maps directly.
//
// The registry itself is intentionally *not* thread-safe. Upstream
// builds the tool list at module-init time and never mutates it again;
// we mirror that contract. If you need concurrent registration, wrap
// Register/Invoke in a sync.RWMutex at the call site — the registry
// stays simple.
type Registry struct {
	tools    map[string]Tool
	disabled map[Category]bool
}

// New returns a fresh Registry with empty tool and disabled maps.
// Returning a pointer is deliberate: subsequent Register calls mutate
// the receiver, and a value copy would silently lose those mutations.
func New() *Registry {
	return &Registry{
		tools:    make(map[string]Tool),
		disabled: make(map[Category]bool),
	}
}

// Register adds a tool to the registry. A second Register with the
// same Name overwrites the previous entry — the chapter's lookup
// semantics match s01's "register-duplicate-overwrites" decision so
// students are not asked to memorize a different rule per chapter.
//
// Registration does NOT fail on an unknown Category. The upstream
// dispatch is permissive (any string the user types in OMC_DISABLE_TOOLS
// is silently ignored if it doesn't match a known group), and our port
// follows suit: an ad-hoc Category like `"experimental"` simply means
// "no env-var disable knob targets this tool yet."
func (r *Registry) Register(t Tool) {
	r.tools[t.Name] = t
}

// WithDisabled returns a *filtered view* of the registry: same tool
// table, but a fresh disabled set drawn from `cats`. The returned
// pointer shares the underlying tools map with the receiver — callers
// who want full isolation should construct a brand-new Registry.
//
// The "view" framing matches the upstream pattern in
// `omc-tools-server.ts` where `parseDisabledGroups()` is called once at
// startup and the resulting set is captured by the dispatcher closure.
// Returning a Registry pointer (rather than a freestanding filter
// function) keeps the call site one-line ergonomic:
//
//	r := New().Register(...).WithDisabled(ParseDisabled(os.Getenv("OMC_DISABLE_TOOLS")))
//
// Passing a nil `cats` is safe and produces an effectively-empty
// disabled set.
func (r *Registry) WithDisabled(cats map[Category]bool) *Registry {
	out := &Registry{
		tools:    r.tools,
		disabled: make(map[Category]bool, len(cats)),
	}
	for k, v := range cats {
		if v {
			out.disabled[k] = true
		}
	}
	return out
}

// Invoke dispatches to the named tool. Three error paths, in priority
// order: missing tool (ErrUnknownTool), disabled category
// (ErrCategoryDisabled), or whatever the handler returns. We check
// "exists" before "disabled" so a typo doesn't get masked by an
// unrelated disable flag.
//
// The handler receives the parent ctx unchanged — we do not impose a
// per-tool timeout here. That decision belongs to the transport layer
// or the caller; chained `context.WithTimeout` keeps the registry
// agnostic.
func (r *Registry) Invoke(ctx context.Context, name string, args json.RawMessage) (string, error) {
	t, ok := r.tools[name]
	if !ok {
		return "", ErrUnknownTool
	}
	if r.disabled[t.Category] {
		return "", ErrCategoryDisabled
	}
	if t.Handler == nil {
		// Defensive: a Tool without a handler is a programming bug, but
		// returning an error beats panicking inside the dispatch loop.
		return "", errors.New("mcp-tool-server: tool has nil handler")
	}
	return t.Handler(ctx, args)
}

// Names returns the registered tool names in lexicographic order. We
// sort so that tests, demos, and snapshot fixtures all see a stable
// listing regardless of map-iteration order. Disabled tools are
// included — Names() reflects what is *registered*, not what is
// *callable*. Callers that want the callable subset can post-filter
// with a Category check themselves.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.tools))
	for name := range r.tools {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// IsDisabled reports whether the given category is currently filtered
// out by this registry view. Exposed mostly so the demo can label
// Names() output with a "[disabled]" suffix without re-implementing
// the lookup.
func (r *Registry) IsDisabled(cat Category) bool {
	return r.disabled[cat]
}
