package main

import "regexp"

// DefaultMaxBackgroundTasks is the soft cap on concurrent background
// tasks. Mirrors upstream `DEFAULT_MAX_BACKGROUND_TASKS = 5` at
// `src/features/background-tasks.ts` L24. The cap is *advisory* — Decide
// returns a foreground recommendation when this many tasks are already
// running, so the runtime never spawns the sixth. Five is the upstream
// value and we keep it; the constant is exported so callers can compare
// or override at construction time.
const DefaultMaxBackgroundTasks = 5

// LongRunningPatterns flag commands that historically take long enough
// (package installs, builds, test suites, image pulls) that the user
// should not be staring at a blocked terminal while they run. Each line
// below cites the upstream source.
//
// Source: src/features/background-tasks.ts L29-L70 (the `LONG_RUNNING_PATTERNS`
// array). The Go port carries every TS regex through verbatim except
// for two minor adjustments forced by Go's RE2 engine:
//
//  1. Inline `(?i)` flags replace TS's `/.../i` suffix because Go's
//     `regexp` package does not accept trailing flag characters.
//  2. The TS source for "make targets" used `\bmake\s*(all|build|install)?\s*$`
//     which trailed an end-of-line anchor; we keep that intent verbatim
//     because trailing whitespace at the end of a command line is the
//     exact signal upstream wants to catch.
//
// Compile-at-init via MustCompile so a typo is a build-time-equivalent
// failure on first import (same posture s07 used for completion regexes).
var LongRunningPatterns = []*regexp.Regexp{
	// Package managers — upstream L31-L40.
	regexp.MustCompile(`(?i)\b(npm|yarn|pnpm|bun)\s+(install|ci|update|upgrade)\b`),
	regexp.MustCompile(`(?i)\b(pip|pip3)\s+install\b`),
	regexp.MustCompile(`(?i)\bcargo\s+(build|install|test)\b`),
	regexp.MustCompile(`(?i)\bgo\s+(build|install|test)\b`),
	regexp.MustCompile(`(?i)\brustup\s+(update|install)\b`),
	regexp.MustCompile(`(?i)\bgem\s+install\b`),
	regexp.MustCompile(`(?i)\bcomposer\s+install\b`),
	regexp.MustCompile(`(?i)\b(maven|mvn)\s+(install|package|test)\b`),
	regexp.MustCompile(`(?i)\bgradle\s+(build|test)\b`),

	// Build commands — upstream L42-L50.
	regexp.MustCompile(`(?i)\b(npm|yarn|pnpm|bun)\s+run\s+(build|compile|bundle)\b`),
	regexp.MustCompile(`(?i)\bmake\s*(all|build|install)?\s*$`),
	regexp.MustCompile(`(?i)\bcmake\s+--build\b`),
	regexp.MustCompile(`(?i)\btsc\s+(--build|-b)?\b`),
	regexp.MustCompile(`(?i)\bwebpack\b`),
	regexp.MustCompile(`(?i)\brollup\b`),
	regexp.MustCompile(`(?i)\besbuild\b`),
	regexp.MustCompile(`(?i)\bvite\s+build\b`),

	// Test suites — upstream L52-L55.
	regexp.MustCompile(`(?i)\b(npm|yarn|pnpm|bun)\s+run\s+test\b`),
	regexp.MustCompile(`(?i)\b(jest|mocha|vitest|pytest|cargo\s+test)\b`),
	regexp.MustCompile(`(?i)\bgo\s+test\b`),

	// Docker operations — upstream L57-L59.
	regexp.MustCompile(`(?i)\bdocker\s+(build|pull|push)\b`),
	regexp.MustCompile(`(?i)\bdocker-compose\s+(up|build)\b`),

	// Database / ORM operations — upstream L61-L62.
	regexp.MustCompile(`(?i)\b(prisma|typeorm|sequelize)\s+(migrate|generate|push)\b`),

	// Linting large codebases — upstream L64-L65. Trailing-dot anchor
	// is preserved verbatim from upstream — it's what catches `eslint .`
	// at a line end.
	regexp.MustCompile(`(?i)\b(eslint|prettier)\s+[^|]*\.\s*$`),

	// Git operations on large repos — upstream L67-L68.
	regexp.MustCompile(`(?i)\bgit\s+(clone|fetch|pull)\b`),
}

// BlockingPatterns flag commands that should always run in the
// foreground because either (a) they finish in milliseconds and the
// user wants the result inline, or (b) they emit output that the next
// step will consume immediately. Each line cites upstream.
//
// Source: src/features/background-tasks.ts L74-L100. We keep upstream's
// short-name patterns as-is. Note that order matters in Decide: the
// foreground hint runs *after* the long-running check, so `git pull`
// (long-running) takes precedence over `git status` (blocking) for any
// hypothetical overlap — there is none in the current pattern set.
var BlockingPatterns = []*regexp.Regexp{
	// Quick status checks — upstream L75-L85.
	regexp.MustCompile(`(?i)\bgit\s+(status|diff|log|branch)\b`),
	regexp.MustCompile(`(?i)\bls\b`),
	regexp.MustCompile(`(?i)\bpwd\b`),
	regexp.MustCompile(`(?i)\bcat\b`),
	regexp.MustCompile(`(?i)\becho\b`),
	regexp.MustCompile(`(?i)\bhead\b`),
	regexp.MustCompile(`(?i)\btail\b`),
	regexp.MustCompile(`(?i)\bwc\b`),
	regexp.MustCompile(`(?i)\bwhich\b`),
	regexp.MustCompile(`(?i)\btype\b`),

	// File operations — upstream L87-L92.
	regexp.MustCompile(`(?i)\bcp\b`),
	regexp.MustCompile(`(?i)\bmv\b`),
	regexp.MustCompile(`(?i)\brm\b`),
	regexp.MustCompile(`(?i)\bmkdir\b`),
	regexp.MustCompile(`(?i)\btouch\b`),

	// Environment checks — upstream L94-L100.
	regexp.MustCompile(`(?i)\benv\b`),
	regexp.MustCompile(`(?i)\bprintenv\b`),
	regexp.MustCompile(`(?i)\bnode\s+-[vpe]\b`),
	regexp.MustCompile(`(?i)\bnpm\s+-v\b`),
	regexp.MustCompile(`(?i)\bpython\s+--version\b`),
}
