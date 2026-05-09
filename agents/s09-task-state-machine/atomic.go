package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeAtomic writes data to path atomically: it first writes the bytes
// to a sibling temp file, then renames the temp file over the target.
// On POSIX systems os.Rename is atomic with respect to readers — any
// concurrent reader sees either the entire old file or the entire new
// file, never a half-written one — which is exactly the property the
// task store relies on.
//
// The crash-safety story (which TestWriteAtomicSurvivesPanicSimulation
// pins down):
//
//  1. If the process dies between os.WriteFile and os.Rename, the temp
//     file is orphaned but the target still points at the prior
//     contents. A reader gets a fully-formed prior task; a future
//     writeAtomic will simply overwrite the orphan with a fresh temp.
//  2. If the process dies *during* os.WriteFile (the temp is
//     partially written), the target is untouched — the writer never
//     promoted a partial file. Recovery is again a no-op.
//  3. If the process dies *during* os.Rename, the kernel either
//     completes the rename or doesn't. There is no in-between state
//     visible to other processes.
//
// Mirrors upstream's `writeAtomic` helper (referenced from tasks.ts L94
// via deps.writeAtomic, implementation in src/team/runtime.ts):
//
//	await fs.writeFile(path + '.tmp', data);
//	await fs.rename(path + '.tmp', path);
//
// Two implementation details worth flagging for readers:
//
//   - The temp file lives in the *same directory* as the target. This
//     is non-negotiable — os.Rename is only guaranteed atomic when
//     both operands sit on the same filesystem, and using
//     os.TempDir() can cross device boundaries on Linux setups with
//     /tmp on tmpfs. We use `<path>.tmp` to make the kinship
//     unmistakable.
//   - The parent directory is created with MkdirAll using 0o755.
//     Callers should not need to mkdir before writing; the store
//     handles directory creation transparently when teams are added.
func writeAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("writeAtomic: mkdir %s: %w", dir, err)
	}

	// We deliberately use a deterministic suffix instead of a random
	// one (e.g., os.CreateTemp). The reason: a deterministic name lets
	// a crash-restart cleanup pass identify and remove orphans without
	// having to glob and parse. Two writers racing on the same target
	// is already a bug at a higher level — the file lock from lock.go
	// is what serializes them — so we accept the simplicity tradeoff.
	tmp := path + ".tmp"

	if err := os.WriteFile(tmp, data, perm); err != nil {
		return fmt.Errorf("writeAtomic: write tmp %s: %w", tmp, err)
	}

	// os.Rename is atomic on POSIX (Darwin and Linux) and "best effort
	// atomic" on Windows. The chapter targets Unix; a Windows-flavored
	// extension would swap to a CAS-style MoveFileEx call.
	if err := os.Rename(tmp, path); err != nil {
		// Rename failed; the temp file is orphaned but the target is
		// intact. A best-effort cleanup keeps the directory tidy; we
		// ignore the cleanup error because the original error is what
		// the caller actually cares about.
		_ = os.Remove(tmp)
		return fmt.Errorf("writeAtomic: rename %s -> %s: %w", tmp, path, err)
	}

	return nil
}
