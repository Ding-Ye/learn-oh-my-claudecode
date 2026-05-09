---
title: "s03 · 魔法关键词（提示词中间件）"
chapter: 3
slug: s03-magic-keywords
est_read_min: 8
---

# 第 3 章 · 魔法关键词（提示词中间件）

> `learn-oh-my-claudecode` 第三章。从 s02 的「文件 I/O + 安全门禁」翻转到
> **纯字符串变换器** —— 没有 I/O，没有 goroutine，也没有任何全局状态。
> 整个包就是一条把用户提示词在送到模型之前重写一遍的正则流水线。

## 问题

不是所有提示词都该原样送给模型。当用户输入 `ultrawork build a server`
时，开头的 `ultrawork` 是一个**信号**（「启用并行子智能体编排模式」），
而不是一个等模型字面阅读的名词。`search`、`analyze`、`ultrathink`
也一样。OMC 内置的「魔法关键词」中间件负责检测这些触发词、改写提示词
（通常是在前面追加一段指令块），然后把结果转交给 LLM。

但匹配逻辑要踩稳三个陷阱：

1. **代码块。** 用户在文档句子里把关键词放进 ``` ```ultrawork``` 是个
   开关 ``` 这种引用形式时，他**不是**在调用关键词。如果触发器在围栏或
   行内代码里也开火，就会平白产生一段指令让模型反向理解。
2. **询问式问句。** `"what is ultrawork?"`、`"ultrawork이 뭐야?"`、
   `"ultrawork とは何ですか"`、`"什么是 ultrawork"` —— 跨英文 / 韩文 /
   日文 / 中文，询问式问句一律不能触发关键词。用户在**问**这个特性，
   而不是在调用它。
3. **组合。** 一句话里同时命中多个关键词的情况（比如
   `ultrawork search OAuth flow` 同时命中 ultrawork 与 search），
   每一个关键词的改写结果必须可以被下一个关键词的检测器看到。
   这条流水线必须串起状态。

上游 `src/features/magic-keywords.ts` L1–L297 一并解决了这三件事。
本章就是那份文件的简化 Go 版，约 180 行。

## 解决方案

一个 `Keyword` 结构体三字段 —— `Triggers []string`、`Description
string`、`Action func(prompt, agentName, modelID string) string` —— 加一个
入口函数：

```go
func Process(prompt, agentName, modelID string, kws []Keyword) string
```

`Process` 按顺序遍历 keyword 切片。每一轮：剥代码块、判断是否落在
informational 上下文（多语言正则）、若任一触发词以全词形式出现于
清洗后的文本中，则调用该关键词的 `Action` —— 它的返回值成为下一轮的
prompt。每个 `Action` 都是一个把指令块拼接到（清洗后的）输入前面的闭包。

`regex.go` 提供两个正则辅助函数：

- `removeCodeBlocks(s)` 剥围栏 ```` ``` ```` 块（用 `(?s).*?` 做跨行惰性
  匹配）以及 `\`inline\`` 行内代码。
- `isInformationalIntent(s)` 当任一语种模式命中时返回 true：英文
  `\b(what|how|why)\b.*\?`、韩文 `이|뭐.*야`、日文 `何|なに`、中文
  `什么|怎么`。

四个内置 keyword 在 `keyword.go`（`UltraworkEnhancement`、
`SearchEnhancement`、`AnalyzeEnhancement`、`UltrathinkEnhancement`），
保留上游 L260–L266 的顺序。

## 工作原理

### 流水线总览

```
              用户提示词
                  │
                  ▼
      ┌───────────────────────┐
      │ for each Keyword in   │  ◀── 有序切片（BuiltIns）
      │   BuiltIns:           │
      │   1. removeCodeBlocks │
      │   2. isInformational? │  ◀── 短路（多语言）
      │   3. trigger 命中?    │  ◀── \b<trigger>\b 大小写不敏感
      │   4. 调用 Action      │  ◀── 前置指令块
      │   ┊  result := Action │
      └───────────────────────┘
                  │
                  ▼
            改写后的提示词
```

### 先剥后配

`removeCodeBlocks` 与触发词匹配的先后顺序是关键。如果先匹配再剥，
用户在文档语句里写 `` `ultrawork` `` 也会触发指令。代码块是**上下文标识符**
—— 它们里面的内容是被讨论，不是被调用。

```go
// regex.go —— 先剥围栏（围栏本身就含有反引号），再剥剩下的行内反引号。
// 顺序反过来的话，行内模式会把围栏分隔符当成行内代码吃掉。
func removeCodeBlocks(s string) string {
    s = codeBlockPattern.ReplaceAllString(s, "")
    s = inlineCodePattern.ReplaceAllString(s, "")
    return s
}
```

### 多语言询问式过滤

四条短的、按语种分的正则，OR 起来，比一条 30 路 alternation 的怪物
要好得多。Go 的 `regexp` 是 RE2，每次 match 都是 O(n)，不存在
catastrophic backtracking 的隐患。新增第五种语言（比如越南语）只需要
追加一行：

```go
var informationalIntentPatterns = []*regexp.Regexp{
    regexp.MustCompile(`(?i)\b(what|how|why)\b.*\?`),  // en
    regexp.MustCompile(`(?s)이|뭐.*야`),                  // ko
    regexp.MustCompile(`何|なに`),                        // ja
    regexp.MustCompile(`什么|怎么`),                       // zh
}
```

## 与 s02 的变化

s02 是文件 I/O + `embed.FS` + 安全正则；整个心智模型是「安全地把这堆
字节读出来」。s03 走到了反面：**完全没有 I/O**。没有文件系统、没有
goroutine、没有 context，连 error 返回都没有。每个函数都是
`string → string`。这是整个课程里最简单的机制，也是 s04 嵌套合并工作
之前一段刻意安排的喘息时间。

```diff
- // s02: loader.go —— 文件系统 + 哨兵错误
- type Loader struct{ fs embed.FS; root string }
- func (l *Loader) Load(name string) (string, error) { ... }

+ // s03: keyword.go —— 纯变换器，不会有失败路径
+ type Keyword struct {
+     Triggers    []string
+     Description string
+     Action      func(prompt, agentName, modelID string) string
+ }
+ func Process(prompt, agentName, modelID string, kws []Keyword) string
```

签名的变化讲完了一切：`(string, error)` 变成了 `string`。这里没有
失败模式 —— 一条没命中的正则不是错误，它就是个 no-op。这是值得感受
一下的姿态：不是每个 Go 函数都需要返回 error。

## 动手试一试

```bash
cd agents/s03-magic-keywords

GOWORK=off go vet ./...     # 应当无任何输出
GOWORK=off go build ./...   # 应当无任何输出
GOWORK=off go test -v -count=1 ./...   # 5 条以上测试通过
GOWORK=off go run .         # 输出与 testdata/expected.txt 完全一致
```

期望输出：

```
[imperative ultrawork]
  in : "ultrawork build a server"
  out: "[ULTRAWORK MODE — PARALLEL AGENT ORCHESTRATION]\nbuild a server"
[informational en  ]
  in : "what is ultrawork?"
  out: "what is ultrawork?"
[inside code block ]
  in : "```ultrawork``` is a keyword"
  out: "```ultrawork``` is a keyword"
[imperative search ]
  in : "search OAuth flow then refactor"
  out: "[SEARCH MODE — EXHAUSTIVE LOOKUP]\nsearch OAuth flow then refactor"
```

进一步练习：

- 新增第五个关键词 `DebugEnhancement`，触发词 `["debug", "trace"]`。
  确认 `"how do I debug this?"`（询问式）依然按原样穿透，而
  `"debug the auth flow"` 触发它。
- 把 `removeCodeBlocks` 改成处理 `[]byte` 而非 `string`，量一下省下来
  的 alloc 在意不在意。（剧透：一条普通的 8 KB 以下提示词，不在意。）

## 上游源码阅读

下面摘自 `src/features/magic-keywords.ts` L1–L297（精简注释版完整文件
见 `upstream-readings/s03-magic-keywords.ts`）：

```typescript
// L13–L22 —— 代码块剥离。Go 版的 `[\s\S]` 写成 `(?s).`
const CODE_BLOCK_PATTERN = /```[\s\S]*?```/g;
const INLINE_CODE_PATTERN = /`[^`]+`/g;
function removeCodeBlocks(text: string): string {
  return text.replace(CODE_BLOCK_PATTERN, '').replace(INLINE_CODE_PATTERN, '');
}

// L25–L30 —— 四语种询问式 intent 模式
const INFORMATIONAL_INTENT_PATTERNS: RegExp[] = [
  /\b(?:what(?:'s|\s+is)|what\s+are|how\s+(?:to|do\s+i)\s+use|explain|tell\s+me\s+about|describe)\b/i,
  /(?:뭐야|무엇(?:이야|인가요)?|어떻게|설명|사용법)/u,
  /(?:とは|って何|使い方|説明)/u,
  /(?:什么是|什麼是|怎(?:么|樣)用|如何使用|解释|说明)/u,
];

// L260–L266 —— 内置 keyword 切片
export const builtInMagicKeywords: MagicKeyword[] = [
  ultraworkEnhancement, searchEnhancement,
  analyzeEnhancement, ultrathinkEnhancement,
];
```

阅读笔记（与 Go 版的对照）：

1. **L13–L22（removeCodeBlocks）→ `regex.go::removeCodeBlocks`**。
   一对一翻译。先围栏后行内的顺序保留。`[\s\S]*?` 这个惰性 DOTALL
   习语换成 Go 的 `(?s).*?`。
2. **L25–L30（informational 模式）→ `regex.go`**。四种语言全保留；
   `(?:…)` 非捕获分组去掉了，因为 Go 的 RE2 直接 alternation 完全够用。
3. **L36–L62（每个匹配点 80 字符滑动窗口）→ 上提到 Process 作用域**。
   上游 `isInformationalKeywordContext` 围绕每一个触发词出现位置滑动
   一个 80 字符窗口、在窗口内跑模式。Go 版每轮 Process 只在清洗后的
   完整 prompt 上跑一次过滤 —— 更严，也更简单。
4. **L260–L266（内置切片）→ `keyword.go::BuiltIns`**。同四项、同顺序。
   Go 版的 Action 只前置一行指令而非上游的多段大块文本 —— 教学重点是
   流水线形态，不是任何一条指令的措辞。
5. **L202–L249（createMagicKeywordProcessor 闭包）→ `Process` 函数**。
   Go 直接遍历切片，所以工厂闭包退化成一个把 keyword 切片当参数的
   普通顶层函数。
