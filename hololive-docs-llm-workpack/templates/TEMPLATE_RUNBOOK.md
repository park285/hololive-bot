# Runbook: <service-name>

## Role

서비스 역할을 한 문장으로 설명합니다.

## Normal status

| Check | Expected |
|---|---|
| Health |  |
| Ready |  |
| Logs |  |
| Queue |  |

## Dependencies

| Dependency | Required | Failure impact |
|---|---|---|
| PostgreSQL | yes/no |  |
| Valkey | yes/no |  |
| Iris | yes/no |  |

## Key environment variables

| Env | Purpose | Required |
|---|---|---|
|  |  |  |

## Logs

```bash
docker compose -f docker-compose.prod.yml logs -f <service>
```

## Metrics

- 검토 필요 항목은 명시합니다.

## Common failure modes

### 1. <failure>

Symptoms:
- 

Diagnosis:
```bash
# command
```

Mitigation:
- 

Rollback:
- 

## Smoke test

```bash
# command
```

## Related contracts

-
