---
title: "s02 · Prompt Loader with embed.FS"
chapter: 2
slug: s02-prompt-loader
est_read_min: 9
---

# Chapter 2 — Prompt Loader with embed.FS

> Second chapter of `learn-oh-my-claudecode`. We pivot from "pure data"
> (s01) to "pure data plus a regex and a security guard" — introducing
> Go's `//go:embed` for build-time bundling, the standard `regexp`
> package, and sentinel errors that callers can match with `errors.Is`.

## Problem

Each of OMC's 19 named roles (`architect`, `executor`, `explore`, …) has
a system prompt stored as a separate Markdown file with YAML frontmatter
at the top. The frontmatter declares metadata (`name`, `description`,
`model`, `level`, `disallowedTools`); the body below the second `---`
fence is the actual prompt text the model sees.

Three jobs need to happen on every load:

1. **Embed the file in the binary** — production deployments cannot rely
   on `agents/architect.md` existing on the host filesystem at runtime.
2. **Validate the requested name** — `loader.Load("../../etc/passwd")`
   must be refused before it ever becomes a path. Path traversal is the
   classic file-loader CVE; we do not roll our own escape logic.
3. **Strip the frontmatter** — the model only consumes the body. The
   leading YAML envelope must come off before the string is returned.

Upstream solves the same three problems in `src/agents/utils.ts`
L83–L131 (`loadAgentPrompt`), but it has to maintain *two* code paths:
one for the esbuild-built CJS bundle (where prompts are inlined into a
`__AGENT_PROMPTS__` global) and one for `node` running the source
directly (where it `readFileSync`s from disk). That dual-mode loader is
30+ lines of plumbing this chapter deletes outright.

## Solution

Go's `//go:embed` directive bakes any directory tree into the binary at
compile time, exposing the bytes through `embed.FS` — an `fs.FS` you can
read from at runtime exactly the same way whether the host filesystem
has the files or not. Combined with:

- `regexp.MustCompile(\`^[a-z0-9-]+$\`)` for name validation, and
- `regexp.MustCompile(\`^---\n(?s).*?\n---\n\s*\`)` for frontmatter strip,

the entire loader fits in 60 lines. One code path, no build-time forks,
no runtime/test divergence. Errors come back as two exported sentinels:
`ErrInvalidName` (caller probably attacked us) and `ErrAgentNotFound`
(caller mistyped). The distinction matters — `errors.Is` lets callers
classify and route audit signals.

## How It Works

### Big picture

```
       compile-time                                 runtime
  ┌────────────────────┐                  ┌──────────────────────────┐
  │  //go:embed agents │                  │   loader.Load("name")    │
  │     ──> embed.FS   │ ───── bound ───▶ │      │                   │
  │                    │                  │      ▼                   │
  │  agents/           │                  │  validateName?           │
  │   architect.md     │                  │   reject ⇒ ErrInvalidName│
  │   executor.md      │                  │      │ pass              │
  │   explore.md       │                  │      ▼                   │
  └────────────────────┘                  │  fs.ReadFile             │
                                          │   miss ⇒ ErrAgentNotFound│
                                          │      │ ok                │
                                          │      ▼                   │
                                          │  frontmatterPattern.     │
                                          │   ReplaceAllString       │
                                          │      │                   │
                                          │      ▼                   │
                                          │  body string             │
                                          └──────────────────────────┘
```

### Core code

```go
// loader.go (the load-bearing 30 lines)
var ErrInvalidName   = errors.New("invalid agent name (must match ^[a-z0-9-]+$)")
var ErrAgentNotFound = errors.New("agent prompt not found")

var frontmatterPattern = regexp.MustCompile(`^---\n(?s).*?\n---\n\s*`)

type Loader struct{ fs embed.FS; root string }

func New(fs embed.FS, root string) *Loader { return &Loader{fs: fs, root: root} }

func (l *Loader) Load(name string) (string, error) {
    if err := validateName(name); err != nil { return "", err }   // 1
    relPath := l.root + "/" + name + ".md"                         // 2
    data, err := l.fs.ReadFile(relPath)                            // 3
    if err != nil {
        if errors.Is(err, fs.ErrNotExist) { return "", ErrAgentNotFound }
        return "", err
    }
    return frontmatterPattern.ReplaceAllString(string(data), ""), nil  // 4
}

// validate.go
var validNamePattern = regexp.MustCompile(`^[a-z0-9-]+$`)
func validateName(name string) error {
    if !validNamePattern.MatchString(name) { return ErrInvalidName }
    return nil
}
```

### Non-obvious points

1. **Why string concatenation, never `filepath.Join`.** `filepath.Join`
   helpfully cleans `..` segments — which is the *opposite* of what we
   want here. If `validateName` ever regressed and let `..` through,
   Join would silently re-introduce traversal. Plain `+` keeps the path
   strictly equal to "what the regex approved." Defense in depth.

2. **Why two sentinel errors and not one generic `ErrLoad`.** A user
   typing `loader.Load("Foo Bar")` is making a typo; a user typing
   `loader.Load("../../../etc/passwd")` is probing your security
   boundary. Both cases fail, but a host program's audit log should
   record them differently. The two exported sentinels make
   `errors.Is(err, ErrInvalidName)` the canonical "log to security
   channel" trigger; `ErrAgentNotFound` stays in the regular log.

3. **Why the regex requires a leading `\n` after the opening fence.**
   The pattern is `^---\n(?s).*?\n---\n\s*`, not `^---(?s).*?---\s*`.
   The required newline rules out files whose first line happens to
   start with `---some-comment` and continue with body — only files
   whose first line is *exactly* `---` followed by a newline are
   recognized as frontmatter. Without this anchor, ad-hoc Markdown that
   begins with a horizontal rule could be silently truncated.

## What Changed (vs. s01)

s01 was pure data: a `map[string]Agent`, a few helpers, no I/O. s02
introduces three Go capabilities for the first time: filesystem-backed
loading via `embed.FS`, regular expressions, and sentinel errors.

```diff
  // s01: agent.go — data only, no failure modes
  type Agent struct {
      Name, Description, Prompt string
      Tools                     []string
      Model, DefaultModel       string
  }

+ // s02: loader.go — filesystem + regex + sentinel errors
+ var ErrInvalidName   = errors.New("invalid agent name (must match ^[a-z0-9-]+$)")
+ var ErrAgentNotFound = errors.New("agent prompt not found")
+ var frontmatterPattern = regexp.MustCompile(`^---\n(?s).*?\n---\n\s*`)
+
+ type Loader struct {
+     fs   embed.FS
+     root string
+ }
+
+ func (l *Loader) Load(name string) (string, error) {
+     if err := validateName(name); err != nil { return "", err }
+     data, err := l.fs.ReadFile(l.root + "/" + name + ".md")
+     // ... ErrAgentNotFound translation, frontmatter strip ...
+ }
```

You leave this chapter knowing how to ship Markdown assets inside a Go
binary, how to write a security-conscious file loader, and how to wire
sentinel errors so callers can act on them with `errors.Is`.

## Try It

```bash
cd agents/s02-prompt-loader

go vet ./...     # silent, no output
go build ./...   # silent, no output
go test -v ./... # 5 tests pass (one sub-tests across 6 malformed names)
go run .         # output exactly matches testdata/expected.txt
```

Expected output:

```
architect prompt (759 bytes), first line: "# Architect"
path traversal rejected: true
unknown agent surfaced as: true
embedded agents: [architect executor explore]
```

Further exercises:

- Add a `agents/qa-tester.md` file (with frontmatter) and re-run.
  `embedded agents:` should grow a fourth entry without you touching
  any Go code — that is `embed.FS` doing its job.
- Change `validNamePattern` to accept uppercase (`^[a-zA-Z0-9-]+$`),
  rerun the tests. `TestLoadRejectsInvalidCharacters/AGENT` will fail —
  use that to confirm the test pins the policy, not just the regex.

## Upstream Source Reading

Excerpt from `src/agents/utils.ts` L83–L131 (full annotated copy at
`upstream-readings/s02-utils.ts`):

```typescript
// L74-L78 — the frontmatter regex
function stripFrontmatter(content: string): string {
  const match = content.match(/^---[\s\S]*?---\s*([\s\S]*)$/);
  return match ? match[1].trim() : content.trim();
}

// L86-L131 — the loader
export function loadAgentPrompt(agentName: string): string {
  // ⭐ Security guard #1
  if (!/^[a-z0-9-]+$/i.test(agentName)) {
    throw new Error(`Invalid agent name: contains disallowed characters`);
  }

  // ⚠ Build-time fast path — esbuild replaces __AGENT_PROMPTS__ with a literal
  try {
    if (typeof __AGENT_PROMPTS__ !== 'undefined' && __AGENT_PROMPTS__ !== null) {
      const prompt = __AGENT_PROMPTS__[agentName];
      if (prompt) return prompt;
    }
  } catch {}

  // Runtime fallback — readFileSync + post-check
  try {
    const agentsDir = join(getPackageDir(), 'agents');
    const agentPath = join(agentsDir, `${agentName}.md`);
    // ⭐ Security guard #2 — path-traversal post-check
    const rel = relative(resolve(agentsDir), resolve(agentPath));
    if (rel.startsWith('..') || isAbsolute(rel)) {
      throw new Error(`Invalid agent name: path traversal detected`);
    }
    return stripFrontmatter(readFileSync(agentPath, 'utf-8'));
  } catch (error) {
    // ⚠ Fake-success fallback (Go drops this in favor of ErrAgentNotFound)
    return `Agent: ${agentName}\n\nPrompt unavailable.`;
  }
}
```

Reading notes (Go-port comparisons):

1. **L91 (the `^[a-z0-9-]+$` test) → Go's `validNamePattern`**.
   One-to-one translation; Go drops the `/i` flag because every
   embedded fixture is lowercase and uppercase names would just become
   confusing `ErrAgentNotFound`s downstream — better to fail loudly here.
2. **L96-L104 (the `__AGENT_PROMPTS__` branch) → Go has nothing**.
   `//go:embed` makes the build-time/runtime distinction disappear.
3. **L116-L122 (the path-traversal post-check) → Go skips it**. Three
   reasons: (a) the regex already forbids `..` and `/`, (b) we use
   string concat not `filepath.Join` (which would clean paths and could
   re-introduce traversal), (c) `embed.FS` itself rejects ascending paths.
4. **L124-L130 (the fake-success placeholder) → Go returns `(string,
   error)`**. Returning a placeholder string would mask "agent missing"
   bugs; Go's two-value return forces callers to deal with it.
5. **L46-L49 (the `[\s\S]*?` lazy DOTALL) → Go's `(?s).*?`**.
   Functionally identical — the Go regexp engine spells DOTALL with
   `(?s)` instead of the `[\s\S]` character-class trick.
