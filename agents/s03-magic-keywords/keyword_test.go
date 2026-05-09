package main

import (
	"strings"
	"testing"
)

// TestProcessTriggersUltrawork — the canonical happy path. An imperative
// "ultrawork ..." prompt fires UltraworkEnhancement, which prepends the
// directive line and strips the trigger word from the body.
func TestProcessTriggersUltrawork(t *testing.T) {
	in := "ultrawork build a server"
	out := Process(in, "executor", "claude-opus-4-7", BuiltIns)

	if !strings.HasPrefix(out, "[ULTRAWORK MODE") {
		t.Fatalf("expected ULTRAWORK directive prefix; got %q", out)
	}
	// Trigger should not appear inline anymore (removeTriggerWords ran).
	if strings.Contains(strings.ToLower(out[len("[ULTRAWORK MODE — PARALLEL AGENT ORCHESTRATION]\n"):]), "ultrawork") {
		t.Fatalf("ultrawork trigger leaked into body: %q", out)
	}
	if !strings.Contains(out, "build a server") {
		t.Fatalf("expected user payload preserved; got %q", out)
	}
}

// TestProcessSkipsInformationalKorean — the multilingual filter. A Korean
// "ultrawork이 뭐야?" ("what is ultrawork?") must NOT fire any keyword. This
// pins the Korean branch of isInformationalIntent.
func TestProcessSkipsInformationalKorean(t *testing.T) {
	in := "ultrawork이 뭐야?"
	out := Process(in, "executor", "claude-opus-4-7", BuiltIns)
	if out != in {
		t.Fatalf("expected Korean informational prompt to pass through unchanged; got %q", out)
	}
}

// TestProcessIgnoresKeywordInsideCodeBlock — the code-block filter. A trigger
// living inside ``` … ``` is invisible to the matcher because removeCodeBlocks
// strips it before hasActionableTrigger sees it.
func TestProcessIgnoresKeywordInsideCodeBlock(t *testing.T) {
	in := "```ultrawork``` is a keyword"
	out := Process(in, "executor", "claude-opus-4-7", BuiltIns)
	if out != in {
		t.Fatalf("expected code-block-only trigger to pass through unchanged; got %q", out)
	}
}

// TestProcessAppliesMultipleKeywordsInOrder — the iteration-order test. A
// prompt that fires two keywords (ultrawork *and* search) must show the
// effects of both. Both Actions prepend a directive line, so each later
// keyword wraps the result of every earlier keyword. After Process runs:
//
//	search( ultrawork( "ultrawork search OAuth flow" ) )
//	  = search( "[ULTRAWORK MODE — …]\nsearch OAuth flow" )
//	  = "[SEARCH MODE — …]\n[ULTRAWORK MODE — …]\nsearch OAuth flow"
//
// So the LATER keyword's directive sits at the OUTERMOST position (line 0)
// — it wrapped the earlier keyword's output. We assert (a) both directives
// are present, (b) the search directive comes first (it ran second and so
// is the outermost wrapper), and (c) the user payload survives.
func TestProcessAppliesMultipleKeywordsInOrder(t *testing.T) {
	in := "ultrawork search OAuth flow"
	out := Process(in, "executor", "claude-opus-4-7", BuiltIns)

	ultraIdx := strings.Index(out, "[ULTRAWORK MODE")
	searchIdx := strings.Index(out, "[SEARCH MODE")
	if ultraIdx == -1 {
		t.Fatalf("expected ULTRAWORK directive present; got %q", out)
	}
	if searchIdx == -1 {
		t.Fatalf("expected SEARCH directive present; got %q", out)
	}
	// search ran second → it prepended to ultrawork's output, so the
	// search directive sits at offset 0 and ultrawork sits later.
	if searchIdx > ultraIdx {
		t.Fatalf("expected SEARCH directive (ran second, prepends) to precede ULTRAWORK in output; ultraIdx=%d searchIdx=%d out=%q", ultraIdx, searchIdx, out)
	}
	if !strings.Contains(out, "OAuth flow") {
		t.Fatalf("expected user payload preserved; got %q", out)
	}
}

// TestProcessLeavesPromptUnchangedWhenNoTrigger — sanity. A prompt with
// none of the trigger words must come back byte-for-byte. This guards
// against accidental whitespace mutation by Action implementations that
// run unconditionally.
func TestProcessLeavesPromptUnchangedWhenNoTrigger(t *testing.T) {
	in := "please refactor the http module"
	out := Process(in, "executor", "claude-opus-4-7", BuiltIns)
	if out != in {
		t.Fatalf("expected pass-through; got %q", out)
	}
}
