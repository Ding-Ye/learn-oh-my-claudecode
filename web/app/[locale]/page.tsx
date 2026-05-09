import Link from "next/link";
import { notFound } from "next/navigation";
import { CURRICULUM, chapterTitle, type Locale } from "@/lib/curriculum";

export default async function Landing({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale } = await params;
  if (locale !== "zh" && locale !== "en") notFound();
  const l = locale as Locale;

  const intro = l === "zh" ? INTRO_ZH : INTRO_EN;
  const ctaLabel = l === "zh" ? "从 s01 开始 →" : "Start at s01 →";

  return (
    <article className="prose-doc">
      <h1>learn-oh-my-claudecode</h1>
      <p className="text-[var(--fg-muted)]">
        {l === "zh"
          ? "用 Go 从零渐进构建一个 OMC 风格的多 agent 编排器，每节末尾对照上游 TypeScript 源码。"
          : "Build an OMC-style multi-agent orchestrator from scratch in Go, session by session — each chapter ends with the upstream TypeScript source."}
      </p>

      {intro.map((p, i) => (
        <p key={i}>{p}</p>
      ))}

      <p>
        <Link
          href={`/${l}/s/s01-agent-registry`}
          className="inline-block mt-2 px-4 py-2 rounded border border-[var(--accent-soft)] hover:border-[var(--accent)]"
        >
          {ctaLabel}
        </Link>
      </p>

      <h2>{l === "zh" ? "课程" : "Curriculum"}</h2>
      <ul>
        {CURRICULUM.map((c) => (
          <li key={c.slug}>
            <span className="font-mono text-[var(--fg-muted)] mr-2">
              {c.num}
            </span>
            {c.available ? (
              <Link href={`/${l}/s/${c.slug}`}>{chapterTitle(c, l)}</Link>
            ) : (
              <span className="text-[var(--fg-muted)]">
                {chapterTitle(c, l)}{" "}
                <span className="text-xs">
                  ({l === "zh" ? "未发布" : "not yet"})
                </span>
              </span>
            )}
          </li>
        ))}
      </ul>
    </article>
  );
}

const INTRO_ZH = [
  "这个仓库的目标不是教你「用」 oh-my-claudecode，是教你「它怎么从零长出来」。",
  "每一节加一个机制——agent registry、prompt loader、magic keywords、config loader、hooks pipeline、MCP tools、continuation enforcement、background task heuristics、task state machine、team watchdog——用 Go 写一份精简实现。看完十节，你会觉得 OMC 不再是一团黑魔法。",
  "Go 实现是教学骨架，oh-my-claudecode 上游是 TypeScript 实现。每节末尾的「上游源码阅读」把这两边对照起来，你能从 mini 版顺着指针读到生产代码。",
  "重要：Go 版用 goroutine + channel 替换上游的 tmux + 文件信号——同样的多 agent 编排，更短的代码，更清晰的 mental model。",
];

const INTRO_EN = [
  "The goal of this repo is not to teach you to *use* oh-my-claudecode — it is to teach you how it grows from scratch.",
  "Each chapter adds one mechanism — agent registry, prompt loader, magic keywords, config loader, hooks pipeline, MCP tools, continuation enforcement, background task heuristics, task state machine, team watchdog — implemented as a small Go module. After ten chapters, OMC stops being black magic.",
  "Go is the teaching skeleton; the upstream TypeScript is the production implementation. The 'Upstream Source Reading' section at the end of every chapter bridges them — you can follow the pointers from the mini version straight into the real code.",
  "Note: the Go version uses goroutines + channels in place of the upstream's tmux + filesystem signals — the same multi-agent orchestration, shorter code, cleaner mental model.",
];
