package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

// withFileLock acquires an exclusive advisory lock on `<lockPath>.lock`,
// runs fn while holding it, and releases the lock on the way out (even
// if fn panics). The lock is process-level, kernel-mediated, and
// cooperative: any process that calls flock on the same path will
// serialize with us; processes that bypass the helper are not
// constrained.
//
// Mirrors upstream's `withTaskClaimLock` helper
// (src/team/state/tasks.ts L43: a generic concurrent-access coordinator
// that returns either {ok:true, value} or {ok:false}). The TS version
// races against a timeout; the Go version blocks indefinitely because
// the on-disk task store should never have a worker holding the lock
// for more than a few microseconds (read JSON → mutate in memory →
// atomic-write → release). A stuck lock means a real bug.
//
// Implementation notes that surface as teaching points:
//
//   - We use github.com/gofrs/flock instead of hand-rolling
//     syscall.Flock because the latter is Linux-specific (POSIX 2008
//     `fcntl(F_OFD_SETLK)` is the portable spelling, but the syscall
//     numbers diverge per-OS). flock the library compiles on Darwin,
//     Linux, FreeBSD, and Windows. The single-dep import is paid once
//     for the whole chapter.
//   - The `.lock` suffix is appended by this helper so callers pass
//     the *task path*, not a synthesized lock path. This keeps the
//     contract aligned with the upstream `withTaskClaimLock(taskId)`
//     signature where the lock derivation is internal.
//   - We MkdirAll the parent directory before opening the lock file.
//     The very first call into a brand-new team directory would
//     otherwise fail with ENOENT — the lock file's purpose is to be
//     created on demand.
//   - Lock release uses defer with the err == nil check so a Lock
//     failure cannot mask the original error from fn; if Lock fails,
//     fn never runs, and the function returns the lock error directly.
func withFileLock(lockPath string, fn func() error) error {
	lockFile := lockPath + ".lock"

	// Ensure the parent directory exists. Lock files for a given team
	// live alongside the team's tasks, so the directory chain is the
	// same one writeAtomic creates lazily.
	if err := os.MkdirAll(filepath.Dir(lockFile), 0o755); err != nil {
		return fmt.Errorf("withFileLock: mkdir %s: %w", filepath.Dir(lockFile), err)
	}

	fl := flock.New(lockFile)

	// Lock blocks until the lock is acquired. There is no timeout
	// because the only legitimate holders of this lock are Read+Mutate
	// +Write cycles measured in microseconds. If the process gets
	// stuck here, a real deadlock has been introduced — make it
	// loud rather than quietly returning success after a timeout.
	if err := fl.Lock(); err != nil {
		return fmt.Errorf("withFileLock: lock %s: %w", lockFile, err)
	}

	// We capture the unlock as a deferred call so a panic inside fn
	// still releases the lock — otherwise a single panic would deadlock
	// the entire team. The Unlock error is discarded because there's
	// nothing the caller can do with it; the OS already released the
	// lock when the process exits anyway.
	defer func() {
		_ = fl.Unlock()
	}()

	return fn()
}
