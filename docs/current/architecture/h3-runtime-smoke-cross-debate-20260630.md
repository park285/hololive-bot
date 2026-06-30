# H3 Runtime Smoke Cross-Debate Review - 2026-06-30

## Scope

This review records the 5-agent `$cross-debate` result for commit
`81927f9dd25f40b6ce9180e9f386d83d1b0f729e`
(`fix(smoke): probe runtime health over h3`).

The debated proposition was:

```text
origin/main commit 81927f9d resolves the hololive-bot H3-only runtime smoke
failure at root cause level, aligns the related smoke scripts, H3 contract
checks, and current operational docs, and is sufficiently verified so no
further code/doc/test/ops work is required before calling this task complete.
```

## Decision

Panel verdict: **disagree for the broad proposition**.

Narrow conclusion:

- The central runtime smoke failure is fixed and freshly verified.
- The broader "no further work required" claim is false.
- Follow-up script, contract, and documentation work remains before the H3
  runtime smoke and operations surface can be called fully closed.

## Verified Positive Evidence

The parent session verified the central smoke path after `81927f9d`:

```bash
./scripts/smoke/smoke-compose-config.sh
./scripts/smoke/smoke-runtime-health.sh
./scripts/deploy/test-compose-h3-contract.sh
bash -n scripts/smoke/smoke-compose-config.sh scripts/smoke/smoke-runtime-health.sh scripts/deploy/test-compose-h3-contract.sh
git diff --check
./scripts/architecture/check-runbook-coverage.sh
./scripts/architecture/check-doc-links-no-local-paths.sh
./scripts/architecture/check-current-docs-no-historical.sh
./scripts/architecture/check-current-docs-no-historical-body.sh
```

Observed result:

- `smoke-compose-config.sh` passed.
- `smoke-runtime-health.sh` passed for `bot`, `admin-api`, `alarm-worker`,
  `alarm-worker-ready`, `llm-scheduler`, and `youtube-producer-c`.
- `test-compose-h3-contract.sh` passed and confirmed the central runtime smoke
  probes are H3-only.
- `HEAD == origin/main == 81927f9d` during the parent verification.

The central smoke script now checks runtime endpoints through container
`./bin/healthcheck` against `https://127.0.0.1:*` instead of host-side
HTTP/1.1 `curl`.

## Blocking Residual Findings

### 1. AP rollback still uses HTTP health probing

`scripts/deploy/ap-rollback.sh` still verifies AP rollback health with:

```bash
curl -fsS "http://127.0.0.1:$port/health"
```

For H3-only AP runtimes this can false-fail rollback verification even when the
runtime is healthy.

Required follow-up:

- Replace the AP rollback HTTP probe with the AP runtime `./bin/healthcheck`
  H3 path.
- Preserve the existing post-rollback checks for container health, `StartedAt`,
  and error marker absence.

### 2. H3 contract guard does not cover rollback

`scripts/deploy/test-compose-h3-contract.sh` checks:

- `scripts/deploy/ap-deploy.sh`
- `scripts/deploy/ap-completion-check.sh`
- `scripts/logs/ap-smoke.sh`

It does not include `scripts/deploy/ap-rollback.sh`, so the guard can pass while
rollback verification still contains an HTTP probe.

Required follow-up:

- Add `scripts/deploy/ap-rollback.sh` to the H3-only script scan.
- Add an explicit positive assertion for the rollback H3 healthcheck command if
  the rollback script uses a non-trivial command shape.

### 3. Docker Compose deployment guide still contains stale operations

`docs/runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md` still contains
stale operational instructions:

- retired split runtime names such as `hololive-bot`,
  `hololive-admin-api`, and `llm-scheduler` in service lists and deploy/log
  examples;
- an HTTP health example for `<tailnet-central>:30001`;
- an HTTP Iris webhook URL example for `:30001`.

The webhook URL may require a separate Iris ingress contract decision, but the
retired service names and HTTP health example are direct blockers for claiming
the deployment guide is aligned with the current H3/runtime contract.

Required follow-up:

- Replace retired split service examples with the current `hololive-api`
  service and plane terminology.
- Convert stale health checks to container `./bin/healthcheck` H3 commands or
  explicitly document a different intentional transport contract.
- Decide whether the Iris webhook URL is intentionally HTTP at the external
  boundary or should be documented as H3/Tailscale ingress, then update the
  guide accordingly.

## Minimum Closure Checklist

Before marking the broader H3 smoke/ops alignment as complete:

- [ ] Update `scripts/deploy/ap-rollback.sh` to use H3 healthcheck for AP
      rollback verification.
- [ ] Extend `scripts/deploy/test-compose-h3-contract.sh` so rollback health
      verification cannot regress to HTTP.
- [ ] Update `docs/runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md` for
      current `hololive-api` service naming and H3 health verification.
- [ ] Re-run:

```bash
bash -n scripts/deploy/ap-rollback.sh scripts/deploy/test-compose-h3-contract.sh
./scripts/deploy/test-compose-h3-contract.sh
./scripts/smoke/smoke-compose-config.sh
./scripts/smoke/smoke-runtime-health.sh
./scripts/architecture/check-runbook-coverage.sh
./scripts/architecture/check-doc-links-no-local-paths.sh
git diff --check
```

For any live AP rollback execution, keep the normal approval gate: do not run
rollback, recreate, deploy, restart, or destructive remote operations without
explicit scoped approval.

## Panel Summary

All five reviewers converged after rebuttal:

- The live central smoke uncertainty was resolved by fresh parent evidence.
- The central `smoke-runtime-health.sh` fix is correct.
- The overall proposition remains false because AP rollback and deployment
  guide surfaces are still out of sync with the H3-only runtime contract.

