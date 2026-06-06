# Runbook: rollback

## Role

Docker Compose runtime rollback과 contract/document rollback 판단 기준입니다.

## Before Rollback

- Identify the changed runtime, config, contract, or document gate.
- Preserve relevant logs and DLQ samples.
- Check whether rollback would break a newer provider/consumer contract.

## Runtime Rollback

Use the repository deployment flow for the affected Compose service. Exact image tag/source depends on the release process and is 검토 필요.

```bash
./scripts/deploy/compose-redeploy-service.sh <service>
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml ps <service>
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs --tail=200 <service>
```

Runtime service names:

- `hololive-bot`
- `hololive-admin-api`
- `hololive-alarm-worker`
- `llm-scheduler`
- `youtube-producer`

## Contract Rollback

- HTTP contract rollback must preserve route constants expected by deployed consumers.
- Queue envelope rollback must keep consumers able to read already-enqueued messages.
- Pub/Sub rollback must tolerate missed messages and trigger startup refresh where needed.
- Iris boundary rollback must verify cert/token/transport compatibility.

## Post-Rollback Smoke Tests

```bash
./scripts/architecture/check-project-map.sh
./scripts/architecture/check-runbook-coverage.sh
./scripts/smoke/smoke-compose-config.sh
./scripts/smoke/smoke-runtime-health.sh
```

## Related documents

- `release.md`
- `../CONTRACT_MAP.md`
- `../QUEUE_AND_PUBSUB_CONTRACTS.md`
- `../../runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md`
