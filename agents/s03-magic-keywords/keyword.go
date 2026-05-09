// Package main implements the s03 magic-keywords prompt-rewriting middleware.
//
// A "magic keyword" is a trigger word (think `ultrawork`, `ultrathink`,
// `search`, `analyze`) which — when spotted in the user's prompt — rewrites
// that prompt before the model ever sees it. Rewriting in: out: a transformed
// string. No I/O, no goroutines, no hidden global state. The whole package is
// a pure-string transformer that fits in two files plus tests.
//
// The data shape is a single struct, mirroring upstream
// `src/features/magic-keywords.ts` L8 (`MagicKeyword`). Each keyword carries:
//
//   - Triggers:    the words that activate it (case-insensitive, word-boundary
//                  anchored — see hasActionableTrigger below).
//   - Description: a human-readable label, useful for `/list-keywords` style
//                  introspection. Not currently consumed by Process.
//   - Action:      the rewrite. Receives the *current* prompt (already rewritten
//                  by any earlier-running keyword), the agent name and model id
//                  (so e.g. ultrawork can produce model-specific guidance), and
//                  returns the next prompt.
//
// The Action signature deliberately accepts the agent and model parameters even
// when most built-ins ignore them — keeping the type uniform lets us iterate
// over a heterogeneous keyword slice without per-call type assertions.
package main

import (
	"regexp"
	"strings"
)

// Keyword is the canonical shape every built-in magic-keyword and every user
// extension shares. Compare with upstream `MagicKeyword` interface (L8).
type Keyword struct {
	// Triggers are the literal words that activate this keyword. Matched
	// case-insensitively at word boundaries (see hasActionableTrigger).
	Triggers []string

	// Description is a one-line human label. Surfaced by future "list
	// keywords" tooling; not consumed by Process today.
	Description string

	// Action receives the prompt as it stands at this stage of the pipeline
	// (after any earlier keyword has rewritten it) and returns the next
	// version. agentName / modelID may be empty strings — Action MUST handle
	// that. Action MUST be deterministic and side-effect free.
	Action func(prompt, agentName, modelID string) string
}

// Process is the entry point — the equivalent of upstream's
// `createMagicKeywordProcessor` closure (L202–L249). It walks each keyword in
// order; when a keyword's trigger fires (in an *actionable* context — i.e. not
// inside a code block and not inside an informational question), the keyword's
// Action is invoked and its return value becomes the prompt for the next
// iteration. Iteration order matters: a later keyword observes the output of
// every earlier keyword.
//
// Two contexts that disable a trigger:
//  1. The keyword sits inside a fenced ``` … ``` or `inline` code block.
//     Stripping these before matching avoids "the keyword is being discussed,
//     not invoked" false positives.
//  2. The keyword sits inside an informational question ("what is ultrawork?",
//     "ultrawork이 뭐야?"). isInformationalIntent recognises this in four
//     languages.
//
// Both filters live in regex.go. Process here is glue.
func Process(prompt, agentName, modelID string, kws []Keyword) string {
	result := prompt
	for _, kw := range kws {
		// Strip code blocks once per iteration: each Action may have
		// inserted new fenced content (e.g. ultrawork prepends
		// directives) and we want the *next* keyword's match to ignore
		// that new content too.
		cleaned := removeCodeBlocks(result)

		// Informational-intent check is global to the prompt: if the
		// user is *asking about* the keyword, no keyword should fire.
		if isInformationalIntent(cleaned) {
			continue
		}

		fired := false
		for _, trigger := range kw.Triggers {
			if hasActionableTrigger(cleaned, trigger) {
				fired = true
				break
			}
		}
		if !fired {
			continue
		}

		result = kw.Action(result, agentName, modelID)
	}
	return result
}

// hasActionableTrigger reports whether `trigger` appears as a whole word in
// `text`. Whole-word matching avoids "ultraworkstation" triggering ultrawork.
// Upstream calls this `hasActionableTrigger` and additionally checks an
// 80-character-window informational-intent test per match site
// (`magic-keywords.ts` L36–L62). We hoist that check up to Process scope —
// simpler, slightly stricter, and good enough for the teaching version.
func hasActionableTrigger(text, trigger string) bool {
	pattern := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(trigger) + `\b`)
	return pattern.MatchString(text)
}

// removeTriggerWords mirrors upstream L186–L194. It strips every occurrence of
// every trigger from `prompt`. Used by Action implementations that prepend a
// directive — without this strip, the trigger word would appear twice (once in
// the directive header, once still inline in the user's text).
func removeTriggerWords(prompt string, triggers []string) string {
	out := prompt
	for _, t := range triggers {
		pattern := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(t) + `\b`)
		out = pattern.ReplaceAllString(out, "")
	}
	return strings.TrimSpace(out)
}

// ---------------------------------------------------------------------------
// Built-in keywords. Mirrors `builtInMagicKeywords` (L260–L266) one-for-one.
// ---------------------------------------------------------------------------
//
// Each Action prepends a single directive line plus the (cleaned) original
// prompt. The upstream Actions construct elaborate multi-paragraph blocks; we
// reduce them to a single recognisable directive so that tests can assert on
// the prefix without freezing 30 lines of prose. The teaching point is the
// pipeline shape, not the wording of any one directive.

// UltraworkEnhancement activates parallel-agent orchestration mode.
// Upstream: L67–L77.
var UltraworkEnhancement = Keyword{
	Triggers:    []string{"ultrawork", "ulw", "uw"},
	Description: "Activates maximum performance mode with parallel agent orchestration",
	Action: func(prompt, agentName, modelID string) string {
		clean := removeTriggerWords(prompt, []string{"ultrawork", "ulw", "uw"})
		return "[ULTRAWORK MODE — PARALLEL AGENT ORCHESTRATION]\n" + clean
	},
}

// SearchEnhancement maximizes search effort. Upstream: L82–L109.
var SearchEnhancement = Keyword{
	Triggers:    []string{"search", "find", "locate", "lookup", "explore", "discover", "scan", "grep", "query", "browse", "detect", "trace", "seek", "track", "pinpoint", "hunt"},
	Description: "Maximizes search effort and thoroughness",
	Action: func(prompt, agentName, modelID string) string {
		return "[SEARCH MODE — EXHAUSTIVE LOOKUP]\n" + prompt
	},
}

// AnalyzeEnhancement activates deep investigation mode. Upstream: L113–L143.
var AnalyzeEnhancement = Keyword{
	Triggers:    []string{"analyze", "analyse", "investigate", "examine", "study", "deep-dive", "inspect", "audit", "evaluate", "assess", "review", "diagnose", "scrutinize", "dissect", "debug", "comprehend", "interpret", "breakdown", "understand"},
	Description: "Activates deep analysis and investigation mode",
	Action: func(prompt, agentName, modelID string) string {
		return "[ANALYZE MODE — CONTEXT GATHERING]\n" + prompt
	},
}

// UltrathinkEnhancement activates extended reasoning. Upstream: L148–L182.
var UltrathinkEnhancement = Keyword{
	Triggers:    []string{"ultrathink", "think", "reason", "ponder"},
	Description: "Activates extended thinking mode for deep reasoning",
	Action: func(prompt, agentName, modelID string) string {
		clean := removeTriggerWords(prompt, []string{"ultrathink", "think", "reason", "ponder"})
		return "[ULTRATHINK MODE — EXTENDED REASONING]\n" + clean
	},
}

// BuiltIns is the canonical ordered slice of all built-in keywords. Order
// matters because earlier Actions feed later ones; the upstream order
// (ultrawork → search → analyze → ultrathink) is preserved.
var BuiltIns = []Keyword{
	UltraworkEnhancement,
	SearchEnhancement,
	AnalyzeEnhancement,
	UltrathinkEnhancement,
}
