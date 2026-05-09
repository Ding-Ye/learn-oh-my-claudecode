package main

import (
	_ "embed"
	"encoding/json"
	"math/rand"
	"sync"
)

// SystemPromptAddition is the Sisyphus-persona addendum the chapter
// embeds at compile time. It mirrors upstream's
// `continuationSystemPromptAddition` (continuation-enforcement.ts L60-L130)
// but is rewritten in teaching-friendly prose. The single
// `//go:embed prompt_addition.md` directive baked into the binary
// replaces upstream's runtime template-literal — there is no Markdown
// file to ship alongside the executable.
//
// First time in the chapter series that `//go:embed` produces a string
// (s02 used `embed.FS`). The Go compiler enforces that the target file
// exists at build time, so a typo here is caught by `go build` rather
// than at runtime.
//
//go:embed prompt_addition.md
var SystemPromptAddition string

// remindersData is the raw bytes of the JSON reminder pool. Embedding
// JSON instead of a hard-coded `[]string` lets a curious student edit
// `reminders.json` and re-run `go run .` without touching Go source —
// the same iteration loop a future "config-driven reminder set" would
// use in production.
//
//go:embed reminders.json
var remindersData []byte

// reminders is the parsed pool, resolved exactly once at first call.
// We use sync.Once so the first call to RandomReminder pays the JSON
// parse cost; subsequent calls take the cached slice. A panic on parse
// failure is appropriate here because the JSON is embedded — a malformed
// blob is a build-time bug, not a runtime condition the caller can
// recover from.
var (
	remindersOnce sync.Once
	reminders     []string
	rng           *rand.Rand
	rngMu         sync.Mutex
)

// initReminders parses the embedded JSON exactly once and seeds an rng.
// Tests can override the seed via SeedReminderRNG to make
// TestRandomReminderRotates deterministic.
func initReminders() {
	if err := json.Unmarshal(remindersData, &reminders); err != nil {
		// Build-time bug: the embedded JSON is malformed. There is no
		// recovery path — fail loudly so the next `go test` catches it.
		panic("s07-continuation: reminders.json is not valid JSON: " + err.Error())
	}
	if len(reminders) == 0 {
		panic("s07-continuation: reminders.json must contain at least one reminder")
	}
	if rng == nil {
		// Default seed: a nonzero constant so the demo's `go run .`
		// output is reproducible. The test suite calls SeedReminderRNG
		// to override this when it wants to assert rotation.
		rng = rand.New(rand.NewSource(1))
	}
}

// RandomReminder picks one reminder string from the embedded pool.
// Concurrent callers are serialized through rngMu — math/rand's default
// Source is goroutine-unsafe, and the chapter intentionally avoids the
// "global rand" footgun by routing every pick through one explicit lock.
func RandomReminder() string {
	remindersOnce.Do(initReminders)
	rngMu.Lock()
	defer rngMu.Unlock()
	return reminders[rng.Intn(len(reminders))]
}

// SeedReminderRNG resets the package's RNG to a known seed. Exposed so
// tests can pin the rotation order; production callers should ignore
// this and let the package's default seed (constant) hold. Calling
// SeedReminderRNG also forces the reminder pool to be parsed on the
// next RandomReminder call.
func SeedReminderRNG(seed int64) {
	rngMu.Lock()
	defer rngMu.Unlock()
	rng = rand.New(rand.NewSource(seed))
}
