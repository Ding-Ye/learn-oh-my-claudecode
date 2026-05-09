---
title: "s07 · 推石上山（继续执行强制）"
chapter: 7
slug: s07-continuation
est_read_min: 8
---

# 第 7 章 · 推石上山（继续执行强制）

> `learn-oh-my-claudecode` 第七章。从 s06 的「闭包注册表」转向一节
> 极小、无 schema 的章节：把**嵌入文本**与**正则驱动的语义分类**
> 拼到一起，对模型的输出做**质量分级**。第一次让 `//go:embed` 直接
> 产出 `string`（s02 用的是 `embed.FS`）；第一次让章节去检查模型
> 输出，**给完成度声明打分**而不是无条件相信它。

## 问题

模型常常在事情还没做完时就宣布完工。生产 trace 里反复出现三种失败
形态：

1. **过早完成声明。** 任务列表里还有三件 `pending`，模型却说「我已
   完成全部任务」。用户相信这一行；活儿其实只做了一半；会话在
   不一致中结束。
2. **带不确定性的完成声明。** 「我觉得这应该可以工作了。」这是一种
   **概率性**说法 —— 模型没跑过测试、没确认文件是否存在、没把行为
   端到端走一遍。它常常是错的，但用户拿不到一个简单的信号告诉自己
   「这个置信度其实不高」。
3. **缺少推动模型继续的人格压力。** 没有一段把「永远把巨石推上去」
   预先写进系统提示的人格附加文，模型会按它的默认训练在第一个
   看起来合理的停顿处礼貌收尾。

上游在 `src/features/continuation-enforcement.ts` L1–L196 里给出三个
表面：提醒池（L18–L24）、Sisyphus 系统提示附加文（L60–L130）、以及
正则分类器 `detectCompletionSignals`（L132–L170）。这一章把三者
端口过来，约 150 行纯标准库 Go。

## 解决方案

公开面共三个文件：

- `prompt_addition.md` —— Sisyphus 人格的纯 Markdown 文，通过
  `//go:embed prompt_addition.md` 作为 `var SystemPromptAddition string`
  烧进二进制。真实运行时会把它前置到 agent 的系统提示里。
- `reminders.json` —— 一个 JSON 字符串数组，作为 `var remindersData []byte`
  嵌入。`RandomReminder() string` 在首次调用时（在 `sync.Once` 里）
  解析一次，后续调用通过串行化的 `*rand.Rand` 取样，这样函数默认
  就是 goroutine-safe 的。
- `detect.go` —— 章节核心。两个 `[]*regexp.Regexp` 切片
  （`completionPatterns`、`uncertaintyPatterns`）在 init 时通过
  `MustCompile` 编译，再加一个 `DetectCompletion(response string) Signal`
  返回 `{Claimed bool; Confidence string; Reason string}`。三条分支：
  没有声明 → `Claimed=false`；声明但没含糊词 → `Confidence="high"`；
  声明且含糊 → `Confidence="low"`。

整个分类循环大概十行代码。

## 工作原理

### 把单个 Markdown 文件嵌成字符串

```go
//go:embed prompt_addition.md
var SystemPromptAddition string
```

这一行声明本身就是一个教学点。s02 用 `//go:embed agents/*.md` +
`embed.FS`，因为加载器要遍历文件名；这一章只有**一个**文件，要的
是它的内容作为值，所以 `string` 才是对的目标类型。Go 编译器在
**构建期**就解析这条路径 —— 把 `prompt_addition.md` 名字打错是
构建错误，不是运行期 nil。

### 正则切片把策略当数据存

```go
var completionPatterns = []*regexp.Regexp{
    regexp.MustCompile(`(?i)\bI(?:'ve| have) (completed|finished|implemented)\b`),
    regexp.MustCompile(`(?i)all (?:tasks?|work|items?) (?:are |is )?(?:now )?(?:complete|done|finished)`),
    // ...
}
var uncertaintyPatterns = []*regexp.Regexp{
    regexp.MustCompile(`(?i)\b(should|might|could|seems|appears|probably)\b`),
    regexp.MustCompile(`(?i)\b(I think|I believe|presumably)\b`),
    // ...
}
```

`DetectCompletion` 就是每个切片一遍 for 循环。没有 switch、没有
庞大的 if 树。要加一种新的「完成」表达或一种新的「含糊」标记，
只是一行 append。某个正则写错了，`MustCompile` 会在第一次 import
时 panic —— 这与 s06 在 `Tool.Handler` 错字上采取的姿态一致，
只是这里被用在了文本分类上。

### 含糊词降级，不否决

```go
sig := DetectCompletion("I think this should be working now.")
// sig.Claimed    == true
// sig.Confidence == "low"
// sig.Reason     == "Completion claimed with uncertainty language"
```

带含糊的声明**仍然是声明**。运行时可以拿这个 `low` 信号去注入一段
验证提醒，而不是直接拒收这条响应。这就是上游的设计选择
（L132–L170），我们一比一端口过来：**只分类、不拦截**。
如何处置这份判决，是运行时的事。

## 与 s06 的变化

s06 引入了「函数作为结构体字段」—— `Tool.Handler` 是塞进 map 的
闭包。s07 引入的是它的互补面：**`//go:embed` 直接产出 `string`
（对照 s02 的 `embed.FS`）、再加上正则驱动的语义分类对模型输出
打分**。这是章节系列第一次去检查文本的**含义**，而不是按名字派发
或匹配文件路径。

| 关注点 | s06 | s07 |
|---|---|---|
| 一等公民数据 | `Tool`（name + category + 闭包） | `Signal`（Claimed + Confidence + Reason） |
| Embed 形态 | （没有 —— 纯注册表） | 提示用 `string`，提醒池用 `[]byte` |
| 行为驱动 | 按 `name` 查表派发 | 按正则匹配分类 |
| 策略存储 | `WithDisabled` 视图集合 | `[]*regexp.Regexp` 切片 |
| 输出 | handler 返回 `(string, error)` | 给运行时消费的 `Signal` 判决 |

两条正则切片加一个三字段结构体就是整章 —— 故意做小、有意为之，
因为接下来的两章（s08、s09）会重新爬升到系统层关注点。

## 动手试一试

```bash
cd agents/s07-continuation

GOWORK=off go vet ./...                # 应当无任何输出
GOWORK=off go build ./...              # 应当无任何输出
GOWORK=off go test -v -count=1 ./...   # 5 条测试通过，亚秒
GOWORK=off go run .                    # 输出与 testdata/expected.txt 一致
```

期望输出：

```
== prompt addendum (first 5 lines, embedded via //go:embed) ==
## CONTINUATION ENFORCEMENT — THE BOULDER NEVER STOPS

### You are bound to your todo list

Like Sisyphus condemned to roll his boulder eternally, you are bound to

== DetectCompletion: three sample responses ==
[high confidence] response="I have completed all tasks."
  -> claimed=true confidence="high" reason="Clear completion claim detected"
[low confidence] response="I think this should be working now."
  -> claimed=true confidence="low" reason="Completion claimed with uncertainty language"
[no claim] response="Still investigating the bug."
  -> claimed=false confidence="" reason="No completion claim detected"

== one reminder (seeded for fixture stability) ==
[VERIFICATION GATE] Before claiming completion, run the tests and re-read the todo list. If anything is pending, continue.
```

三种分类把 `DetectCompletion` 的每条分支都覆盖到了。最后一次
`RandomReminder()` 调用用的是固定 seed，所以捕获的 fixture 是
可复现的 —— 真实运行时会用 `time.Now()` 起种。

进一步练习：

- 编辑 `reminders.json` 添加一行新的提醒。重新跑 `go run .` ——
  embed 会在下一次 `go build` 时把它带上，Go 端不用动一行。
- 与 s05 的钩子流水线组合：注册一个 `Stop` 钩子，对模型最近一条
  响应调 `DetectCompletion`，判决为 `Claimed=true, Confidence="low"`
  时调 `RandomReminder()` 投递。这一个组合就给出了上游「巨石永不
  停歇」的完整行为。

## 上游源码阅读

下面摘自 `src/features/continuation-enforcement.ts` L1–L196（完整带
注释版见 `upstream-readings/s07-continuation.ts`）：

```ts
// L17-L24 —— 提醒池（5 条逐级升压的字符串）
const CONTINUATION_REMINDERS = [
  '[SYSTEM REMINDER - TODO CONTINUATION] Incomplete tasks remain ...',
  '[TODO CONTINUATION ENFORCED] Your todo list has incomplete items. The boulder does not stop. ...',
  '[OMC REMINDER] You attempted to stop with incomplete work. ...',
  '[CONTINUATION REQUIRED] Incomplete tasks detected. You are BOUND to your todo list. ...',
  '[THE BOULDER NEVER STOPS] Your work is not done. ...'
];

// L132-L170 —— 完成度信号分类器
export function detectCompletionSignals(response: string) {
  const completionPatterns = [
    /all (?:tasks?|work|items?) (?:are |is )?(?:now )?(?:complete|done|finished)/i,
    /I(?:'ve| have) (?:completed|finished|done) (?:all|everything)/i,
    /everything (?:is|has been) (?:complete|done|finished)/i,
    /no (?:more|remaining|outstanding) (?:tasks?|work|items?)/i,
  ];
  const uncertaintyPatterns = [
    /(?:should|might|could) (?:be|have)/i,
    /I think|I believe|probably|maybe/i,
    /unless|except|but/i,
  ];
  // ... 三分支判决
}
```

阅读笔记（与 Go 版的对照）：

1. **L17–L24（提醒池）→ `reminders.json`。** 上游把池子写成 TS
   `const` 数组，靠打包器内联进编译产物。Go 版把同一组字符串放进
   JSON 文件并通过 `//go:embed` 嵌入，首次调用时在 `sync.Once` 里
   解析一次。形状相同；**可编辑性更好**：一个好奇的学生编辑
   `reminders.json` 然后跑 `go run .`，根本不用碰 Go 源码。
2. **L60–L130（系统提示附加文）→ `prompt_addition.md`。** 上游把
   Sisyphus 人格写成 TS 模板字符串
   （`export const continuationSystemPromptAddition = \`...\``）。
   Go 版通过 `//go:embed prompt_addition.md` 把 Markdown 嵌成
   `string`。我们用更面向**读源码的人**的散文重写了正文 —— 同样
   的四条神圣戒律、同样的检查清单、同样的誓言，但语气是为了让
   *读源码*的人理解，而不是给那个会内联收到它的模型看的。
3. **L132–L170（`detectCompletionSignals`）→ `DetectCompletion`。**
   形状一比一对齐：`{claimed, confidence, reason}` →
   `Signal{Claimed, Confidence, Reason}`。两条正则切片直接搬过来；
   按章节规格我们补了几条上游漏掉的常见简短表达
   （`\b(done|complete|finished|implemented)\b`、
   `\b(I think|I believe|presumably)\b`），让分类器对常见简短句式
   更敏感。
4. **L36–L67（`createContinuationHook`）—— 不端口。** 上游把分类器
   接到一个 `Stop` 钩子里，用 `hasIncompleteTasks = false` 占位，
   等待真实 todo state 接入。Go 版章节有意把这个面留给将来的合成器：
   s05 已经讲过钩子流水线，把两者拼起来是一页纸的练习，没必要在
   这章里再多写三个文件。
5. **L172–L196（`generateVerificationPrompt`）—— 不端口。** 一个
   小助手，让模型在收尾前自我验证。超出本章范围：`DetectCompletion`
   给出的**判决**才是运行时该消费的信号；验证 prompt 是它众多可能
   动作里的一个，并非唯一。
6. **`medium` 置信度档位虽然在 union 类型里，但谁都没真正发出过。**
   上游的联合类型是 `'high' | 'medium' | 'low'`，我们也保留三档；
   但两边的实现都从来不发 `medium`。这个槽留着，是为了将来某次
   细化（比如「I think」单独是 medium；「I think this might possibly」
   才是 low）能直接落地，不用动 `Signal` 的形状。
