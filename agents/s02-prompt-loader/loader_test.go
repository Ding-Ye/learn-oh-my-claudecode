package main

import (
	"embed"
	"errors"
	"strings"
	"testing"
)

// testFS is a second, test-only embed.FS rooted at testdata/. We embed it
// here (in the _test.go file) so the directory layout stays clean: the
// production embed (main.go) covers ./agents; the test embed covers
// ./testdata. The test fixture for "no frontmatter" lives at
// testdata/raw-body.md.
//
//go:embed testdata
var testFS embed.FS

// productionFS mirrors the //go:embed in main.go so individual tests can
// exercise the real fixtures without going through main(). Keeping a
// second copy here is fine — Go deduplicates embed bytes at link time.
//
//go:embed agents
var productionFS embed.FS

// TestLoadStripsFrontmatter — the load-bearing test. agents/architect.md
// begins with a 6-line YAML frontmatter block; after Load returns, the
// body must NOT contain any of the frontmatter keys, and MUST contain the
// "# Architect" body header that follows the closing fence.
func TestLoadStripsFrontmatter(t *testing.T) {
	l := New(productionFS, "agents")
	body, err := l.Load("architect")
	if err != nil {
		t.Fatalf("Load returned err=%v; want nil", err)
	}
	if strings.HasPrefix(body, "---") {
		t.Fatalf("frontmatter not stripped; body starts with %q", body[:clip(20, len(body))])
	}
	for _, leak := range []string{"name: architect", "model: opus", "disallowedTools:"} {
		if strings.Contains(body, leak) {
			t.Fatalf("frontmatter key leaked into body: %q", leak)
		}
	}
	if !strings.Contains(body, "# Architect") {
		t.Fatalf("expected body to contain '# Architect' header; got first 80 chars %q", body[:clip(80, len(body))])
	}
}

// TestLoadRejectsPathTraversal — the security test. A literal
// path-traversal payload must trip validateName BEFORE any embed.FS
// access, surfacing as ErrInvalidName (not ErrAgentNotFound). The
// distinction matters: it lets a host program's audit log distinguish
// "user mistyped" from "user attacked".
func TestLoadRejectsPathTraversal(t *testing.T) {
	l := New(productionFS, "agents")
	_, err := l.Load("../../../etc/passwd")
	if !errors.Is(err, ErrInvalidName) {
		t.Fatalf("Load(\"../../../etc/passwd\") err=%v; want ErrInvalidName", err)
	}
}

// TestLoadRejectsInvalidCharacters — the input-policy test. Spaces,
// uppercase, dots, and Unicode are all outside `^[a-z0-9-]+$` and must
// be refused with ErrInvalidName.
func TestLoadRejectsInvalidCharacters(t *testing.T) {
	l := New(productionFS, "agents")
	cases := []string{
		"Foo Bar",     // spaces + uppercase
		"agent.md",    // a dot — banned
		"a/b",         // a slash — banned
		"",            // empty
		"agent\x00",   // null byte
		"AGENT",       // uppercase only
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := l.Load(name)
			if !errors.Is(err, ErrInvalidName) {
				t.Fatalf("Load(%q) err=%v; want ErrInvalidName", name, err)
			}
		})
	}
}

// TestLoadReturnsErrAgentNotFoundForUnknownAgent — well-formed name, no
// matching file. This is the recoverable error case: callers may log it
// and fall back to a default prompt without security implications.
func TestLoadReturnsErrAgentNotFoundForUnknownAgent(t *testing.T) {
	l := New(productionFS, "agents")
	_, err := l.Load("nonexistent-agent")
	if !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("Load(\"nonexistent-agent\") err=%v; want ErrAgentNotFound", err)
	}
	// Sanity check: the well-formed-but-missing name must not be
	// confused with the malformed-name case.
	if errors.Is(err, ErrInvalidName) {
		t.Fatalf("got ErrInvalidName for a well-formed unknown name; lifecycle classification is wrong")
	}
}

// TestLoadReturnsRawBodyWhenNoFrontmatter — pins the regex's "leading
// fence required" semantics. testdata/raw-body.md has no YAML block, so
// frontmatterPattern must NOT match and Load must return the file
// verbatim. This is what guarantees ad-hoc files (without frontmatter)
// keep working as the loader's API gains features.
func TestLoadReturnsRawBodyWhenNoFrontmatter(t *testing.T) {
	l := New(testFS, "testdata")
	body, err := l.Load("raw-body")
	if err != nil {
		t.Fatalf("Load returned err=%v; want nil", err)
	}
	if !strings.HasPrefix(body, "# Raw body fixture") {
		t.Fatalf("expected verbatim body to start with '# Raw body fixture'; got %q", body[:clip(40, len(body))])
	}
	if strings.Contains(body, "---") {
		t.Fatalf("body unexpectedly contains '---' — regex misfired on no-frontmatter file")
	}
}

// clip is a small int-min helper used solely for slicing diagnostic
// output safely. Named `clip` to avoid colliding with Go 1.21+ builtin
// `min`, which would also work but produces a less search-friendly name.
func clip(a, b int) int {
	if a < b {
		return a
	}
	return b
}
