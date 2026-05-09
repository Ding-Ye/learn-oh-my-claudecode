package main

import "fmt"

// main demonstrates the four canonical Process outcomes:
//
//  1. An imperative trigger ("ultrawork build a server") fires the keyword
//     and the prompt comes back with a directive prepended.
//  2. An informational question ("what is ultrawork?") is left alone — the
//     informational-intent filter wins over trigger detection.
//  3. A trigger living inside a fenced code block ("```ultrawork```") is
//     left alone — the code-block stripper hides it from the matcher.
//  4. A combined imperative ("search OAuth flow then refactor") fires the
//     search keyword. Note: "refactor" is not a built-in trigger; the test
//     here is that "search" alone is enough.
//
// The output is captured into testdata/expected.txt and re-asserted by the
// chapter docs' "Try It" section so any drift in directive wording is caught
// at review time.
func main() {
	cases := []struct {
		label, input string
	}{
		{"imperative ultrawork", "ultrawork build a server"},
		{"informational en  ", "what is ultrawork?"},
		{"inside code block ", "```ultrawork``` is a keyword"},
		{"imperative search ", "search OAuth flow then refactor"},
	}

	for _, c := range cases {
		out := Process(c.input, "executor", "claude-opus-4-7", BuiltIns)
		fmt.Printf("[%s]\n  in : %q\n  out: %q\n", c.label, c.input, out)
	}
}
