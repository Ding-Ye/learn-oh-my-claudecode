---
title: "s01 · 智能体注册表与模型分层"
chapter: 1
slug: s01-agent-registry
est_read_min: 8
---

# 第 1 章 · 智能体注册表与模型分层

> 本章是 `learn-oh-my-claudecode` 的开篇。我们用最少的 Go 代码（约 100 行
> 实现 + 100 行测试）确立 `Agent` 这个贯穿全书的核心数据结构，并把上游
> `getAgentDefinitions()` 里那条四层模型优先级链原汁原味地翻译成 Go 函数。

## 问题

OMC 在编排 Claude Code 时，要在 19 个内置子角色（`architect`、`planner`、
`executor`、`critic` 等）之间分发任务。每个角色有：

- 一段独立的 system prompt（在 `agents/<name>.md` 里）；
- 一份允许使用的工具白名单；
- 一个推荐的模型层级（Haiku / Sonnet / Opus）。

**关键张力**：模型层级不能写死。同一个 `executor` 角色，用户既可能想用
Sonnet（默认），也可能在某次调用时用 `--model=haiku` 临时降本，还可能在
环境变量里设置 `OMC_ROUTING_FORCE_INHERIT` 让所有子任务继承父进程的模型。
**注册表必须既存得下静态数据，又允许查询时动态决议层级**。

上游用 TypeScript 接口 + 一个 19 项的字面量 + 一条 4 段式空值合并表达式解决
这个问题（`src/agents/definitions.ts` L289）。本章用 Go struct + map +
一个纯函数 `ResolveModel` 完成等价工作，并刻意丢弃上游的 kebab/camelCase
名字翻译表（这是计划里钦点的「不要复刻的反模式 #4」）。

## 解决方案

`map[string]Agent` 一次注册、多次查询；查询时通过 **五入参** 的
`ResolveModel(agent, override, envInherit, configured)` 决议出最终模型字符串。
五个候选按优先级从高到低取第一个非空值：

```
override → envInherit → configured → agent.Model → agent.DefaultModel
```

注册表本身不缓存解析后的模型——这与上游每次 `getAgentDefinitions()` 都
重新计算的纪律一致：让动态参数（per-call override、env 变量）天然透传，
无需维护「失效缓存」。整章无 I/O、无并发、无正则，最适合作为 Go 学习曲线的
第一个落点。

## 工作原理

### 总览图

```
                   register-time                  query-time
         ┌─────────────────────────┐    ┌──────────────────────────┐
Agent{   │                         │    │   ResolveModel(a, ovr,   │
 Name,   │  Registry.Register(a)   │    │      envInherit, cfg)    │
 Prompt, │   ──> agents[a.Name]=a  │    │       │                  │
 Model,  │                         │    │       ▼                  │
 ...} ──▶│  Registry.Get(name)     │───▶│  override?            ┐  │
         │   ──> (a, ok bool)      │    │  envInherit?          │  │
         │                         │    │  configured?          │ first non-empty
         │  Registry.Names()       │    │  agent.Model?         │  │
         │   ──> sorted []string   │    │  agent.DefaultModel?  ┘  │
         └─────────────────────────┘    └──────────────────────────┘
```

### 核心代码

```go
// agent.go
type Agent struct {
    Name         string
    Description  string
    Prompt       string
    Tools        []string
    Model        string // tier-resolved string like "claude-opus-4-7"
    DefaultModel string // fallback if Model is empty
}

// registry.go
type Registry struct{ agents map[string]Agent }

func New() *Registry              { return &Registry{agents: make(map[string]Agent)} }
func (r *Registry) Register(a Agent) { r.agents[a.Name] = a }       // 后写覆盖前写
func (r *Registry) Get(name string) (Agent, bool) {
    a, ok := r.agents[name]
    return a, ok
}
func (r *Registry) Names() []string {
    names := make([]string, 0, len(r.agents))
    for n := range r.agents { names = append(names, n) }
    sort.Strings(names)
    return names
}

// 四层（Go 里因为加了 DefaultModel 实际是五层）优先级链
func ResolveModel(a Agent, override, envInherit, configured string) string {
    for _, c := range []string{override, envInherit, configured, a.Model, a.DefaultModel} {
        if c != "" { return c }
    }
    return ""
}
```

### 三个不那么显然的点

1. **为什么 `Register` 选「后写覆盖」而不是返回错误？**
   上游用对象字面量 + spread 语法构建注册表（`definitions.ts` L211-L260），
   后赋值天然覆盖前赋值。我们沿用同一语义，便于用户在 init 阶段追加自定义
   agent 而无需先 `Unregister`。代价是丢失了「重复注册」这一类拼写错误的
   早期信号——s01 把这点写进单元测试 `TestRegisterDuplicateNameOverwrites`
   作为契约钉死。

2. **为什么 `ResolveModel` 收 4 个参数而不是 1 个 struct？**
   把 4 个候选都放进一个 `Options` struct 看似更整洁，但会让调用方多写
   一层 `Options{Override: x}` 字面量。s01 的 `ResolveModel` 是热路径
   候选——每个 agent 派发都会调用一次——位置参数让调用点更短，且 4 个参数
   都是 `string` 类型，Go 编译器无法因为顺序错误给出帮助。这是个有意识
   的简洁优先选择，s04 配置加载器章节里我们会改用 struct。

3. **为什么 Go 版多了一层 `agent.DefaultModel` 但上游只有四层？**
   上游让 `defaultModel` 与 `model` 平行暴露给 Claude Agent SDK，由 SDK
   自己决定何时降级。Go 没有 SDK 层级——`ResolveModel` 是注册表与下游消费
   者之间唯一的接口——所以把 `DefaultModel` 折叠成「最后一档兜底」比让调用
   方手动二次回退更符合人体工学。如果你需要严格还原上游行为，传入
   `agent.DefaultModel = ""` 即可让该层透明跳过。

## 与上一节的变化

s01 是全书第一章，没有「上一节」可对照。它要为后续 9 章奠定的是
**`Agent` 这个数据契约**：

| 章节 | 复用 `Agent` 的子集 |
|---|---|
| s02 提示词加载器 | `Name + Prompt` |
| s04 配置加载器 | `Name + Model`（在 `Config.Agents` 里间接出现） |
| s10 团队 Watchdog | `Name + Model`（仅消费） |
| s03/s07/s08 | 不复用——是纯字符串/正则章节 |

读完本章你应该能答出三个问题：(a) 19 个 agent 在内存中是什么形状？
(b) 调用方如何强制覆盖某次的模型？(c) 为什么我们刻意不抄上游的
`AGENT_CONFIG_KEY_MAP`？

## 动手试一试

```bash
cd agents/s01-agent-registry

go vet ./...     # 应当无任何输出
go build ./...   # 应当无任何输出
go test -v ./... # 5 个测试全部通过
go run .         # 应当输出与 testdata/expected.txt 完全一致
```

期望输出：

```
architect found=true model=claude-opus-4-7
with override: claude-haiku-4-5
without override: claude-opus-4-7
registered: [architect executor]
```

进一步练习：

- 把 `main.go` 里的 `architect.Model` 改成空串，再次运行——你会发现
  `without override` 行变成空。这正是「全部为空时返回空串」的契约。
- 把第二次 `r.Register(...)` 改成 `Name: "architect"`（覆盖第一个），
  观察 `Names()` 数组是否仍只有一项。

## 上游源码阅读

下面摘自 `src/agents/definitions.ts` L210–L298（精简注释版完整文件见
`upstream-readings/s01-definitions.ts`）：

```typescript
const AGENT_CONFIG_KEY_MAP = {
  explore: 'explore',
  // ... 17 行更多映射 ...
  'security-reviewer': 'securityReviewer',
  'code-reviewer': 'codeReviewer',
  // ⚠ kebab-vs-camelCase 翻译表 — Go 版直接丢弃
} as const;

export function getAgentDefinitions(options?: {...}) {
  const agents: Record<string, AgentConfig> = {
    explore: exploreAgent, analyst: analystAgent, /* ... 17 个 ... */
  };

  const resolvedConfig = options?.config ?? loadConfig();
  const inheritModel = resolvedConfig.routing?.forceInherit
    ? resolveInheritedModelFromEnv()
    : undefined;

  for (const [name, agentConfig] of Object.entries(agents)) {
    const override = options?.overrides?.[name];
    const configuredModel = getConfiguredAgentModel(name, resolvedConfig);

    // ⭐ 四层优先级链（本章的核心一行）
    const resolvedModel =
      override?.model ?? inheritModel ?? configuredModel ?? agentConfig.model;

    result[name] = { /* ...装回结果... */ model: resolvedModel };
  }
  return result;
}
```

阅读笔记（与 Go 版的对照）：

1. **L211-L260（agents 字面量）→ Go 的 `Register` 循环**。上游一次性
   字面量构造，Go 用多次 `Register` 调用——这让用户能在自己的代码里追加
   私有 agent，更符合 Go 的「显式构造」习惯。
2. **L289（核心一行）→ Go 的 `ResolveModel` 函数体**。一对一翻译，只是
   多了一个 `agent.DefaultModel` 兜底。
3. **L143-L162（KEY_MAP）→ Go 没有对应物**。上游的 camelCase 在
   `omc.jsonc` 配置文件中也是 camelCase；如果 Go 版未来要加配置加载（s04），
   我们会让 JSON 配置直接用 kebab-case，所以翻译表永远不需要。
4. **`appendSkininthegamebrosGuidance`（在 prompt 字段上）→ Go 版直接保留
   原始 prompt**。这个上游小工具会向每个 agent 的 prompt 末尾追加一句
   「skin in the game」式的承诺；它和注册表本身解耦，属于 s07
   continuation-enforcement 章节的范畴。
5. **`resolveInheritedModelFromEnv()` → Go 版让调用方自己处理 `OMC_…` 环境
   变量**。`ResolveModel` 收下解析后的字符串，关心「读环境」是 main.go
   的事。这个分层更适合测试。
