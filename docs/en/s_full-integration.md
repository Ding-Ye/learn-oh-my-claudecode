---
title: "s_full · End-to-end Integration"
chapter: full
slug: s_full-integration
est_read_min: 12
---

# s_full · End-to-end Integration

> What this teaches: how the previous 10 chapters compose into a full OMC flow — the trace of `/team 2:executor "fix lint errors"` from start to finish.

---

## Putting the chapters together

If you worked through s01–s10, congratulations — you've hand-built almost every core mechanism in OMC. This chapter writes no new code; it just answers one question: **when a real `/team` command fires, how do these 10 mechanisms cooperate?**

The diagram below stitches them. Each box corresponds to a Go module you've written:

```
                  user input: /team 2:executor "fix lint errors in src/auth/"
                          │
          ┌───────────────▼───────────────┐
          │ s05  hooks-pipeline           │  ← UserPromptSubmit hook fires
          └───────────────┬───────────────┘
                          │ JSON envelope flows in
          ┌───────────────▼───────────────┐
          │ s03  magic-keywords           │  ← rewrites prompt if `ultrawork` etc.
          └───────────────┬───────────────┘
                          │
          ┌───────────────▼───────────────┐
          │ s04  config-loader            │  ← defaults → user → project → env
          └───────────────┬───────────────┘
                          │
          ┌───────────────▼───────────────┐
          │ s01  agent-registry           │  ← lookup: "executor" → opus
          │ s02  prompt-loader            │  ← embed.FS yields executor.md
          └───────────────┬───────────────┘
                          │
          ┌───────────────▼───────────────┐
          │ s07  continuation             │  ← attach Sisyphus system prompt
          └───────────────┬───────────────┘
                          │
          ┌───────────────▼───────────────┐
          │ s06  mcp-tool-server          │  ← register tools by category, OMC_DISABLE_TOOLS filters
          └───────────────┬───────────────┘
                          │
          ┌───────────────▼───────────────┐
          │ s09  task-state-machine       │  ← create 2 tasks on disk (CAS+lease)
          └───────────────┬───────────────┘
                          │
          ┌───────────────▼───────────────┐
          │ s10  team-watchdog            │  ← start 2 goroutine workers
          │   ↻ s08  background-tasks     │  ← workers decide FG vs BG per command
          └───────────────┬───────────────┘
                          │
                       results
```

---

## 16-step execution trace (each step labeled with its Go session)

The dossier's "real-world `/team` trace" — every step mapped to a Go module you wrote:

| Step | Upstream behavior | Go session |
|---|---|---|
| 1 | `/team N:type "..."` plugin routes to `team` skill | **s05** hooks-pipeline fires `UserPromptSubmit` |
| 2 | Parse N and agent-type | **s04** config-loader keyboard layer |
| 3 | Write `team-state.json` phase=plan | **s09** task-state-machine |
| 4 | Spawn `explore` (Haiku) | **s01** lookup + **s02** prompt + **s10** spawn |
| 5 | Spawn `planner` (Opus) | **s01** + **s02** + **s10** |
| 6 | Write handoff `team-plan.md` | **s09** (handoff carried in task description) |
| 7 | `TeamCreate("fix-lint-errors")` | **s10** Pool init |
| 8 | `TaskCreate` × 2 | **s09** Store.Write |
| 9 | `Task(...)` worker dispatch × 2 | **s10** Pool.Submit |
| 10 | Worker-1 edits + `SendMessage` | **s06** SendMessage tool |
| 11 | Worker-2 in parallel | **s10** goroutine pool |
| 12 | Lead's monitor loop | **s10** watchdog tick |
| 13 | Spawn `verifier` (Sonnet) | **s01** + **s02** + **s10** |
| 14 | Verify passes, skip `team-fix` | **s09** transition done |
| 15 | `SendMessage(shutdown)` + cleanup | **s10** Shutdown drain |
| 16 | Print summary to user | (caller layer; not a session concern) |

Notice that no single step "owns" the orchestration — each session contributes a slice. That's the heart of OMC's design.

---

## Thought exercise: if you actually wanted to wire these into a CLI

Suppose you wanted to graduate from the learning repo and stitch the 10 modules into a runnable `omc` CLI. The teaching repo deliberately does **not** do this, but walking through the thought experiment helps clarify each chapter's boundary.

The minimal version would look like:

```go
// cmd/omc/main.go (hypothetical)
package main

import (
    s01 "github.com/Ding-Ye/learn-oh-my-claudecode/agents/s01-agent-registry"
    s02 "github.com/Ding-Ye/learn-oh-my-claudecode/agents/s02-prompt-loader"
    // ... s03..s10
)

func main() {
    cfg, _ := s04.Load(".")            // s04
    registry := s01.New()              // s01
    loader := s02.New(promptFS, "agents")  // s02
    tools := s06.New().WithDisabled(s06.ParseDisabled(os.Getenv("OMC_DISABLE_TOOLS")))
    store := s09.NewStore(".omc/state/team")
    pool := s10.New(ctx, 3, store)
    // Then route the user prompt: s03 magic-keywords first, then s07 attach Sisyphus,
    // finally s10 dispatch a worker. s05 hooks fire at each lifecycle point.
    // s08 background-tasks decisions happen inside each worker.
    pool.Run()
}
```

But this `cmd/omc/main.go` is **not** in the repo — by design. The teaching repo's promise is "each chapter readable in isolation"; introducing a global `main.go` would tempt students to change one piece and ripple it everywhere. Leaving "how to wire it up" as a thought exercise teaches more than shipping a runnable demo.

---

## Deliberate omissions

The learning version is a **functional subset**, not a 1:1 rewrite of OMC. Here's what we deliberately drop and why:

| Upstream has | We skip | Why |
|---|---|---|
| Real LLM call layer | All mocked | OMC itself has no LLM call layer (dossier `has_llm_call_layer: false`); upstream merely constructs `queryOptions` for the Claude Agent SDK |
| 19 real agent prompts | 3 placeholder fixtures (architect/executor/explore) | Teaching needs to demonstrate the loading mechanism, not the prompt content |
| Real MCP wire protocol | s06 uses in-process registry | The teaching point is "category disable + closure-as-data pattern"; wire protocol is noise |
| tmux multi-pane orchestration | s10 uses goroutines | The teaching point is "goroutine + channel replaces external IPC" |
| Git worktree isolation | Not implemented | s10's extension exercise hints at how to add it |
| Telegram / Lark / WeChat IM adapters | Skipped | These are product differentiation, not core architecture |
| HUD statusline | Skipped | Rendering layer, unrelated to orchestration |
| Parallel `runtime-v2.ts` etc. | Single canonical implementation | The teaching repo must have only one "right way" |

If you actually want to build a **production** OMC-style orchestrator, you'd have to address all of the above — but during learning, they're noise.

---

## Next: appendices

- **Appendix A · Mental Models** — OMC's non-code dimensions: state machine, stage routing, handoff documents as durable memory
- **Appendix B · Upstream Source Map** — a full reading path through `Yeachan-Heo/oh-my-claudecode`, where to start and which thread to follow

After reading those two, your understanding of OMC will graduate from "I implemented the 10 mechanisms once" to "I can navigate the 100K-LOC real upstream by myself".
