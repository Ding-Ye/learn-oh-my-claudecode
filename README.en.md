# learn-oh-my-claudecode

English · [中文](README.md)

> Build an OMC-style multi-agent orchestrator from scratch in Go, session by session — each chapter ends with the upstream TypeScript source.

This repo doesn't teach you to **use** [oh-my-claudecode](https://github.com/Yeachan-Heo/oh-my-claudecode) (OMC). It teaches you **how it grows from scratch**.

Each chapter adds one mechanism — agent registry, prompt loader, magic keywords, config loader, hooks pipeline, MCP tools, continuation enforcement, background task heuristics, task state machine, team watchdog — implemented as a small Go module. After ten chapters, OMC stops being black magic.

## Why Go

| Upstream (TypeScript) | This repo (Go) | Teaching gain |
|---|---|---|
| `tmux split-window` to spawn workers | goroutine + channel | Clear concurrency |
| `done.json` filesystem-signal polling | `time.Ticker` + `select` | Zero external IPC |
| esbuild `__AGENT_PROMPTS__` injection | `//go:embed` | One-line compile-time embed |
| JSONC + ajv | `encoding/json` + struct | Stdlib is enough |
| 1034 lines `runtime.ts` | ~250 Go lines | Same feature, shorter code |

## Curriculum

| # | Chapter | Status |
|---|---|---|
| s01 | [Agent Registry & Model Tiers](docs/en/s01-agent-registry.md) | ✅ |
| s02 | [Prompt Loader with embed.FS](docs/en/s02-prompt-loader.md) | ✅ |
| s03 | [Magic Keywords (Prompt Middleware)](docs/en/s03-magic-keywords.md) | ✅ |
| s04 | [Layered Config & deepMerge](docs/en/s04-config-loader.md) | ✅ |
| s05 | [Hooks Pipeline via os/exec](docs/en/s05-hooks-pipeline.md) | ✅ |
| s06 | [MCP Tool Registry with Categories](docs/en/s06-mcp-tool-server.md) | ✅ |
| s07 | [Continuation Enforcement (Sisyphus)](docs/en/s07-continuation.md) | ✅ |
| s08 | [Background Task Heuristics](docs/en/s08-background-tasks.md) | ✅ |
| s09 | [File-backed Task State Machine](docs/en/s09-task-state-machine.md) | ✅ |
| s10 | Team Runtime & Watchdog (Goroutine Pool) | ⏳ |
| s_full | End-to-end Integration | ⏳ |
| A | Appendix A · Mental Models | ⏳ |
| B | Appendix B · Upstream Source Map | ⏳ |

## Quickstart

```bash
# Run the s01 demo
cd agents/s01-agent-registry
go run .

# Run s01 tests
go test -v ./...

# View the docs online
cd web && npm install && npm run dev
# → http://localhost:3000/en
```

## Repo layout

```
learn-oh-my-claudecode/
├── agents/              # Each chapter is its own Go module (NOT cross-imported)
│   └── s01-agent-registry/
├── docs/                # Bilingual chapter docs (six-section spine)
│   ├── zh/
│   └── en/
├── upstream-readings/   # Annotated upstream source excerpts
├── web/                 # Next.js doc site
└── go.work              # Go workspace (one `use` per chapter)
```

Each chapter module is self-contained: clone, `cd agents/sNN-*`, `go run .` and you're off. **No cross-chapter imports** — fork any chapter for isolated study.

## Acknowledgements

- Upstream: [Yeachan-Heo/oh-my-claudecode](https://github.com/Yeachan-Heo/oh-my-claudecode) (MIT) — every mechanism has a real reference.
- Pedagogy: borrowed from [shareAI-lab/learn-claude-code](https://github.com/shareAI-lab/learn-claude-code) — the six-section "mental model → ASCII diagram → 30-60 lines of core code → diff → try it → upstream source reading" spine.

## License

MIT — see [LICENSE](LICENSE).
