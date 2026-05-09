---
title: "s02 · 提示词加载器（embed.FS）"
chapter: 2
slug: s02-prompt-loader
est_read_min: 9
---

# 第 2 章 · 提示词加载器（embed.FS）

> `learn-oh-my-claudecode` 第二章。从 s01 的「纯数据」推进到「数据 + 正则 +
> 安全门禁」：本章首次引入 Go 的 `//go:embed`（编译期把目录打包进二进制）、
> 标准库 `regexp`、以及可被 `errors.Is` 匹配的哨兵错误（sentinel error）。

## 问题

OMC 的 19 个角色（`architect`、`executor`、`explore` 等）每一个的 system
prompt 都是一份独立的 Markdown 文件，文件开头有一段 YAML frontmatter
描述元数据（`name`、`description`、`model`、`level`、`disallowedTools`），
第二个 `---` 之下才是真正喂给模型的正文。

每一次加载需要做三件事：

1. **把文件嵌进二进制** —— 生产环境不能依赖宿主机上一定存在
   `agents/architect.md`。
2. **校验 agent 名字** —— `loader.Load("../../etc/passwd")` 必须在它有机会
   变成真实路径之前就被拒绝。路径穿越是文件加载器最经典的 CVE，本章不打算
   自己手写转义逻辑。
3. **剥掉 frontmatter** —— 模型只关心正文。开头那段 YAML 信封必须在返回
   字符串前剥掉。

上游 `src/agents/utils.ts` L83–L131（`loadAgentPrompt`）解决的是同样
三个问题，但它必须维护两套代码路径：一套面向 esbuild 打出的 CJS bundle
（提示词被内联进 `__AGENT_PROMPTS__` 全局对象）、另一套面向 `node`
直接跑源码的开发模式（`readFileSync` 直接读盘）。本章一口气把这 30 多行
冗余删掉。

## 解决方案

Go 的 `//go:embed` 指令在编译期把任何目录树嵌进二进制，运行时通过
`embed.FS` 暴露，无论宿主机上是否还存在原文件，读法完全一致。再配合：

- `regexp.MustCompile(\`^[a-z0-9-]+$\`)` 校验名字；
- `regexp.MustCompile(\`^---\n(?s).*?\n---\n\s*\`)` 剥 frontmatter，

整个加载器只需 60 行。一条代码路径，无编译期分叉，无运行/测试态分裂。
错误以两个导出哨兵的形式回传：`ErrInvalidName`（调用方很可能在试探安全
边界）与 `ErrAgentNotFound`（只是手抖打错）。这两类一定要分开 ——
`errors.Is` 让上游能据此分流审计信号。

## 工作原理

### 总览图

```
       编译期                                       运行期
  ┌────────────────────┐                  ┌──────────────────────────┐
  │  //go:embed agents │                  │   loader.Load("name")    │
  │     ──> embed.FS   │ ───── 绑定 ────▶ │      │                   │
  │                    │                  │      ▼                   │
  │  agents/           │                  │  validateName?           │
  │   architect.md     │                  │   拒绝 ⇒ ErrInvalidName  │
  │   executor.md      │                  │      │ 通过              │
  │   explore.md       │                  │      ▼                   │
  └────────────────────┘                  │  fs.ReadFile             │
                                          │   miss ⇒ ErrAgentNotFound│
                                          │      │ ok                │
                                          │      ▼                   │
                                          │  frontmatterPattern.     │
                                          │   ReplaceAllString       │
                                          │      │                   │
                                          │      ▼                   │
                                          │  body string             │
                                          └──────────────────────────┘
```

### 核心代码

```go
// loader.go（30 行核心）
var ErrInvalidName   = errors.New("invalid agent name (must match ^[a-z0-9-]+$)")
var ErrAgentNotFound = errors.New("agent prompt not found")

var frontmatterPattern = regexp.MustCompile(`^---\n(?s).*?\n---\n\s*`)

type Loader struct{ fs embed.FS; root string }

func New(fs embed.FS, root string) *Loader { return &Loader{fs: fs, root: root} }

func (l *Loader) Load(name string) (string, error) {
    if err := validateName(name); err != nil { return "", err }   // 1
    relPath := l.root + "/" + name + ".md"                         // 2
    data, err := l.fs.ReadFile(relPath)                            // 3
    if err != nil {
        if errors.Is(err, fs.ErrNotExist) { return "", ErrAgentNotFound }
        return "", err
    }
    return frontmatterPattern.ReplaceAllString(string(data), ""), nil  // 4
}

// validate.go
var validNamePattern = regexp.MustCompile(`^[a-z0-9-]+$`)
func validateName(name string) error {
    if !validNamePattern.MatchString(name) { return ErrInvalidName }
    return nil
}
```

### 三个不那么显然的点

1. **为什么用 `+` 拼接路径，而不是 `filepath.Join`？** 因为 `filepath.Join`
   会自动「清理」`..` 段——这恰好是我们最不希望的行为。如果 `validateName`
   将来出现回归、放过了 `..`，Join 会悄悄把路径穿越「合法化」。
   纯字符串拼接保证了运行时路径严格等于「正则放行的那一串」。这是纵深防御。

2. **为什么用两个哨兵错误而不是一个泛型的 `ErrLoad`？** 用户输入
   `loader.Load("Foo Bar")` 是手抖，输入 `loader.Load("../../etc/passwd")`
   是在试探安全边界。两种情况都会返回 error，但宿主程序的审计日志显然要
   把它们分开记录。两个导出哨兵让 `errors.Is(err, ErrInvalidName)` 成为
   「写入安全审计通道」的标准触发器，而 `ErrAgentNotFound` 只进普通日志。

3. **为什么正则在开始的 `---` 后强制要求一个 `\n`？** 模式是
   `^---\n(?s).*?\n---\n\s*`，而不是 `^---(?s).*?---\s*`。这个换行符的
   存在，意味着「首行恰好等于 `---` 加换行」才会被识别为 frontmatter；
   首行如果是 `---some-comment` 这种「以 `---` 开头但还跟着别的字符」的
   合法 Markdown 内容，就不会被误判。没有这个锚点，一份首行是水平分割线
   的纯 Markdown 文档可能被悄悄截断。

## 与 s01 的变化

s01 是纯数据：一个 `map[string]Agent` + 几个工具函数 + 零 I/O。s02 第一次
引入三种 Go 能力：基于 `embed.FS` 的文件加载、正则表达式、可被 `errors.Is`
匹配的哨兵错误。

```diff
  // s01: agent.go —— 纯数据，没有失败路径
  type Agent struct {
      Name, Description, Prompt string
      Tools                     []string
      Model, DefaultModel       string
  }

+ // s02: loader.go —— 文件系统 + 正则 + 哨兵错误
+ var ErrInvalidName   = errors.New("invalid agent name (must match ^[a-z0-9-]+$)")
+ var ErrAgentNotFound = errors.New("agent prompt not found")
+ var frontmatterPattern = regexp.MustCompile(`^---\n(?s).*?\n---\n\s*`)
+
+ type Loader struct {
+     fs   embed.FS
+     root string
+ }
+
+ func (l *Loader) Load(name string) (string, error) {
+     if err := validateName(name); err != nil { return "", err }
+     data, err := l.fs.ReadFile(l.root + "/" + name + ".md")
+     // ... ErrAgentNotFound 转换、frontmatter 剥离 ...
+ }
```

读完本章，你应当掌握三件事：把 Markdown 资源打进 Go 二进制、写一个安全
意识在线的文件加载器、用哨兵错误让调用方有据可分流。

## 动手试一试

```bash
cd agents/s02-prompt-loader

go vet ./...     # 应当无任何输出
go build ./...   # 应当无任何输出
go test -v ./... # 5 个测试通过（其中一个对 6 种非法名字做子测试）
go run .         # 输出与 testdata/expected.txt 完全一致
```

期望输出：

```
architect prompt (759 bytes), first line: "# Architect"
path traversal rejected: true
unknown agent surfaced as: true
embedded agents: [architect executor explore]
```

进一步练习：

- 新增 `agents/qa-tester.md`（带 frontmatter）后重新运行。`embedded
  agents:` 那一行会无需改动任何 Go 代码就多出一项——这正是 `embed.FS`
  的工作。
- 把 `validNamePattern` 放宽到 `^[a-zA-Z0-9-]+$`（接受大写），跑测试。
  `TestLoadRejectsInvalidCharacters/AGENT` 会失败——这能让你确认测试钉死的
  是策略本身，而不只是当前的正则。

## 上游源码阅读

下面摘自 `src/agents/utils.ts` L83–L131（精简注释版完整文件见
`upstream-readings/s02-utils.ts`）：

```typescript
// L74-L78 —— frontmatter 正则
function stripFrontmatter(content: string): string {
  const match = content.match(/^---[\s\S]*?---\s*([\s\S]*)$/);
  return match ? match[1].trim() : content.trim();
}

// L86-L131 —— 加载器
export function loadAgentPrompt(agentName: string): string {
  // ⭐ 安全门禁 #1
  if (!/^[a-z0-9-]+$/i.test(agentName)) {
    throw new Error(`Invalid agent name: contains disallowed characters`);
  }

  // ⚠ 编译期快速路径 —— esbuild 把 __AGENT_PROMPTS__ 替换为字面量
  try {
    if (typeof __AGENT_PROMPTS__ !== 'undefined' && __AGENT_PROMPTS__ !== null) {
      const prompt = __AGENT_PROMPTS__[agentName];
      if (prompt) return prompt;
    }
  } catch {}

  // 运行期回退 —— readFileSync + 路径事后校验
  try {
    const agentsDir = join(getPackageDir(), 'agents');
    const agentPath = join(agentsDir, `${agentName}.md`);
    // ⭐ 安全门禁 #2 —— 路径穿越事后校验
    const rel = relative(resolve(agentsDir), resolve(agentPath));
    if (rel.startsWith('..') || isAbsolute(rel)) {
      throw new Error(`Invalid agent name: path traversal detected`);
    }
    return stripFrontmatter(readFileSync(agentPath, 'utf-8'));
  } catch (error) {
    // ⚠ 假成功兜底（Go 版改用 ErrAgentNotFound 哨兵）
    return `Agent: ${agentName}\n\nPrompt unavailable.`;
  }
}
```

阅读笔记（与 Go 版的对照）：

1. **L91（`^[a-z0-9-]+$` 校验）→ Go 的 `validNamePattern`**。一对一翻译；
   Go 版去掉了 `/i` flag，因为所有嵌入的 fixture 都是小写——大写名字
   只会以让人迷惑的 `ErrAgentNotFound` 形式失败，倒不如在源头响亮地拒绝。
2. **L96-L104（`__AGENT_PROMPTS__` 分支）→ Go 版完全没有**。`//go:embed`
   让「编译期 vs 运行期」这件事彻底消失。
3. **L116-L122（路径穿越事后校验）→ Go 版省略**。三个理由：(a) 正则
   已经禁掉了 `..` 与 `/`；(b) 我们用字符串 `+` 拼接而不是
   `filepath.Join`（后者会清理路径，反而可能让 `..` 死灰复燃）；(c)
   `embed.FS` 自身就拒绝越级访问。
4. **L124-L130（假成功 placeholder）→ Go 版返回 `(string, error)`**。
   返回 placeholder 字符串会掩盖「prompt 丢了」这种本应抛出的 bug；Go
   的双返回值强迫调用方面对它。
5. **L46-L49（`[\s\S]*?` 惰性 DOTALL）→ Go 版的 `(?s).*?`**。语义
   完全等价——Go 的 `regexp` 引擎用 `(?s)` 标志开启 DOTALL，而不是借助
   `[\s\S]` 这个字符类技巧。
