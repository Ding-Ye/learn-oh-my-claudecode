package main

import (
	"strings"
	"testing"
)

// TestRemoveCodeBlocksHandlesFencedAndInline — pins the order-of-operations
// in removeCodeBlocks. Both fenced ``` … ``` blocks (across newlines) and
// `inline` spans must be stripped; non-code text must survive untouched.
func TestRemoveCodeBlocksHandlesFencedAndInline(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{
			name: "fenced multi-line block",
			in:   "before\n```\nultrawork\n```\nafter",
			want: "before\n\nafter",
		},
		{
			name: "fenced single-line block",
			in:   "see ```ultrawork``` for details",
			want: "see  for details",
		},
		{
			name: "inline span",
			in:   "the `ultrawork` keyword",
			want: "the  keyword",
		},
		{
			name: "fenced AND inline",
			in:   "```ultrawork``` and `ulw` are aliases",
			// Lazy fence eats `` ```ultrawork``` `` as one block,
			// leaving " and `ulw` are aliases"; inline then eats
			// the `ulw` span. Single leading space survives.
			want: " and  are aliases",
		},
		{
			name: "no code blocks",
			in:   "plain prose with ultrawork inline",
			want: "plain prose with ultrawork inline",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := removeCodeBlocks(c.in)
			if got != c.want {
				t.Fatalf("removeCodeBlocks(%q) = %q; want %q", c.in, got, c.want)
			}
		})
	}
}

// TestIsInformationalIntentMultiLanguage — the four-language filter. Each
// language's pattern must trigger on a representative question and miss on a
// declarative form.
func TestIsInformationalIntentMultiLanguage(t *testing.T) {
	informational := []struct{ lang, in string }{
		{"en", "what is ultrawork?"},
		{"en", "how do I use ultrawork?"},
		{"ko", "ultrawork이 뭐야?"},
		{"ja", "ultrawork とは何ですか"},
		{"zh", "什么是 ultrawork"},
		{"zh", "怎么用 ultrawork"},
	}
	for _, c := range informational {
		t.Run("informational/"+c.lang+"/"+c.in, func(t *testing.T) {
			if !isInformationalIntent(c.in) {
				t.Fatalf("expected isInformationalIntent(%q) to be true (lang=%s)", c.in, c.lang)
			}
		})
	}

	imperative := []string{
		"ultrawork build a server",
		"please refactor",
		"search OAuth flow then refactor",
	}
	for _, in := range imperative {
		t.Run("imperative/"+in, func(t *testing.T) {
			if isInformationalIntent(in) {
				t.Fatalf("expected isInformationalIntent(%q) to be false", in)
			}
		})
	}
}

// TestRemoveCodeBlocksStripsBeforeTriggerMatch — integration-flavoured: the
// invariant the whole package depends on. A trigger inside a fenced block must
// be invisible to a substring search on the cleaned prompt.
func TestRemoveCodeBlocksStripsBeforeTriggerMatch(t *testing.T) {
	in := "```ultrawork``` is the topic"
	cleaned := removeCodeBlocks(in)
	if strings.Contains(strings.ToLower(cleaned), "ultrawork") {
		t.Fatalf("removeCodeBlocks left trigger visible: %q", cleaned)
	}
}
