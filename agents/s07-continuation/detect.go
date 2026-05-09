package main

import "regexp"

// Signal is the verdict DetectCompletion returns about a model's
// response. Mirror of upstream's `detectCompletionSignals` return shape
// (continuation-enforcement.ts L132-L142):
//
//	{ claimed: boolean; confidence: 'high' | 'medium' | 'low'; reason: string }
//
// We keep the same three fields. Confidence is a free-form string
// instead of a Go enum so the caller can grep for "low" / "medium" /
// "high" without an extra import; the values that ship are the same
// three the upstream uses.
type Signal struct {
	// Claimed reports whether the response *claims* completion. False
	// means no completion claim was detected at all (the response is
	// still in progress, asking a question, etc.).
	Claimed bool

	// Confidence is "high", "medium", or "low" when Claimed is true.
	// When Claimed is false the field is the empty string — there is
	// nothing to be confident about. Callers should branch on Claimed
	// first and only then read Confidence.
	Confidence string

	// Reason is a short human-readable summary of why we returned this
	// verdict. Useful for logs; never used for control flow.
	Reason string
}

// completionPatterns match phrases that suggest the model believes its
// work is done. Mirrors upstream L143-L148 with two small additions
// (`\b(done|complete|finished|implemented)\b` and `\bI have …`)
// requested by the chapter spec — the broader patterns catch more
// real-world phrasings without changing the upstream semantics.
//
// Compiled at package init via MustCompile so a typo in a regex is a
// build-time-equivalent failure on first import. The slice is package
// private; DetectCompletion reaches in through a method, not a copy.
var completionPatterns = []*regexp.Regexp{
	// Catches "I have completed all tasks", "I've finished everything",
	// etc. The leading word boundary ensures we don't match inside an
	// unrelated word.
	regexp.MustCompile(`(?i)\bI(?:'ve| have) (completed|finished|implemented)\b`),

	// "all tasks are complete", "all work is done", "all items finished"
	regexp.MustCompile(`(?i)all (?:tasks?|work|items?) (?:are |is )?(?:now )?(?:complete|done|finished)`),

	// "everything is complete", "everything has been done"
	regexp.MustCompile(`(?i)everything (?:is|has been) (?:complete|done|finished)`),

	// "no remaining tasks", "no more work"
	regexp.MustCompile(`(?i)no (?:more|remaining|outstanding) (?:tasks?|work|items?)`),

	// Bare keywords as a final catch-net — present a "done" / "complete"
	// claim even when the surrounding phrasing is terse. We keep this
	// LAST so the more specific patterns above carry the reason.
	// `working` and `works` cover the common code-context phrasing
	// "this should be working now" / "the test works"; in this chapter
	// they DO count as completion claims so the hedge layer can demote
	// them to low confidence (which is exactly the canonical
	// uncertain-completion sample the chapter ships).
	regexp.MustCompile(`(?i)\b(done|complete|finished|implemented|working|works)\b`),
}

// uncertaintyPatterns match hedge words that downgrade a completion
// claim from "high" to "low" confidence. Same set as upstream L150-L154
// plus an explicit "I think / I believe / presumably" branch the
// chapter spec calls out — these are the standard markers an LLM emits
// when it's unsure but trying to sound authoritative.
var uncertaintyPatterns = []*regexp.Regexp{
	// "should be working", "might have", "could be"
	regexp.MustCompile(`(?i)\b(should|might|could|seems|appears|probably)\b`),

	// "I think", "I believe", "presumably"
	regexp.MustCompile(`(?i)\b(I think|I believe|presumably)\b`),

	// "unless / except / but" — qualifiers that indicate scope is not
	// fully covered.
	regexp.MustCompile(`(?i)\b(unless|except|but)\b`),
}

// DetectCompletion classifies a model response as one of three states:
//
//	(1) no completion claim       -> {Claimed: false, Confidence: "",     Reason: "..."}
//	(2) confident completion      -> {Claimed: true,  Confidence: "high", Reason: "..."}
//	(3) hedged completion         -> {Claimed: true,  Confidence: "low",  Reason: "..."}
//
// The "medium" tier is reserved: the upstream type signature includes
// it, and a future version of this function could promote some hedge
// patterns there (e.g., "I think" alone might be medium; "I think this
// might possibly" is low). We keep the union open so callers can switch
// on three values without changing types later.
//
// The function is pure and allocates nothing on the no-claim path, so
// it is safe to call on every model response without a regret budget.
func DetectCompletion(response string) Signal {
	hasCompletion := false
	for _, p := range completionPatterns {
		if p.MatchString(response) {
			hasCompletion = true
			break
		}
	}

	if !hasCompletion {
		return Signal{
			Claimed:    false,
			Confidence: "",
			Reason:     "No completion claim detected",
		}
	}

	// A completion claim is on the table. Now check the uncertainty
	// markers: if any fire, we downgrade the verdict to "low" so the
	// outer runtime knows to demand verification before accepting it.
	for _, p := range uncertaintyPatterns {
		if p.MatchString(response) {
			return Signal{
				Claimed:    true,
				Confidence: "low",
				Reason:     "Completion claimed with uncertainty language",
			}
		}
	}

	return Signal{
		Claimed:    true,
		Confidence: "high",
		Reason:     "Clear completion claim detected",
	}
}
