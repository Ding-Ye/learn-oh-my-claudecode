// Source: https://raw.githubusercontent.com/Yeachan-Heo/oh-my-claudecode/main/src/agents/utils.ts
// Lines: L83–L131 (loadAgentPrompt + companion regex)
// License: MIT, Copyright (c) 2025 Yeachan Heo
//
// Annotated excerpt for s02. The Go port collapses upstream's dual
// build-time/runtime branch into one embed.FS read. Read alongside
// Section 6 of the s02 chapter docs.

// (1) Strip YAML frontmatter (utils.ts L74-L78).
// `[\s\S]*?` is the lazy DOTALL trick — without `?` the regex would
// gobble up to the LAST '---' in the document. Go's regexp engine has
// no `[\s\S]` idiom and uses `(?s)` for DOTALL: `^---\n(?s).*?\n---\n\s*`.
function stripFrontmatter(content: string): string {
  const match = content.match(/^---[\s\S]*?---\s*([\s\S]*)$/);
  return match ? match[1].trim() : content.trim();
}

// (2) Loader entry point (utils.ts L86-L131).
//
// Three branches: validate, build-time map, runtime read. Branch 2 is what
// Go's `//go:embed` deletes wholesale.
export function loadAgentPrompt(agentName: string): string {
  // ⭐ Security guard #1 — the line everything else relies on.
  // /i is permissive; the Go port tightens to ASCII-lowercase-only because
  // uppercase agents would just fail later as ErrAgentNotFound — better to
  // reject loudly here as ErrInvalidName.
  if (!/^[a-z0-9-]+$/i.test(agentName)) {
    throw new Error(`Invalid agent name: contains disallowed characters`);
  }

  // ⚠ Build-time fast path. esbuild's `define` swaps __AGENT_PROMPTS__ for
  // an object literal during the bridge build. At dev time the typeof guard
  // makes this a no-op. Go has no equivalent — `//go:embed` bakes the bytes
  // into the binary's read-only segment so this branch is unnecessary.
  try {
    if (typeof __AGENT_PROMPTS__ !== 'undefined' && __AGENT_PROMPTS__ !== null) {
      const prompt = __AGENT_PROMPTS__[agentName];
      if (prompt) return prompt;
    }
  } catch {
    // fall through to runtime file read
  }

  // Runtime branch — the only one the Go port keeps (filesystem swapped
  // out for embed.FS).
  try {
    const agentsDir = join(getPackageDir(), 'agents');
    const agentPath = join(agentsDir, `${agentName}.md`);

    // ⭐ Security guard #2 — path-traversal post-check. Go does NOT need
    // this second guard: validateName already rejects '..' and '/', the
    // loader uses string concat (not filepath.Join, which would clean
    // paths and could re-introduce traversal), and embed.FS itself
    // refuses ascending paths.
    const resolvedPath = resolve(agentPath);
    const resolvedAgentsDir = resolve(agentsDir);
    const rel = relative(resolvedAgentsDir, resolvedPath);
    if (rel.startsWith('..') || isAbsolute(rel)) {
      throw new Error(`Invalid agent name: path traversal detected`);
    }

    const content = readFileSync(agentPath, 'utf-8');
    return stripFrontmatter(content);
  } catch (error) {
    // ⚠ Last-ditch placeholder. The Go port chooses (string, error) over
    // a fake-success placeholder so callers can errors.Is(err,
    // ErrAgentNotFound) and decide for themselves whether to fall back.
    const message = error instanceof Error && error.message.includes('Invalid agent name')
      ? error.message
      : 'Agent prompt file not found';
    console.warn(`[loadAgentPrompt] ${message}`);
    return `Agent: ${agentName}\n\nPrompt unavailable.`;
  }
}

// Go-port reading map: regex → validate.go; __AGENT_PROMPTS__ → //go:embed
// (deleted); traversal post-check → not needed; placeholder → ErrAgentNotFound.
