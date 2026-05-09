package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Store is a tiny task store inlined locally so s10 has a self-
// contained module. It is a deliberately stripped version of s09's
// `Store` — no flock, no claim tokens, no atomic-rename ceremony with
// `.tmp` siblings. The teaching scope of this chapter is the
// goroutine pool + watchdog, not the on-disk CAS dance; s09 already
// did that work and the inlined version here imports nothing from it.
//
// What we keep from s09:
//
//   - JSON-per-task layout at <root>/tasks/<id>.json
//   - Read / Write / List trio
//   - Tasks are durable across process restarts (Resume relies on this)
//
// What we drop:
//
//   - Per-task flock. The Pool serializes mutations through channels,
//     so the only reader/writer is the goroutine that owns the Pool's
//     mutex. A second process touching the directory is out of scope.
//   - Atomic rename. A pool crash mid-write will leave a partial file
//     and Resume will report the orphan. For the chapter's scope
//     ("goroutines replace tmux"), the simpler write path is louder
//     about its limitations.
//   - Version + Claim fields on Task. The watchdog uses Retries
//     instead, which is the Go-side simplification of s09's lease.
//
// A `sync.Mutex` on the Store guards List/Read/Write so concurrent
// goroutines (Pool + Resume + tests) cannot race on the directory
// scan. This is the in-process equivalent of s09's flock.
type Store struct {
	root string
	mu   sync.Mutex
}

// NewStore constructs a Store rooted at the given directory. The
// directory is created lazily on the first Write; reads and lists
// against a non-existent root return an empty list / os.ErrNotExist
// rather than failing.
func NewStore(root string) *Store {
	return &Store{root: root}
}

func (s *Store) taskPath(id string) string {
	return filepath.Join(s.root, "tasks", id+".json")
}

// Write persists a task as <root>/tasks/<id>.json. JSON is indented
// for diff-friendliness when the team directory is checked into a
// worktree (a future exercise — out of scope here).
func (s *Store) Write(t *Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.taskPath(t.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("Store.Write: mkdir: %w", err)
	}

	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("Store.Write: marshal: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("Store.Write: write: %w", err)
	}
	return nil
}

// Read returns the task at the given ID. Returns os.ErrNotExist
// (matchable with errors.Is) for a missing task — Resume checks this
// to distinguish "directory drained" from a real error.
func (s *Store) Read(id string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.taskPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("Store.Read: %w", err)
	}

	var t Task
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("Store.Read: parse: %w", err)
	}
	return &t, nil
}

// List returns sorted task IDs from <root>/tasks. An empty slice (not
// nil) is returned when the directory does not exist — Resume relies
// on this to handle "fresh root" without a special case.
func (s *Store) List() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(filepath.Join(s.root, "tasks"))
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("Store.List: %w", err)
	}

	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(name, ".json"))
	}
	sort.Strings(ids)
	return ids, nil
}
