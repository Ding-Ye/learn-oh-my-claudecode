---
title: "s_full · 端到端集成"
chapter: full
slug: s_full-integration
est_read_min: 12
---

# s_full · 端到端集成

> 教什么：把前 10 章拼成一个完整的 OMC 流程，看 `/team 2:executor "fix lint errors"` 的完整轨迹。

---

## 把所有章节合在一起看

如果你顺着读完了 s01–s10，恭喜——你已经手写过 OMC 几乎所有的核心机制。这一章不写新代码，只回答一个问题：**真跑一条 `/team` 命令时，这 10 个机制是怎么协同工作的？**

下面这张图把它们串起来。每个方块对应你已经写过的一节 Go 代码：

```
                    user 输入: /team 2:executor "fix lint errors in src/auth/"
                          │
          ┌───────────────▼───────────────┐
          │ s05  hooks-pipeline           │  ← UserPromptSubmit hook 触发
          └───────────────┬───────────────┘
                          │ JSON envelope 流入
          ┌───────────────▼───────────────┐
          │ s03  magic-keywords           │  ← 如果包含 `ultrawork` 等关键词，重写 prompt
          └───────────────┬───────────────┘
                          │
          ┌───────────────▼───────────────┐
          │ s04  config-loader            │  ← 加载 defaults → user → project → env
          └───────────────┬───────────────┘
                          │
          ┌───────────────▼───────────────┐
          │ s01  agent-registry           │  ← 查表："executor" → opus
          │ s02  prompt-loader            │  ← embed.FS 取出 executor.md prompt
          └───────────────┬───────────────┘
                          │
          ┌───────────────▼───────────────┐
          │ s07  continuation             │  ← 拼上 Sisyphus 系统提示词
          └───────────────┬───────────────┘
                          │
          ┌───────────────▼───────────────┐
          │ s06  mcp-tool-server          │  ← 按类别注册工具，OMC_DISABLE_TOOLS 过滤
          └───────────────┬───────────────┘
                          │
          ┌───────────────▼───────────────┐
          │ s09  task-state-machine       │  ← 创建 2 个 task，写到磁盘（CAS+lease）
          └───────────────┬───────────────┘
                          │
          ┌───────────────▼───────────────┐
          │ s10  team-watchdog            │  ← 启动 2 个 goroutine worker
          │   ↻ s08  background-tasks     │  ← worker 内部决策每个命令是否后台执行
          └───────────────┬───────────────┘
                          │
                       results
```

---

## 16 步执行轨迹（每步标注哪节的代码）

下表把研究 dossier 里那条「`/team 2:executor` 真实路径」的 16 步，每步对应到你已经写过的 Go 模块：

| 步 | 上游行为 | 对应 Go session |
|---|---|---|
| 1 | `/team N:type "..."` 进 plugin 路由到 `team` skill | **s05** hooks-pipeline 触发 `UserPromptSubmit` |
| 2 | 解析 N 和 agent-type | **s04** config-loader 读取键值层 |
| 3 | 写 `team-state.json` phase=plan | **s09** task-state-machine |
| 4 | 派 `explore` 子 agent（Haiku） | **s01** lookup + **s02** prompt + **s10** spawn |
| 5 | 派 `planner` 子 agent（Opus） | **s01** + **s02** + **s10** |
| 6 | 写 handoff `team-plan.md` | **s09**（用 task 的 description 字段承载 handoff） |
| 7 | `TeamCreate("fix-lint-errors")` | **s10** Pool init |
| 8 | `TaskCreate` × 2 | **s09** Store.Write |
| 9 | `Task(...)` worker 派遣 × 2 | **s10** Pool.Submit |
| 10 | Worker-1 编辑 + `SendMessage` | **s06** SendMessage 工具 |
| 11 | Worker-2 并发执行 | **s10** goroutine 池 |
| 12 | Lead 监控循环 | **s10** watchdog 心跳 |
| 13 | 派 `verifier` 子 agent（Sonnet） | **s01** + **s02** + **s10** |
| 14 | Verify 通过，跳过 `team-fix` | **s09** transition done |
| 15 | `SendMessage(shutdown)` + 清理 | **s10** Shutdown 排空 |
| 16 | 给用户打印总结 | （上层应用代码，不属于任何 session） |

可以看出：每节都有自己的职责切片，没有任何一节"霸占"一个步骤——这是 OMC 设计的精髓。

---

## 思考练习：如果你想真把这 10 个 module 拼成一个 CLI

假设你想从这个学习仓库再前进一步，把 10 个独立 module 真拼成一个能跑的 `omc` CLI。教学版本**不**做这件事，但走通这个 thought experiment 能帮你理解每节的边界。

最简版本会这样：

```go
// cmd/omc/main.go (假想的)
package main

import (
    s01 "github.com/Ding-Ye/learn-oh-my-claudecode/agents/s01-agent-registry"
    s02 "github.com/Ding-Ye/learn-oh-my-claudecode/agents/s02-prompt-loader"
    s03 "github.com/Ding-Ye/learn-oh-my-claudecode/agents/s03-magic-keywords"
    s04 "github.com/Ding-Ye/learn-oh-my-claudecode/agents/s04-config-loader"
    s05 "github.com/Ding-Ye/learn-oh-my-claudecode/agents/s05-hooks-pipeline"
    s06 "github.com/Ding-Ye/learn-oh-my-claudecode/agents/s06-mcp-tool-server"
    s07 "github.com/Ding-Ye/learn-oh-my-claudecode/agents/s07-continuation"
    s08 "github.com/Ding-Ye/learn-oh-my-claudecode/agents/s08-background-tasks"
    s09 "github.com/Ding-Ye/learn-oh-my-claudecode/agents/s09-task-state-machine"
    s10 "github.com/Ding-Ye/learn-oh-my-claudecode/agents/s10-team-watchdog"
)

func main() {
    cfg, _ := s04.Load(".")            // s04
    registry := s01.New()              // s01
    loader := s02.New(promptFS, "agents")  // s02
    tools := s06.New().WithDisabled(s06.ParseDisabled(os.Getenv("OMC_DISABLE_TOOLS")))
    store := s09.NewStore(".omc/state/team")
    pool := s10.New(ctx, 3, store)
    // ... 然后处理 user prompt：先过 s03 magic-keywords，再 s07 拼 Sisyphus，
    // 最后 s10 派 worker。s05 的 hooks 在每个生命周期点触发。
    // s08 的 background-tasks 由 worker 内部调用。
    pool.Run()
}
```

但这个 `cmd/omc/main.go` 没在仓库里——故意的。教学版本的承诺是"每章独立可读"，引入一个全局 main.go 会让学生忍不住改一处影响多处。把"如何拼起来"留作思考题，比"提供一个能跑的 demo"教学价值更高。

---

## 我们故意没做的（Deliberate Omissions）

学习版本是**功能子集**，不是 OMC 的 1:1 重写。下面是教学版**故意放弃**的能力，配合理由：

| 上游有 | 我们不做 | 理由 |
|---|---|---|
| 真实 LLM 调用层 | 全程 mock | OMC 自己也没有 LLM 调用层（dossier `has_llm_call_layer: false`），上游就是构造 `queryOptions` 给 Claude Agent SDK |
| 19 个真 agent prompts | 3 个示意 fixture（architect/executor/explore） | 教学只需要展示加载机制，prompt 内容不是重点 |
| 真 MCP wire protocol | s06 用 in-process registry | 教学点是"分类禁用 + closure-as-data 模式"，wire 协议是噪音 |
| tmux 多 pane 编排 | s10 用 goroutine | 教学点是"goroutine + channel 替代外部 IPC" |
| Git worktree 隔离 | 无 | s10 扩展练习里提示如何加 |
| Telegram / 飞书 / 微信 IM 适配器 | 无 | 这是 OMC 的产品差异化，不是核心架构 |
| HUD statusline | 无 | 渲染层，跟编排无关 |
| `runtime-v2.ts` 之类的并行实现 | 只一份 canonical | 教学版必须只有一种"对的写法" |

如果你真要做**生产级**的 OMC 风格编排器，这些是绕不开的工作量——但学习时它们是噪音。

---

## 下一步：附录

- **附录 A · 心智模型** —— OMC 的非代码维度：state machine、stage routing、handoff 文档作为耐久内存
- **附录 B · 上游源码导读地图** —— 完整的 `Yeachan-Heo/oh-my-claudecode` 阅读路径，从哪个文件入手到读完哪条线

读完上面两份，你对 OMC 的理解会从「我把 10 个机制实现过一遍」升级到「我能在 100K LOC 的真上游里自己导航」。
