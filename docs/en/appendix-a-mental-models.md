---
title: "Appendix A · Mental Models"
chapter: appendix-a
slug: appendix-a-mental-models
est_read_min: 15
---

# Appendix A · Mental Models

> This appendix is not about code — it's about OMC's **non-code dimensions**. These are upstream product decisions that explain *why* the 10 mechanisms look the way they do.

OMC is a product. Four mental models behind its design are worth treating separately. Each explains *why*, not *what*.

---

## A1 · Team orchestration as a state machine

OMC's `/team` command is really a **finite state machine**:

```
        ┌──────────────┐  fail   ┌─────────┐
        │   team-plan  │────────▶│ team-fix│
        └──────┬───────┘         └────┬────┘
               │ pass                 │ pass (≤ N rounds)
               ▼                      │
        ┌──────────────┐               │
        │   team-prd   │               │
        └──────┬───────┘               │
               │                       │
               ▼                       │
        ┌──────────────┐               │
        │  team-exec   │◀──────────────┘
        └──────┬───────┘
               │
               ▼
        ┌──────────────┐  pass   ┌──────────┐
        │ team-verify  │────────▶│ complete │
        └──────────────┘         └──────────┘
```

### Precise definition of the five stages

- **team-plan** — Enter: `/team` command fires. Exit: planner emits a task graph (subtasks + dependsOn).
- **team-prd** (optional) — Enter: plan flagged that PRD is needed. Exit: spec written to `.omc/specs/`. Simple cases skip this stage entirely.
- **team-exec** — Enter: handoff `team-plan.md` written. Exit: all subtasks reach a terminal status ∈ {done, failed}.
- **team-verify** — Enter: all subtasks complete. Exit: verifier runs tests and renders pass/fail.
- **team-fix** — Enter: verify failed. Exit: fix succeeded OR `maxValidationRounds` reached. If the bound is exceeded, the whole `/team` command fails.

### Why sequential, not parallel?

Each stage **consumes the previous stage's output**: planner needs explore's results, executor needs plan, verifier needs executor's changes. This isn't a performance bottleneck — it's a **correctness constraint**. OMC parallelizes inside a stage (multiple workers run subtasks concurrently) but stays strictly serial across stages.

### Failure modes

1. **Fix loop exceeded** → state enters `failed`, artifacts retained for manual debugging
2. **Verify never passes** → same; usually means the plan stage was wrong
3. **A worker dies** → s10 watchdog takes over: respawn once, then mark task failed, but **the whole `/team` doesn't fail immediately** — other workers continue

### In your Go code

s09 (task-state-machine) implements *which stage and which task is in what status*; s10 (team-watchdog) implements *the state machine's tick + transition + recover*. Together they are the runtime engine of this state machine. **State and execution decoupled** — the single most copyable design lesson in OMC.

### Thought exercise

Try mentally adding a `team-spec` stage between plan and prd, used to clarify requirements. You'd need to change: (a) state.json's allowed values, (b) the plan agent's prompt, (c) verify's entry condition. The thought experiment teaches you the **marginal cost of a stage** — adding one isn't cheap.

---

## A2 · Stage-aware model routing

OMC assigns 19 agents to model tiers based on stage needs:

| Tier | Typical model | OMC uses for | Intuition |
|---|---|---|---|
| HIGH | Opus | planner / analyst / architect / critic | Long context + strategic thinking |
| MEDIUM | Sonnet | executor / debugger / designer / verifier | Writing code, fixing bugs, drawing diagrams — workhorse |
| LOW | Haiku | explore / formatter / linter | Scanning files, mechanical ops — cheap and fast |

### Why is `explore` Haiku?

`explore` scans files, greps keywords, lists candidates. That work doesn't need Opus's reasoning — it needs **fast** and **cheap**, because the plan stage might dispatch 5–10 explores. Haiku is 5× cheaper and 3× faster, with no quality loss on this task.

### Why must `planner` be Opus?

`planner` takes a fuzzy requirement plus a pile of explore results and **produces a task DAG**. That's classification-hard work; one mistake ruins the whole `/team`. Opus's sweet spot.

### The four-fold priority chain

The chain (which s01 implements):

```
override.model  ?? envInheritModel  ?? configuredModel  ?? agent.Model
```

The point of the chain is to let users **override at any layer**:

- `override.model`: per-call temporary override (rare)
- `envInheritModel`: controlled by `OMC_ROUTING_FORCE_INHERIT` — if set, **all** agents inherit the current session's model (cost mode)
- `configuredModel`: per-agent override in user's `omc.jsonc`
- `agent.Model`: the agent definition's default tier

### `OMC_MODEL_HIGH/_MEDIUM/_LOW`

These three env vars resolve tier names to specific model IDs at default-build time. When the user switches to Bedrock (`CLAUDE_CODE_USE_BEDROCK=1`), they become `arn:aws:bedrock-runtime:...`.

### Thought exercise

If you added a BUDGET tier (cheaper than LOW, perhaps GPT-4o-mini), the minimum changes are: (1) tier literals, (2) default assignment (which agents get BUDGET?), (3) priority-chain order. Don't actually edit code — walk through it mentally and see if you can enumerate all 8 change points in one pass.

---

## A3 · Magic keywords as middleware

The most surprising design in OMC is its prompt-rewriting middleware: `ultrawork`, `ultrathink`, `search` — words that **transparently rewrite the user's prompt before the model sees it**.

### The middleware metaphor

If you've written Express / Koa / Echo / Gin, you know this pattern: each middleware sees an input, decides whether to rewrite / short-circuit / pass through. OMC's magic keywords are middleware on prompts:

```
user input: "ultrawork build a server"
   │
   ▼
[ultrawork middleware] detects trigger, prepends high-intensity directive
   │
   ▼
"[Ultrawork mode active. Maximum performance. ...]\nbuild a server"
   │
   ▼
[search middleware] no trigger, pass through
   │
   ▼
[Sisyphus system prompt] attached
   │
   ▼
LLM
```

### Three skip rules

Not every prompt containing a trigger word gets rewritten:

1. **Inside code blocks** — `​`​`​`ultrawork​`​`​` is a code example, not a command
2. **Informational context** — "what is ultrawork?" / "ultrawork이 뭐야?" / "ultrawork は何？" asks questions, doesn't issue commands
3. **Already rewritten** — prevents recursive expansion (s03's implementation doesn't enforce idempotency, but the prepend pattern naturally avoids loops)

### Why is Action a closure, not a constant string?

```go
type Keyword struct {
    Triggers []string
    Action   func(prompt, agentName, modelID string) string
}
```

Action is a function rather than a string because **different agent/model combinations need different expansions**. `ultrawork` for Opus vs. for Haiku should yield different prompts — Opus already "thinks hard," Haiku needs more explicit guidance. The closure makes "expand by context" possible without growing the Keyword type itself.

### Thought exercise

Design a `terse` keyword that **strips** filler words ("please," "could you," "I would like to") from the user prompt, leaving only the imperative core. This is the inverse transformation: not prepend, but substitute. What field do you need to add to s03's `Keyword`? Or do you not need to — does swapping out the Action implementation suffice?

---

## A4 · Handoff documents as durable memory

OMC's most elegant design might be this: **stage-to-stage state transfer relies neither on memory nor RPC, but on markdown files**.

### The five-section handoff format

`.omc/handoffs/team-plan.md` looks like:

```markdown
# team-plan handoff

## Decided
- Split the work into 2 subtasks (fix login.ts, fix oauth.ts)
- Use executor agent, not designer

## Rejected
- Dispatching 5 workers at once: too few lint errors, would waste tokens
- Pre-running verifier: the verify stage will run it anyway

## Risks
- oauth.ts contains OAuth logic that executor might break (verifier should focus here)

## Files
- src/auth/login.ts
- src/auth/oauth.ts

## Remaining
- Wait for team-exec to finish
- Then team-verify
```

10–20 lines of markdown — but **a complete decision log of the plan stage**.

### Why does this format solve the problem?

OMC's core problem: **the lead agent's context will be compacted**. A full `/team` run produces hundreds of KB of LLM dialogue history. By the time the lead enters the verify stage, it no longer remembers why it cut a particular subtask in plan.

If state lives in memory, compaction loses it. If state lives in an RPC database, you need schema + serialization + network. **In a markdown file**:

1. **Token-friendly** — 10–20 lines, never compressed into a meaningless summary
2. **Human-readable** — `cat` it during debugging
3. **Recoverable** — if `/team` crashes mid-flight, the handoff is still there; resume can pick up

### Write vs. read timing

- **Write** — at stage *exit* (not entry). Exiting means the stage's decisions are settled.
- **Read** — at the next stage's *entry* (i.e., inject the markdown at the top of the LLM system prompt as "memo from the previous stage").

### Failure mode

What if a stage crashes before writing its handoff? The next stage "doesn't see prior decisions." OMC's response: when next stage starts and finds the expected handoff missing, it **refuses to start** and surfaces a clear error ("team-plan stage's handoff missing — re-run team-plan").

### Thought exercise

Write a `lint-handoff` tool that scans `.omc/handoffs/*.md`, asserts each file has all 5 sections (Decided / Rejected / Risks / Files / Remaining) non-empty, and warns on violations. ~50 lines of Go, but deployed it would prevent "I forgot to write the Risks section" silent bugs.

---

## Why these four are enough

OMC has 4 more candidate mental models (dossier listed 8 total), but the four above are **most directly relevant to your Go code's understanding**:

- **A1 State machine** → explains why s09 + s10 are split
- **A2 Model routing** → gives meaning to s01's `Model` field
- **A3 Middleware** → makes s03 look like a **universal design pattern**, not an OMC quirk
- **A4 Handoff documents** → explains why s09 uses the filesystem rather than sqlite

The remaining four ("Don't learn Claude Code, just use OMC" marketing posture; the skills / commands / agents three-layer abstraction; the seminar / missions teaching mode; kill-switches and graceful interrupt) are more product than architecture — extension reading.

---

## Further reading

- Upstream `docs/ARCHITECTURE.md`, `docs/HOOKS.md`, `docs/TEAM-WORKTREE-MODE.md`
- `skills/team/SKILL.md` (most complete description of the stage-handoff convention)
- Appendix B is the chapter-by-chapter source-reading map
