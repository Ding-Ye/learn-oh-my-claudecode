package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// main is the chapter's runnable demo. It registers two tools (one in
// the lsp category, one in python), disables `lsp` via the
// upstream-shaped env-var string, and exercises three behaviors:
//
//  1. Invoking the python tool succeeds (the registry is otherwise
//     enabled).
//  2. Invoking the lsp tool returns ErrCategoryDisabled — no handler
//     work, just a sentinel error.
//  3. Names() reports BOTH tools, with the disabled one labeled, so a
//     human reading the captured fixture can see the "registered but
//     filtered" distinction the WithDisabled view encodes.
//
// The output format mirrors s05's compact one-line-per-event style so
// testdata/expected.txt stays small enough to render in a fenced block.
func main() {
	r := New()

	// Tool 1: a fake "lsp.find_definition" — the kind of static
	// analysis tool the upstream LSP package provides.
	r.Register(Tool{
		Name:        "lsp.find_definition",
		Description: "Locate the definition of a symbol via LSP textDocument/definition.",
		Category:    CategoryLSP,
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			// In a real port this would call out to a language server;
			// the demo just echoes the payload so the test surface stays
			// pure and timeout-free.
			return fmt.Sprintf("lsp.find_definition called with args=%s", string(args)), nil
		},
	})

	// Tool 2: a fake "python.repl" — the kind of side-effecting tool
	// the upstream `python_repl` exposes for in-sandbox script eval.
	r.Register(Tool{
		Name:        "python.repl",
		Description: "Evaluate a Python snippet inside the OMC sandbox.",
		Category:    CategoryPython,
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			return fmt.Sprintf("python.repl evaluated args=%s", string(args)), nil
		},
	})

	// Disable just the LSP category. ParseDisabled accepts the literal
	// upstream env-var format ("lsp,python,memory") so this line works
	// equally well with `os.Getenv("OMC_DISABLE_TOOLS")` in production.
	view := r.WithDisabled(ParseDisabled("lsp"))

	ctx := context.Background()

	// Case 1: python is enabled -> handler runs, output is the
	// echoed-args string.
	fmt.Println("== invoke python.repl (enabled) ==")
	out, err := view.Invoke(ctx, "python.repl", json.RawMessage(`{"code":"print(1+1)"}`))
	fmt.Printf("out=%q err=%s\n\n", out, summarize(err))

	// Case 2: lsp is in the disabled set -> registry short-circuits and
	// returns the sentinel without invoking the handler. Note that
	// `out` is empty — handlers don't run on disabled tools.
	fmt.Println("== invoke lsp.find_definition (disabled via OMC_DISABLE_TOOLS=lsp) ==")
	out, err = view.Invoke(ctx, "lsp.find_definition", json.RawMessage(`{"symbol":"main"}`))
	fmt.Printf("out=%q err=%s\n\n", out, summarize(err))

	// Case 3: Names() reflects what's registered, not what's callable.
	// Both tools appear; the LSP one is annotated [disabled] so the
	// fixture line shows the WithDisabled view's gate is purely runtime.
	fmt.Println("== Names() (disabled tools annotated) ==")
	for _, name := range view.Names() {
		// We re-fetch the tool to ask its category — Names() returns
		// just strings to keep the public API minimal.
		t := view.tools[name]
		flag := ""
		if view.IsDisabled(t.Category) {
			flag = " [disabled]"
		}
		fmt.Printf("- %s (category=%s)%s\n", name, t.Category, flag)
	}
}

// summarize collapses sentinel errors to a stable short string so the
// captured fixture survives Go-version-dependent error wrapping
// changes. errors.Is is the canonical way to compare wrapped errors;
// we use it explicitly so the demo doubles as a usage example.
func summarize(err error) string {
	switch {
	case err == nil:
		return "<nil>"
	case errors.Is(err, ErrCategoryDisabled):
		return "category-disabled"
	case errors.Is(err, ErrUnknownTool):
		return "unknown-tool"
	default:
		return err.Error()
	}
}

// init is a tiny safety net: if the user runs the demo with
// OMC_DISABLE_TOOLS set in their shell, we deliberately ignore it so
// the captured fixture stays deterministic. Comment this out to see
// how external env vars would compose with our hard-coded "lsp"
// disable above.
func init() {
	_ = os.Unsetenv("OMC_DISABLE_TOOLS")
}
