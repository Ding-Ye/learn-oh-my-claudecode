---
title: "s09 · 文件态任务状态机"
chapter: 9
slug: s09-task-state-machine
est_read_min: 11
---

# 第 9 章 · 文件态任务状态机（CAS+租约）

> `learn-oh-my-claudecode` 第九章。从 s08 的「纯正则 + 轻 `os/exec`」转到
> 章节系列里第一次把**文件系统本身当作协调原语**：一个由 JSON 文件支撑
> 的任务存储、每条任务一把 `flock` 互斥锁、原子 rename 写入、外加 15
> 分钟租约的 claim token。s10 的 goroutine 池就架在这一层上面。

## 问题

多个 worker 同时盯着一份 pending 任务列表，要在不踩到对方的前提下挑活儿
干。生产中至少有四种翻车形态：

1. **两个 worker 同时声称自己拿到了任务 #7。** 没有互斥的话，A 和 B
   的 read-modify-write 会交错：两人都看到 status=pending，两人都改成
   in_progress，最后磁盘上写下的是后到的人。两个进程都开始干活，
   产出会冲突。
2. **A 拿到任务后崩溃，#7 永远卡在 in_progress。** 只用单字段 owner
   是「占住就一辈子是你的」模型——A 走了之后没人来收尸。需要某种
   过期机制让继任者接手。
3. **写到一半进程被 kill，磁盘上的 task.json 半成品。** 一个 reader
   看到的可能是「{"status":"in_pr…」，json 解析直接爆掉。需要保证
   读者要么看到旧文件、要么看到新文件，绝不见半截。
4. **B 没拿到 token 但伪造一个去推 transition。** 没有 token 校验，
   任何 reader 都能假装自己是 owner，把任务标成 done——状态机就成了
   可读写的开放数据库。

上游在 `src/team/state/tasks.ts` L1-L120 用三块原语解决：一把
per-task `flock`（`withTaskClaimLock`，L43）、一个原子 rename 的
`writeAtomic`（在 `src/team/runtime.ts`，L80-L140 调用），以及
`claimTask`（L50-L99）里把 token + leased_until 一起塞进 JSON 文件。
本章把三块都端口过来，外加 `crypto/rand` 出来的 token，共约 470 行 Go。

## 解决方案

四个文件，各管一件事，互不交叉：

- **`atomic.go`**：`writeAtomic(path, data, perm)`。先写 `path.tmp`、
  再 `os.Rename`。POSIX 保证 rename 对 reader 原子——半成品的 `.tmp`
  绝不会变成 target，崩溃中的 writer 只会留下旧文件原样。
- **`lock.go`**：`withFileLock(lockPath, fn)`。在 `lockPath+".lock"`
  上拿排他 `flock`，跑 fn，defer 放锁。用
  `github.com/gofrs/flock` 而不是手写 `syscall.Flock`，因为后者绑死
  Linux；这一处 dep 一次缴清，Darwin / Linux / FreeBSD / Windows 都
  能跑。
- **`store.go`**：`Store{ root }` 三件套——`Read` / `Write` / `List`。
  没有内存 cache，每次读都走盘。布局
  `<root>/<team>/tasks/<id>.json`，跟上游
  `taskFilePath` 一字不差。
- **`claim.go`**：状态机。`ClaimTask` 在锁里 read → 检查终态 →
  比对 lease → mint token → bump Version → atomic-write。
  `TransitionTask` 在锁里检查 status CAS、token 相等、lease
  仍在窗口内，三个全过才允许写。`crypto/rand` 出 16 字节的 hex
  当 token——128 bit 熵，与 UUIDv4 同档位。

互斥来自 `flock`，持久性来自 `os.Rename`，身份来自 `crypto/rand`。
全章不出现一个 `sync.Mutex`。

## 工作原理

### 一把 flock + 一次原子 rename = 整个崩溃故事

```go
// claim.go 里 ClaimTask 的核心，去掉错误处理只看主线
err := withFileLock(s.taskPath(team, taskID), func() error {
    t, _ := s.Read(team, taskID)
    if t.Claim != nil && time.Now().Before(t.Claim.LeasedUntil) {
        return ErrLeaseStillValid
    }
    newToken, _ := randomUUID()
    t.Status, t.Owner = "in_progress", worker
    t.Claim = &Claim{Token: newToken, Owner: worker,
                     LeasedUntil: time.Now().Add(LeaseDuration)}
    t.Version++
    return s.Write(team, t)  // 内部走 writeAtomic
})
```

锁释放后磁盘上的 task.json 要么是 worker A 的版本（成功），要么是
原样（任何一步失败）。两个 worker 同时调 ClaimTask：第一个抢到锁、
拿到 token、写入；第二个在 Lock 处阻塞，进 fn 后看到 Claim 还活着，
返回 ErrLeaseStillValid。没有半截状态、没有双重 owner。

### Token 是「你以为你是」与「你确实是」的分水岭

```go
// claim.go 里 TransitionTask 的三步检查
if t.Status != fromStatus { return ErrIllegalTransition }
if t.Claim == nil || t.Claim.Token != token || t.Claim.Owner != worker {
    return ErrTokenMismatch
}
if !time.Now().Before(t.Claim.LeasedUntil) { return ErrLeaseStillValid }
```

token 用 `crypto/rand` 生成、随 ClaimTask 返回。它是 worker 唯一能
证明自己是这把活的合法持有者的凭据。即便另一个 worker 拿到磁盘上的
task.json 读到 token 字段，它也没有继续推进的合法路径——除非它
发起了那次 ClaimTask。`TestTransitionRequiresMatchingToken` 就是
用一个伪造的 32 字符 hex 串 `deadbeef…` 去推 transition、断言失败。

### 租约让死掉的 worker 不会卡死任务

```go
// claim_test.go 模拟过期：把 LeasedUntil 拨到一分钟前再写回
stale.Claim.LeasedUntil = time.Now().Add(-time.Minute)
s.Write(team, stale)
// 第二个 worker 现在能成功 claim
tokenB, _ := s.ClaimTask(team, id, "worker-B")
```

LeaseDuration = 15 分钟（`time.Now().Add(LeaseDuration)`）。一旦
墙钟越过 LeasedUntil，下一个调 ClaimTask 的 worker 看到 lease 已
失效，就允许 mint 新 token、覆写 owner。原 owner 的 token 仍留在
磁盘那条记录的「上一次 Write」里——但下一次 atomic-write 会把它
直接顶掉。死 worker 即使奇迹复活拿原 token 调 TransitionTask，
也会因 token 不再匹配而被 ErrTokenMismatch 挡下。

## 与 s08 的变化

s08 是**字符串 → Decision 推荐器 + 一个会真去 fork 的 Executor**——
两层 API，但全是单进程内的事，没有跨进程协调。s09 引入根本不同的
东西：**文件系统作为协调原语**。第一次出现真正的并发原语
（`flock`），第一次出现真正的崩溃安全 I/O（`os.Rename`），第一次
出现「optimistic concurrency 用 token 取代 owner 字段」的状态机。

| 关注点 | s08 | s09 |
|---|---|---|
| 一等公民数据 | `Decision`、`Handle` | `Task`、`Claim`、`LeaseDuration` |
| 协调原语 | （无——单进程内） | `flock` 互斥锁 + `os.Rename` 原子写 + crypto/rand token |
| 崩溃模型 | （未触及——`Setpgid` 防的是孤儿） | reader 见旧或新、绝不见半截；token 阻止冒名顶替 |
| 副作用范围 | 一次 fork、一个进程的生命期 | 一组 JSON 文件 + `.lock` + `.tmp` 兄弟，跨进程持久 |
| 外部依赖 | 无（仅 stdlib） | `github.com/gofrs/flock`（一处） |

这也是章节系列第一次必须谈**租约语义**——「拿到不等于一直拿着，
LeasedUntil 之后任何继任者可以接手」。这套肌肉记忆会直接用于 s10
的 watchdog：那里每秒一次心跳，超过 60 秒没动静的 worker 被判死。

## 动手试一试

```bash
cd agents/s09-task-state-machine

GOWORK=off go vet ./...                # 应当无任何输出
GOWORK=off go build ./...              # 应当无任何输出
GOWORK=off go test -v -count=1 ./...   # 6 条测试通过，亚秒
GOWORK=off go run .                    # 输出与 testdata/expected.txt 一致
```

期望输出：

```
== Seed: pending task ==
team=demo id=fix-login status=pending version=1

== Worker A: ClaimTask ==
worker=worker-A token_len=32

== Worker B: ClaimTask (must fail) ==
worker=worker-B err=ErrLeaseStillValid (expected)

== Worker A: TransitionTask in_progress -> done ==
transition=ok

== Final task on disk ==
{
  "id": "fix-login",
  "status": "done",
  "version": 3,
  "description": "Fix the broken login flow in src/auth/login.go",
  "created_at": "0001-01-01T00:00:00Z",
  "updated_at": "0001-01-01T00:00:00Z"
}
```

最终态把 token 长度（不是值）打印出来，因为 token 是随机的，每次
跑都不一样；32 个 hex 字符就是 16 字节熵的 hex 编码——稳定可断言。
时间戳为了 fixture 可比对置零，上盘流程没变。

进一步练习：

- 加一个 `RenewClaim` 的端到端测试：worker A claim、Renew 之前
  把 LeasedUntil 拨到一分钟前、Renew 把它拉回未来；中间可以并发
  跑一个 worker B 的 ClaimTask 看会不会被打回 ErrLeaseStillValid。
- 把 `Task` 加一个 `DependsOn []string` 字段，仿上游
  `computeTaskReadiness`（tasks.ts L20-L37）做成一个 readiness
  检查；ClaimTask 在拿锁之后再判一次 readiness，避免依赖未就绪
  时被抢锁。

## 上游源码阅读

下面摘自 `src/team/state/tasks.ts` L1-L120（完整带注释版见
`upstream-readings/s09-tasks.ts`）：

```ts
// L43 —— withTaskClaimLock 的签名（实现在别处）
withTaskClaimLock: <T>(
  teamName, taskId, cwd, fn
) => Promise<{ ok: true; value: T } | { ok: false }>;

// L50-L99 —— claimTask，本章主菜
export async function claimTask(taskId, workerName, expectedVersion, deps) {
  const lock = await deps.withTaskClaimLock(deps.teamName, taskId, deps.cwd,
    async () => {
      const current = await deps.readTask(...);
      if (!current) return { ok: false, error: 'task_not_found' };
      const v = deps.normalizeTask(current);
      if (expectedVersion !== null && v.version !== expectedVersion)
        return { ok: false, error: 'claim_conflict' };
      if (deps.isTerminalTaskStatus(v.status))
        return { ok: false, error: 'already_terminal' };
      if (v.status === 'in_progress')
        return { ok: false, error: 'claim_conflict' };

      // L85-L92 —— mint token + 15 分钟租约 + 版本号自增
      const claimToken = randomUUID();
      const updated = { ...v, status: 'in_progress', owner: workerName,
        claim: { owner: workerName, token: claimToken,
                 leased_until: new Date(Date.now() + 15 * 60 * 1000).toISOString() },
        version: v.version + 1 };
      await deps.writeAtomic(deps.taskFilePath(...), JSON.stringify(updated, null, 2));
      return { ok: true, task: updated, claimToken };
    });
  if (!lock.ok) return { ok: false, error: 'claim_conflict' };
  return lock.value;
}
```

阅读笔记（与 Go 版的对照）：

1. **L43（`withTaskClaimLock` 注入）→ `withFileLock`。** 上游用依赖
   注入是因为同一个锁原语既会被 claimTask 用、又会被 transition、
   release、renew 用——四处共享一份实现。Go 版直接用顶层函数，
   因为 Go 不需要 mock 锁来写测试（`flock` 本身就是真东西，
   t.TempDir() 一开就用）。返回结构 `{ok, value}` 在 Go 这边
   退化成 `(value, err)`——锁拿不到直接返回 err，没有「拿到了
   但 fn 想表示失败」的中间态需要单独区分。
2. **L66-L67（lock 内重读 `current`）→ Read+Write 同一锁。** 这是
   关键的「双读」模式：lock 外的 `existing` 只是用来快速失败
   （task 不存在直接返回），真正决定状态的 read 必须在锁内做。
   Go 版只做一次 read（在锁内），不做锁外的快速失败，因为锁本身
   就极便宜（flock 是单次 syscall）；牺牲一微秒换一个分支
   的简化是值得的。
3. **L70-L71（version CAS）→ Task.Version。** Go 版保留了
   Version 字段、每次 Write 自增，但**目前没有把 expectedVersion
   暴露在 ClaimTask 的签名里**。理由是：claim+transition 的状态机
   单独靠 status equality + token 已经能闭合，version 是为后续
   exercise 准备的钩子（README 给了延伸题，让读者把 version
   接进 TransitionTask）。
4. **L77-L78（终态 + in_progress 拒绝）→ `Status == "done"||"failed"`、
   `t.Claim != nil && Now().Before(LeasedUntil)`。** Go 版把
   终态判断和「lease 还活着」分成两个独立 branch，因为前者返回
   ErrIllegalTransition、后者返回 ErrLeaseStillValid——两类失败
   的恢复策略截然不同（终态：放弃；lease 活着：稍后重试）。
   上游统一用 `claim_conflict` 概括，Go 这边把它拆细一点。
5. **L85（`randomUUID()`）→ `crypto/rand` + `hex.EncodeToString`。**
   16 字节随机就是 128 bit 熵，与 UUIDv4 同档位。我们刻意不引
   `github.com/google/uuid` 这条 dep——chapter 已经引了
   `gofrs/flock`，再加一个 dep 会模糊 chapter 的教学焦点
   （状态机纪律）。
6. **L90（`15 * 60 * 1000` 毫秒）→ `LeaseDuration = 15 * time.Minute`。**
   常量形式而非裸字面量，让 `TestSecondClaimSucceedsAfterLeaseExpires`
   的「过期模拟」不必跟魔数较劲。15 分钟覆盖 ~900 个心跳秒数，
   足够防误杀又能在咖啡时间内回收死任务。
7. **L94（`writeAtomic`）→ `atomic.go`。** 上游的实现就两行
   （write tmp + rename）。Go 版多了一个 MkdirAll，因为新建团队
   首次写入时父目录还不存在；rename 失败时多了一句 `os.Remove(tmp)`
   尽量打扫，但不掩盖原始 error。`TestWriteAtomicSurvivesPanicSimulation`
   就是用来证明这套两行原语能挡住「写一半就死掉」的 worker。
