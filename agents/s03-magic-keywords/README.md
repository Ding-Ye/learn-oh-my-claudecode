# s03 — Magic Keywords (Prompt Middleware) / 魔法关键词

> Third chapter of `learn-oh-my-claudecode`. Pure-string transform, zero
> I/O, zero state. The whole package is a regex pipeline that rewrites
> the user's prompt before any model call — middleware, not a service.

## Scope (one line)

A `Process(prompt, agentName, modelID, []Keyword) string` pipeline plus
two regex helpers (`removeCodeBlocks`, `isInformationalIntent`) and four
built-in keywords (`Ultrawork`, `Search`, `Analyze`, `Ultrathink`),
porting upstream `src/features/magic-keywords.ts` L1–L297 into ~180 Go
lines that fit on a screen.

## Files

| Path | Role |
|---|---|
| `keyword.go` | `Keyword` struct, `Process` pipeline, `hasActionableTrigger` / `removeTriggerWords` helpers, the four `BuiltIns`. |
| `regex.go` | `removeCodeBlocks` (fenced + inline) and `isInformationalIntent` (4-language: en/ko/ja/zh). |
| `main.go` | Demo: four canonical inputs (imperative trigger, informational question, code-block-only trigger, multi-keyword imperative). |
| `keyword_test.go` | Five tests covering happy path, Korean informational, code-block ignore, multi-keyword order, no-trigger pass-through. |
| `regex_test.go` | Three tests covering the two regex helpers and their interaction. |
| `testdata/expected.txt` | Captured `go run .` stdout — drift detector for the docs' "Try It" block. |
| `go.mod` | `go 1.21`, stdlib only, no external deps. |

## Run

```bash
cd agents/s03-magic-keywords

GOWORK=off go vet ./...     # silent
GOWORK=off go build ./...   # silent
GOWORK=off go test -v -count=1 ./...   # 5+ tests pass
GOWORK=off go run .         # output matches testdata/expected.txt
```

## Three teaching points

1. **Middleware shape: pure transformer, no state.** Each `Keyword` is a
   struct carrying a slice of triggers and an `Action func` field. The
   pipeline is just `for _, kw := range kws { result = kw.Action(result, …) }`.
   No goroutines, no channels, no context — string in, string out. This
   is the simplest possible "prompt rewriting before LLM call" pattern,
   and it composes by sequencing: a later keyword sees the output of
   every earlier keyword.
2. **Strip-then-match.** Code blocks are stripped *before* trigger
   matching so the user can write `` `ultrawork` `` in a docs sentence
   without invoking it. Going the other way — stripping after matching —
   would still flag the keyword. The order is load-bearing.
3. **Multi-language intent filter.** Four small per-language regexes
   beat one giant alternation. Adding Vietnamese is one line; debugging
   a 30-alternative monster regex is hours. Go's `regexp` is RE2 (no
   backtracking), so each `MatchString` is O(n) — running four of them
   sequentially is still O(n).

## Upstream lineage

- `src/features/magic-keywords.ts` L1–L40 — `removeCodeBlocks`,
  `INFORMATIONAL_INTENT_PATTERNS`, the 80-character context window.
- `src/features/magic-keywords.ts` L260–L297 — `builtInMagicKeywords`,
  `createMagicKeywordProcessor`, `detectMagicKeywords`.
- See `upstream-readings/s03-magic-keywords.ts` for the annotated
  excerpt with Go-port comparisons.
