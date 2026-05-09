---
title: "s10 · 团队 Runtime 与 Watchdog（goroutine 池）"
chapter: 10
slug: s10-team-watchdog
est_read_min: 14
---

# 第 10 章 · 团队 Runtime 与 Watchdog（goroutine 池）

> `learn-oh-my-claudecode` 的第十章，也是收官章。**capstone**。
> s09 把文件系统当成协调原语；s10 把 goroutine + channel 当成
> **执行原语**——三 goroutine 架构（worker 们 + watchdog + 主循环）
> 顶替上游 tmux + `done.json` 轮询，把 1,034 行的 `runtime.ts`
> 折叠到 ~810 行的 runtime 核心（外加 demo 与测试，整章总计 ~1,290
> 行）。这一章是 Go 这门语言交房租的章节。

## 问题

s09 把团队编排的故事讲到一半就停了：任务带着 claim token、租约、
原子写入躺在磁盘上——但**谁来执行它们？** 上游的答案是
`src/team/runtime.ts`：一个 tmux session、每个 worker 一个 pane、
每个 pane 跑一个 CLI agent 在干完活时写 `done.json`，watchdog
每 1 秒去 poll 这些信号文件。整套实现 1,034 行，外加
`runtime-v2.ts`、`runtime-cli.ts`、`worker-bootstrap.ts` 三个并行
runtime。

runtime 必须解决五个具体问题，上游全靠文件系统信号 + tmux：

1. **Spawn N 个 worker。** 上游：`tmux split-window` × N，
   `tmux send-keys` 在每个 pane 里启动 agent。重。
2. **把任务派给空闲 worker。** 上游：每个 worker 读自己 pane 的
   任务信封文件 `<paneId>.task.json`。
3. **检测任务完成。** 上游：每秒 poll `done.json`。
4. **检测 worker 卡死。** 上游：poll `heartbeat.json` 的 mtime；
   gap > 60s 计一笔 strike，连续三笔 kill pane。
5. **进程重启后恢复。** 上游：重读 tmux pane 列表，重扫任务文件，
   重建 `activeWorkers` map。

Go 版用语言一等公民全部替换：goroutine、`chan`、`time.Ticker`、
`context.CancelFunc`、`sync.Mutex`。整章的中心教学点是 1,034 行
TypeScript 折叠到 ~810 行 Go——折叠并非来自巧妙压缩，而是来自
**移除概念**：没 tmux、没 `done.json`、没 per-pane heartbeat 文件、
没 mtime 算术、resume 时也没 `tmux has-session` 检查。

## 解决方案

每个 Pool 有三类 goroutine：

- **N 个 worker**（`worker.go`）：每个跑 `workerLoop`，select
  在共享的 `tasks <-chan Task` 上。拿到任务后写 `currentTask`、
  调 `runTask`（睡 `t.WorkSeconds` 模拟干活，若 `t.Panic` 则
  panic 并被 defer-recover 转成 error），然后把 `WorkerResult`
  发到 `done <-chan WorkerResult`。

- **1 个 watchdog**（`watchdog.go`）：`for { select { case <-ctx.Done():
  return; case <-ticker.C: watchdogTick(p) } }`。每个 tick 扫
  heartbeat map；若某 worker 的 gap 超过 60 秒，strike 计数加一；
  到三笔时调 `pool.killWorker(name)`——cancel 该 worker 的
  `context.Context` 并以同名生成替补 goroutine。

- **1 个主循环**（`pool.go` 的 `Run`）：select 在 results channel 上。
  每个 result 触发 `handleResult`：成功 → `status=done`、
  `pendingCount--`；失败且 `Retries < maxRetries` → `Retries++`、
  status 翻回 pending、重新入队；失败且超过 cap → `status=failed`、
  `pendingCount--`。pendingCount 归零或 ctx 触发时 Run 返回。

`Resume`（`resume.go`）是一次性动作：扫 `<root>/tasks/*.json`、
把 pending+orphaned 的 ID 推到 dispatch channel、返回准备好
`.Run()` 的 `Pool`。~25 LOC——没 tmux session 要验证、没 pane
要枚举。

## 工作原理

### tmux pane 就是 goroutine

```go
// worker.go：整个「pane 生命周期」就是一个 for-select
func workerLoop(ctx context.Context, name string, tasks <-chan Task,
                done chan<- WorkerResult, beat *time.Time, beatMu *sync.Mutex,
                currentTask *string, currentTaskMu *sync.Mutex) {
    updateBeat(beat, beatMu)
    for {
        select {
        case <-ctx.Done():
            return
        case t, ok := <-tasks:
            if !ok { return }
            currentTaskMu.Lock(); *currentTask = t.ID; currentTaskMu.Unlock()
            err := runTask(ctx, t, beat, beatMu)
            currentTaskMu.Lock(); *currentTask = ""; currentTaskMu.Unlock()
            done <- WorkerResult{Worker: name, TaskID: t.ID, Err: err}
        }
    }
}
```

对照 `runtime.ts` L582-L770（`spawnWorkerForTask`）：tmux split-window、
拼 CLI argv、send-keys 发 prompt、挂 heartbeat writer、把 pane
注册到查找表。同样的行为：~190 行 vs. 25 行。

### `time.Ticker` + `context.CancelFunc` 就是 watchdog

```go
// watchdog.go：心跳过期 → strike → 重生
func watchdog(ctx context.Context, pool *Pool, tick time.Duration) {
    t := time.NewTicker(tick); defer t.Stop()
    for {
        select {
        case <-ctx.Done(): return
        case <-t.C: watchdogTick(pool)
        }
    }
}
// watchdogTick：扫 beats；gap > 60s → strike++；到 3 → killWorker。
// killWorker cancel context（worker 经 ctx.Done 退出），再以同名
// 起替补 goroutine。
```

上游 `watchdogCliWorkers`（L466-L580，~115 行）做同样的算术，但
分三个子循环：done.json poll、`isWorkerAlive(paneId)` 死 pane
检测、heartbeat stale 检测。Go 版把前两个删掉了：channel 顶替
done.json，defer-recover 把 panic 转成 channel 结果——死 pane
检测变成跟自然失败同一个码路。

### Channel 干掉了 `done.json` 来回

```go
// pool.go：一个 select 顶掉整个轮询循环
case res := <-p.results:
    p.handleResult(res)
    if pendingCount == 0 { return nil }
```

上游每个 tick 都得跑这个：

```ts
// runtime.ts L481-L504（粗略）
for (const [name, state] of activeWorkers) {
    const donePath = path.join(state.taskDir, 'done.json');
    if (await fs.exists(donePath)) {
        const done = JSON.parse(await fs.readFile(donePath));
        await markTaskFromDone(...);
        await fs.unlink(donePath);
        await tmux.killPane(state.paneId);
        await spawnNextPendingTask(...);
    }
}
```

整段——文件存在性检查、JSON parse、mark、unlink、kill、respawn——
就折叠成 Go 的 select 加 `handleResult`。清理 `fs.unlink` 消失了
因为没信号文件要删；`tmux.killPane` 消失了因为 worker 用同一个
goroutine 接下一单；poll 循环（`for ... await fs.exists`）消失了
因为 `select` 是推驱动的。

## 与 s09 的变化

s09 是协调首次跨进程的章节：文件系统 CAS、文件锁、原子 rename。
**文件系统是协调原语。** s10 留下了那份磁盘状态（Resume 从中
读取），但引入了根本不同的原语：**goroutine 池是执行原语**。

| 关注点 | s09 | s10 |
|---|---|---|
| 协调原语 | `flock` + `os.Rename` + `crypto/rand` token | `chan Task` + `chan WorkerResult` + `time.Ticker` + `context.CancelFunc` |
| 一等公民数据 | `Task`、`Claim`、`LeaseDuration` | `WorkerResult`、`Worker`、`Pool`，外加重新声明的 `Task` |
| 进程模型 | 单进程，但靠 flock 跨进程协调 | 单进程，N+2 个 goroutine，channel 干一切 |
| 崩溃恢复 | 下一个 worker 读到旧 Claim、看 lease 过期、再 claim | `Resume()` 读 pending+orphaned 文件，推回 channel |
| 外部依赖 | `github.com/gofrs/flock`（一处） | **仅 stdlib** |
| 状态在哪 | 磁盘上（持久但慢） | 内存里（快但易失）——磁盘只是审计日志 |

整门课程概念跨度最大的一次：s09 是「文件 + 锁互相对话」，s10 是
「goroutine 互相对话，文件做审计跟踪」。Resume 是桥——读审计日志
来重新填充内存状态。Resume 一返回，磁盘 store 就纯粹是观察者；
channel 在台前主导。

这也是**删除上游架构**而不是翻译它的章节。Tmux：没了。
`done.json`：没了。`heartbeat.json` mtime 轮询：没了。「1,034 行 →
~810 行」低估了认知量的缩水：那 1,034 行里相当一部分是
**在单进程 goroutine 模型下根本不再适用**的概念。

## 动手试一试

```bash
cd agents/s10-team-watchdog

GOWORK=off go vet ./...                # 应当无任何输出
GOWORK=off go build ./...              # 应当无任何输出
GOWORK=off go test -v -count=1 -timeout=30s ./...   # 6 条测试通过，~1s
GOWORK=off go run .                    # 输出与 testdata/expected.txt 一致
```

期望输出：

```
== Setup ==
workers=3 tasks=7 store=<tmpdir>

== Submitting 7 tasks ==
submitted id=task-0 work=0.10s
submitted id=task-1 work=0.20s
submitted id=task-2 work=0.15s
submitted id=task-3 work=0.30s
submitted id=task-4 work=0.20s
submitted id=task-5 work=0.25s
submitted id=task-6 work=0.15s

== Completions ==
id=task-0 status=done
id=task-1 status=done
id=task-2 status=done
id=task-3 status=done
id=task-4 status=done
id=task-5 status=done
id=task-6 status=done

== Final tallies ==
done=7 failed=0 total=7
```

注意 demo 故意没在输出里打印每个任务的 `Retries`——150ms 时
watchdog 重生的具体是哪个 task 取决于调度时序，所以 retries 是
非确定性的。状态收敛（每个任务最终都到 `done`）**是**确定性的；
那才是稳定可断言的不变量。

进一步练习：

- 把 `runTask` 的 sleep 替换成 `exec.CommandContext` 调真正的
  CLI 二进制。defer-recover 应当照旧工作；非零 exit 变成
  `WorkerResult{Err: ...}`。
- 给 `Task` 加一个 `worktree string` 字段，让 worker 在跑之前
  `git worktree add`、跑完 `git worktree remove`——一个 worktree
  对应一个并发 worker，等价于上游的 `TEAM-WORKTREE-MODE.md`
  转写到 Go。
- 加一个 `tmux` build tag 提供可选的 tmux-pane 后端，证明
  Worker 这层抽象是可插拔的。

## 上游源码阅读

下面摘自 `src/team/runtime.ts` L466-L580（`watchdogCliWorkers`），
Go 版最直接替换的就是这个函数。完整带注释版见
`upstream-readings/s10-runtime.ts`：

```ts
async function watchdogCliWorkers(runtime: TeamRuntime): Promise<void> {
  // (a) done.json poll。~30 行。Go 版删掉。
  for (const [name, state] of runtime.activeWorkers) {
    const donePath = path.join(state.taskDir, 'done.json');
    if (await fs.exists(donePath)) {
      const done = JSON.parse(await fs.readFile(donePath, 'utf8'));
      await markTaskFromDone(runtime.teamName, state.taskId, runtime.cwd, done);
      await fs.unlink(donePath);
      await tmux.killPane(state.paneId);
      await spawnNextPendingTask(runtime, name);
    }
  }
  // (b) 死 pane 检测——`isWorkerAlive(paneId)`。
  for (const [name, state] of runtime.activeWorkers) {
    if (!await isWorkerAlive(state.paneId)) {
      await applyDeadPaneTransition(runtime, name, state);
      await spawnNextPendingTask(runtime, name);
    }
  }
  // (c) heartbeat stale。Go watchdog 仅保留这一支。
  const now = Date.now();
  for (const [name, state] of runtime.activeWorkers) {
    const heartbeat = await readHeartbeat(state.heartbeatPath);
    if (now - new Date(heartbeat.updatedAt).getTime() > 60_000) {
      state.unresponsiveCount = (state.unresponsiveCount || 0) + 1;
      if (state.unresponsiveCount >= 3) {
        await tmux.killPane(state.paneId);
        await applyDeadPaneTransition(runtime, name, state);
        await spawnNextPendingTask(runtime, name);
      }
    } else {
      state.unresponsiveCount = 0;
    }
  }
}
```

阅读笔记（与 Go 版的对照）：

1. **子循环 (a) 直接删掉。** Worker → orchestrator 的信号在 Go 这边
   是缓冲 `chan WorkerResult` 的写入，而不是 `done.json` 文件
   会合。Orchestrator 经 `<-p.results` 在 Pool 主循环里读。
   没文件存在性检查、没 JSON 解析、没 `fs.unlink`。~30 行
   TypeScript 折成 4 行 Go。
2. **子循环 (b) 折叠进 worker 自我恢复。** 上游的
   `isWorkerAlive(paneId)` 之所以存在，是因为 tmux pane 可以
   不写 done.json 就死掉（用户 kill、OOM、segfault）。Goroutine
   有等价失败：panic。`worker.go` 的 `runTask` 用
   `defer func() { if r := recover(); r != nil { err = ... } }()`
   把 panic 兜起来，转成普通的 `WorkerResult{Err: ...}`——同一个
   channel、同一个 handler、同一条重试路径。死 pane 检测整体消失。
3. **子循环 (c) 是唯一活下来的分支。** 这就是 `watchdog.go` 的
   `watchdogTick` 镜像的：读心跳、计算 gap、累计 strike、到阈值
   kill+respawn。常量 `HeartbeatStaleThreshold = 60s`、
   `UnresponsiveStrikeMax = 3` 暴露出来供调参。Go 版用
   `*time.Time` 单元在 `beatsMu` 锁下做，而不是 `heartbeat.json`
   的 mtime。
4. **`tmux.killPane(state.paneId)` 变成 `worker.cancel()`。**
   两者都终止执行单元。cancel 经 worker 的 `context.Context`
   传播；worker 的 `runTask` 返回 `ctx.Err()`；worker 在下一次
   select 时退出。替补由 `spawnWorker` 以同名生成。
5. **`spawnNextPendingTask` 隐式发生。** 上游挑下一条 pending
   并赋给刚重生的 worker。Go 重生的 worker 直接从共享 `tasks`
   channel 读——下一条到的就是它跑的。「指派」步骤消失，因为
   channel 本身就是指派。
6. **`watchdog-failed.json` 兜底（L552-L564）也删了。** 上游在
   watchdog 自身记连续错误，到 3 次就放弃并写一个 marker 文件。
   Go watchdog 读的是内存状态，唯一失败模式是 Pool 死锁——靠
   `Shutdown` 的宽限超时兜住，而不是 marker 文件。

1,034 行的缩水是真实的，但行数只是表象，背后是**概念表面积**的
缩小。上游 runtime 维护 tmux session 状态、pane id 表、
heartbeat 文件、done.json 文件、retry-marker 文件、
watchdog-failed 文件。Go runtime 维护内存 map + 缓冲 channel。
读完两边对照的学生会带走一个观感：「goroutine + channel」就是
Go 对「我要造一个 worker pool」始终如一的回答。
