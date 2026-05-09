package main

import (
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// errInterleaved is the sentinel returned from inside withFileLock when
// the test detects two goroutines entering the same critical section.
// Defined as a sentinel rather than a fmt.Errorf so the test can match
// on identity instead of substring.
var errInterleaved = errors.New("two callers entered the critical section concurrently")

// TestWithFileLockSerializesConcurrentCallers is the lock's behavioral
// pin. We launch N goroutines that each try to enter the same
// withFileLock critical section; inside, each goroutine increments a
// shared counter and asserts that the *snapshot* of the counter at
// entry equals the counter after a tiny sleep. If the lock fails to
// serialize, two goroutines see the same entry value and the assertion
// trips.
//
// We deliberately do NOT use a syscall-level race here (e.g., reading
// /proc/locks) because the chapter is teaching the *contract* the
// helper exposes, not the kernel implementation. The contract is
// "fn observes its mutations as exclusive"; the test pins exactly
// that.
func TestWithFileLockSerializesConcurrentCallers(t *testing.T) {
	dir := t.TempDir()
	lockTarget := filepath.Join(dir, "thing.json")

	const goroutines = 8
	var (
		counter int32
		wg      sync.WaitGroup
		errCh   = make(chan error, goroutines)
	)
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()

			err := withFileLock(lockTarget, func() error {
				before := atomic.LoadInt32(&counter)
				atomic.AddInt32(&counter, 1)

				// A 5ms hold is enough to let any contender block on
				// Lock; longer would slow CI without buying coverage.
				time.Sleep(5 * time.Millisecond)

				after := atomic.LoadInt32(&counter)
				if after != before+1 {
					return errInterleaved
				}
				return nil
			})
			if err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("withFileLock: %v", err)
	}

	// The counter must equal the goroutine count — one increment per
	// successful entry, no double-counting.
	if got := atomic.LoadInt32(&counter); got != goroutines {
		t.Errorf("counter: got %d, want %d", got, goroutines)
	}
}
