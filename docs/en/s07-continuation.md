---
title: "s07 · Continuation Enforcement (Sisyphus)"
chapter: 7
slug: s07-continuation
est_read_min: 8
---

# Chapter 7 — Continuation Enforcement (Sisyphus)

> Seventh chapter of `learn-oh-my-claudecode`. We pivot from s06's
> registry-of-closures to a tiny, schema-free chapter that mixes
> **embedded text** with **regex-driven semantic classification of
> model output**. First time `//go:embed` produces a `string` (s02
> used `embed.FS`); first time the chapter inspects model output to
> grade the *quality* of a completion claim instead of trusting it.

## Problem

Models tend to declare a task done when it isn't. Three failure modes
recur in production traces:

1. **Premature completion claims.** "I have completed all tasks." said
   while three items in the todo list are still `pending`. The user
   trusts the line; the work is half-done; the session ends in
   confusion.
2. **Hedged completion claims.** "I think this should be working now."
   This is a *probabilistic* claim — the model has not run the test,
   has not verified the file exists, has not exercised the behavior.
   It often turns out wrong, but the user has no easy signal that the
   confidence is low.
3. **No persona pressure to continue.** Without a system-prompt
   addendum that pre-commits the model to push the boulder forever,
   the model will follow its default training to wrap up politely at
   the first plausible stopping point.

Upstream solves this in `src/features/continuation-enforcement.ts`
L1–L196 with three surfaces: a reminder pool (L18–L24), a Sisyphus
system-prompt addendum (L60–L130), and a regex classifier
`detectCompletionSignals` (L132–L170). We port all three in ~150
stdlib-only Go lines.

## Solution

Three files compose the public surface:

- `prompt_addition.md` — the Sisyphus persona in plain Markdown,
  embedded into the binary via `//go:embed prompt_addition.md` as a
  `var SystemPromptAddition string`. A real runtime would prepend
  this to the agent's system prompt.
- `reminders.json` — a JSON array of reminder strings, embedded as
  `var remindersData []byte`. `RandomReminder() string` parses it
  once on first call (under `sync.Once`) and serves picks under a
  serialized `*rand.Rand` so the function is goroutine-safe by
  default.
- `detect.go` — the centerpiece. Two `[]*regexp.Regexp` slices
  (`completionPatterns`, `uncertaintyPatterns`) compiled at init via
  `MustCompile`, plus `DetectCompletion(response string) Signal`
  returning `{Claimed bool; Confidence string; Reason string}`. Three
  branches: no claim → `Claimed=false`; claim without hedge words →
  `Confidence="high"`; claim with hedge words → `Confidence="low"`.

The whole loop is ~10 lines of classification code.

## How It Works

### Embedding a single Markdown file as a string

```go
//go:embed prompt_addition.md
var SystemPromptAddition string
```

That single declaration is a teaching point in itself. s02 used
`//go:embed agents/*.md` with an `embed.FS` because the loader walked
filenames; this chapter has *one* file and wants its contents as a
value, so `string` is the right target type. The Go compiler resolves
the path at build time — a typo in `prompt_addition.md` is a build
failure, not a runtime nil.

### Regex slices encode policy as data

```go
var completionPatterns = []*regexp.Regexp{
    regexp.MustCompile(`(?i)\bI(?:'ve| have) (completed|finished|implemented)\b`),
    regexp.MustCompile(`(?i)all (?:tasks?|work|items?) (?:are |is )?(?:now )?(?:complete|done|finished)`),
    // ...
}
var uncertaintyPatterns = []*regexp.Regexp{
    regexp.MustCompile(`(?i)\b(should|might|could|seems|appears|probably)\b`),
    regexp.MustCompile(`(?i)\b(I think|I believe|presumably)\b`),
    // ...
}
```

`DetectCompletion` is one for-loop per slice. No switch statements, no
giant if-tree. Adding a new completion phrasing or a new hedge marker
is a one-line append. A malformed regex is caught at first import via
`MustCompile`'s panic — the same posture s06 uses for `Tool.Handler`
typos, here applied to text classification.

### Hedge words demote, they don't reject

```go
sig := DetectCompletion("I think this should be working now.")
// sig.Claimed    == true
// sig.Confidence == "low"
// sig.Reason     == "Completion claimed with uncertainty language"
```

A hedged claim is *still a claim*. The runtime can use the `low`
confidence signal to inject a verification reminder rather than to
reject the response outright. This is the upstream design choice
(L132–L170) we faithfully port: classify, don't gate. The runtime is
responsible for what to do with the verdict.

## What Changed (vs. s06)

s06 introduced `func` as a struct field — `Tool.Handler` is a closure
stored in a map. s07 introduces something complementary: **`//go:embed`
that returns a `string` (vs s02's `embed.FS`), plus regex-driven
semantic classification of model output**. For the first time the
chapter inspects the *meaning* of text rather than dispatching by name
or matching a file path.

| Concern | s06 | s07 |
|---|---|---|
| First-class data | `Tool` (name + category + closure) | `Signal` (Claimed + Confidence + Reason) |
| Embed shape | (none — pure registry) | `string` for prompt, `[]byte` for JSON pool |
| Behavior driver | dispatch by `name` lookup | classify by regex match |
| Policy storage | `WithDisabled` view set | `[]*regexp.Regexp` slices |
| Output | `(string, error)` from handler | `Signal` verdict for runtime use |

Two slices of regex and one struct of three fields is the entire
chapter — intentionally tiny, on purpose, because the next two
chapters (s08, s09) ramp back up into systems concerns.

## Try It

```bash
cd agents/s07-continuation

GOWORK=off go vet ./...                # silent
GOWORK=off go build ./...              # silent
GOWORK=off go test -v -count=1 ./...   # 5 tests pass, sub-second
GOWORK=off go run .                    # output matches testdata/expected.txt
```

Expected output:

```
== prompt addendum (first 5 lines, embedded via //go:embed) ==
## CONTINUATION ENFORCEMENT — THE BOULDER NEVER STOPS

### You are bound to your todo list

Like Sisyphus condemned to roll his boulder eternally, you are bound to

== DetectCompletion: three sample responses ==
[high confidence] response="I have completed all tasks."
  -> claimed=true confidence="high" reason="Clear completion claim detected"
[low confidence] response="I think this should be working now."
  -> claimed=true confidence="low" reason="Completion claimed with uncertainty language"
[no claim] response="Still investigating the bug."
  -> claimed=false confidence="" reason="No completion claim detected"

== one reminder (seeded for fixture stability) ==
[VERIFICATION GATE] Before claiming completion, run the tests and re-read the todo list. If anything is pending, continue.
```

Three classifications cover every branch of `DetectCompletion`. The
final `RandomReminder()` call uses a fixed seed so the captured
fixture is reproducible — a real runtime would seed off `time.Now()`.

Further exercises:

- Edit `reminders.json` to add a new line. Re-run `go run .` — the
  embed picks it up on the next `go build` with no Go-side change.
- Compose with s05's hooks pipeline: register a `Stop` hook that
  calls `DetectCompletion` over the model's last response and emits
  `RandomReminder()` when the verdict is `Claimed=true,
  Confidence="low"`. That single composition gives you the full
  upstream "boulder never stops" behavior.

## Upstream Source Reading

Excerpt from `src/features/continuation-enforcement.ts` L1–L196 (full
annotated copy at `upstream-readings/s07-continuation.ts`):

```ts
// L17-L24 — the reminder pool (5 escalating strings)
const CONTINUATION_REMINDERS = [
  '[SYSTEM REMINDER - TODO CONTINUATION] Incomplete tasks remain ...',
  '[TODO CONTINUATION ENFORCED] Your todo list has incomplete items. The boulder does not stop. ...',
  '[OMC REMINDER] You attempted to stop with incomplete work. ...',
  '[CONTINUATION REQUIRED] Incomplete tasks detected. You are BOUND to your todo list. ...',
  '[THE BOULDER NEVER STOPS] Your work is not done. ...'
];

// L132-L170 — the completion-signal classifier
export function detectCompletionSignals(response: string) {
  const completionPatterns = [
    /all (?:tasks?|work|items?) (?:are |is )?(?:now )?(?:complete|done|finished)/i,
    /I(?:'ve| have) (?:completed|finished|done) (?:all|everything)/i,
    /everything (?:is|has been) (?:complete|done|finished)/i,
    /no (?:more|remaining|outstanding) (?:tasks?|work|items?)/i,
  ];
  const uncertaintyPatterns = [
    /(?:should|might|could) (?:be|have)/i,
    /I think|I believe|probably|maybe/i,
    /unless|except|but/i,
  ];
  // ... three-branch verdict
}
```

Reading notes (Go-port comparisons):

1. **L17–L24 (reminder pool) → `reminders.json`.** Upstream stores the
   pool as a TypeScript `const` array and ships it inlined into the
   compiled bundle. The Go port stores the same strings in a JSON file
   embedded via `//go:embed`, parsed once on first call under
   `sync.Once`. The shape is identical; the *editability* is better:
   a curious student edits `reminders.json` and re-runs `go run .`
   without touching Go source.
2. **L60–L130 (system prompt addendum) → `prompt_addition.md`.**
   Upstream embeds the Sisyphus persona as a TypeScript template
   literal (`export const continuationSystemPromptAddition = \`...\``).
   The Go port embeds Markdown via `//go:embed prompt_addition.md`
   into a `string`. We rewrote the body in teaching-friendly prose —
   same four sacred rules, same checklist, same oath, but the tone
   is calibrated for someone *reading the source*, not the model
   that will receive it inline.
3. **L132–L170 (`detectCompletionSignals`) → `DetectCompletion`.**
   Field-for-field shape match: `{claimed, confidence, reason}` →
   `Signal{Claimed, Confidence, Reason}`. The two regex slices port
   directly; we add a small handful of patterns the chapter spec
   calls for (`\b(done|complete|finished|implemented)\b`,
   `\b(I think|I believe|presumably)\b`) so the classifier matches
   common terse phrasings that upstream's broader regexes miss.
4. **L36–L67 (`createContinuationHook`) — NOT ported.** Upstream wires
   the classifier to a `Stop` hook with `hasIncompleteTasks = false`
   as a placeholder until real todo-state is available. The Go
   chapter intentionally leaves this surface for a future composer:
   s05 already teaches the hook pipeline, so combining the two is
   one-page exercise rather than three more files.
5. **L172–L196 (`generateVerificationPrompt`) — NOT ported.** A small
   helper that asks the model to self-verify before concluding. Out
   of scope: the *verdict* from `DetectCompletion` is the signal a
   runtime would act on; the verification prompt is one possible
   action, not the only one.
6. **The `medium` confidence tier exists in the type but never fires.**
   Upstream's union is `'high' | 'medium' | 'low'` and so is ours,
   but neither implementation ever emits `medium`. We keep the slot
   open so a future refinement (e.g., "I think" alone is medium; "I
   think this might possibly" is low) can land without changing the
   `Signal` shape.
