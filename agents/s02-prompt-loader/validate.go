package main

import "regexp"

// validNamePattern is the canonical guard for agent names. It mirrors
// upstream's `^[a-z0-9-]+$` regex (src/agents/utils.ts L91-L94) and exists
// to defeat path-traversal attempts before the loader ever touches the
// embedded filesystem. The pattern is deliberately strict:
//
//   - lowercase ASCII letters, digits, and hyphens only;
//   - no dots, slashes, backslashes, or whitespace;
//   - no empty string ("+" requires at least one character);
//   - no uppercase (the upstream regex uses /i, but our embed.FS keys are
//     all lowercase, so accepting "FoO" would just produce a confusing
//     ErrAgentNotFound — failing fast at validateName is friendlier).
//
// Compiled once at package init via MustCompile; the panic-on-bad-pattern
// behavior is desirable here because the regex is a compile-time constant.
var validNamePattern = regexp.MustCompile(`^[a-z0-9-]+$`)

// validateName returns ErrInvalidName if name does not match the strict
// `^[a-z0-9-]+$` policy. Any error from this function should be surfaced
// to the caller verbatim — never converted into "not found" — because the
// distinction matters for security audit trails.
func validateName(name string) error {
	if !validNamePattern.MatchString(name) {
		return ErrInvalidName
	}
	return nil
}
