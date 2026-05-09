# Raw body fixture

This file intentionally has no YAML frontmatter. The loader's
frontmatter-strip regex must NOT match it, and Load must return the
contents verbatim. Used by TestLoadReturnsRawBodyWhenNoFrontmatter.
