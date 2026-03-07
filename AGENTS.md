# AGENTS.md

## Purpose

Repository-specific defaults for the `hololive-bot` monorepo. Keep cross-project behavior in `~/.codex/AGENTS.md`; keep this file focused on repo constraints and workflow.

## Scope

- Project map: `docs/PROJECT_MAP.md`
- Deeper `AGENTS.md` files override this file for their subtrees.
- If a subtree has a `CONVENTIONS.md`, follow it before changing code there.

<instruction_priority>

- Order:
  1. system, safety, and tool constraints
  2. explicit user request
  3. deeper subtree `AGENTS.md`
  4. subtree docs, tests, configs, `CONVENTIONS.md`, and local conventions
  5. this file
  6. inferred repository patterns
- Preserve non-conflicting earlier instructions.
- Safety, honesty, privacy, and permission constraints never yield.

</instruction_priority>

<repo_output_contract>

- Respond in Korean unless the user explicitly requests another language.
- Put the main result first unless the user requested a different format.
- Keep prose compact, specific, and information-dense.
- Separate facts, assumptions, risks, blockers, and verification status when they materially affect decisions.

</repo_output_contract>

## Repo rules

- Inspect the nearest `AGENTS.md`, relevant source files, tests, configs, and docs before implementation.
- Prefer the smallest reviewable change that fixes the root cause and is easy to verify and roll back.
- Avoid new dependencies, tools, or workflows unless already used or clearly justified.
- Preserve public contracts unless the task explicitly requires changing them.
- Never claim inspection, execution, testing, or verification that did not happen.
- Increase verification rigor for production, migrations, security, or schema changes.

## Repository Rules

| Rule | Spec |
|------|------|
| Error Wrapping (Go) | `fmt.Errorf("action: context: %w", err)` |
| Error Wrapping (Rust) | `thiserror` for domain; `anyhow::Context` at app boundary |
| Context (Go) | Pass `context.Context` as the first arg; NEVER `context.Background()` in handlers |
| Cancellation (Rust) | Pass `CancellationToken`; no fire-and-forget spawns |
| Logging | Go: `slog` only, Rust: `tracing` only; no `fmt`/`println` |
| Sensitive data | Mask before logging; Rust: `SecretString` for tokens/passwords |
| Error format | `"action: context: cause"` |
| Health contract | `/health` (liveness), `/ready` (readiness) |

## Prohibited Patterns

| Pattern | Use Instead |
|---------|-------------|
| Runtime panics | Proper error handling |
| Hardcoded constants, keys, or TTLs | Constants package or config |
| Unstructured logging (`fmt.Println`, `println!`) | `slog` or `tracing` |
| Secrets in code or logs | ENV vars and masking |

## Skills

- Use the `pr` skill for PR requests.
- Use the `backend-guide` skill for backend work.
- Use the `session-handoff` skill when the user asks to persist progress.

## Repo Commands

```bash
./build-all.sh --no-bump
./scripts/deploy/compose-redeploy-service.sh llm-scheduler
# NEVER: kill, nohup, direct go run/cargo run in production
```
