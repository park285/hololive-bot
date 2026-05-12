# Service: <service-name>

## Runtime identity

| Field | Value |
|---|---|
| Module | `<module>` |
| Binary | `<binary>` |
| Compose service | `<compose-service>` |
| Port | `<port>` |
| Health endpoint | `<endpoint>` |
| Ready endpoint | `<endpoint or none>` |

## Role

서비스의 한 문장 역할을 적습니다.

## Owns

- 이 서비스가 소유하는 도메인 책임
- 이 서비스가 최종적으로 결정하는 상태
- 이 서비스가 시작/중지하는 worker 또는 scheduler

## Provides

| Contract | Type | Path/Event/Queue | Consumers |
|---|---|---|---|
|  |  |  |  |

## Consumes

| Dependency | Purpose | Failure impact |
|---|---|---|
| PostgreSQL |  |  |
| Valkey |  |  |
| Iris |  |  |

## Must not own

- 이 서비스에 넣으면 안 되는 책임
- 다른 서비스의 소유 도메인
- forbidden import 또는 forbidden runtime 역할

## Startup requirements

- 필요한 env
- 필요한 secret
- 필요한 external dependency

## Shutdown behavior

- graceful shutdown 절차
- worker stop 순서
- queue/retry 처리 주의점

## Observability

- logs
- metrics
- health/readiness
- common alert signal

## Related documents

- Project Map:
- Contract Map:
- Runbook:
