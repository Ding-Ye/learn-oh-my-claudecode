package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Store is the filesystem-backed task store. It is rooted at a single
// directory; all teams live as subdirectories under it. The struct is
// stateless beyond its root path — there is no in-memory cache, no
// lazy index, no warm-up step. Every read goes to disk; every write
// goes through writeAtomic. This is intentional and is the chapter's
// teaching point: a real distributed task queue can be built on
// nothing but `os.Rename` and `flock`.
//
// Layout:
//
//	root/
//	  <team>/
//	    tasks/
//	      <id>.json           ← Task record (UTF-8 JSON, 2-space indent)
//	      <id>.json.lock      ← flock target for the per-task critical section
//	      <id>.json.tmp       ← transient sibling created by writeAtomic
//
// Mirrors upstream's `taskFilePath(teamName, taskId, cwd)` helper
// (src/team/runtime.ts) which composes
// `<cwd>/.omc/state/team/<teamName>/tasks/<taskId>.json`. The Go store
// expects callers to pass the equivalent of `<cwd>/.omc/state/team`
// as `root`, keeping the path discipline identical.
type Store struct {
	root string
}

// NewStore constructs a Store rooted at the given directory. The
// directory is NOT created up front — it materializes lazily on first
// write via writeAtomic's MkdirAll. This keeps NewStore total: it
// never fails, never touches the filesystem, and never blocks. A
// caller that wants pre-creation can call os.MkdirAll(root, 0o755)
// themselves before invoking us.
func NewStore(root string) *Store {
	return &Store{root: root}
}

// taskPath composes the on-disk path for a single task. Kept private
// because the path discipline is an implementation detail; callers
// should reach tasks through Read / Write / List.
func (s *Store) taskPath(team, id string) string {
	return filepath.Join(s.root, team, "tasks", id+".json")
}

// Read returns the Task at <root>/<team>/tasks/<id>.json. Returns
// os.ErrNotExist (matchable with errors.Is) when the file is missing,
// so callers can distinguish "task does not exist" from "task is
// corrupt" without parsing the error message.
//
// The read is a plain os.ReadFile + json.Unmarshal — no locking, no
// retries. Any concurrent writer is serialized through withFileLock at
// the higher level (claim.go), and writeAtomic guarantees we either
// see the entire prior task or the entire new task, never a mixture.
// A reader who races ClaimTask is therefore safe by construction.
func (s *Store) Read(team, id string) (*Task, error) {
	path := s.taskPath(team, id)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("Store.Read: read %s: %w", path, err)
	}

	var t Task
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("Store.Read: parse %s: %w", path, err)
	}

	return &t, nil
}

// Write persists the Task. Used by claim.go after every successful
// state-machine transition. Bypasses no checks: the caller is
// responsible for holding the file lock and validating invariants.
//
// Encoded with 2-space indent both for readability when a developer
// inspects the on-disk state and for diff-friendliness if the team
// directory ends up checked into a worktree. Encoding and writing are
// separated so a JSON-marshal failure aborts before the temp file is
// touched.
func (s *Store) Write(team string, t *Task) error {
	path := s.taskPath(team, t.ID)

	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("Store.Write: marshal task %s: %w", t.ID, err)
	}

	if err := writeAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf("Store.Write: %w", err)
	}

	return nil
}

// List returns the sorted task IDs under a team, derived from the
// `.json` filenames in <root>/<team>/tasks. Returns an empty slice
// (not nil) when the team directory does not exist — callers that
// distinguish "no team" from "team with no tasks" can re-issue an
// existence check on the directory itself.
//
// The result is sorted lexicographically so the demo and tests have
// stable output. We deliberately do NOT filter out `.tmp` or `.lock`
// siblings via a denylist; instead we filter to exact `.json` suffix
// since both writeAtomic and withFileLock produce other extensions
// that must not show up in the task list.
func (s *Store) List(team string) ([]string, error) {
	dir := filepath.Join(s.root, team, "tasks")

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("Store.List: read dir %s: %w", dir, err)
	}

	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(name, ".json") {
			// Skip .lock, .tmp, and any other transient siblings.
			continue
		}
		ids = append(ids, strings.TrimSuffix(name, ".json"))
	}

	sort.Strings(ids)
	return ids, nil
}
