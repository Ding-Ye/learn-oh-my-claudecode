---
title: "s03 · Magic Keywords (Prompt Middleware)"
chapter: 3
slug: s03-magic-keywords
est_read_min: 8
---

# Chapter 3 — Magic Keywords (Prompt Middleware)

> Third chapter of `learn-oh-my-claudecode`. We pivot from "filesystem
> I/O plus a security guard" (s02) to a **pure-string transformer** —
> no I/O, no goroutines, no global state. The whole package is a regex
> pipeline that rewrites the user's prompt before any model call.

## Problem

Some prompts shouldn't go to the model verbatim. When a user types
`ultrawork build a server`, the leading word `ultrawork` is a *signal*
("activate parallel-agent mode") rather than a noun the model should
read literally. The same is true of `search`, `analyze`, and
`ultrathink`. OMC ships a "magic keyword" middleware that detects these
triggers and rewrites the prompt — typically prepending a directive
block — before forwarding to the LLM.

But the matcher has to be careful. Three trap doors:

1. **Code blocks.** A user discussing the keyword in a docs sentence —
   ``` ```ultrawork``` is a flag ``` — is *not* invoking it. Trigger
   detection that fires inside fenced or inline code generates spurious
   directives the model has to fight through.
2. **Informational questions.** `"what is ultrawork?"`, `"ultrawork이 뭐야?"`,
   `"ultrawork とは何ですか"`, `"什么是 ultrawork"` — across English, Korean,
   Japanese, and Chinese, an informational question must NOT trigger
   the keyword. The user is asking *about* the feature, not invoking it.
3. **Composition.** When the prompt fires more than one keyword
   (`ultrawork search OAuth flow` triggers both ultrawork and search),
   each keyword's rewrite has to be visible to the next keyword's
   detector. The pipeline must thread state through.

Upstream solves all three at `src/features/magic-keywords.ts` L1–L297.
This chapter ports that file, simplified, into ~180 lines of pure Go.

## Solution

A `Keyword` struct with three fields — `Triggers []string`, `Description
string`, `Action func(prompt, agentName, modelID string) string` — and a
single entry point:

```go
func Process(prompt, agentName, modelID string, kws []Keyword) string
```

`Process` walks the keyword slice in order. For each keyword: strip code
blocks, short-circuit on informational intent (multi-language regex), if
any trigger appears as a whole word in the cleaned text, invoke the
keyword's `Action` — its return value becomes the prompt for the next
iteration. Each `Action` is a closure that prepends a directive block to
the (cleaned) input.

Two regex helpers in `regex.go`:

- `removeCodeBlocks(s)` strips fenced ```` ``` ```` blocks (lazy across
  newlines via `(?s).*?`) and `\`inline\`` spans.
- `isInformationalIntent(s)` returns true if any of four per-language
  patterns matches — English `\b(what|how|why)\b.*\?`, Korean `이|뭐.*야`,
  Japanese `何|なに`, Chinese `什么|怎么`.

Four built-in keywords live in `keyword.go` (`UltraworkEnhancement`,
`SearchEnhancement`, `AnalyzeEnhancement`, `UltrathinkEnhancement`),
preserving upstream's L260–L266 order.

## How It Works

### Pipeline diagram

```
              user prompt
                  │
                  ▼
      ┌───────────────────────┐
      │ for each Keyword in   │  ◀─── ordered slice (BuiltIns)
      │   BuiltIns:           │
      │   1. removeCodeBlocks │
      │   2. isInformational? │  ◀── short-circuit (multi-lang)
      │   3. trigger match?   │  ◀── \b<trigger>\b case-insensitive
      │   4. invoke Action    │  ◀── prepend directive
      │   ┊  result := Action │
      └───────────────────────┘
                  │
                  ▼
            rewritten prompt
```

### Strip-then-match

The order of `removeCodeBlocks` then trigger match is load-bearing. If
we matched first and stripped second, a user writing `` `ultrawork` `` in
a docs sentence would still trigger the directive. Code blocks are
*context-bracketing* markers — anything inside them is being discussed,
not invoked.

```go
// regex.go — strip fenced first (which themselves contain backticks),
// then any remaining inline backticks. Reversing this order would have
// the inline-pattern eat fence delimiters.
func removeCodeBlocks(s string) string {
    s = codeBlockPattern.ReplaceAllString(s, "")
    s = inlineCodePattern.ReplaceAllString(s, "")
    return s
}
```

### Multi-language informational filter

Four small per-language regexes, ORed together, beat one giant
alternation. This is RE2 (Go's `regexp` engine), so each match is O(n)
and there's no catastrophic backtracking risk. Adding a fifth language
(Vietnamese, say) is one append:

```go
var informationalIntentPatterns = []*regexp.Regexp{
    regexp.MustCompile(`(?i)\b(what|how|why)\b.*\?`),  // en
    regexp.MustCompile(`(?s)이|뭐.*야`),                  // ko
    regexp.MustCompile(`何|なに`),                        // ja
    regexp.MustCompile(`什么|怎么`),                       // zh
}
```

## What Changed (vs. s02)

s02 was filesystem I/O plus `embed.FS` plus a security regex; the entire
mental model was "load these bytes safely". s03 is the opposite end of
the spectrum: **no I/O whatsoever**. No filesystem, no goroutines, no
context, no error returns. Every function is `string → string`. This is
the simplest mechanism in the curriculum and a deliberate breather
before s04's nested-merge work.

```diff
- // s02: loader.go — filesystem + sentinel errors
- type Loader struct{ fs embed.FS; root string }
- func (l *Loader) Load(name string) (string, error) { ... }

+ // s03: keyword.go — pure transformer, no errors possible
+ type Keyword struct {
+     Triggers    []string
+     Description string
+     Action      func(prompt, agentName, modelID string) string
+ }
+ func Process(prompt, agentName, modelID string, kws []Keyword) string
```

The signature change tells the whole story: `(string, error)` becomes
`string`. There is no failure mode here — a regex that doesn't match is
not an error, it's just a no-op. This is a useful posture to feel: not
every Go function needs to return an error.

## Try It

```bash
cd agents/s03-magic-keywords

GOWORK=off go vet ./...     # silent, no output
GOWORK=off go build ./...   # silent, no output
GOWORK=off go test -v -count=1 ./...   # 5+ tests pass
GOWORK=off go run .         # output matches testdata/expected.txt
```

Expected output:

```
[imperative ultrawork]
  in : "ultrawork build a server"
  out: "[ULTRAWORK MODE — PARALLEL AGENT ORCHESTRATION]\nbuild a server"
[informational en  ]
  in : "what is ultrawork?"
  out: "what is ultrawork?"
[inside code block ]
  in : "```ultrawork``` is a keyword"
  out: "```ultrawork``` is a keyword"
[imperative search ]
  in : "search OAuth flow then refactor"
  out: "[SEARCH MODE — EXHAUSTIVE LOOKUP]\nsearch OAuth flow then refactor"
```

Further exercises:

- Add a fifth keyword `DebugEnhancement` whose triggers are `["debug",
  "trace"]`. Make sure `"how do I debug this?"` (informational) still
  passes through, while `"debug the auth flow"` triggers it.
- Rewrite `removeCodeBlocks` to operate on a `[]byte` instead of
  `string` — measure whether the saved allocations matter.
  (Spoiler: for a typical prompt under 8 KB, no.)

## Upstream Source Reading

Excerpt from `src/features/magic-keywords.ts` L1–L297 (full annotated
copy at `upstream-readings/s03-magic-keywords.ts`):

```typescript
// L13–L22 — code-block stripping. The Go port spells `[\s\S]` as `(?s).`
const CODE_BLOCK_PATTERN = /```[\s\S]*?```/g;
const INLINE_CODE_PATTERN = /`[^`]+`/g;
function removeCodeBlocks(text: string): string {
  return text.replace(CODE_BLOCK_PATTERN, '').replace(INLINE_CODE_PATTERN, '');
}

// L25–L30 — four-language informational-intent patterns
const INFORMATIONAL_INTENT_PATTERNS: RegExp[] = [
  /\b(?:what(?:'s|\s+is)|what\s+are|how\s+(?:to|do\s+i)\s+use|explain|tell\s+me\s+about|describe)\b/i,
  /(?:뭐야|무엇(?:이야|인가요)?|어떻게|설명|사용법)/u,
  /(?:とは|って何|使い方|説明)/u,
  /(?:什么是|什麼是|怎(?:么|樣)用|如何使用|解释|说明)/u,
];

// L260–L266 — the canonical built-in slice
export const builtInMagicKeywords: MagicKeyword[] = [
  ultraworkEnhancement, searchEnhancement,
  analyzeEnhancement, ultrathinkEnhancement,
];
```

Reading notes (Go-port comparisons):

1. **L13–L22 (removeCodeBlocks) → `regex.go::removeCodeBlocks`**. Direct
   port. Order of operations is preserved — fenced first, inline second.
   The `[\s\S]*?` lazy DOTALL idiom becomes `(?s).*?` in Go.
2. **L25–L30 (informational patterns) → `regex.go`**. All four languages
   kept; `(?:…)` non-capturing wrappers dropped because Go's RE2 alternation
   is fine without them.
3. **L36–L62 (per-match 80-char window) → moved to Process scope**. The
   upstream `isInformationalKeywordContext` slides an 80-char window
   around each trigger occurrence and runs the patterns inside that
   window. The Go port runs the filter once per Process iteration on the
   full cleaned prompt — stricter, simpler.
4. **L260–L266 (built-in slice) → `keyword.go::BuiltIns`**. Same four
   entries, same order. The Go Actions emit a single directive line
   each rather than the multi-paragraph upstream blocks — the teaching
   point is the pipeline shape, not the wording of any one directive.
5. **L202–L249 (createMagicKeywordProcessor closure) → `Process`
   function**. Go's slice iteration is direct, so the factory closure
   collapses into a regular top-level function with the keyword slice
   as a parameter.
