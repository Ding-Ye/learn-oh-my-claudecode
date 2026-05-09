// Source: https://raw.githubusercontent.com/Yeachan-Heo/oh-my-claudecode/main/src/features/background-tasks.ts
// Lines:  L1-L100 (heuristic core only; the lifecycle manager at L114+
//         is intentionally left for s09 + s10 to compose)
// License: MIT, Copyright (c) 2025 Yeachan Heo
//
// Annotated excerpt for s08. The upstream feature has FOUR surfaces:
//   (1) a soft cap on concurrent background tasks;
//   (2) regex patterns that classify long-running commands;
//   (3) regex patterns that classify always-blocking commands;
//   (4) a `shouldRunInBackground` decision returning {ok, reason, ...}.
//
// We port (1)-(4) as pure Go. The lifecycle manager that actually
// tracks running tasks (`BackgroundTaskManager` class around L114+) is
// intentionally NOT ported — the chapter teaches the recommender plus
// a minimal spawn primitive, and a real runtime would compose them
// with s09's filesystem-CAS task store.
// Read alongside Section 6 of the s08 chapter docs.

import type { BackgroundTask, SessionState, PluginConfig } from '../shared/types.js';

// ─── (1) Concurrency cap (L24) ───
//
// A single integer constant. Upstream uses 5 because at six or more
// long-running commands a developer's machine starts thrashing. The
// Go port keeps the same value as `DefaultMaxBackgroundTasks`. Cap is
// advisory — Decide returns foreground when running == max.
export const DEFAULT_MAX_BACKGROUND_TASKS = 5;

// ─── (2) Long-running patterns (L29-L70) ───
//
// 24 regexes covering five families: package managers, build commands,
// test suites, docker, database/ORM, lint-on-large-trees, slow git.
// One quirk to flag: TS uses the `/.../i` suffix; Go's RE2 requires
// the `(?i)` inline flag. Every Go regex picks up the leading `(?i)`.
export const LONG_RUNNING_PATTERNS = [
  /\b(npm|yarn|pnpm|bun)\s+(install|ci|update|upgrade)\b/i,
  /\b(pip|pip3)\s+install\b/i,
  /\bcargo\s+(build|install|test)\b/i,
  /\bgo\s+(build|install|test)\b/i,
  /\bdocker\s+(build|pull|push)\b/i,
  /\bgit\s+(clone|fetch|pull)\b/i,
  // ... 18 more covering rustup, gem, composer, maven, gradle,
  //     webpack/rollup/esbuild/vite/tsc, jest/mocha/vitest/pytest,
  //     docker-compose, prisma/typeorm/sequelize, eslint/prettier
];

// ─── (3) Blocking patterns (L74-L100) ───
//
// 20 regexes for "always foreground" commands. Two design choices
// upstream made and we preserve verbatim:
//   - `\bls\b` does not look at flags: `ls -la` stays foreground.
//     The runtime trusts the user's `ls` even if it streams 100k
//     lines — piping into a pager is the user's choice.
//   - `\bnode\s+-[vpe]\b` matches version checks but not `node script.js`.
//     A misspelled `node -V` would fall through to the default branch.
export const BLOCKING_PATTERNS = [
  /\bgit\s+(status|diff|log|branch)\b/i,
  /\bls\b/i,
  /\bpwd\b/i,
  /\bcat\b/i,
  /\becho\b/i,
  /\bnode\s+-[vpe]\b/i,
  // ... 14 more covering head/tail/wc/which/type, cp/mv/rm/mkdir/touch,
  //     env/printenv, npm -v, python --version
];

// ─── (4) shouldRunInBackground (L150-L210, paraphrased) ───
//
// Upstream returns {ok, reason, confidence}. Decision order matches
// our Go Decide: cap first, then long-running, then blocking, then
// default. The ONE behavioral difference is the default branch:
// upstream returns `ok=true, confidence='low'` ("when in doubt,
// background it"); the Go chapter inverts to `Background=false,
// Confidence="low"` ("when in doubt, keep it visible to the user").
// The plan calls this out as a deliberate teaching choice —
// backgrounding the unknown is the wrong default for an interactive
// CLI agent. Not ported here; the comment is the whole port.
