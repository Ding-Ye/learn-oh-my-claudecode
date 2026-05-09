# Quality report: learn-oh-my-claudecode

Generated: 2026-05-09T12:09:07Z
Repo: https://github.com/Ding-Ye/learn-oh-my-claudecode
Total commits: 11
CI status (last 5 runs on main): docs ✅ · web ❌ · web ❌ · docs ✅ · go ✅
  → 2 of last 5 runs are `web` failures (recurring, every push), all `go` and `docs` runs pass.

## Summary

- P0 issues: 1
- P1 issues: 0
- P2 issues: 0

## P0 issues (must fix)

### P0.6.1 — Web CI workflow has been failing on every push since the start

**Detail.** `.github/workflows/web.yml` references `web/package-lock.json` for `actions/setup-node@v4`'s npm cache and then runs `npm ci` (which strictly requires a lockfile). The repo does **not** contain `web/package-lock.json` — only `web/package.json`. CI therefore fails at the setup-node step with:

```
##[error]Some specified paths were not resolved, unable to cache dependencies.
```

…and would fail again at `npm ci` even if the cache step were tolerant. Confirmed against runs `25600742639`, `25600624331`, `25600259011`, `25600017704` — all on `main`, all `web` workflow, all `failure`.

The local web build itself is healthy (`npm install` + `npm run typecheck` + `npm run build` all pass; 32 static pages prerender cleanly). The failure is a CI-only packaging gap, not a code defect — but it blocks the "all green" claim.

**Files involved.**
- `/Users/yeding/learn-oh-my-claudecode/.github/workflows/web.yml` (lines 28-32 — `cache-dependency-path` + `npm ci`)
- `/Users/yeding/learn-oh-my-claudecode/web/package.json` (no lockfile sibling)

**Suggested fix.** Run `npm install` inside `web/` and commit the resulting `web/package-lock.json`. (The repo's `.gitignore` does not exclude lockfiles, so this is just a missed-commit issue.) Alternatively, change the workflow to drop the cache-dependency-path and use `npm install` instead of `npm ci`, but committing the lockfile is the standard fix and unblocks reproducible builds.

---

All other P0 checks pass:

- **P0.1 Bilingual heading parity** — every `docs/zh/*.md` and `docs/en/*.md` pair has identical `^##` count (13 file pairs checked, zero mismatches).
- **P0.2 Six-section spine** — all 20 of `docs/{zh,en}/sNN-*.md` (s01–s10 × 2 locales) contain `Problem|问题`, `Solution|解决方案`, `How It Works|工作原理`, `Try It|动手试`, `Upstream Source|上游源码` headings.
- **P0.3 No cross-session imports** — `grep -r 'github.com/Ding-Ye/learn-oh-my-claudecode/agents/sNN' agents/<other>/` empty for every pair. Each chapter is its own self-contained Go module (confirmed in `go.work`'s 10 `use` directives).
- **P0.4 Upstream-readings present** — all 10 expected files exist (`s01-definitions.ts` through `s10-runtime.ts`, with `s05-hooks.json`). Spot-checked 2 cited upstream URLs via WebFetch:
  - `s01-definitions.ts` → https://raw.githubusercontent.com/Yeachan-Heo/oh-my-claudecode/main/src/agents/definitions.ts → 200, valid TypeScript.
  - `s10-runtime.ts` → https://raw.githubusercontent.com/Yeachan-Heo/oh-my-claudecode/main/src/team/runtime.ts → 200, valid TypeScript.
- **P0.5 Tests pass** — `go vet ./... && go build ./... && go test -count=1 -timeout=30s ./...` is **ok** in all 10 session modules. Slowest test is s05 at 1.65s; total wall-time across all 10 modules ~7s. No flakes observed in the single run sampled.

## P1 issues (should fix)

None. All P1 checks pass:

- **P1.1 Web build (local)** — `npm install` (184 packages, ~48s), `npm run typecheck` (clean tsc --noEmit), `npm run build` (Next 15 builds 32 routes, all `(SSG)` or `(Static)`).
- **P1.2 Curriculum.ts ↔ READMEs parity** — `web/lib/curriculum.ts` has exactly 13 entries (s01–s10 + s_full + appendix-a + appendix-b), every one `available: true`. `README.md` and `README.en.md` curriculum tables both list all 13 with ✅. Slugs match `docs/{zh,en}/*.md` filenames.
- **P1.3 README links resolve** — every relative `[...](path)` link in `README.md` and `README.en.md` resolves to an existing file (verified via Python parser handling parens-in-link-text correctly).
- **P1.4 Glossary terms in docs** — `Sisyphus` appears in 6 docs (s07, s_full, appendix-a, both locales); `Handoff` in 6 docs (s_full, appendix-a, appendix-b, both locales); `Watchdog`/`看门狗`/`守护` in 14+ docs across s01, s08, s09, s10, s_full, appendices; `Magic keyword`/`魔法关键词` in 4 docs (s03 + appendix-a, both locales).

## P2 issues (nice to have)

None. All P2 checks pass:

- **P2.1 testdata/expected.txt** — every one of the 10 session modules has `testdata/expected.txt`.
- **P2.2 Per-session README.md ≥ 40 lines** — all 10 session READMEs are at least 40 lines.

## Strengths

- **Strict module isolation works.** `go.work` lists all 10 chapters, every session compiles, vets, and tests cleanly in isolation, with zero cross-chapter imports — a learner can fork any single session and `go run .` without touching the rest of the repo.
- **Six-section spine is consistently enforced.** All 20 chapter docs (s01–s10 × zh/en) contain the full Problem / Solution / How It Works / What Changed / Try It / Upstream Source spine; bilingual heading counts are identical pair-by-pair, indicating the docs were translated structurally, not loosely.
- **Upstream traceability is real.** The 10 `upstream-readings/*` files all carry header comments with the exact upstream raw URL and line ranges (e.g. `runtime.ts L289-L390`), and spot-checked URLs return 200 with the cited content. The annotated `// ⚠ Anti-pattern #4` style commentary makes the contrast with the Go port pedagogically explicit.
- **The web doc viewer is healthy locally.** Next 15 prerenders 32 routes (1 root, 2 locale indexes, 26 chapter pages, 2 not-found, plus shared shell) with clean tsc and no telemetry calls — the only thing standing between local-green and CI-green is the missing lockfile.
- **Glossary discipline.** Distinctive OMC vocabulary (`Sisyphus`/`推石上山`, `Handoff`, `Watchdog`/`看门狗`, `Magic keyword`/`魔法关键词`) is used consistently and reappears in the right chapters plus the appendices, suggesting the appendices genuinely cross-reference the chapter material rather than being orphan glossary dumps.

## Recommendations

If shipping:

- **Address P0 before announcing.** Commit `web/package-lock.json` (one command: `cd web && npm install && git add package-lock.json`). This single change flips the badge for the recurring `web` CI failures and gives the repo a fully-green CI surface — important for a learning repo that learners will judge by the README badges.
- **P1 can be follow-up.** No P1 issues found.
- **P2 is exercise for the reader.** No P2 issues found.

A note on scope: the report focused on the parent agent's specified P0/P1/P2 checks and did not audit doc *content* quality, code idiomaticity, or the appropriateness of individual upstream-reading line ranges — those would require human review by an OMC subject-matter expert.
