# s07 — Continuation Enforcement / 推石上山

> Seventh chapter of `learn-oh-my-claudecode`. We pivot from s06's
> registry-of-closures to **a tiny, schema-free chapter that mixes
> embedded text with regex-driven semantic classification** of model
> output. First time `//go:embed` produces a `string` (s02 used
> `embed.FS`); first time the chapter inspects model output to grade
> the *quality* of a completion claim instead of trusting it.

## Scope (one line)

A `var SystemPromptAddition string` baked into the binary via
`//go:embed prompt_addition.md`, a `RandomReminder() string` over a
JSON-embedded reminder pool, and a `DetectCompletion(response string)
Signal` that classifies a response as no-claim, high-confidence claim,
or hedged (low-confidence) claim — ported from
`src/features/continuation-enforcement.ts` L1–L196 in ~150
stdlib-only Go lines.

## Files

| Path | Role |
|---|---|
| `prompt_addition.md` | The Sisyphus-persona system-prompt addendum, embedded into the binary. ~50 lines, four sacred rules + completion checklist + Sisyphean oath. Teaching-friendly rewrite of upstream's L60–L130 production text. |
| `reminders.json` | Seven reminder strings (`"[THE BOULDER NEVER STOPS] …"`, etc.). Parsed once on first call to `RandomReminder()`. |
| `prompt.go` | `var SystemPromptAddition string` (the `//go:embed` target), `RandomReminder() string`, and a `SeedReminderRNG(int64)` test hook. |
| `detect.go` | `Signal` struct, `completionPatterns`, `uncertaintyPatterns`, `DetectCompletion`. Pure-regex classifier with no allocations on the no-claim path. |
| `main.go` | Demo: print the first five lines of the embedded prompt; classify three sample responses; pick one reminder under a fixed seed. |
| `detect_test.go` | Five tests — high / low / no-claim verdicts, RNG rotation under a seed, persona-name pin. |
| `testdata/expected.txt` | Captured `go run .` stdout for drift detection. |
| `go.mod` | `go 1.21`, stdlib only. |

## Run

```bash
cd agents/s07-continuation

GOWORK=off go vet ./...                # silent
GOWORK=off go build ./...              # silent
GOWORK=off go test -v -count=1 ./...   # 5 tests pass, sub-second
GOWORK=off go run .                    # output matches testdata/expected.txt
```

## Three teaching points

1. **`//go:embed` returns a `string` here, not an `embed.FS`.** s02
   embedded a directory tree because the loader walked filenames; this
   chapter embeds a *single* Markdown file as a value, so `string` is
   the right type. The Go compiler validates the file at build time —
   a typo in the path is a build failure, not a runtime nil.
2. **Regex slices encode policy as data.** `completionPatterns` and
   `uncertaintyPatterns` are package-level `[]*regexp.Regexp` slices
   compiled at init via `MustCompile`. `DetectCompletion` is one for-
   loop per slice, no switch statements, and a malformed regex is
   caught at first import. This is the same "behavior as data" pattern
   s06 used for `Tool.Handler`, here applied to text classification.
3. **Hedge words demote, they don't reject.** A response that says "I
   have completed everything, but tests are still failing" is still a
   completion claim — just a low-confidence one. The runtime can use
   that signal to inject a verification reminder rather than to reject
   the message outright. This is the upstream design choice (L132–L170)
   we faithfully port.

## Anti-pattern callout

Plan §"Anti-pattern #1" reads "Pre-built CJS bundles committed to the
repo." Upstream ships `bridge/cli.cjs` (3.27 MB) and the prompt text is
inlined into that bundle by esbuild's `define` mechanism. The Go port
embeds the Markdown via `//go:embed` and produces a single binary —
no committed `bridge/` blob, no template-literal escape hell, no
build-time vs. runtime branch:

```go
//go:embed prompt_addition.md
var SystemPromptAddition string
```

One declaration, two Go-stdlib lines later you have the same string
the upstream gets via a 50 KB build-tooling chain.

## Upstream lineage

- `src/features/continuation-enforcement.ts` L1–L196 — the whole
  feature in one file. We port three of its surfaces: the reminder
  pool (L18–L24) → `reminders.json`; the system prompt addendum
  (L60–L130) → `prompt_addition.md`; and `detectCompletionSignals`
  (L132–L170) → `DetectCompletion` in `detect.go`. The `Stop`-event
  hook half (L36–L67) is *not* ported — that's the s05 hooks-pipeline
  surface, and a future chapter could compose the two.
- See `upstream-readings/s07-continuation.ts` for the annotated
  excerpt that mirrors what we ported and what we deliberately left.
