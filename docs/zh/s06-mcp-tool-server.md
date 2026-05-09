---
title: "s06 · MCP 工具注册表与分类禁用"
chapter: 6
slug: s06-mcp-tool-server
est_read_min: 10
---

# 第 6 章 · MCP 工具注册表与分类禁用

> `learn-oh-my-claudecode` 第六章。从 s05 的进程管理转向**把函数当
> 一等值放进结构体字段**：一个工具就是一个结构体，它的 `Handler`
> 字段是 `func(ctx, args) (string, error)` 类型的闭包；工具被注册进
> `map[string]Tool`，运行期通过解析自 `OMC_DISABLE_TOOLS=lsp,python,
> memory` 环境变量约定的「类别禁用集合」做派发与过滤。

## 问题

OMC 给它的 agent 暴露大约 40 个 in-process 工具 —— 12 个 LSP 探针、
2 个 AST 搜索变体、1 个 python REPL，外加若干 state/notepad/memory
助手，再加一长串 skill / interop / codex / gemini / wiki 适配器。
两件运维事实让「每个工具一个工厂函数」的朴素做法撑不住：

1. **有些用户需要一键关掉一整个类别。** 一台没有可用语言服务器的
   机器上，用户希望 `OMC_DISABLE_TOOLS=lsp` 在启动时一次性把 12 个
   LSP 工具全部静音，而不是去清单里一条条改。`python-repl` 在沙箱
   机器上、`memory` 在无状态 CI runner 上 —— 一样的诉求。
2. **派发器必须与传输层无关。** 真正的 MCP 服务器走 stdio JSON-RPC；
   测试想直接当 Go 函数调用 handler；将来某一章可能把同一批工具
   包成 HTTP。注册表那一层根本不该知道当下跑在哪种传输上 —— 它
   只接受「名字 + 原始 JSON 参数」、找到对应的工具、然后返回 handler
   交出来的结果。

上游在 `src/mcp/omc-tools-server.ts` L1–L100 解决了这件事：一个
`ToolDef` 接口（L21–L29）、一张 `DISABLE_TOOLS_GROUP_MAP` 白名单
（L41–L59），以及 `parseDisabledGroups(env)` 助手（L73–L87）—— 它
返回的 Set 被派发器闭包捕获。这一章把 in-process 那一半端口过来，
约 280 行纯标准库 Go。

## 解决方案

公开面共三个文件：

- `tool.go` —— `type Category string`（一个**命名别名**而不是 enum，
  以便 grep）、几个标杆类别常量（`CategoryLSP`、`CategoryPython`、
  `CategoryAST` 等），以及带 `Handler func(ctx, args) (string, error)`
  字段的 `Tool` 结构体。
- `registry.go` —— `New() *Registry`、`Register(t)`、
  `WithDisabled(map[Category]bool) *Registry` 返回**过滤视图**、
  `Invoke(ctx, name, args) (string, error)`、`Names()` 返回排好序的
  切片。两个 sentinel 错误：`ErrUnknownTool` 与 `ErrCategoryDisabled`。
- `env.go` —— `ParseDisabled(env string) map[Category]bool` 解析
  上游的逗号分隔环境变量格式：去除每个 token 周围的空白、跳过空
  token；空输入返回非 nil 的空 map。

一个 `Tool` 值把闭包揣在结构体里。`Register` 把它丢进 map；
`Invoke` 按名字查、按类别拦、再调闭包。整个派发循环大概十行。

## 工作原理

### 工具就是「值 + 闭包」

```go
type Tool struct {
    Name        string
    Description string
    Category    Category
    Handler     func(ctx context.Context, args json.RawMessage) (string, error)
}
```

`Handler` 是一个类型为函数的结构体字段。这一行就是这一章的整个
要点：一个工具就是**数据 + 行为**，打包到一个值里、塞进 map、
**用不着 switch 语句**就能派发。这把戏可以扩展：s10 会用同样的
办法把 goroutine 入口函数当 worker 派发体。

### WithDisabled 返回视图，而不是拷贝

```go
view := r.WithDisabled(ParseDisabled("lsp,python"))
view.Invoke(ctx, "lsp.def", args)   // → "", ErrCategoryDisabled
r.Invoke(ctx, "lsp.def", args)      // → handler 跑了（源 registry 不受影响）
```

`WithDisabled` 返回一个新的 `*Registry`，它的 `tools` map 与接收者
**共享**，但 `disabled` 集合是从 `cats` 重新种出来的一份新 map。
这与上游「parse 一次环境变量、由派发器闭包捕获那个 Set」的写法
对齐 —— 也方便测试为每个用例构造一份干净的 view 而不重建工具列表。
被禁用的工具仍然出现在 `Names()` 里；禁用是**运行期门**，不是
**移除**。

### Sentinel 让错误路径可 grep

```go
out, err := view.Invoke(ctx, "lsp.def", args)
switch {
case errors.Is(err, ErrUnknownTool):       // 拼写错、插件未加载……
case errors.Is(err, ErrCategoryDisabled):  // OMC_DISABLE_TOOLS 包含这个类别
case err != nil:                           // handler 自己失败
}
```

两个导出的 sentinel 编码注册表两条具名失败路径；`errors.Is` 会穿过
任何包装比对。对照 s05 的 `Result.Err` 字段 —— 一样的姿态（错误是
值，不是 panic），不同的形状（每行 vs. 单一 error 返回）。

## 与 s05 的变化

s05 引入了用 context-aware `os/exec` 起子进程 —— 章节系列第一次
跨进程边界产生副作用。s06 引入的东西更含蓄但同样承重：**`func`
作为结构体字段、加上把闭包当数据存起来的注册表模式**。

| 关注点 | s05 | s06 |
|---|---|---|
| 副作用 | `os/exec` 起子进程 | 没有 —— handler 是 in-process 闭包 |
| 派发形态 | 声明式清单 → `[]Result` 切片 | 名字 + 参数 → `(string, error)` |
| 失败模型 | `[]Result` 每行带 `Err` | 通过 `errors.Is` 比对 sentinel |
| 过滤 | 每个钩子的 matcher 谓词 | 每个 registry view 的类别门 |
| 一等公民数据 | `Hook`（matcher + command + timeout） | `Tool`（name + category + **闭包**） |

这就是 Go 的「函数是值」从一句口号变成**结构性承重**的那一章：
要是不能把 `func` 放进 `struct`，整套注册表模式直接塌掉。s10
的 worker pool 会原模原样再用一次这把戏。

## 动手试一试

```bash
cd agents/s06-mcp-tool-server

GOWORK=off go vet ./...                # 应当无任何输出
GOWORK=off go build ./...              # 应当无任何输出
GOWORK=off go test -v -count=1 ./...   # 5 条测试 + 4 条子测试通过，亚秒
GOWORK=off go run .                    # 输出与 testdata/expected.txt 一致
```

期望输出：

```
== invoke python.repl (enabled) ==
out="python.repl evaluated args={\"code\":\"print(1+1)\"}" err=<nil>

== invoke lsp.find_definition (disabled via OMC_DISABLE_TOOLS=lsp) ==
out="" err=category-disabled

== Names() (disabled tools annotated) ==
- lsp.find_definition (category=lsp) [disabled]
- python.repl (category=python)
```

两个用例演练了核心抽象：(a) python 工具能跑 —— 它的类别没被禁用；
(b) lsp 工具被 `ErrCategoryDisabled` 短路 —— `ParseDisabled("lsp")`
把它推进了 view 的禁用集合。`Names()` 把两个都列出来，因为禁用是
**运行期门**，不是**移除**。

进一步练习：

- 加第三个工具到第三个类别（比如 `CategoryAST`），用
  `OMC_DISABLE_TOOLS=lsp,ast` 重跑。注意 `Names()` 输出多了一行
  `[disabled]`，但 demo 打印代码一行都不用改。
- 把 in-memory 的 `Invoke` 换成一个用 `github.com/modelcontextprotocol/
  go-sdk` 跑 stdio MCP server 的实现。注册表本身完全不变；只是多
  一层传输包装。（这是附录 B 习题 #3。）

## 上游源码阅读

下面摘自 `src/mcp/omc-tools-server.ts` L21–L87 与 `src/mcp/servers.ts`
L20–L75（完整带注释版见 `upstream-readings/s06-mcp.ts`）：

```ts
// omc-tools-server.ts L21-L29 —— 每个工具填进的行类型
interface ToolDef {
  name: string;
  description: string;
  category?: ToolCategory;                     // lsp / ast / python / …
  schema: Record<string, unknown>;             // Go 版本里被丢掉
  handler: (args: unknown) => Promise<{ content: ...; isError?: boolean }>;
}

// omc-tools-server.ts L73-L87 —— 环境变量解析器
export function parseDisabledGroups(envValue?: string): Set<ToolCategory> {
  const disabled = new Set<ToolCategory>();
  const value = envValue ?? process.env.OMC_DISABLE_TOOLS;
  if (!value || !value.trim()) return disabled;
  for (const name of value.split(',')) {
    const trimmed = name.trim().toLowerCase();
    if (!trimmed) continue;
    const category = DISABLE_TOOLS_GROUP_MAP[trimmed];
    if (category !== undefined) disabled.add(category);
  }
  return disabled;
}

// servers.ts L20-L75 —— 上游 MCP 故事的另一半
// （走 npx 起的 out-of-process 工厂，本章不端口）
export function createExaServer(apiKey?: string) { /* {command, args, env} */ }
export function createMemoryServer() { /* {command, args} */ }
```

阅读笔记（与 Go 版的对照）：

1. **L21–L29（`ToolDef`）→ `tool.go::Tool`。** 字段一一对照搬过来，
   做了两处简化：`schema` 被丢掉（schema 校验是传输层关注点；
   handler 自己从 RawMessage 上 `json.Unmarshal` 想要的形状），
   `{ content, isError }` 这层 Promise 信封被压扁成 Go 习惯的
   `(string, error)` 二元返回。
2. **L21（`type ToolCategory`）→ `tool.go::type Category string`。**
   上游把类别建模成一个字面量字符串联合（约 15 个名字）；我们用
   `type Category string` 命名别名加几个导出常量（`CategoryLSP`、
   `CategoryPython`……）。字符串别名让环境变量解析最直接（`Category(token)`
   就是一次类型转换，不需要查表），还能让 `grep -rn 'Category("lsp")' .`
   找到每个调用点 —— 教学仓库里最便宜的交叉引用。
3. **L41–L59（`DISABLE_TOOLS_GROUP_MAP`）。** 上游用白名单去校验
   环境变量 token；像 `python-repl` 这样的别名会折叠到 `python`。
   Go 版**没有**白名单 —— 注册表对自定义 Category 字符串保持开放，
   拼错的 token 在派发时只是「没有任何工具被禁」。代价是：拼错
   会**安静地通过**，不会大声报错。教学仓库里我们偏好更简单的
   派发器。
4. **L73–L87（`parseDisabledGroups`）→ `env.go::ParseDisabled`。**
   形状一致（按 `,` 拆、trim、跳空），但去掉了 `process.env`
   兜底（Go 调用方自己传 `os.Getenv("OMC_DISABLE_TOOLS")`），也
   去掉了 `.toLowerCase()`（保留用户大小写，让自定义 Category
   的形状不被偷偷改写）。
5. **`servers.ts` L20–L75 —— 另一半。** 五个外部 MCP 工厂返回
   `{ command, args, env }` 记录，运行期由 `npx` 起进程。它们
   **不是** in-process，本章**不端口**它们 —— in-process 注册表
   是一个独立可教的关注点，out-of-process 监管是另一个。将来
   某一章可以把 `os/exec`（s05 的肌肉记忆）和一个小小的 stdio
   framing 助手配成对来覆盖那一半；in-process 注册表那一半完全
   不用动。
6. **In-process 与 out-of-process 是上游 MCP 层最大的一道分水岭。**
   omc-tools-server.ts 把闭包当数据存；servers.ts 是给外部监管者
   消费的配置记录。知道一个工具属于哪一半，就知道**该改哪一半**
   才能不动派发器添新能力。
