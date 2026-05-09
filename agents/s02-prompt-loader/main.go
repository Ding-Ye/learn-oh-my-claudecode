package main

import (
	"embed"
	"errors"
	"fmt"
)

// embedded is the build-time bundle of agent prompt files. The directive
// below tells `go build` and `go run` to copy every file under ./agents
// into the binary; at runtime we read them through embed.FS without
// touching the host filesystem.
//
// Why use //go:embed instead of os.ReadFile? Three reasons:
//   1. The binary becomes self-contained — copy it anywhere and the
//      prompts come along.
//   2. The "where do these files live in production" question vanishes —
//      `embed.FS` is platform- and packaging-agnostic.
//   3. Reads are zero-allocation after the first call (the bytes already
//      live in the binary's read-only data segment).
//
//go:embed agents
var embedded embed.FS

func main() {
	loader := New(embedded, "agents")

	// Happy path: load architect.md, strip frontmatter, print first line.
	body, err := loader.Load("architect")
	if err != nil {
		fmt.Printf("unexpected error loading architect: %v\n", err)
		return
	}
	fmt.Printf("architect prompt (%d bytes), first line: %q\n",
		len(body), firstLine(body))

	// Path-traversal rejection: validateName must refuse "../" before any
	// I/O happens. We assert ErrInvalidName, not ErrAgentNotFound — the
	// difference is security-relevant.
	_, err = loader.Load("../../../etc/passwd")
	fmt.Printf("path traversal rejected: %v\n", errors.Is(err, ErrInvalidName))

	// Unknown but well-formed name: ErrAgentNotFound.
	_, err = loader.Load("nonexistent")
	fmt.Printf("unknown agent surfaced as: %v\n", errors.Is(err, ErrAgentNotFound))

	// All registered prompts, sorted (uses fs.ReadDir under the hood).
	fmt.Printf("embedded agents: %v\n", listAgents(embedded, "agents"))
}

// firstLine returns the substring up to (but not including) the first
// newline; if there is no newline the whole string is returned. Caps the
// result at 80 chars so demo output stays compact.
func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			s = s[:i]
			break
		}
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

// listAgents walks the embed root and returns the .md basenames in the
// order embed.FS reports them (which is alphabetical for our fixture).
// This is here purely so the demo can show "what is loadable", which is
// the closest a Go program comes to upstream's `__AGENT_PROMPTS__` map.
func listAgents(fsys embed.FS, root string) []string {
	entries, err := fsys.ReadDir(root)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if len(name) > 3 && name[len(name)-3:] == ".md" {
			names = append(names, name[:len(name)-3])
		}
	}
	return names
}
