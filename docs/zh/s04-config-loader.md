---
title: "s04 · 分层配置与深合并"
chapter: 4
slug: s04-config-loader
est_read_min: 9
---

# 第 4 章 · 分层配置与深合并

> `learn-oh-my-claudecode` 第四章。从 s03 的纯字符串世界翻到带嵌套
> map 的类型化结构体、四层叠加合并，以及一行对应上游 JavaScript 运行
> 时一类 CVE 的安全关键代码。

## 问题

真实的 OMC 用户**有想法**。他想保留绝大部分内置默认值，但要把
`executor` 钉死在某个具体的 Sonnet 版本上、在低内存笔记本上关掉
LSP-tools 这个特性、还要让某个项目里的 `analyst` 强制走 Opus，无视
宿主机器上的层级环境变量。同一个用户切到另一个项目，又希望**项目
本地**的覆盖不要污染其他仓库。

这就是四层配置 —— 每一层对它下面那一层都有一票否决权 —— 全部塌缩
到一个 `Config` 结构体里的故事：

```
1. defaults                          （二进制内置基线）
2. ~/.config/claude-omc/config.json  （用户级覆盖）
3. <workingDir>/.claude/omc.json     （项目级覆盖）
4. OMC_MODEL_HIGH / _MEDIUM / _LOW   （env 覆盖，最后说话）
```

这幅图里藏了三个子问题：

1. **部分覆盖。** 一个项目文件只写 `features.lspTools` 时，不能把
   `features` 其他字段或别的顶层 key 一并清掉。合并必须**深** ——
   是字段级别的，不是文件级别的。
2. **原型污染。** 上游跑在 JavaScript 上，一份
   `{"__proto__": {"polluted": true}}` 这样的 payload 合到
   `Object.prototype` 上，整个运行时里所有对象都被污染。Go 端没有
   原型链，但配置会跨语言边界 —— 防御纵深要求我们在这里就把这三个
   key 剥掉。
3. **env vs. 用户主动指定。** 当用户已经把 `executor` 主动钉死成
   `claude-sonnet-4-5` 时，`OMC_MODEL_MEDIUM=foo` 这条 env 变量
   **不能**把这个选择推翻。env 是覆盖**默认值**，不是覆盖用户的
   显式选择。

上游在 `src/config/loader.ts` L1–L80 加上 `src/agents/utils.ts`
L367–L393 的 `deepMerge` 工具一次性解决这三件事。本章就是那两段代码
的 Go 翻译，纯标准库，约 250 行。

## 解决方案

公开面共五个文件：

- `config.go` —— `Config / AgentRef / Features / MCPRef` 结构体
  家族，定义 JSON 形态。
- `defaults.go` —— `DefaultConfig() Config`，返回 19 个智能体跨三个
  层级的默认值，外加给 env 推理用的层级回退字符串常量。
- `merge.go` —— `deepMerge(dst, src map[string]any) map[string]any`
  里带 `__proto__ / constructor / prototype` 跳过逻辑。
- `loader.go` —— `Load(workingDir string) (Config, error)` 跑那个
  四层 fold。
- `env.go` —— `applyEnvOverlay(*Config)` 用 `OMC_MODEL_*` 改写仍处
  在层级默认值上的智能体模型。

合并是在 `map[string]any` 上跑的，所以嵌套部分覆盖「自然就能用」——
`encoding/json.Unmarshal` 解到 `map[string]any` 的形态正是
`deepMerge` 操作的对象，最后通过 `mapToConfig` 再 round-trip 一次
把结果还原回 `Config` 结构体。

## 工作原理

### 分层流水线

```
        DefaultConfig()
              │
              ▼ (configToMap)
       map[string]any  ◀── 起点
              │
              ▼ deepMerge(_, user.json)
              │
              ▼ deepMerge(_, project.json)
              │
              ▼ (mapToConfig)
        Config 结构体
              │
              ▼ applyEnvOverlay(&cfg)
              │
              ▼
         resolved Config
```

每一次 `deepMerge` 调用都返回一张全新的 map，因此每一层都是它下面
所有层的纯函数。没有原地变动，没有共享状态。

### 保留 key 防护

```go
// merge.go
var reservedKeys = map[string]struct{}{
    "__proto__":   {},
    "constructor": {},
    "prototype":   {},
}

for k, sv := range src {
    if _, banned := reservedKeys[k]; banned {
        continue   // ⭐ 安全关键，对应 utils.ts L376
    }
    // ... 普通合并逻辑
}
```

三个名字、一个 `for ... continue`。整个 CVE 缓解就这点东西。
`_test.go` 里的 `TestDeepMergeIgnoresProtoPollutionKey` 喂入一份
教科书级的攻击 payload，断言这个 key 不会出现在合并结果里。

### env 覆盖的微妙之处

```go
// env.go —— 只改写仍处在层级回退字符串上的智能体。
switch ref.Model {
case tierHighFallback:    // "claude-opus-4-7"
    if high != "" { ref.Model = high; cfg.Agents[name] = ref }
case tierMediumFallback:  // "claude-sonnet-4-7"
    if medium != "" { ref.Model = medium; cfg.Agents[name] = ref }
case tierLowFallback:     // "claude-haiku-4-7"
    if low != "" { ref.Model = low; cfg.Agents[name] = ref }
}
```

如果用户在项目文件里写了 `"executor": {"model": "claude-sonnet-4-5"}`，
那么 `executor.Model == "claude-sonnet-4-5"` —— 不命中三个层级回退
里的任何一个。switch 直接穿过，env 变量被忽略。这就是上游
`OMC_ROUTING_FORCE_INHERIT 默认关` 的意图，不需要再多挂一个 flag。

## 与 s03 的变化

s03 是纯字符串变换 —— 每个函数都是 `string → string`，没有 I/O，
没有 error，没有结构体。s04 一次引入三件事：

| 关注点 | s03 | s04 |
|---|---|---|
| 状态形态 | 没有（只有字符串） | `Config { Agents map[…]; Features; MCPServers map[…] }` |
| I/O | 没有 | `os.ReadFile`、`os.UserHomeDir`、`os.Getenv` |
| 错误 | 不可能发生 | 类型化 `(Config, error)` 返回；缺文件没问题，格式错误就报错 |
| 安全思维 | 不适用 | 针对原型污染的保留 key 防护 |

s02 时见过的 `(string, error)` 姿态回来了。「保留 key vs. 用户 key」
是个新的心智范畴 —— 直到 s04 之前，没有哪一章需要以**安全**之名
**拒绝**一个输入。

## 动手试一试

```bash
cd agents/s04-config-loader

GOWORK=off go vet ./...                # 应当无任何输出
GOWORK=off go build ./...              # 应当无任何输出
GOWORK=off go test -v -count=1 ./...   # 8 条测试通过
GOWORK=off go run .                    # 输出与 testdata/expected.txt 一致
```

期望输出：

```
== defaults ==
  agents.executor.model    = claude-sonnet-4-7
  agents.architect.model   = claude-opus-4-7
  features.parallelExec    = true
  mcpServers.exa.enabled   = true

== defaults + user ==
  agents.executor.model    = claude-sonnet-4-5
  agents.architect.model   = claude-opus-4-7
  features.parallelExec    = true
  mcpServers.exa.enabled   = false

== defaults + user + project ==
  agents.executor.model    = project-pinned-executor
  agents.architect.model   = claude-opus-4-7
  features.parallelExec    = false
  mcpServers.exa.enabled   = false

== defaults + user + project + env(OMC_MODEL_HIGH=opus-test) ==
  agents.executor.model    = project-pinned-executor
  agents.architect.model   = opus-test
  features.parallelExec    = false
  mcpServers.exa.enabled   = false
```

进一步练习：

- 在 project 与 env 之间再插一层：一个 `--config <path>` CLI flag，
  再读一个 JSON 文件。`Load` 里 slice 的 append 应该加在哪？
- 把 `encoding/json` 换成你选的某个 JSONC 解析器（比如
  `github.com/tailscale/hujson`）。比较一下依赖体积与「内联注释」
  这一点便利的价值。

## 上游源码阅读

下面摘自 `src/config/loader.ts` L41–L72 与 `src/agents/utils.ts`
L367–L393（精简注释版完整文件见 `upstream-readings/s04-loader.ts`）：

```typescript
// loader.ts L41–L72 —— 我们在 DefaultConfig() 里翻译的默认配置字面量。
export function buildDefaultConfig(): PluginConfig {
  const defaultTierModels = getDefaultTierModels();
  return {
    agents: {
      omc:       { model: defaultTierModels.HIGH },
      explore:   { model: defaultTierModels.LOW },
      analyst:   { model: defaultTierModels.HIGH },
      planner:   { model: defaultTierModels.HIGH },
      architect: { model: defaultTierModels.HIGH },
      executor:  { model: defaultTierModels.MEDIUM },
      // ... 还有 14 个
    },
    features: { parallelExecution: true, lspTools: true },
    mcpServers: { exa: { enabled: true }, context7: { enabled: true } },
  };
}

// utils.ts L367–L393 —— deepMerge。
export function deepMerge<T>(target: T, source: Partial<T>): T {
  const result = { ...target };
  for (const key of Object.keys(source)) {
    if (key === '__proto__' || key === 'constructor' || key === 'prototype') continue; // ⭐ L376
    // ... 合并主体
  }
  return result;
}
```

阅读笔记（与 Go 版的对照）：

1. **L1–L9（load 顺序文档块）→ `loader.go::Load`。** 四层顺序原样
   保留。缺文件静默跳过（新用户压根没有这些文件），格式错误是硬
   错误，因为静默吃掉解析失败会把用户配置里的拼写错误藏起来。
2. **L41–L72（`buildDefaultConfig`）→ `defaults.go::DefaultConfig`。**
   智能体 map 的一对一翻译。层级 ID 通过小工具 `envOr` 从
   `OMC_MODEL_{HIGH,MEDIUM,LOW}` 取 —— 这就是 Go 拼写的
   `getDefaultTierModels`。
3. **⭐ L376（保留 key 跳过）→ `merge.go::reservedKeys`。** THE
   关键一行。即便 Go 没有原型链，这一守卫被保留作为**边界防御**：
   下游 JS 消费这份 merged map 时无需再验证就安全。
4. **L378–L391（递归合并主体）→ `merge.go::deepMerge`。** 上游
   `Array.isArray` 守卫翻成 Go 类型断言 —— 只有在 `dst[k]` 与
   `src[k]` 都是 `map[string]any` 时才递归。`sourceValue !== undefined`
   分支变成 `sv != nil`。
5. **JSONC vs JSON。** 上游用 `parseJsonc` 支持 `// 注释`。Go 版主动
   放弃这一点（plan §"Risks #2"）：`encoding/json` 是标准库，注释属于
   README 文档。
6. **env 覆盖**在合并之后跑，所以 env 总是赢过默认 —— 但 Go 版只对
   仍处在层级回退字符串上的智能体生效，用户主动钉的 ID 原封不动。
   对应上游 `OMC_ROUTING_FORCE_INHERIT 默认关` 的语义。
