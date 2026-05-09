# learn-oh-my-claudecode

[English](README.en.md) · 中文

> 用 Go 从零渐进构建一个 OMC 风格的多 agent 编排器，每节末尾对照上游 TypeScript 源码。

这个仓库不是教你**用** [oh-my-claudecode](https://github.com/Yeachan-Heo/oh-my-claudecode)（OMC），而是教你**它怎么从零长出来**。

每一节加一个机制——agent registry、prompt loader、magic keywords、config loader、hooks pipeline、MCP tools、continuation enforcement、background task heuristics、task state machine、team watchdog——用 Go 写一份精简实现。看完十节，你会觉得 OMC 不再是一团黑魔法。

## 为什么 Go

| 上游 (TypeScript) | 本仓库 (Go) | 教学增益 |
|---|---|---|
| `tmux split-window` 起 worker | goroutine + channel | 一目了然的并发 |
| `done.json` 文件信号轮询 | `time.Ticker` + `select` | 零外部 IPC |
| esbuild `__AGENT_PROMPTS__` 注入 | `//go:embed` | 编译期内嵌一行解决 |
| JSONC + ajv | `encoding/json` + struct | 标准库够用 |
| 1034 行 `runtime.ts` | ~250 行 Go | 同等功能，更短代码 |

## 课程

| # | 章节 | 状态 |
|---|---|---|
| s01 | [智能体注册表与模型分层](docs/zh/s01-agent-registry.md) | ✅ |
| s02 | [提示词加载器（embed.FS）](docs/zh/s02-prompt-loader.md) | ✅ |
| s03 | [魔法关键词（提示词中间件）](docs/zh/s03-magic-keywords.md) | ✅ |
| s04 | [分层配置与深合并](docs/zh/s04-config-loader.md) | ✅ |
| s05 | [钩子流水线（os/exec）](docs/zh/s05-hooks-pipeline.md) | ✅ |
| s06 | [MCP 工具注册表与分类禁用](docs/zh/s06-mcp-tool-server.md) | ✅ |
| s07 | [推石上山（继续执行强制）](docs/zh/s07-continuation.md) | ✅ |
| s08 | [后台任务启发式调度](docs/zh/s08-background-tasks.md) | ✅ |
| s09 | [文件态任务状态机（CAS+租约）](docs/zh/s09-task-state-machine.md) | ✅ |
| s10 | 团队 Runtime 与 Watchdog（goroutine 池） | ⏳ |
| s_full | 端到端集成 | ⏳ |
| A | 附录 A · 心智模型 | ⏳ |
| B | 附录 B · 上游源码导读地图 | ⏳ |

## 快速开始

```bash
# 跑 s01 demo
cd agents/s01-agent-registry
go run .

# 跑 s01 测试
go test -v ./...

# 看在线版课程文档
cd web && npm install && npm run dev
# → http://localhost:3000/zh
```

## 仓库结构

```
learn-oh-my-claudecode/
├── agents/              # 每章一个独立 Go module（不互相 import）
│   └── s01-agent-registry/
├── docs/                # 双语章节文档（六段式：Problem / Solution / How It Works / What Changed / Try It / Upstream Source Reading）
│   ├── zh/
│   └── en/
├── upstream-readings/   # 真实上游源码片段，带注解
├── web/                 # Next.js 文档站点
└── go.work              # Go workspace（每章一个 use）
```

每章模块自包含：克隆下来、`cd agents/sNN-*`、`go run .` 即可。**没有跨章 import**，方便单章 fork 学习。

## 致谢

- 上游：[Yeachan-Heo/oh-my-claudecode](https://github.com/Yeachan-Heo/oh-my-claudecode)（MIT），让本仓库的所有机制都有真实参考。
- 教学法：仿照 [shareAI-lab/learn-claude-code](https://github.com/shareAI-lab/learn-claude-code) 的「心智模型 → ASCII 图 → 30-60 行核心代码 → diff → 动手试 → 上游源码阅读」六段式。

## License

MIT — see [LICENSE](LICENSE).
