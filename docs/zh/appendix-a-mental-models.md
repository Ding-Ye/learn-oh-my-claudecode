---
title: "附录 A · 心智模型"
chapter: appendix-a
slug: appendix-a-mental-models
est_read_min: 15
---

# 附录 A · 心智模型

> 这一节不讲代码——讲 OMC 的**非代码维度**。这些是上游产品决策，决定了为什么 10 个机制要长成那样。

OMC 是一个产品。它的设计选择背后有四个值得单独讲的心智模型。每个都解释了"为什么"而不是"是什么"。

---

## A1 · 团队编排作为状态机

OMC 的 `/team` 命令实际上是一个**有限状态机**：

```
        ┌──────────────┐  fail   ┌─────────┐
        │   team-plan  │────────▶│ team-fix│
        └──────┬───────┘         └────┬────┘
               │ pass                 │ pass (≤ N 轮)
               ▼                      │
        ┌──────────────┐               │
        │   team-prd   │               │
        └──────┬───────┘               │
               │                       │
               ▼                       │
        ┌──────────────┐               │
        │  team-exec   │◀──────────────┘
        └──────┬───────┘
               │
               ▼
        ┌──────────────┐  pass   ┌──────────┐
        │ team-verify  │────────▶│ complete │
        └──────────────┘         └──────────┘
```

### 五个阶段的精确定义

- **team-plan** —— 进入条件：`/team` 命令触发。出口条件：planner 输出了一个任务图（subtasks + dependsOn）。
- **team-prd**（可选） —— 进入条件：plan 表示需要补充 PRD。出口条件：spec 文档写入 `.omc/specs/`。简单情况会跳过这一步。
- **team-exec** —— 进入条件：handoff `team-plan.md` 已写。出口条件：所有 subtasks 状态都 ∈ {done, failed}。
- **team-verify** —— 进入条件：所有 subtasks 完成。出口条件：verifier 跑测试 + 给 pass/fail 判断。
- **team-fix** —— 进入条件：verify fail。出口条件：fix 完成或循环达到 `maxValidationRounds`。如果失败循环达上限，整个 `/team` 命令报失败。

### 为什么必须是顺序而非并行？

每个阶段都依赖**前一个阶段的产出**：planner 需要 explore 的结果，executor 需要 plan，verifier 需要 executor 的修改。这不是性能瓶颈——这是**正确性约束**。OMC 在 stage 内部并行（多 worker 并发执行 subtasks），在 stage 之间严格串行。

### 失败模式

1. **fix loop 跑满** → 状态进入 `failed`，工件保留供人工 debug
2. **verify 永不通过** → 同上；通常意味着 plan 阶段就有问题
3. **某个 worker 死亡** → s10 watchdog 接管：respawn 一次，仍失败则把 task 标 failed，但**整个 `/team` 不立即失败**——其他 worker 继续

### 在你的 Go 代码里

s09（task-state-machine）实现了"哪个阶段、哪个 task 在哪个状态"的存储；s10（team-watchdog）实现了"这个状态机的 tick + transition + recover"。两者合起来才是这套状态机的运行引擎。**state 和 execution 解耦**，这是 OMC 设计上最值得抄的一点。

### 思考练习

试着在脑子里给 `/team` 加一个 `team-spec` 阶段（位于 plan 和 prd 之间），用于澄清需求。需要改：(a) state.json 的合法值，(b) plan agent 的 prompt，(c) verify 的 entry 条件。这个 thought experiment 能让你理解「state machine 设计的边际成本」——加一个 stage 不便宜。

---

## A2 · Stage-aware 模型路由

OMC 把 19 个 agent 按"阶段需求"分配 model tier：

| Tier | 典型模型 | OMC 用在哪 | 直觉 |
|---|---|---|---|
| HIGH | Opus | planner / analyst / architect / critic | 需要长上下文 + 战略性思考 |
| MEDIUM | Sonnet | executor / debugger / designer / verifier | 写代码、改 bug、画图——主力 |
| LOW | Haiku | explore / formatter / linter | 扫文件、机械操作——便宜快 |

### 为什么 explore 是 Haiku？

`explore` 的工作是**快速扫文件、grep 关键词、列文件清单**。这种工作不需要 Opus 的推理能力——它需要**快**和**便宜**，因为 plan 阶段可能要派 5-10 个 explore。Haiku 5 倍便宜、3 倍快，质量在这个任务上和 Opus 没差。

### 为什么 planner 必须是 Opus？

`planner` 要在一份模糊需求 + 一堆 explore 结果上**生成 task DAG**。这是分类困难的工作，错一步整个 `/team` 都跑废。这是 Opus 的甜蜜点。

### 四重优先链

研究 dossier 里提过的四重优先链（s01 实现了）：

```
override.model  ?? envInheritModel  ?? configuredModel  ?? agent.Model
```

这条链的意义是**让用户能在任意层覆盖**：
- `override.model`: 单次调用临时覆盖（不常用）
- `envInheritModel`: 由 `OMC_ROUTING_FORCE_INHERIT` 控制——如果设了，**所有** agent 都继承当前会话的 model（用于 cost mode）
- `configuredModel`: 用户在 `omc.jsonc` 里给某个 agent 单独配的
- `agent.Model`: agent 定义里的 default tier

### `OMC_MODEL_HIGH/_MEDIUM/_LOW`

这三个 env vars 在 default-build 时就把 tier 名字解析成具体 model。当用户切到 Bedrock（`CLAUDE_CODE_USE_BEDROCK=1`）时，这些就成了 `arn:aws:bedrock-runtime:...`。

### 思考练习

如果加一个 BUDGET tier（比 LOW 还便宜，可能用 GPT-4o-mini），最少要改：(1) tier 字面量列表，(2) 默认分配（哪些 agent 进 BUDGET），(3) 优先链顺序。先别真改代码——脑子里走一遍，看你能否一次想清楚 8 个改点。

---

## A3 · 魔法关键词作为中间件

`ultrawork`、`ultrathink`、`search` 这些关键词的设计最让人意外——它们是**用户输入的 prompt 之前的一层透明改写**。

### 中间件这个比喻

如果你写过 Express / Koa / Echo / Gin，你认得这个 pattern：每层 middleware 拿到一份 input，决定是否改写 / 直接返回 / 交给下一层。OMC 的 magic keywords 就是 prompt 上的 middleware：

```
用户输入: "ultrawork build a server"
   │
   ▼
[ultrawork middleware] 检测触发词，prepend 高强度指令
   │
   ▼
"[Ultrawork mode active. Maximum performance. ...]\nbuild a server"
   │
   ▼
[search middleware] 没触发，pass through
   │
   ▼
[Sisyphus 系统提示词] 拼到 system prompt
   │
   ▼
LLM
```

### 三条跳过规则

不是任何包含触发词的 prompt 都改写：

1. **代码块内** —— `​`​`​`ultrawork​`​`​` 是代码示例，不是命令
2. **信息查询语境** —— "what is ultrawork?" / "ultrawork이 뭐야?" / "ultrawork は何？" 是问问题，不是命令
3. **已经被改写过的** —— 防止递归改写（s03 的实现里没强制 idempotency，但实际上 prepend 操作天然不会产生死循环）

### 为什么 Action 是 closure 而非常量字符串？

```go
type Keyword struct {
    Triggers []string
    Action   func(prompt, agentName, modelID string) string
}
```

Action 是函数而非字符串，因为不同的 agent / model 可能要拼不同的指令。例如 `ultrawork` 对 Opus 和对 Haiku 应该展开成不同强度的提示词——Opus 已经能"thinking hard"，Haiku 需要更明确的指令。Closure 让这种"按上下文展开"成为可能而不增加 keyword 类型本身的复杂度。

### 思考练习

设计一个 `terse` 关键词，触发后**剥掉**用户 prompt 里的填充词（"please"、"could you"、"I would like to"），只保留指令骨架。这是反向 transformation：不是 prepend，是 substitute。需要在 s03 的 `Keyword` 类型里加什么字段？或者完全不用加，只是换一个 Action 实现？

---

## A4 · Handoff 文档作为耐久内存

OMC 的最优雅设计可能就是这个：**stage 之间的状态传递不靠内存或 RPC，靠 markdown 文件**。

### 五段式 handoff 格式

`.omc/handoffs/team-plan.md` 长这样：

```markdown
# team-plan handoff

## Decided
- 把任务拆成 2 个 subtask（修 login.ts、修 oauth.ts）
- 用 executor agent，不用 designer

## Rejected
- 一次性派 5 个 worker：理由是 lint 错误数少，5 个会浪费 token
- 用 verifier 先跑一遍：理由是 verify 阶段会跑

## Risks
- oauth.ts 有 OAuth 逻辑，executor 可能误删（需要 verifier 重点查）

## Files
- src/auth/login.ts
- src/auth/oauth.ts

## Remaining
- 等 team-exec 阶段完成
- 然后 team-verify
```

10-20 行 markdown，但**包含了 plan 阶段的完整决策日志**。

### 为什么这种格式能解决问题？

OMC 面对的核心问题：**lead agent 的 context 会被压缩**。一个完整 `/team` 跑下来可能产生几百 KB 的 LLM 对话历史。当 lead 进 verify 阶段时，它已经不记得 plan 阶段为什么砍了那个 subtask。

如果状态存在内存，压缩后丢失。如果存在 RPC 数据库，需要 schema + 序列化 + 网络。**存在 markdown 文件**：
1. **token 友好** —— 10-20 行，永远不会被压缩成无意义的摘要
2. **人可读** —— debug 时直接 `cat` 就行
3. **可恢复** —— `/team` 中途崩溃，handoff 还在，resume 能接上

### 写 vs 读的时机

- **写** —— 一个 stage 退出时（不是进入）。退出时知道这一阶段的决策已经定了。
- **读** —— 下一个 stage 启动时（即在 LLM 系统提示词的开头注入这份 markdown 作为"上一阶段的备忘录"）。

### 失败模式

如果某 stage 崩溃前没写 handoff？那么下一 stage 会"看不到上一阶段决策"。OMC 的处理：next stage 启动时如果发现 expected handoff 不存在，**拒绝启动**，并给出明确错误（"team-plan stage's handoff missing — re-run team-plan"）。

### 思考练习

写一个 `lint-handoff` 工具：扫描 `.omc/handoffs/*.md`，对每个 file 验证 5 个 section（Decided / Rejected / Risks / Files / Remaining）都非空。如果有空的，报警告。这个 tool 大概 50 行 Go，但部署后能防止"我忘了写 Risks 这一段"这类隐性 bug。

---

## 这四个心智模型为什么够用

OMC 还有 4 个备选 mental model（dossier 里列了 8 个），但上面 4 个**对你 Go 代码的理解最直接相关**：

- **A1 状态机** → 解释了 s09 + s10 为什么要分开
- **A2 模型路由** → 给 s01 的那个 `Model` 字段以意义
- **A3 中间件** → 让 s03 看起来像**普适设计模式**而非 OMC 特例
- **A4 handoff 文档** → 解释了 s09 为什么用文件系统而不是 sqlite

剩下的 4 个（"Don't learn Claude Code, just use OMC" 营销定位、skills/commands/agents 三层抽象、seminar/missions 教学模式、kill-switch 与优雅中断）更偏产品而非架构，作为延伸阅读。

---

## 进一步阅读

- 上游 `docs/ARCHITECTURE.md`、`docs/HOOKS.md`、`docs/TEAM-WORKTREE-MODE.md`
- skills/team/SKILL.md（最完整的 stage-handoff convention 描述）
- 附录 B 给出按章节顺序的源码导读地图
