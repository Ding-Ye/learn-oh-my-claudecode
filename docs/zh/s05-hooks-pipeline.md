---
title: "s05 · 钩子流水线（os/exec）"
chapter: 5
slug: s05-hooks-pipeline
est_read_min: 10
---

# 第 5 章 · 钩子流水线（os/exec）

> `learn-oh-my-claudecode` 第五章。从 s04 的类型化结构体世界翻到
> **进程管理**：用 JSON 清单声明生命周期钩子，每个钩子用 `os/exec`
> 外起进程，每个钩子单独配 `context.WithTimeout` 超时预算，payload
> 走 stdin 喂进去，单个钩子的报错收集进结果不影响其它兄弟节点。
> **整个章节系列里第一次 shell out**。

## 问题

Claude Code 的生命周期大概有十来个事件：`UserPromptSubmit`、
`SessionStart`、`PreToolUse`、`PostToolUse`、`PermissionRequest`、
`Stop`、`SessionEnd` 等等。每个事件都希望跑一段**很小**的程序去
检视事件 payload 然后做点副作用 —— 注入一个 skill 提示、记一笔
project-memory 快照、拒绝某条危险的 Bash 命令。每个事件可以挂**多个**
程序，而且某个慢程序**绝对不能**阻塞别的、也不能把整个 agent 卡死。

这一个特性里其实藏着三个子问题：

1. **声明式接线。** 每个事件的钩子集合是数据（`hooks/hooks.json`），
   不是代码。用户（或一个插件）应该能通过编辑 JSON 文件来添一个新的
   事件处理器 —— 不需要重新编译。
2. **单钩子超时。** 一个有 bug 的脚本死循环，不能把整个 dispatcher
   一起拖下水。每个钩子有自己的时间预算（上游 3–60s；测试里我们用 1s
   让套件跑得快）。
3. **兄弟隔离。** 五个钩子里第三个超时的时候，#1、#2、#4、#5 必须
   还能给出结果。失败该怎么处理交给调用方决定 —— dispatcher 中途
   不抛 `panic`、不 `return error` 跳出。

上游在 `hooks/hooks.json` L1–L212（清单）加上一个 TypeScript dispatcher
（用 `child_process.spawn` 与 `Promise.allSettled`）解决这三件事。
本章一对一搬了清单，再用约 260 行纯标准库 Go 重写 dispatcher。

## 解决方案

公开面共三个文件：

- `hook.go` —— `Hook / Entry / Manifest`，外加自定义 `UnmarshalJSON`
  把磁盘上的 `"timeout": 5`（秒，整数）转换成 `time.Duration`。
- `dispatcher.go` —— `Dispatcher.Dispatch(ctx, event, payload) []Result`、
  matcher 谓词、单钩子的 `runHook` 助手。每个钩子跑在自己的进程组里；
  超时时我们 SIGKILL `-pgid` 这样后裔 `sleep` 会跟着 leader 一起死。
- `main.go` —— 演示：加载 `testdata/hooks.json`，用两份 payload
  触发两次 `UserPromptSubmit`（第一份安静，第二份带 `boulder`
  以触发那条 1 秒超时的条目）。

每个 `Result` 里都带着 `Event / Matcher / Command / ExitCode / Stdout
/ Stderr / Err`。我们从不抛错；每次 `Dispatch` 调用返回一个
`[]Result`，`Err` 总是逐行携带。

## 工作原理

### 流水线总览

```
  Dispatch(ctx, "UserPromptSubmit", payload)
        │
        ▼  Manifest["UserPromptSubmit"] → []Entry
        │
        ▼  for each Entry: matches(entry.Matcher, payload) ? continue
        │
        ▼  for each Hook in Entry.Hooks:
        │       hookCtx, _ := context.WithTimeout(ctx, hook.Timeout)
        │       cmd := exec.CommandContext(hookCtx, "sh", "-c", hook.Command)
        │       cmd.Setpgid + cmd.Cancel = SIGKILL(-pgid) + cmd.WaitDelay
        │       cmd.Stdin = payload  ;  capture stdout/stderr  ;  cmd.Run()
        │       package outcome → Result
        │
        ▼  []Result（每个触发的钩子一行，按声明顺序）
```

第三格里那三个旋钮 —— `Setpgid`、`Cancel`、`WaitDelay` —— 是本章
最有看头的系统编程课；`## 工作原理` 接下来就要细看它们。

### 进程组小把戏

`exec.CommandContext` 只杀直接子进程。所以一条形如 `sh
testdata/scripts/sleep_too_long.sh` 的清单条目，最后会变成
`sh -c "sh script"`：外层 `sh` fork 出内层 `sh`，内层再 fork `sleep`。
杀掉外层会留下里面两个孤儿，而 Go 的 `cmd.Wait` 会卡在**继承下来**的
stdout 管道上 —— 一个 1 秒 context-timeout 的测试，会跑掉 30 秒的
真实墙钟时间。

三行修好：

```go
cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
cmd.Cancel = func() error {
    return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)  // 负 pid → 整组
}
cmd.WaitDelay = 500 * time.Millisecond
```

`Setpgid: true` 把这个钩子放进自己专属的进程组，组 ID 等于 leader
的 PID。`Cancel` 是 Go 1.20+ 提供的「context 过期时该怎么办」的
重写口 —— 我们对 `-pgid` 发 SIGKILL，整棵子树一起死。`WaitDelay`
是兜底网：万一杀完之后还有后裔握着继承的管道不放，Go 会在 500 ms
之后强制把管道排干。

### 把错误当成一等公民兄弟

```go
results = append(results, d.runHook(ctx, event, entry.Matcher, h, payloadJSON))
// 没有 break，没有 err 时 return —— 兄弟节点继续跑
```

`runHook` 把每一种结局 —— 成功、退出码非零、deadline exceeded、
找不到二进制 —— 都打包成一个 `Result`。dispatcher 只管 append。
调用方遍历这个 slice 的时候按声明顺序看到每行的 `Err` 字段，
没有任何 `error` 聚合的奇技淫巧。

## 与 s04 的变化

s04 是纯数据层：结构体、JSON、deep merge，除了 `os.ReadFile` 之外
没有 I/O，也没有并发。s05 一次引入四件事：

| 关注点 | s04 | s05 |
|---|---|---|
| 副作用 | 没有（纯函数） | `os/exec` 起子进程 |
| 失败模型 | `(Config, error)` | `[]Result` 每行带 `Err` |
| 时间 | 不适用 | `time.Duration`、`context.WithTimeout`、`WaitDelay` |
| 资源清理 | 不适用 | 进程组、SIGKILL、管道排干 |

这就是 Go 的「错误即值」从一句口号变成一种**姿态**的那一章：
dispatcher 之所以返回 slice，是因为 slice 里某些行**就是**预期的
失败 —— 这没问题。

## 动手试一试

```bash
cd agents/s05-hooks-pipeline

GOWORK=off go vet ./...                # 应当无任何输出
GOWORK=off go build ./...              # 应当无任何输出
GOWORK=off go test -v -count=1 ./...   # 6 条测试通过，墙钟约 2s
GOWORK=off go run .                    # 输出与 testdata/expected.txt 一致
```

期望输出：

```
== UserPromptSubmit (payload={"prompt":"hello"}) ==
[UserPromptSubmit/*] sh testdata/scripts/echo.sh -> exit=0 stdout="{\"prompt\":\"hello\"}" err=<nil>

== UserPromptSubmit (payload={"prompt":"push the boulder"}) ==
[UserPromptSubmit/*] sh testdata/scripts/echo.sh -> exit=0 stdout="{\"prompt\":\"push the boulder\"}" err=<nil>
[UserPromptSubmit/boulder] sh testdata/scripts/sleep_too_long.sh -> exit=-1 stdout="" err=deadline-exceeded
```

第一份 payload 只命中 `"*"`。第二份带了 `boulder`，第三个条目也跟着
点火 —— 它的 `sleep 30` 撞上 1 秒预算被砍掉。

进一步练习：

- 把 `os/exec` 那套 shell-out 换成 README 反模式注脚里那 8 行的
  in-process 函数表。注意每个测试是怎么瘦身的；注意还有什么再去演练
  进程组。
- 在 `testdata/hooks.json` 里给 `PostToolUseFailure` 加两个 matcher
  （一个 `*`、一个 `Bash`）。dispatcher 要改什么？（答案：什么也不用 ——
  `Manifest` 在事件名上是开放的，本就是这么设计的。）

## 上游源码阅读

下面摘自 `hooks/hooks.json` L1–L62（完整带注释版见
`upstream-readings/s05-hooks.json`）：

```jsonc
{
  "description": "OMC orchestration hooks with async capabilities",
  "hooks": {
    "UserPromptSubmit": [               // L4–L19 —— 单 "*" matcher，2 个钩子
      { "matcher": "*",
        "hooks": [
          { "type": "command",
            "command": "node ... keyword-detector.mjs",
            "timeout": 5 },
          { "type": "command",
            "command": "node ... skill-injector.mjs",
            "timeout": 3 }
        ] }
    ],
    "SessionStart": [                   // L21–L62 —— 三个 matcher！
      { "matcher": "*",                 //   总是触发（3 个钩子，每个 5s）
        "hooks": [ /* ... */ ] },
      { "matcher": "init",              //   只在首次运行（1 个钩子，30s）
        "hooks": [ /* ... */ ] },
      { "matcher": "maintenance",       //   每周维护（1 个钩子，60s）
        "hooks": [ /* ... */ ] }
    ]
  }
}
```

阅读笔记（与 Go 版的对照）：

1. **L4–L19（UserPromptSubmit）→ `Manifest["UserPromptSubmit"]`。**
   形态是「event: [{matcher, hooks: [...]}, ...]」—— 正好是
   `type Manifest map[string][]Entry` 编码出来的样子。上游通过
   `Promise.allSettled` 并发跑两个钩子；Go 版按声明顺序串行跑，
   断言里能看到稳定顺序。
2. **L21–L62（多 matcher 的 SessionStart）→ `dispatcher.go::matches`。**
   本章里最难的那个用例。一份 payload 为 `{"reason":"init"}` 的
   SessionStart 应该触发条目 1（`*`）和条目 2（`init`），但不触发
   条目 3（`maintenance`）。Go 版用 substring 直接对原始 payload
   字节做匹配 —— 教学上够用；生产代码会真把 payload 解析一遍再取出
   matcher 字段比对。
3. **`"timeout": 5`（秒）→ `time.Duration`。** 上游靠 JS 运行时
   「`setTimeout` 里数字代表毫秒」这一惯例。Go 没有这种惯例；
   要是没写 `hook.go::UnmarshalJSON`，这个字段会按 5 *纳秒* 反序列
   化，每个钩子立刻超时。
4. **反模式 #5（「靠 CLI 起进程」）。** plan §「反模式」明确点过
   上游这种「每个钩子起一个进程」的代价 —— 每个钩子都是一次 Node
   启动（约 30–60 ms）。Go 版**仍然**保留 shell-out，因为本章的
   教学要点正是进程管理这套表面：超时、stdin、错误收集、进程组杀
   ——把这些去掉了就没剩多少东西。那 8 行 in-process 替代方案放在
   README 注脚里，而不是当本章的正典实现。
5. **我们没翻译的部分。** 上游文件后面还有八个事件类型（PreToolUse、
   PermissionRequest、PostToolUse、PostToolUseFailure、
   SubagentStart/Stop、PreCompact、Stop、SessionEnd）—— 形态完全一样。
   `Manifest` 是一个开放的 `map[string][]Entry`，所以加任何新事件
   名都「自然就能用」，不需要碰 dispatcher。
6. **为什么 upstream-readings 的副本用 JSONC。** 上游本身是纯 JSON；
   那里写注释非法。我们的注释副本只是把 JSONC 当一种**注释媒介**
   用。教学仓库真正在磁盘上的 fixture（`testdata/hooks.json`）是
   纯 JSON，这样 `encoding/json` 不需要任何 JSONC 依赖就能解析。
