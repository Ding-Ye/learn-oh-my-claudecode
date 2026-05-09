---
title: "s08 · 后台任务启发式调度"
chapter: 8
slug: s08-background-tasks
est_read_min: 9
---

# 第 8 章 · 后台任务启发式调度

> `learn-oh-my-claudecode` 第八章。从 s07 的「纯正则分类器」转向
> **同一章里两层 API**：一个不做任何 I/O、返回
> `{Background, Reason, Confidence}` 的纯函数 `Decide`，搭配一个
> 通过 `os/exec` 真去拉起进程的非纯 `Executor`。这是章节系列第一次
> 把「确定性核心 + 副作用外壳」作为教学点摊开来讲。

## 问题

长时间运行的 shell 命令会打断交互节奏。生产 trace 里反复出现三种
失败形态：

1. **`npm install` 把终端卡死。** 一次 install 可能跑 90 秒；模型
   发出命令、运行时同步等待，这 90 秒里用户面对的是冻住的提示符。
   更糟的是，在 install 期间模型也无法接受新的指令——一条
   `npm install` 让整个会话停摆。
2. **`git status` 被丢去后台。** 反过来的错误：太热衷于「什么
   shell 命令都丢去后台」的推荐器，会把 `git status` 异步发出，
   逼模型跑一个文件系统轮询循环来取本应内联的输出。用户键入问题、
   答案 200ms 才到，整体节奏立刻不对。
3. **40 个并发的 `cargo build`。** 没有软上限的话，过度激进的
   agent 一口气 fork 出十几个长任务，把开发机直接打满。推荐器
   需要知道**当前在跑几个**，并在预算用尽时拒绝再起新任务。

上游在 `src/features/background-tasks.ts` L1–L100 用三块原语解决：
一个上限常量（`DEFAULT_MAX_BACKGROUND_TASKS = 5`，L24）、一个
长任务正则数组（L29–L70）、以及一个总是阻塞的正则数组
（L74–L100）。本章把三者全部端口过来，外加一个能跑的 spawn
原语，约 250 行纯标准库 Go。

## 解决方案

同一章里两层 API：

- **纯函数 `Decide(cmd, running, max) Decision`**，写在 `decide.go`。
  不碰 I/O、不开 goroutine、不看时间。四分支优先级：cap →
  long-running → blocking → default。函数只分配一个 `Decision`、
  最多扫约 45 条正则；可以无脑对每条命令调用，没有「悔恨预算」。
- **非纯 `Executor.Run(ctx, cmd) (*Handle, error)`**，写在
  `executor.go`。用 `exec.CommandContext("sh", "-c", cmd)`，于是
  调用方可以传任意 shell 表达式。在 Unix 上设 `Setpgid`，让
  context 取消时 SIGKILL 能传到孙进程——没有它，被取消的
  `sh -c "sleep 30"` 只会杀掉 `sh`，留下孤儿 `sleep` 自己跑 30 秒。

这种切分让正则层与 `os/exec` 互不依赖——一个测试可以只用
其中一边，不必把整套都拉起来。两条 `[]*regexp.Regexp` 切片
承载策略，一个 `Decision` 结构体承载判决，一个 `Handle` 结构体
承载已 spawn 进程的状态。合计约 250 行。

## 工作原理

### 两条正则切片，每条都标注上游来源

```go
var LongRunningPatterns = []*regexp.Regexp{
    regexp.MustCompile(`(?i)\b(npm|yarn|pnpm|bun)\s+(install|ci|update|upgrade)\b`),
    regexp.MustCompile(`(?i)\b(pip|pip3)\s+install\b`),
    regexp.MustCompile(`(?i)\bcargo\s+(build|install|test)\b`),
    // …还有 21 条，每条都标注它端口自上游的哪一行
}
var BlockingPatterns = []*regexp.Regexp{
    regexp.MustCompile(`(?i)\bgit\s+(status|diff|log|branch)\b`),
    regexp.MustCompile(`(?i)\bls\b`),
    // …还有 18 条
}
```

`Decide` 就是每个切片一遍 for 循环，没有 switch。要新增一类
长任务，只是一行 append。某条正则写错了，`MustCompile` 会在
首次 import 时 panic——和 s07 在 completion 正则上的姿态一致。

### 纯函数推荐器，四分支优先级

```go
func Decide(cmd string, runningCount, max int) Decision {
    if runningCount >= max { return Decision{Background: false,
        Reason: "concurrency cap reached", Confidence: "high"} }
    for _, p := range LongRunningPatterns { if p.MatchString(cmd) {
        return Decision{Background: true, ...} } }
    for _, p := range BlockingPatterns { if p.MatchString(cmd) {
        return Decision{Background: false, ...} } }
    return Decision{Background: false,
        Reason: "no pattern matched; defaulting to foreground",
        Confidence: "low"}
}
```

cap 检查放在**最前**，因为它是硬上限——即使是 `npm install`，
当已有五个长任务在跑时也得返回 foreground。Reason 字段写明
**为什么**选 foreground，于是运行时能区分「队列起来稍后再跑」
和「快得很，内联跑完吧」。

### Setpgid 是「杀掉 wrapper」与「杀掉真活儿」的差别

```go
cmd := exec.CommandContext(ctx, "sh", "-c", command)
cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
if err := cmd.Start(); err != nil { return nil, err }
```

没有 `Setpgid` 的话，`exec.CommandContext` 取消信号只到达 `sh`，
被孤立的孙进程会继续跑。被取消的 `sh -c "sleep 30"` 会让
`sleep` 跑满整个 30 秒、还没人收尸。`Setpgid: true` 把孩子放进
独立的进程组，这样内核信号会通过 `sh` 传到 `sleep`——
`TestExecutorCancelsOnContext` 这条测试就是用来在有人删掉
`SysProcAttr` 行时大声报错的。

## 与 s07 的变化

s07 是一个**字符串 → Signal 的分类器**——一份输入、一个判决、
没有副作用、不碰并发原语。s08 引入的是根本不同的东西：
**同一章里两层 API**，一个纯推荐器（`Decide`）配上一个真去
fork 进程的非纯 executor。章节系列第一次干净地把「确定性、
无副作用」的代码与「碰 OS」的代码分到两个文件里，两边互不
认识。

| 关注点 | s07 | s08 |
|---|---|---|
| 一等公民数据 | `Signal`（Claimed + Confidence + Reason） | `Decision`（Background + Reason + Confidence）和 `Handle`（Cmd + Wait） |
| Embed 形态 | 提示用 `string`、提醒池用 `[]byte` | （没有——纯正则切片） |
| 行为驱动 | 按正则匹配分类 | 按正则匹配分类 + `exec.CommandContext` 拉起进程 |
| 副作用 | 无 | `os/exec` fork + `Setpgid` + `Wait` |
| 输出 | 给运行时的 `Signal` 判决 | `Decision` 判决 **或** 指向活进程的 `*Handle` |

这也是章节系列第一次 `context.WithCancel` 控制的是真实操作系统
资源——一个进程——而不仅仅是塑造内存里的控制流。这块肌肉
记忆会直接延续到 s10 的 goroutine 池 watchdog：那里用同样的
模式去停掉 worker。

## 动手试一试

```bash
cd agents/s08-background-tasks

GOWORK=off go vet ./...                # 应当无任何输出
GOWORK=off go build ./...              # 应当无任何输出
GOWORK=off go test -v -count=1 ./...   # 5 条测试通过，亚秒
GOWORK=off go run .                    # 输出与 testdata/expected.txt 一致
```

期望输出：

```
== Decide: seven sample commands ==
[long-running] cmd="npm install" running=0 max=5
  -> background=true confidence="high" reason="matches long-running pattern"
[long-running] cmd="cargo build --release" running=0 max=5
  -> background=true confidence="high" reason="matches long-running pattern"
[long-running] cmd="docker pull alpine" running=0 max=5
  -> background=true confidence="high" reason="matches long-running pattern"
[blocking] cmd="git status" running=0 max=5
  -> background=false confidence="high" reason="matches blocking pattern"
[blocking] cmd="ls -la" running=0 max=5
  -> background=false confidence="high" reason="matches blocking pattern"
[default] cmd="hello-world" running=0 max=5
  -> background=false confidence="low" reason="no pattern matched; defaulting to foreground"
[cap-reached] cmd="npm install" running=5 max=5
  -> background=false confidence="high" reason="concurrency cap reached"

== Executor: spawn `sleep 0.3` and Wait ==
started: pid>0=true
exited: code=0
```

七个样本把 `Decide` 的每条分支（long-running、blocking、default、
cap-reached）都覆盖了。Executor 演示用 `sleep 0.3`，因为它够长，
能证明 `Run` 在进程退出前就返回；又够短，让 `go run .` 整体在
一秒之内。

进一步练习：

- 给 `patterns.go` 加一个新的长任务族（比如 `pip-tools compile`），
  重新跑测试。append 一行就行。
- 与 s05 的钩子流水线组合：注册一个 `PreToolUse` 钩子，对待执行
  命令调一次 `Decide`，当判决是 `Background=false, Confidence="low"`
  且用户设了 `Force-Background` 标志时，拦下这条工具调用。

## 上游源码阅读

下面摘自 `src/features/background-tasks.ts` L1–L100（完整带注释版
见 `upstream-readings/s08-background-tasks.ts`）：

```ts
// L24 —— 上限常量
export const DEFAULT_MAX_BACKGROUND_TASKS = 5;

// L29-L70 —— 长任务正则（24 条，覆盖 5 个家族）
export const LONG_RUNNING_PATTERNS = [
  /\b(npm|yarn|pnpm|bun)\s+(install|ci|update|upgrade)\b/i,
  /\b(pip|pip3)\s+install\b/i,
  /\bcargo\s+(build|install|test)\b/i,
  /\bgo\s+(build|install|test)\b/i,
  // ... 包管理、构建、测试套件、docker、db、lint、慢 git
];

// L74-L100 —— 阻塞正则（20 条）
export const BLOCKING_PATTERNS = [
  /\bgit\s+(status|diff|log|branch)\b/i,
  /\bls\b/i,
  /\bpwd\b/i,
  // ... 快速状态检查、文件操作、环境检查
];
```

阅读笔记（与 Go 版的对照）：

1. **L24（上限常量）→ `DefaultMaxBackgroundTasks`。** 同样的值
   （5）、同样的「咨询」语义。Go 这一侧把常量导出，调用方可在
   构造时比对或覆盖。cap 检查是 `Decide` 里的**第一**步，因为它
   是硬上限；预算用尽时正则判决毫无意义。
2. **L29–L70（长任务正则）→ `LongRunningPatterns`。** 24 条全部
   逐字端口。两处因 Go RE2 引擎不得不调整：TS 的 `/.../i` 后缀
   改成 Go 的 `(?i)` 内联标志；`make` 那条尾部的 `$` 锚保留——
   它正是用来区分 `make` 在行尾（长任务）与 `make ./target`
   （走默认分支）的关键。
3. **L74–L100（阻塞正则）→ `BlockingPatterns`。** 20 条全部逐字
   端口。注意 `\bls\b` 不看 flag：`ls -la` 与 `ls --color=never`
   都保持 foreground。即使 `ls` 喷出十万行，运行时也信任用户的
   选择——把它接到 pager 是用户该做的事，不是推荐器该决定的。
4. **L150–L210（`shouldRunInBackground`，意译）→ `Decide`。**
   决策顺序与上游一致：cap → long-running → blocking → default。
   **唯一**的行为差异在 default 分支。上游返回
   `ok=true, confidence='low'`（「拿不准的就丢去后台」）。Go 版
   特意反过来：`Background=false, Confidence="low"`（「拿不准的
   就让用户看见」）。规划文档把这一点列成有意为之的教学选择
   ——对一个交互式 CLI agent，把不确定的命令塞去后台是错误的
   默认。
5. **`BackgroundTaskManager` 类（L114+）—— 不端口。** 上游把一个
   有状态的任务追踪器（在跑列表、单任务超时、完成回调）和分类
   器塞进同一个文件。Go 章节有意把这块表面留给 s09（文件系统
   CAS 任务存储）加 s10（带 watchdog 的 goroutine 池）一起合成。
   保持推荐器纯净，就意味着测试不必把整套运行时都拉起来。
6. **`Executor` 层是 Go 这边新增的。** 上游借助 Claude Code 自带
   的 `run_in_background` Bash 工具 flag 完成实际 fork，源码里
   并没有 `BackgroundTaskManager.spawn` 方法。Go 章节加一个小小
   的 `Executor`，是为了让 spawn-与-cancel 的细节在这个仓库里
   **看得见**——具体说，是为了让 `Setpgid` 是读者看到的一行代码
   而不是脚注。
