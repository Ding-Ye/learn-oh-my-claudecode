// The locked curriculum from .learn/plan.md. SessionNav and the landing page
// both read from this single source of truth. Slugs match docs/{zh,en}/<slug>.md.
//
// "available: false" means the chapter exists in the curriculum but its docs
// aren't written yet — the link will render but go to a placeholder. As each
// session lands in Phase E, that session's writer flips its row to true.

export type ChapterMeta = {
  slug: string;
  num: string; // "s01", "s02", "s_full", "A", "B"
  title: { zh: string; en: string };
  available: boolean;
};

export const CURRICULUM: ChapterMeta[] = [
  {
    slug: "s01-agent-registry",
    num: "s01",
    title: {
      zh: "智能体注册表与模型分层",
      en: "Agent Registry & Model Tiers",
    },
    available: true,
  },
  {
    slug: "s02-prompt-loader",
    num: "s02",
    title: {
      zh: "提示词加载器（embed.FS）",
      en: "Prompt Loader with embed.FS",
    },
    available: true,
  },
  {
    slug: "s03-magic-keywords",
    num: "s03",
    title: {
      zh: "魔法关键词（提示词中间件）",
      en: "Magic Keywords (Prompt Middleware)",
    },
    available: true,
  },
  {
    slug: "s04-config-loader",
    num: "s04",
    title: {
      zh: "分层配置与深合并",
      en: "Layered Config & deepMerge",
    },
    available: true,
  },
  {
    slug: "s05-hooks-pipeline",
    num: "s05",
    title: {
      zh: "钩子流水线（os/exec）",
      en: "Hooks Pipeline via os/exec",
    },
    available: true,
  },
  {
    slug: "s06-mcp-tool-server",
    num: "s06",
    title: {
      zh: "MCP 工具注册表与分类禁用",
      en: "MCP Tool Registry with Categories",
    },
    available: true,
  },
  {
    slug: "s07-continuation",
    num: "s07",
    title: {
      zh: "推石上山（继续执行强制）",
      en: "Continuation Enforcement (Sisyphus)",
    },
    available: false,
  },
  {
    slug: "s08-background-tasks",
    num: "s08",
    title: {
      zh: "后台任务启发式调度",
      en: "Background Task Heuristics",
    },
    available: false,
  },
  {
    slug: "s09-task-state-machine",
    num: "s09",
    title: {
      zh: "文件态任务状态机（CAS+租约）",
      en: "File-backed Task State Machine",
    },
    available: false,
  },
  {
    slug: "s10-team-watchdog",
    num: "s10",
    title: {
      zh: "团队 Runtime 与 Watchdog（goroutine 池）",
      en: "Team Runtime & Watchdog (Goroutine Pool)",
    },
    available: false,
  },
  {
    slug: "s_full-integration",
    num: "s_full",
    title: { zh: "端到端集成", en: "End-to-end Integration" },
    available: false,
  },
  {
    slug: "appendix-a-mental-models",
    num: "A",
    title: {
      zh: "附录 A · 心智模型",
      en: "Appendix A · Mental Models",
    },
    available: false,
  },
  {
    slug: "appendix-b-upstream-map",
    num: "B",
    title: {
      zh: "附录 B · 上游源码导读地图",
      en: "Appendix B · Upstream Source Map",
    },
    available: false,
  },
];

export type Locale = "zh" | "en";

export function chapterTitle(c: ChapterMeta, locale: Locale): string {
  return c.title[locale];
}
