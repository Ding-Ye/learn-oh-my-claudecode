---
title: "附录 B · 上游源码导读地图"
chapter: appendix-b
slug: appendix-b-upstream-map
est_read_min: 12
---

# 附录 B · 上游源码导读地图

> 你已经看完 10 章 + s_full + 附录 A，现在去读真上游 `Yeachan-Heo/oh-my-claudecode`，**按这个顺序读**最有效率。

上游有 1,025 个 `.ts` 文件 + 22+ 篇设计文档 + 19 个 agent prompt。一头扎进去会迷路。下面给你一条**linear 阅读路径**，10 站，每站 30-90 分钟。

---

## 阅读地图（按顺序）

### 站 1 · `README.md`

**做什么**：感知项目定位。已经读过研究 dossier 的话直接跳过，dossier 比 README 更密。

**读完应该知道**：OMC 是 Claude Code 的多 agent 编排插件，不是 Claude Code 替代品。

---

### 站 2 · `src/agents/types.ts` (整个文件，约 200 行) → 对应 **s01**

**做什么**：理解 OMC 整套架构的"原子"——`AgentConfig`。

**读完应该知道**：每个 agent 是一份元数据 + prompt + tool 列表 + model tier。这是其它机制的基础。

**重点行**：
- L64-L83 — `AgentConfig` interface
- L139-L165 — `getDefaultModelForCategory`（tier → model 字符串）

---

### 站 3 · `src/agents/definitions.ts` L210-L298 → 对应 **s01**

**做什么**：看 19 个 agent 的真定义 + 四重优先链。

**重点行**：
- L210-L260 — 19 个 agent 实例，按 role 排列
- L289-L298 — `override.model ?? envInheritModel ?? configuredModel ?? agentConfig.model` —— 这就是你 s01 的 `ResolveModel` 函数

**思考**：为什么 OMC 把这条优先链放在 lookup 时计算，而不是 register 时？答案在 dossier。

---

### 站 4 · `src/agents/utils.ts` L83-L131 + L367-L393 → 对应 **s02 + s04**

**做什么**：两个截然不同的 utility，作为同一个文件邻居。

- L83-L131 `loadAgentPrompt` —— **s02** 的原型。注意它的 dual-mode（build-time inline + runtime fallback）。Go 的 `embed.FS` 让这个 dual-mode 消失了，是质量提升。
- L367-L393 `deepMerge` —— **s04** 的原型。**重点看 L376** 的 `__proto__/constructor/prototype` skip——这是 prototype-pollution 防御。Go 没有 prototype 但保留这个 mental model 仍有价值。

---

### 站 5 · `src/features/magic-keywords.ts` L1-L297 → 对应 **s03**

**做什么**：完整通读。这是 OMC 设计上最独立的一块，可以单独读。

**重点行**：
- L20-L22 — `removeCodeBlocks`
- L25-L30 — `INFORMATIONAL_INTENT_PATTERNS`（4 语言）
- L221-L233 — 迭代改写的 loop

**注意**：上游用对象 + closure + array of-Keyword，Go 用 struct + func field + slice-of-Keyword。**形态等价，语法换皮**——这是函数式 vs 命令式的微型对照实验。

---

### 站 6 · `src/config/loader.ts` L1-L80 → 对应 **s04**

**做什么**：理解 layered config 的真实样貌。

**重点行**：
- L4-L9 — 三层 source 的注释
- L41-L72 — `buildDefaultConfig` 的 shape，含 tier 名字解析

读完会发现 OMC 用 JSONC 而你 Go 版用纯 JSON——不是简化，是因为 Go `encoding/json` 不接受 JSONC，且配置文件的 comment 价值低于"严格 schema"价值。

---

### 站 7 · `hooks/hooks.json` (整个文件，212 行) → 对应 **s05**

**做什么**：这不是 TS，是配置——但**信息密度极高**。10 个生命周期事件、各种 matcher、各种 timeout 都在这里。

**重点行**：
- L4-L19 — `UserPromptSubmit` 的标准结构
- L21-L62 — `SessionStart` 的多 matcher 范例
- 每个 hook 的 `command` 字段都指向 `scripts/run.cjs <script>.mjs`，是 sh-out 的真证据

读完去 `scripts/keyword-detector.mjs` 看一个 hook 内部长什么样（约 80 行）。

---

### 站 8 · `src/mcp/omc-tools-server.ts` L1-L100 + `src/mcp/servers.ts` L20-L75 → 对应 **s06**

**做什么**：搞清楚 OMC 的"两类 MCP"。

- `omc-tools-server.ts` — in-process tool（你 s06 实现的那种）
- `servers.ts` — external MCP server 工厂（spawn `npx ...`）

**思考**：为什么 OMC 把这两件事放在不同文件？因为 in-process 和 external 的生命周期完全不同——一个在主进程内 register，一个 fork 子进程。Go 版要做真 external 时也得拆。

---

### 站 9 · `src/features/continuation-enforcement.ts` L1-L196 + `src/features/background-tasks.ts` L1-L357 → 对应 **s07 + s08**

**做什么**：两个 pattern-driven recommender，**一起读最高效**。

它们的形态高度相似：
- 一组 regex
- 一个 `detect/decide` 函数返回 `{flag, reason, confidence}`
- 都是 stateless

读完你会强化 mental model：OMC 用了大量"小型 classifier"——不调 LLM，纯 regex + 表，把 LLM 从"决定一切"改成"决定难的部分"。

**重点行**：
- continuation L18-L24 — 提醒池
- continuation L132-L170 — 完成度分类
- background L29-L70 — long-running 模式
- background L24 — 并发上限常量 `DEFAULT_MAX_BACKGROUND_TASKS = 5`

---

### 站 10 · `src/team/state/tasks.ts` L1-L315 → `src/team/runtime.ts` L289-L990 → 对应 **s09 + s10**

**做什么**：压轴。这两个文件加起来 ≈ 1300 行——OMC 最重的子系统。**留到最后读**，因为前 9 站建立了所有需要的 mental model。

按这个内部顺序：

1. 先读 `tasks.ts` 整个文件 → 这是 s09 的源材料
   - L1-L99 — 类型定义 + `claimTask`（看 L92-L99 的 token 生成）
   - L100-L200 — `transitionTaskStatus` + 错误分支
   - L200-L315 — `withTaskClaimLock` + `writeAtomic`

2. 再读 `runtime.ts` 关键 section：
   - L80-L140 — `writeTask`/`readTask` 配套
   - L289-L390 — `startTeam`（团队初始化）
   - L466-L580 — `watchdogCliWorkers`（**核心 loop**，对应你 s10 的 `watchdog`）
   - L582-L770 — `spawnWorkerForTask`（这里是 tmux 调用真的发生的地方）
   - L850-L990 — `shutdownTeam` + `resumeTeam`

读这一站会看到：**1034 行 TS 解决了你 ~810 行 Go 解决的同一个问题**。差异在哪？tmux + done.json + heartbeat file + watchdog-failed.json + pane lookup tables——这些在 Go 版里全部消失。**这就是语言生态对架构的影响**。

---

## 上游文件 → 课程章节对照表

| 上游文件 | 行范围 | 章节 |
|---|---|---|
| `src/agents/types.ts` | L64-L83, L139-L165 | s01 |
| `src/agents/definitions.ts` | L143-L162, L210-L298 | s01 |
| `src/agents/utils.ts` | L83-L131 | s02 |
| `src/agents/utils.ts` | L367-L393 | s04 |
| `agents/architect.md` | L1-L7 | s02（fixture 模板） |
| `src/features/magic-keywords.ts` | L1-L297 | s03 |
| `src/config/loader.ts` | L1-L80 | s04 |
| `hooks/hooks.json` | L1-L212 | s05 |
| `src/mcp/omc-tools-server.ts` | L1-L100 | s06 |
| `src/mcp/servers.ts` | L20-L75 | s06 |
| `src/features/continuation-enforcement.ts` | L1-L196 | s07 |
| `src/features/background-tasks.ts` | L1-L357 | s08 |
| `src/team/state/tasks.ts` | L1-L315 | s09 |
| `src/team/runtime.ts` | L80-L140 | s09 |
| `src/team/runtime.ts` | L289-L990 | s10 |

`upstream-readings/sNN-*.ts` 里收录了上面每个文件的核心节选 + 注解，本地 `cat` 可看。

---

## 五个进阶练习（按难度递增）

### 1. Wire s10 到 `os/exec ./omc-worker`

把 s10 的 worker 从 goroutine 改成单独二进制，由 `os/exec` 启动。`Worker` interface 不变，只换实现。所有现有测试应该仍能 pass（关键：抽象选对了）。

预计：~150 LOC 改动。难点：worker 怎么读 task？还是文件系统（s09），但 worker 现在是独立进程，需要自己 import s09。这立即破坏"自包含"约束。**反思：是不是抽象选错了？**

### 2. 给 s10 加 `worktree` 模式

每个 worker 创建一个 `git worktree`（mirroring 上游 `team/git-worktree.ts`），在隔离的工作树里工作。需要 `os/exec git worktree add/remove`。

预计：~200 LOC，且要处理 worktree leak（崩溃后没清理）。这是**生产级**才会面对的问题。

### 3. 把 s06 接到真 MCP server

替换 in-memory `Invoke` 为真正的 MCP wire protocol，用 `github.com/modelcontextprotocol/go-sdk` 在 stdio 上跑。用 MCP inspector 工具验证。

预计：~300 LOC + 一个新 dependency。这一步教你"把 in-memory abstraction 换成 wire abstraction"该怎么不破坏调用方。

### 4. 给 s10 再加一种 worker backend：tmux

现在 worker 有两种实现（goroutine、`os/exec`），第三种是 tmux pane（**真还原上游！**）。证明 `Worker` interface 抽象到位——watchdog 代码完全不动。

预计：~250 LOC + 跨平台坑（tmux 不在 Windows 上）。**走完这一步你算彻底懂了上游的 runtime.ts**。

### 5. 真把 10 个 module 拼成 `cmd/omc/main.go`

新建 `omc-app/` 目录（独立 `go.mod`），import 所有 10 个 chapter module（用 `replace` 指向本地路径）。给一个真能跑的 mock `claude` binary（输入 prompt，echo 回去），然后跑 16 步 trace。

预计：~500 LOC + 大量胶水代码。**学习仓库故意不做这一步**，因为做了就破坏"每章独立"的承诺。但你做完之后能拿出去当 portfolio 项目。

---

## 收尾

读完整套（10 章 + s_full + 两个附录）你应该能：

- 在 OMC 真上游里独立 navigate
- 知道每个机制为什么这么设计、不是怎么设计的也行
- 能用 Go 复刻类似规模的多 agent 编排器
- 在自己项目里挪用这些 pattern（state machine + handoff + middleware + filesystem CAS）

如果你做了 5 个练习里的 3 个以上，恭喜——你已经超出"学完"，进入"能改"的阶段了。
