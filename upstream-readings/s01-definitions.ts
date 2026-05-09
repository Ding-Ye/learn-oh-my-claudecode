// Source: https://raw.githubusercontent.com/Yeachan-Heo/oh-my-claudecode/main/src/agents/definitions.ts
// Lines: L143-L298 (excerpts, simplified)
// License: MIT, Copyright (c) 2025 Yeachan Heo
//
// This file is a teaching annotation of the upstream registry definition. We
// drop irrelevant try/catch and logging and add inline comments explaining
// each piece. The Go counterpart in agents/s01-agent-registry/ is a faithful
// minimum-viable port. Read this file alongside Section 6 ("Upstream Source
// Reading") of the s01 chapter docs.

// ---------------------------------------------------------------------------
// (1) The kebab-vs-camelCase translation map (definitions.ts L143-L162)
// ---------------------------------------------------------------------------
//
// The registry keys agents by kebab-case canonical name ("security-reviewer",
// "code-reviewer", …). The user-facing JSONC config under `~/.config/
// claude-omc/config.jsonc` uses camelCase property names ("securityReviewer",
// "codeReviewer", …) because TypeScript discourages dashed identifiers.
// AGENT_CONFIG_KEY_MAP bridges the two namespaces.
//
// ⚠ Anti-pattern #4 in the learn-oh-my-claudecode plan. Our Go port uses
// kebab-case everywhere — both in the registry and (eventually) in the JSON
// config file we'll write in s04 — so this entire map evaporates.
const AGENT_CONFIG_KEY_MAP = {
  explore: 'explore',
  analyst: 'analyst',
  planner: 'planner',
  architect: 'architect',
  debugger: 'debugger',
  executor: 'executor',
  verifier: 'verifier',
  'security-reviewer': 'securityReviewer',  // kebab → camel
  'code-reviewer': 'codeReviewer',
  'test-engineer': 'testEngineer',
  designer: 'designer',
  writer: 'writer',
  'qa-tester': 'qaTester',
  scientist: 'scientist',
  tracer: 'tracer',
  'git-master': 'gitMaster',
  'code-simplifier': 'codeSimplifier',
  critic: 'critic',
  'document-specialist': 'documentSpecialist',
} as const satisfies Partial<Record<string, keyof NonNullable<PluginConfig['agents']>>>;

// Helper: translate a registry key to its config-key form, look up the user's
// per-agent model override (if any). Returns undefined if the user did not
// override this agent. In Go we collapse this into the `configured` argument
// to ResolveModel — the caller does the lookup once before invoking us.
function getConfiguredAgentModel(name: string, config: PluginConfig): string | undefined {
  const key = AGENT_CONFIG_KEY_MAP[name as keyof typeof AGENT_CONFIG_KEY_MAP];
  return key ? config.agents?.[key]?.model : undefined;
}

// ---------------------------------------------------------------------------
// (2) The registry itself (definitions.ts L210-L260)
// ---------------------------------------------------------------------------
//
// `getAgentDefinitions` returns the full record of agents that the Claude
// Agent SDK consumes. The body builds a fresh map every call so per-call
// `overrides` and the env-var-driven `inheritModel` don't bleed between
// invocations. In Go we split this responsibility:
//   - storage of base Agent values   → registry.go's Registry struct
//   - per-call model resolution      → registry.go's ResolveModel function
export function getAgentDefinitions(options?: {
  overrides?: Partial<Record<string, Partial<AgentConfig>>>;
  config?: PluginConfig;
}): Record<string, {
  description: string;
  prompt: string;
  tools?: string[];
  disallowedTools?: string[];
  model?: string;
  defaultModel?: string;
}> {
  const agents: Record<string, AgentConfig> = {
    // BUILD/ANALYSIS LANE
    explore: exploreAgent,
    analyst: analystAgent,
    planner: plannerAgent,
    architect: architectAgent,
    debugger: debuggerAgent,
    executor: executorAgent,
    verifier: verifierAgent,
    // REVIEW LANE
    'security-reviewer': securityReviewerAgent,
    'code-reviewer': codeReviewerAgent,
    // DOMAIN SPECIALISTS
    'test-engineer': testEngineerAgent,
    designer: designerAgent,
    writer: writerAgent,
    'qa-tester': qaTesterAgent,
    scientist: scientistAgent,
    tracer: tracerAgent,
    'git-master': gitMasterAgent,
    'code-simplifier': codeSimplifierAgent,
    // COORDINATION
    critic: criticAgent,
    // BACKWARD COMPATIBILITY (Deprecated)
    'document-specialist': documentSpecialistAgent
  };

  // ---------------------------------------------------------------------------
  // (3) The four-fold resolution loop (definitions.ts L260-L298)
  // ---------------------------------------------------------------------------
  //
  // ⭐ The single most important lines in this file. Read carefully.
  //
  // For each agent, four candidate models are considered in priority order;
  // the first non-undefined wins. The chain is:
  //
  //   override?.model ?? inheritModel ?? configuredModel ?? agentConfig.model
  //
  // Mapped onto our Go ResolveModel signature:
  //
  //   override?.model       → override     (caller-supplied per-call model)
  //   inheritModel          → envInherit   (set when OMC_ROUTING_FORCE_INHERIT)
  //   configuredModel       → configured   (loaded from user's omc.jsonc)
  //   agentConfig.model     → agent.Model  (the agent author's choice)
  //
  // The Go port adds a fifth fallback (agent.DefaultModel) so a single
  // ResolveModel call always returns the most informative non-empty answer.

  const resolvedConfig = options?.config ?? loadConfig();
  const inheritModel = resolvedConfig.routing?.forceInherit
    ? resolveInheritedModelFromEnv()
    : undefined;
  const result: Record<string, {
    description: string; prompt: string;
    tools?: string[]; disallowedTools?: string[];
    model?: string; defaultModel?: string;
  }> = {};

  for (const [name, agentConfig] of Object.entries(agents)) {
    const override = options?.overrides?.[name];
    const configuredModel = getConfiguredAgentModel(name, resolvedConfig);
    const disallowedTools = agentConfig.disallowedTools ?? parseDisallowedTools(name);

    // ⭐ THE FOUR-FOLD CHAIN ⭐
    const resolvedModel = override?.model ?? inheritModel ?? configuredModel ?? agentConfig.model;
    const resolvedDefaultModel = override?.defaultModel ?? agentConfig.defaultModel;

    result[name] = {
      description: override?.description ?? agentConfig.description,
      prompt: appendSkininthegamebrosGuidance(
        override?.prompt ?? agentConfig.prompt,
        'agent',
      ),
      tools: override?.tools ?? agentConfig.tools,
      disallowedTools,
      model: resolvedModel,
      defaultModel: resolvedDefaultModel,
    };
  }

  return result;
}

// Reading map:
// - For the AgentConfig data shape, see src/agents/types.ts L64-L83.
// - For getDefaultModelForCategory (used by some agents to derive their own
//   .model field), see src/agents/types.ts L139-L165 — beyond s01's scope.
// - For the runtime registry consumer (the Claude Agent SDK call site that
//   feeds the result of this function into `query()`), see src/index.ts —
//   we do not port this; treat it as a black box.
// - For the JSONC config file format that drives `configuredModel`, see
//   src/config/loader.ts — that is the subject of session s04.
