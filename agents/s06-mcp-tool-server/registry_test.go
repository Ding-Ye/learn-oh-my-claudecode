package main

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

// TestRegisterAndInvoke covers the happy path: register one tool,
// invoke it, see the handler's output round-trip back through the
// registry. The handler captures `args` so we can also assert that the
// JSON payload reaches it byte-for-byte.
func TestRegisterAndInvoke(t *testing.T) {
	r := New()
	var seen json.RawMessage
	r.Register(Tool{
		Name:        "echo",
		Description: "echoes back its args",
		Category:    CategoryLSP,
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			seen = args
			return "ok:" + string(args), nil
		},
	})

	out, err := r.Invoke(context.Background(), "echo", json.RawMessage(`{"x":1}`))
	if err != nil {
		t.Fatalf("Invoke: unexpected error: %v", err)
	}
	if out != `ok:{"x":1}` {
		t.Errorf("output: got %q, want %q", out, `ok:{"x":1}`)
	}
	if string(seen) != `{"x":1}` {
		t.Errorf("handler did not receive payload: got %q", string(seen))
	}

	// Names() should list exactly the registered tool.
	if got := r.Names(); !reflect.DeepEqual(got, []string{"echo"}) {
		t.Errorf("Names: got %v, want [echo]", got)
	}
}

// TestInvokeReturnsErrUnknownTool checks the missing-tool path. Even
// when the registry is empty, the error must be the exported sentinel
// (errors.Is comparable) and the returned string must be empty so
// callers can rely on `out == ""` distinguishing "no result" from "an
// empty handler result."
func TestInvokeReturnsErrUnknownTool(t *testing.T) {
	r := New()

	out, err := r.Invoke(context.Background(), "nonexistent", json.RawMessage(`{}`))
	if !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("error: want ErrUnknownTool, got %v", err)
	}
	if out != "" {
		t.Errorf("output: want empty string on error path, got %q", out)
	}

	// Same assertion when at least one *other* tool is registered —
	// presence of unrelated tools must not mask the unknown-tool
	// sentinel.
	r.Register(Tool{Name: "real", Category: CategoryAST,
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) { return "", nil }})
	if _, err := r.Invoke(context.Background(), "still-not-real", nil); !errors.Is(err, ErrUnknownTool) {
		t.Errorf("with one registered tool: want ErrUnknownTool, got %v", err)
	}
}

// TestParseDisabledHandlesEmpty exercises the three cheap cases that
// the parser must get right before anything else: empty string, all
// whitespace, and a token surrounded by whitespace. Each subtest is
// independent so a future failure points at the exact symptom.
func TestParseDisabledHandlesEmpty(t *testing.T) {
	t.Run("empty string returns empty map", func(t *testing.T) {
		got := ParseDisabled("")
		if len(got) != 0 {
			t.Errorf("empty input should produce empty map; got %v", got)
		}
		// Must not be nil so callers can read keys without a panic
		// guard. (`map[Category]bool(nil)[k]` returns false safely in
		// Go, but we still document the contract: it's a non-nil
		// empty map.)
		if got == nil {
			t.Error("ParseDisabled(\"\") returned nil; want non-nil empty map")
		}
	})

	t.Run("only whitespace returns empty map", func(t *testing.T) {
		got := ParseDisabled("   \t  ")
		if len(got) != 0 {
			t.Errorf("whitespace-only input should produce empty map; got %v", got)
		}
	})

	t.Run("trims surrounding whitespace per token", func(t *testing.T) {
		got := ParseDisabled("  lsp ,  python  , memory")
		want := map[Category]bool{
			CategoryLSP:    true,
			CategoryPython: true,
			CategoryMemory: true,
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("parse: got %v, want %v", got, want)
		}
	})

	t.Run("ignores empty tokens between commas", func(t *testing.T) {
		// `OMC_DISABLE_TOOLS=,lsp,,memory,` should yield {lsp, memory}
		// — empty tokens are not categories, just trailing-comma noise.
		got := ParseDisabled(",lsp,,memory,")
		want := map[Category]bool{CategoryLSP: true, CategoryMemory: true}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("parse: got %v, want %v", got, want)
		}
	})
}

// TestRegistryFiltersDisabledCategories verifies that WithDisabled
// returns a *view*: the underlying tools map is shared (Names() on the
// view sees the same entries) but Invoke gates on the disabled set.
// This is the central abstraction the chapter teaches.
func TestRegistryFiltersDisabledCategories(t *testing.T) {
	r := New()
	r.Register(Tool{Name: "lsp.def", Category: CategoryLSP,
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) { return "lsp-out", nil }})
	r.Register(Tool{Name: "py.eval", Category: CategoryPython,
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) { return "py-out", nil }})
	r.Register(Tool{Name: "ast.search", Category: CategoryAST,
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) { return "ast-out", nil }})

	view := r.WithDisabled(ParseDisabled("lsp,ast"))

	// The view sees all three names — disabling does not remove.
	got := view.Names()
	want := []string{"ast.search", "lsp.def", "py.eval"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Names: got %v, want %v", got, want)
	}

	// The original registry was not mutated by WithDisabled. (Disable
	// state lives on the view, not the source.)
	if r.IsDisabled(CategoryLSP) {
		t.Error("WithDisabled should not retroactively disable categories on the source registry")
	}

	// IsDisabled exposes the view's gate honestly.
	if !view.IsDisabled(CategoryLSP) {
		t.Error("view should report CategoryLSP as disabled")
	}
	if view.IsDisabled(CategoryPython) {
		t.Error("view should NOT report CategoryPython as disabled")
	}
}

// TestInvokeOnDisabledToolReturnsErrCategoryDisabled is the negative
// twin of TestRegistryFiltersDisabledCategories: invoking a tool whose
// Category is in the disabled set must return the sentinel without
// running the handler. We assert "without running" by capturing a flag
// inside the handler and checking it stays false.
func TestInvokeOnDisabledToolReturnsErrCategoryDisabled(t *testing.T) {
	handlerRan := false
	r := New()
	r.Register(Tool{
		Name:     "lsp.def",
		Category: CategoryLSP,
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			handlerRan = true
			return "should-not-be-returned", nil
		},
	})
	view := r.WithDisabled(ParseDisabled("lsp"))

	out, err := view.Invoke(context.Background(), "lsp.def", json.RawMessage(`{}`))
	if !errors.Is(err, ErrCategoryDisabled) {
		t.Fatalf("error: want ErrCategoryDisabled, got %v", err)
	}
	if out != "" {
		t.Errorf("output: want empty string when disabled, got %q", out)
	}
	if handlerRan {
		t.Error("handler ran even though its category was disabled — short-circuit failed")
	}

	// And as a final cross-check: an enabled tool on the SAME view
	// still works. (This guards against a regression where a single
	// disabled category accidentally disables every tool.)
	r.Register(Tool{
		Name:     "py.eval",
		Category: CategoryPython,
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) { return "ok", nil },
	})
	if out, err := view.Invoke(context.Background(), "py.eval", nil); err != nil || out != "ok" {
		t.Errorf("py.eval after lsp disable: got out=%q err=%v, want out=ok err=nil", out, err)
	}
}
