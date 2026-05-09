---
title: "Appendix B · Upstream Source Map"
chapter: appendix-b
slug: appendix-b-upstream-map
est_read_min: 12
---

# Appendix B · Upstream Source Map

> You've finished 10 chapters + s_full + Appendix A. Now to read the real upstream `Yeachan-Heo/oh-my-claudecode`, **read in this order** for max efficiency.

Upstream has 1,025 `.ts` files + 22+ design docs + 19 agent prompts. Diving in headfirst, you'll get lost. Below is a **linear reading path** — 10 stops, 30–90 minutes each.

---

## Reading map (in order)

### Stop 1 · `README.md`

**Goal**: Sense the project's positioning. Skip if you've read research-notes — it's denser than the README.

**Takeaway**: OMC is a multi-agent orchestration plugin for Claude Code, not a replacement.

---

### Stop 2 · `src/agents/types.ts` (whole file, ~200 lines) → maps to **s01**

**Goal**: Understand the "atom" of the entire architecture — `AgentConfig`.

**Takeaway**: Every agent is a metadata bundle + prompt + tool list + model tier. This is the foundation of everything else.

**Key lines**:
- L64-L83 — `AgentConfig` interface
- L139-L165 — `getDefaultModelForCategory` (tier → model string)

---

### Stop 3 · `src/agents/definitions.ts` L210-L298 → maps to **s01**

**Goal**: See the 19 real agent definitions + the four-fold priority chain.

**Key lines**:
- L210-L260 — 19 agent instances laid out by role
- L289-L298 — `override.model ?? envInheritModel ?? configuredModel ?? agentConfig.model` — this is your s01's `ResolveModel`

**Reflection**: why does OMC compute the chain at lookup time rather than at register time? The answer is in research-notes.

---

### Stop 4 · `src/agents/utils.ts` L83-L131 + L367-L393 → maps to **s02 + s04**

**Goal**: Two completely different utilities that happen to live as file neighbors.

- L83-L131 `loadAgentPrompt` — prototype for **s02**. Notice the dual-mode (build-time inline + runtime fallback). Go's `embed.FS` collapses dual-mode into one path — a real quality win.
- L367-L393 `deepMerge` — prototype for **s04**. **Focus on L376** — the `__proto__/constructor/prototype` skip is prototype-pollution defense. Go has no prototype, but the mental model is still worth carrying.

---

### Stop 5 · `src/features/magic-keywords.ts` L1-L297 → maps to **s03**

**Goal**: Read the whole file. This is OMC's most self-contained module — readable in isolation.

**Key lines**:
- L20-L22 — `removeCodeBlocks`
- L25-L30 — `INFORMATIONAL_INTENT_PATTERNS` (4 languages)
- L221-L233 — the iterative rewrite loop

**Notice**: upstream uses object + closure + array-of-Keyword; Go uses struct + func field + slice-of-Keyword. **Equivalent shape, different syntax** — a tiny functional vs imperative side-by-side.

---

### Stop 6 · `src/config/loader.ts` L1-L80 → maps to **s04**

**Goal**: Understand layered config in its real form.

**Key lines**:
- L4-L9 — comments naming the three sources
- L41-L72 — `buildDefaultConfig` shape, with tier-name resolution

After reading, you'll see OMC uses JSONC while your Go version uses plain JSON — not simplification, but because Go's `encoding/json` rejects JSONC, and the comments-in-config benefit is lower than the strict-schema benefit.

---

### Stop 7 · `hooks/hooks.json` (whole file, 212 lines) → maps to **s05**

**Goal**: Not TS, but config — yet **information-dense**. 10 lifecycle events, various matchers, various timeouts all here.

**Key lines**:
- L4-L19 — standard `UserPromptSubmit` shape
- L21-L62 — `SessionStart`'s multi-matcher pattern
- Every hook's `command` field points at `scripts/run.cjs <script>.mjs` — proof of the shell-out architecture

After reading, peek at `scripts/keyword-detector.mjs` to see what a hook implementation looks like (~80 lines).

---

### Stop 8 · `src/mcp/omc-tools-server.ts` L1-L100 + `src/mcp/servers.ts` L20-L75 → maps to **s06**

**Goal**: Sort out OMC's "two kinds of MCP."

- `omc-tools-server.ts` — in-process tools (the kind your s06 implements)
- `servers.ts` — external MCP server factories (`npx ...`)

**Reflection**: why does OMC put these in different files? Because in-process and external have completely different lifecycles — one registers within the main process, the other forks a child process. The Go version would also need to split if you ship real external MCP.

---

### Stop 9 · `src/features/continuation-enforcement.ts` L1-L196 + `src/features/background-tasks.ts` L1-L357 → maps to **s07 + s08**

**Goal**: Two pattern-driven recommenders — **read together for efficiency**.

Their shapes are nearly identical:
- A regex slice
- A `detect`/`decide` function returning `{flag, reason, confidence}`
- Both stateless

After reading, your mental model crystallizes: OMC uses many small classifiers — not LLMs, just regex + tables — to push LLM use from "decide everything" to "decide the hard parts."

**Key lines**:
- continuation L18-L24 — reminder pool
- continuation L132-L170 — completion classification
- background L29-L70 — long-running patterns
- background L24 — concurrency cap constant `DEFAULT_MAX_BACKGROUND_TASKS = 5`

---

### Stop 10 · `src/team/state/tasks.ts` L1-L315 → `src/team/runtime.ts` L289-L990 → maps to **s09 + s10**

**Goal**: The capstone. These two files together are ~1,300 lines — OMC's heaviest subsystem. **Save for last** — the previous 9 stops have built every mental model you need.

Internal order:

1. Read `tasks.ts` whole → source material for s09
   - L1-L99 — type defs + `claimTask` (look at the L92-L99 token mint)
   - L100-L200 — `transitionTaskStatus` + error branches
   - L200-L315 — `withTaskClaimLock` + `writeAtomic`

2. Read key sections of `runtime.ts`:
   - L80-L140 — `writeTask`/`readTask` companions
   - L289-L390 — `startTeam` (team init)
   - L466-L580 — `watchdogCliWorkers` (**core loop**; corresponds to your s10 `watchdog`)
   - L582-L770 — `spawnWorkerForTask` (where the tmux call actually happens)
   - L850-L990 — `shutdownTeam` + `resumeTeam`

This stop reveals: **1,034 lines of TS solve the same problem your ~810 lines of Go solve**. Where's the difference? tmux + done.json + heartbeat file + watchdog-failed.json + pane lookup tables — all gone in the Go version. **This is the impact of language ecosystem on architecture.**

---

## Upstream file → curriculum chapter map

| Upstream file | Lines | Session |
|---|---|---|
| `src/agents/types.ts` | L64-L83, L139-L165 | s01 |
| `src/agents/definitions.ts` | L143-L162, L210-L298 | s01 |
| `src/agents/utils.ts` | L83-L131 | s02 |
| `src/agents/utils.ts` | L367-L393 | s04 |
| `agents/architect.md` | L1-L7 | s02 (fixture template) |
| `src/features/magic-keywords.ts` | L1-L297 | s03 |
| `src/config/loader.ts` | L1-L80 | s04 |
| `hooks/hooks.json` | L1-L212 | s05 |
| `src/mcp/omc-tools-server.ts` | L1-L100 | s06 |
| `src/mcp/servers.ts` | L20-L75 | s06 |
| `src/features/continuation-enforcement.ts` | L1-L196 | s07 |
| `src/features/background-tasks.ts` | L1-L357 | s08 |
| `src/team/state/tasks.ts` | L1-L315 | s09 |
| `src/team/runtime.ts` | L80-L140 | s09 |
| `src/team/runtime.ts` | L289-L990 | s10 |

`upstream-readings/sNN-*.ts` files in this repo collect annotated excerpts — `cat` them locally to revisit.

---

## Five extension exercises (graduating difficulty)

### 1. Wire s10 to `os/exec ./omc-worker`

Replace s10's goroutine workers with a separate binary, launched via `os/exec`. The `Worker` interface stays; only the implementation swaps. All existing tests should still pass (which proves the abstraction was right).

Estimate: ~150 LOC. Catch: how does the worker read tasks? Still the filesystem (s09), but the worker is now a separate process and needs to import s09. That **immediately breaks the self-contained constraint**. Reflect: was the abstraction wrong?

### 2. Add `worktree` mode to s10

Each worker creates a `git worktree` (mirroring upstream `team/git-worktree.ts`), works in an isolated tree. Requires `os/exec git worktree add/remove`.

Estimate: ~200 LOC, plus dealing with worktree leaks (orphans after crashes). This is a **production-grade** problem.

### 3. Wire s06 to a real MCP server

Replace the in-memory `Invoke` with a real MCP wire protocol using `github.com/modelcontextprotocol/go-sdk` over stdio. Verify with the MCP inspector tool.

Estimate: ~300 LOC + a new dependency. This step teaches you "how to swap an in-memory abstraction with a wire abstraction" without breaking callers.

### 4. Add a third worker backend to s10: tmux

You now have two implementations (goroutine, `os/exec`). The third is tmux pane (**actually mirrors upstream!**). Proves the `Worker` interface is good — the watchdog code stays untouched.

Estimate: ~250 LOC + cross-platform pain (tmux isn't on Windows). **Doing this means you've truly understood upstream's runtime.ts.**

### 5. Actually wire all 10 modules into `cmd/omc/main.go`

Create an `omc-app/` directory (separate `go.mod`), import all 10 chapter modules (using `replace` to point at local paths). Provide a runnable mock `claude` binary (echoes its prompt) and run the 16-step trace end-to-end.

Estimate: ~500 LOC + lots of glue. **The learning repo deliberately doesn't ship this** because it would break the "each chapter independent" promise. But after building it, you have a portfolio project.

---

## Wrap-up

After the full set (10 chapters + s_full + two appendices), you should be able to:

- Navigate the real OMC source without getting lost
- Know *why* each mechanism is designed this way (and not just *what*)
- Reproduce a similar-scale multi-agent orchestrator in Go
- Borrow these patterns into your own projects (state machine + handoff + middleware + filesystem CAS)

If you've completed 3+ of the 5 exercises, congrats — you've graduated past "I learned this" into "I can extend this."
