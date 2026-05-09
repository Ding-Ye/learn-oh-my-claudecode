// Package main implements the Chapter 2 prompt loader: agent system prompts
// live in Markdown files with YAML frontmatter (`agents/<name>.md`), get
// embedded into the binary at build time via //go:embed, and are returned
// to callers as a plain string body — frontmatter stripped, name validated.
//
// This file owns the Loader type. Companion files:
//
//   - validate.go  — the `^[a-z0-9-]+$` name guard.
//   - main.go      — a small demo that loads "architect" and shows what a
//                    rejection looks like.
//
// Upstream lineage: src/agents/utils.ts L83-L131 (loadAgentPrompt). The Go
// port collapses upstream's dual build-time/runtime branch into a single
// embed.FS read because Go's `embed` package serves both `go run` and
// `go build` from the same code path.
package main

import (
	"embed"
	"errors"
	"io/fs"
	"regexp"
)

// ErrInvalidName is returned when an agent name fails the name policy.
// Callers SHOULD surface this verbatim (do not convert to "not found")
// because it indicates a security-relevant input — typically a path
// traversal attempt like "../../../etc/passwd".
var ErrInvalidName = errors.New("invalid agent name (must match ^[a-z0-9-]+$)")

// ErrAgentNotFound is returned when the agent name is well-formed but no
// `<name>.md` file exists under the embed root. This is the benign case:
// callers can safely retry with a different name, log it, or fall through
// to a default prompt.
var ErrAgentNotFound = errors.New("agent prompt not found")

// frontmatterPattern matches a leading YAML frontmatter block plus any
// blank lines that immediately follow it.
//
// Anatomy of the regex:
//
//	^---\n          opening fence at the very start of the file
//	(?s).*?         any characters including newlines, lazy
//	\n---\n         closing fence followed by a newline
//	\s*             any trailing whitespace (so the body starts at content)
//
// `(?s)` enables DOTALL so `.` crosses newlines; the lazy quantifier `*?`
// is critical — without it, the regex would greedily consume up to the
// LAST `\n---\n` in the document if the body itself happened to contain
// a horizontal rule. The trailing `\s*` mirrors upstream's
// `^---[\s\S]*?---\s*([\s\S]*)$` (src/agents/utils.ts L46-L49) and
// keeps the returned body free of the blank line that authors usually
// leave between the frontmatter and the first heading.
var frontmatterPattern = regexp.MustCompile(`^---\n(?s).*?\n---\n\s*`)

// Loader reads agent prompts out of an embed.FS rooted at a configurable
// directory. It is intentionally tiny — five lines of state and one
// public method — because everything interesting happens in the regex
// and the validator.
type Loader struct {
	fs   embed.FS
	root string
}

// New constructs a Loader.
//
//	l := New(embeddedFS, "agents")
//
// The root argument is the directory inside the embed where prompt files
// live (e.g. "agents" matches files added via `//go:embed agents`). It is
// kept as an explicit parameter rather than a constant so the test suite
// (and any host program with a different layout) can point the loader at
// a different subtree without touching the loader's source.
func New(fs embed.FS, root string) *Loader {
	return &Loader{fs: fs, root: root}
}

// Load returns the prompt body for the given agent name.
//
// Steps, in strict order:
//
//  1. Validate name against `^[a-z0-9-]+$`.  Any failure → ErrInvalidName.
//     This is what stops "../etc/passwd" before it ever becomes a path.
//  2. Build the embed-relative path "<root>/<name>.md" using string
//     concatenation only — never filepath.Join, never path traversal.
//  3. Read via embed.FS. fs.ErrNotExist → ErrAgentNotFound; any other
//     filesystem error is wrapped and returned.
//  4. Strip a leading YAML frontmatter block via frontmatterPattern.
//     If the file has no frontmatter, the regex does not match and the
//     full body is returned verbatim — a deliberate choice (s01 fixtures
//     and ad-hoc files just work).
//
// The function performs no I/O outside the embed.FS; concurrent calls are
// safe because embed.FS is read-only.
func (l *Loader) Load(name string) (string, error) {
	if err := validateName(name); err != nil {
		return "", err
	}

	// Plain string concatenation, NOT filepath.Join: embed.FS uses forward
	// slashes on every platform, and Join would also helpfully clean
	// "..", which would re-introduce the very traversal we just rejected.
	relPath := l.root + "/" + name + ".md"

	data, err := l.fs.ReadFile(relPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", ErrAgentNotFound
		}
		return "", err
	}

	body := frontmatterPattern.ReplaceAllString(string(data), "")
	return body, nil
}
