package main

import "regexp"

// codeBlockPattern matches a fenced code block, lazy across newlines.
// Upstream: `magic-keywords.ts` L14, `/```[\s\S]*?```/g`. Go's regexp engine
// doesn't accept the `[\s\S]` idiom — `.` in the default mode already excludes
// only `\n`, and `(?s)` flips DOTALL on. The `(?s)` flag plus `.*?` (lazy
// matching) is the canonical Go translation.
var codeBlockPattern = regexp.MustCompile("(?s)```.*?```")

// inlineCodePattern matches a backtick-delimited inline span.
// Upstream: L15, `/`[^`]+`/g`. This deliberately uses negated char class
// rather than `.*?` because inline code never contains a backtick.
var inlineCodePattern = regexp.MustCompile("`[^`]+`")

// removeCodeBlocks strips fenced and inline code from `s`. The result is what
// keyword detection should match against — a user *discussing* `ultrawork` in
// a code example is not invoking it.
//
// Order matters: strip fenced blocks first (which themselves contain
// backticks), then any remaining inline backticks. Reversing this order would
// have inlineCodePattern eat fence-delimiter backticks and leave behind the
// fenced body unstripped.
//
// Upstream: L20–L22.
func removeCodeBlocks(s string) string {
	s = codeBlockPattern.ReplaceAllString(s, "")
	s = inlineCodePattern.ReplaceAllString(s, "")
	return s
}

// informationalIntentPatterns is the multi-language ("en/ko/ja/zh") signal
// that the prompt is *asking about* a keyword rather than *invoking* it.
// Upstream: L25–L30.
//
// Two simplifications versus the upstream:
//
//  1. Go's `regexp` rejects some Unicode lookbehind / non-capturing-group
//     forms the upstream uses (`(?:뭐야|...)`). We rewrite them as plain
//     alternation — no semantics lost; the upstream non-capturing wrappers
//     were only there to keep the |-alternative scoped, which alternation at
//     the top level already does.
//
//  2. We anchor English patterns more loosely than upstream (`what`, `how`,
//     `why` plus a `?` somewhere later) because the upstream's wider net
//     ("explain", "tell me about", …) doubles the regex without doubling the
//     teaching value.
//
// Despite the simplifications, all four languages are kept — the design point
// is that magic keywords are language-aware middleware, not English-only.
var informationalIntentPatterns = []*regexp.Regexp{
	// English — a `what`/`how`/`why` followed (anywhere) by a question mark.
	// `(?i)` makes the leading word case-insensitive without requiring an
	// outer flag.
	regexp.MustCompile(`(?i)\b(what|how|why)\b.*\?`),

	// Korean — "이" object marker (often appears as "X이 뭐야?") or "뭐"
	// (what) followed eventually by "야" (the casual is/are copula).
	// `(?s)` is needed for the `.*` since prompts may contain newlines.
	regexp.MustCompile(`(?s)이|뭐.*야`),

	// Japanese — "何" (what) or its kana form "なに".
	regexp.MustCompile(`何|なに`),

	// Chinese — "什么" (what) or "怎么" (how). Both Simplified — Traditional
	// equivalents (`什麼`, `怎樣`) would extend this list; we keep two for
	// clarity. The teaching point is the multi-language pipeline, not
	// orthographic completeness.
	regexp.MustCompile(`什么|怎么`),
}

// isInformationalIntent reports whether any of the four-language patterns
// match `s`. Used by Process to short-circuit keyword firing in informational
// contexts.
//
// Why the union-of-languages design instead of one giant regex? Two reasons:
//   - Per-language regexes are individually readable; one merged regex with
//     20 alternatives is not.
//   - When a future user adds a fifth language (Vietnamese, say), they
//     append one entry; they don't refactor an alternation tree.
func isInformationalIntent(s string) bool {
	for _, p := range informationalIntentPatterns {
		if p.MatchString(s) {
			return true
		}
	}
	return false
}
