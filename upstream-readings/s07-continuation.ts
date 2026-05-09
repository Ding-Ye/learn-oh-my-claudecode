// Source: https://raw.githubusercontent.com/Yeachan-Heo/oh-my-claudecode/main/src/features/continuation-enforcement.ts
// Lines:  L1-L196 (the whole file)
// License: MIT, Copyright (c) 2025 Yeachan Heo
//
// Annotated excerpt for s07. The upstream feature has THREE surfaces:
//   (1) a reminder pool used to nudge the model when it stops early;
//   (2) a system-prompt addendum that pre-commits the model to never
//       stop early ("the Sisyphus persona");
//   (3) a regex classifier that grades completion claims.
//
// We port surfaces (1)-(3); the Stop-event hook half (L36-L67) belongs
// to s05's pipeline and is intentionally left for a future composer.
// Read alongside Section 6 of the s07 chapter docs.

import type { HookDefinition, HookContext, HookResult } from '../shared/types.js';

// ─── (1) Reminder pool (L17-L24) ───
//
// Five escalating reminders. The runtime picks one at random when the
// model attempts to stop with incomplete work; the language is
// deliberately blunt because the goal is to break the "I think I'm
// done" reflex, not to be polite. Our Go port stores the same shape in
// `reminders.json`, embedded into the binary via `//go:embed` so
// editing the pool requires no code change.
const CONTINUATION_REMINDERS = [
  '[SYSTEM REMINDER - TODO CONTINUATION] Incomplete tasks remain in your todo list. Continue working on the next pending task. ...',
  '[TODO CONTINUATION ENFORCED] Your todo list has incomplete items. The boulder does not stop. ...',
  '[OMC REMINDER] You attempted to stop with incomplete work. ...',
  '[CONTINUATION REQUIRED] Incomplete tasks detected. You are BOUND to your todo list. ...',
  '[THE BOULDER NEVER STOPS] Your work is not done. ...'
];

// ─── (2) Stop-event hook (L36-L67) — NOT ported here ───
//
// A HookDefinition consumed by the s05 hooks-pipeline. It would, in a
// real runtime, examine todo state and inject a reminder. The
// placeholder `hasIncompleteTasks = false` makes this branch a no-op
// in upstream too, pending real-todo-state wiring. Our chapter does
// not port this surface — composing s05 + s07 is a future exercise.
export function createContinuationHook(): HookDefinition {
  return {
    event: 'Stop',
    handler: async (_ctx: HookContext): Promise<HookResult> => ({ continue: true }),
  };
}

// ─── (3) System-prompt addendum (L70-L130) ───
//
// The Sisyphus persona in 60 lines of Markdown wrapped in a template
// literal. Four "sacred rules", a completion checklist, three "when
// can you stop" conditions, anti-stopping mechanisms, and the oath.
// The teaching repo rewrites this in plain prose under
// `prompt_addition.md` and embeds it via `//go:embed` — the upstream
// ships it as a TypeScript string, our version ships it as a file the
// reader can edit without recompiling.
export const continuationSystemPromptAddition = `
## CONTINUATION ENFORCEMENT - THE BOULDER NEVER STOPS

### YOU ARE BOUND TO YOUR TODO LIST

Like Sisyphus condemned to roll his boulder eternally, you are BOUND
to your task list. Stopping with incomplete work is not a choice -
it is a FAILURE. ...

### THE SACRED RULES OF PERSISTENCE

**RULE 1: NEVER ABANDON INCOMPLETE WORK** ...
**RULE 2: VERIFICATION IS MANDATORY** ...
**RULE 3: BLOCKERS ARE OBSTACLES TO OVERCOME** ...
**RULE 4: THE COMPLETION CHECKLIST** ...

### THE SISYPHEAN OATH

"I will not rest until my work is done.
 I will not claim completion without verification.
 I will not abandon my users mid-task.
 The boulder stops at the summit, or not at all."
`;

// ─── (4) detectCompletionSignals (L132-L170) — the chapter's centerpiece ───
//
// Two regex slices. Completion patterns assert "I have done X" / "all
// tasks complete". Uncertainty patterns flag hedge words like
// `should|might|could`, `I think|I believe|probably`, and
// `unless|except|but`. The verdict is one of three: no claim,
// confident claim, hedged claim. The Go port mirrors the shape
// field-for-field as `DetectCompletion(response) Signal`.
export function detectCompletionSignals(response: string): {
  claimed: boolean;
  confidence: 'high' | 'medium' | 'low';
  reason: string;
} {
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
  const hasCompletion = completionPatterns.some(p => p.test(response));
  const hasUncertainty = uncertaintyPatterns.some(p => p.test(response));
  if (!hasCompletion) return { claimed: false, confidence: 'high', reason: 'No completion claim detected' };
  if (hasUncertainty) return { claimed: true, confidence: 'low', reason: 'Completion claimed with uncertainty' };
  return { claimed: true, confidence: 'high', reason: 'Clear completion claim detected' };
}

// ─── (5) generateVerificationPrompt (L172-L196) — NOT ported ───
//
// A small helper that asks the model to verify its own work before
// concluding. Out of scope for the chapter — would be a one-liner in
// Go that we elide because the `DetectCompletion` verdict is the
// signal a runtime would act on.
