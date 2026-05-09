package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Hook is one declarative process invocation. The shape mirrors upstream
// hooks/hooks.json verbatim: each entry has a Type ("command" is the only
// kind we care about), a shell command string, and an integer Timeout
// expressed in seconds in the JSON file. We carry the timeout as a
// time.Duration in memory so the dispatcher can hand it straight to
// context.WithTimeout — the JSON-vs-runtime conversion happens in
// UnmarshalJSON below.
type Hook struct {
	Type    string        `json:"type"`
	Command string        `json:"command"`
	Timeout time.Duration `json:"-"`
}

// hookWire is the on-disk shape. We keep it private so callers see only
// the Go-friendly Hook with Timeout as a Duration. The split matters
// because Go's encoding/json understands time.Duration as nanoseconds in
// a number — but the upstream manifest uses seconds, and we want to be
// faithful to that file format rather than invent a new one.
type hookWire struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

// UnmarshalJSON converts the on-disk seconds-int into a time.Duration so
// the public Hook struct stays ergonomic for callers. A timeout of 0 is
// treated as "no explicit timeout"; the dispatcher applies a default in
// that case so a typo can't hang forever.
func (h *Hook) UnmarshalJSON(data []byte) error {
	var w hookWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	h.Type = w.Type
	h.Command = w.Command
	h.Timeout = time.Duration(w.Timeout) * time.Second
	return nil
}

// Entry is one matcher group. Every event in the manifest is a slice of
// Entry: each entry says "if the event matches my Matcher, run all of
// these Hooks". A Matcher of "*" wildcards everything; otherwise we treat
// it as a substring test against the matcher target. Upstream uses
// matcher names like "Bash" (PermissionRequest) or "init" (SessionStart);
// substring matching is enough to honor those without a regex compiler.
type Entry struct {
	Matcher string `json:"matcher"`
	Hooks   []Hook `json:"hooks"`
}

// Manifest maps a lifecycle event name (UserPromptSubmit, SessionStart,
// PostToolUse, etc.) to its declared entries. We do NOT validate the
// event name set — upstream invents new events from time to time, and
// being permissive lets a project add custom event names without forking
// this file.
type Manifest map[string][]Entry

// manifestFile is the on-disk envelope. The upstream file has a top-level
// "description" string and a "hooks" object; we ignore the description
// because it's purely human commentary.
type manifestFile struct {
	Description string   `json:"description,omitempty"`
	Hooks       Manifest `json:"hooks"`
}

// LoadManifest reads a hooks JSON file and returns its parsed Manifest.
// The split between manifestFile (envelope) and Manifest (payload) means
// callers see a clean event→entries map without having to dereference a
// wrapper struct on every lookup.
func LoadManifest(path string) (Manifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var mf manifestFile
	if err := json.Unmarshal(raw, &mf); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if mf.Hooks == nil {
		mf.Hooks = Manifest{}
	}
	return mf.Hooks, nil
}
