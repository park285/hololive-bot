# LLM Worker Prompt

아래 prompt를 새 LLM 작업자에게 그대로 전달하세요. `<PHASE_FILE>`만 실제 phase 파일 경로로 바꾸세요.

```text
You are working in the `hololive-bot` repository.

Read these files first, in order:
1. docs/history/plan-kits/youtube-producer-active-active-20260520/READ.md
2. docs/history/plan-kits/youtube-producer-active-active-20260520/phases/00_INDEX.md
3. <PHASE_FILE>

Work on exactly one phase. Do not implement later phases early.

Repository-specific constraints:
- Current runtime is `youtube-producer`, not retired `youtube-scraper`.
- Do not add `YOUTUBE_SCRAPER_*` env names.
- Do not revive `hololive-stream-ingester` YouTube runtime paths.
- Do not add `channel_id` to Prometheus labels.
- Do not disable JobRunGuard to recover active-active traffic.
- Do not run live deploy, restart, rollback, or secret writes without explicit operator approval.
- Do not read or use secrets, API keys, auth tokens, or authenticated metrics credentials without explicit operator approval. If `/metrics` returns 401 or requires auth, record that as a blocker.
- Preserve unrelated user changes.

Before editing:
- inspect the phase file's Scope, Allowed changes, Stop rules, and Verification sections.
- run the smallest read-only commands needed to ground the task.

After editing or checking:
- run the verification command listed in the phase if feasible.
- if verification cannot run, state exactly why and name the next best check.
- record evidence using docs/history/plan-kits/youtube-producer-active-active-20260520/appendix/evidence-template.md.

Final response format:
- outcome first
- changed files
- verification commands and results
- blockers or residual risk
```

## PR/Commit Title Examples

Use only if the user asks for commit/PR work.

- `youtube-producer: document active-active readiness gates`
- `youtube-producer: tighten active-active Valkey readiness`
- `youtube-producer: document photo sync failover policy`
- `youtube poller: add active-active scheduler regression`
- `youtube-producer: document lease TTL and claim metrics`
