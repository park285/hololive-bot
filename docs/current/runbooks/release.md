# Runbook: release

## Role

Runtime, document, and contract changes의 release checklist입니다.

## Compose Service Redeploy

Use the repository deploy script for service-level redeploys:

```bash
./scripts/deploy/compose-redeploy-service.sh <service>
```

On the current main production host, include the live-compat overlay because
`/run/hololive-bot/env` preserves the live Postgres and certificate contract:

```bash
sudo -n env COMPOSE_FILE=deploy/compose/docker-compose.prod.yml:deploy/compose/docker-compose.live-compat.yml \
  COMPOSE_ENV_FILE=/run/hololive-bot/env \
  ./scripts/deploy/compose-redeploy-service.sh <service>
```

For the main-host active-active AP, include both main-ap overlays:

```bash
sudo -n env \
  COMPOSE_FILE=deploy/compose/docker-compose.prod.yml:deploy/compose/docker-compose.live-compat.yml:deploy/compose/docker-compose.main-ap.yml:deploy/compose/docker-compose.main-ap.live-compat.yml \
  COMPOSE_PROFILES=main-ap \
  COMPOSE_ENV_FILE=/run/hololive-bot/env \
  ./scripts/deploy/compose-redeploy-service.sh youtube-producer-c
```

> 배포 스크립트는 holo-postgres가 host network(live-compat 토폴로지)로 실행 중인데
> `COMPOSE_FILE`에 live-compat overlay가 없으면 fail-closed로 거부한다. overlay 없이
> 배포하면 holo-postgres가 bridge network로 재생성되어 host:5433 소비자(AP
> youtube-producer 등)의 DB 연결이 끊기기 때문이다. 의도적 토폴로지 변경 시에만
> `ALLOW_POSTGRES_TOPOLOGY_CHANGE=true`를 설정한다.

Current Go runtime services:

- `hololive-bot`
- `hololive-admin-api`
- `hololive-alarm-worker`
- `llm-scheduler`
- `youtube-producer-c` on the main host; `youtube-producer-a/b` use the AP host wrappers.

## Required Checks

```bash
./scripts/architecture/ci-boundary-gate.sh
go test . -run TestRuntimeSplitStandaloneModulesContract
```

For contract/document changes:

```bash
./scripts/architecture/check-current-docs-no-historical.sh
./scripts/architecture/check-current-docs-no-historical-body.sh
./scripts/architecture/check-doc-links-no-local-paths.sh
./scripts/architecture/check-runbook-coverage.sh
./scripts/architecture/check-contract-map.sh
./scripts/architecture/check-internal-route-hardcoding.sh
./scripts/architecture/check-error-contracts.sh
./scripts/architecture/check-release-governance-assets.sh
```

## Contract Change Release Rules

- Keep provider and consumer compatible during rollout.
- Use dual-read/dual-write or additive fields for queue/envelope changes.
- Update `CONTRACT_MAP.md`, matching `contracts/*.md`, and runbook impacts before release.
- Include rollback notes for old and new contract versions.

## Release Notes

Use:

- `docs/runbook_execution/RELEASE_NOTES_TEMPLATE_20260303.md`

## Smoke Tests

These scripts do not rebuild, redeploy, or recreate Docker Compose services. `smoke-runtime-health.sh` expects local services to already be running.

```bash
./scripts/smoke/smoke-compose-config.sh
./scripts/smoke/smoke-runtime-health.sh
```

Equivalent manual checks:

```bash
curl http://127.0.0.1:30001/health
curl http://127.0.0.1:30006/health
curl http://127.0.0.1:30007/health
curl http://127.0.0.1:30003/health
curl http://127.0.0.1:30025/health
```

## Related documents

- `../DEPLOYMENT_BASELINE.md`
- `rollback.md`
- `../../runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md`
