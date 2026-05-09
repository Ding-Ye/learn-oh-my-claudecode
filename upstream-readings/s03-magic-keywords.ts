// Source: src/features/magic-keywords.ts (Yeachan-Heo/oh-my-claudecode, MIT)
// Lines: L1–L40 + L260–L297 (core types + built-ins)
// License: MIT, Copyright (c) 2025 Yeachan Heo
//
// Annotated excerpt for s03. The Go port collapses upstream's per-match
// 80-character-window check into a single Process-scope informational-intent
// filter, and replaces the regex `[\s\S]` DOTALL idiom with Go's `(?s)` flag.
// Read alongside Section 6 of the s03 chapter docs.

// ---------------------------------------------------------------------------
// (1) Code-block stripping (L13–L22).
// ---------------------------------------------------------------------------
//
// Two patterns: fenced ```...``` (lazy across newlines) and inline `...`.
// The Go port spells DOTALL `(?s).*?` instead of `[\s\S]*?` because the Go
// regexp engine has no `[\s\S]` shorthand.
const CODE_BLOCK_PATTERN = /```[\s\S]*?```/g;
const INLINE_CODE_PATTERN = /`[^`]+`/g;

function removeCodeBlocks(text: string): string {
  return text.replace(CODE_BLOCK_PATTERN, '').replace(INLINE_CODE_PATTERN, '');
}

// ---------------------------------------------------------------------------
// (2) The four-language informational-intent filter (L25–L37).
// ---------------------------------------------------------------------------
//
// ⭐ The chapter's most distinctive design choice. The patterns recognise
// "what / how / why" questions in English, "뭐야 / 어떻게 / 설명" in Korean,
// "とは / 何 / 使い方" in Japanese, and "什么是 / 怎么用 / 解释" in Chinese.
// Upstream slides an 80-character window around each trigger position and
// runs all four patterns inside that window. The Go port simplifies to a
// global per-prompt match — slightly stricter, far easier to reason about.
const INFORMATIONAL_INTENT_PATTERNS: RegExp[] = [
  /\b(?:what(?:'s|\s+is)|what\s+are|how\s+(?:to|do\s+i)\s+use|explain|tell\s+me\s+about|describe)\b/i,
  /(?:뭐야|무엇(?:이야|인가요)?|어떻게|설명|사용법)/u,
  /(?:とは|って何|使い方|説明)/u,
  /(?:什么是|什麼是|怎(?:么|樣)用|如何使用|解释|说明)/u,
];
const INFORMATIONAL_CONTEXT_WINDOW = 80;

// ---------------------------------------------------------------------------
// (3) Built-in keyword list (L260–L266).
// ---------------------------------------------------------------------------
//
// Order matters — earlier keywords' Actions feed into later ones. The Go
// port preserves this exact order in `BuiltIns`.
export const builtInMagicKeywords: MagicKeyword[] = [
  ultraworkEnhancement,
  searchEnhancement,
  analyzeEnhancement,
  ultrathinkEnhancement,
];

// ---------------------------------------------------------------------------
// (4) The processor closure (L202–L249, abbreviated).
// ---------------------------------------------------------------------------
//
// Returns a (prompt, agentName, modelId) → string function that walks the
// keyword list in order. Each iteration:
//   a. clean = removeCodeBlocks(result)
//   b. fired = any trigger in cleaned text (via hasActionableTrigger)
//   c. if fired: result = keyword.action(result, agentName, modelId)
// The Go port translates this 1:1 into the Process function — no closure,
// since Go's iteration over `[]Keyword` is direct.
export function createMagicKeywordProcessor(): (p: string, a?: string, m?: string) => string {
  const keywords = builtInMagicKeywords;
  return (prompt, agentName, modelId) => {
    let result = prompt;
    for (const keyword of keywords) {
      const cleaned = removeCodeBlocks(result);
      const fired = keyword.triggers.some(t => hasActionableTrigger(cleaned, t));
      if (fired) result = keyword.action(result, agentName, modelId);
    }
    return result;
  };
}

// Reading map (Go-port comparisons):
//
// 1. **L13–L22 (removeCodeBlocks) → Go's regex.go::removeCodeBlocks**.
//    Direct port. The only subtlety is the `[\s\S]` → `(?s).*?` substitution
//    forced by Go's regexp engine.
//
// 2. **L25–L37 (INFORMATIONAL_INTENT_PATTERNS) → Go's regex.go**. We keep all
//    four languages but simplify each pattern: the upstream patterns use
//    `(?:…)` non-capturing groups and `\b` word boundaries; Go's RE2 supports
//    plain alternation just fine, so we drop the wrappers.
//
// 3. **L36–L62 (per-match 80-char window) → moved to Process scope**. The
//    upstream slides a window around each trigger occurrence; the Go port
//    runs the filter once at the top of each Process iteration. Stricter,
//    simpler, sufficient for the teaching scope.
//
// 4. **L186–L194 (removeTriggerWords) → Go's keyword.go::removeTriggerWords**.
//    Used by Actions that prepend a directive — without the strip the trigger
//    word would appear twice (once in the directive header, once inline).
//
// 5. **L260–L266 (builtInMagicKeywords) → Go's keyword.go::BuiltIns**.
//    Same four entries, same order. The Go Action functions emit a single
//    directive line each rather than the multi-paragraph upstream blocks
//    — teaching the pipeline shape, not the wording.
//
// 6. **L202–L249 (createMagicKeywordProcessor) → Go's keyword.go::Process**.
//    Closure → top-level function: Go can pass a `[]Keyword` directly so the
//    factory pattern collapses into a parameter.
