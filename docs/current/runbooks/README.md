# Current Runbooks

현재 운영 runbook의 루트 인덱스입니다.

## Runtime Runbooks

| Runtime | Runbook |
|---|---|
| `hololive-api` | `hololive-api.md` |
| `alarm-worker` | `alarm-worker.md` |
| `collector` | `collector.md` |

Collector runbook은 중앙 `c`·iris-seoul `b`·kapu `d`의 Podman Quadlet과 Osaka `a`의 native systemd 절차를 함께 소유합니다.
통합 관리자 웹은 iris-seoul의 `iris-console.service`가 소유하며, 이 저장소의 연결·shortlink 경계는
[`admin-dashboard.md`](admin-dashboard.md)를 따릅니다.

## Infra And Release

- `dlq-replay.md` - alarm dispatch DLQ 확인/재처리 기준
- `alarm-dispatch-quarantine-closeout.md` - 검토한 격리 send unit의 재발송 없는 종료 영수증
- `release.md` - release checklist
- `rollback.md` - rollback 기준
- `postgres-replication.md` - 중앙 `hololive-seoul` single-primary 운영 기준과 physical standby/failover 재활성화 전제(지운 절차·자산은 커밋 `bb000290e`)
- `postgres-observability.md` - 선택적 PostgreSQL CPU·대기 계측 확장의 검증·활성화·복구
- [PostgreSQL transaction pooling](../../../deploy/quadlet/README.md#transaction-pooling) - 역할별 용량·인증·최초 quiesce/resume 전환과 endpoint/HBA 조정 복구
- `integration-tests.md` - opt-in integration 테스트 주기 실행 경로
- `member-cache-v2-rollout.md` - durable epoch 기반 member cache expand/rollback 절차
- `../../runbook_execution/RELEASE_NOTES_TEMPLATE_20260303.md` - release notes template

## Existing Operational Reports

- `YOUTUBE_COMMUNITY_SHORTS_TARGET_BASELINE.md`
- `YOUTUBE_COMMUNITY_SHORTS_CHANNEL_ROUTE_VERIFICATION.md`
- `YOUTUBE_COMMUNITY_SHORTS_SEND_COUNTS_LAST_24H.md`
- `YOUTUBE_COMMUNITY_SHORTS_CHANNEL_SUMMARY_LAST_24H.md`
- `YOUTUBE_COMMUNITY_SHORTS_DELIVERY_LOGS.md`
- `YOUTUBE_COMMUNITY_SHORTS_ROUTE_USAGE_LAST_24H.md`
- `YOUTUBE_COMMUNITY_SHORTS_LATENCY_PERIOD_SUMMARY.md`
- `YOUTUBE_COMMUNITY_SHORTS_LATENCY_CAUSE_REPORT.md`

## Rule

- 새로운 현재 운영 runbook은 이 인덱스에서 발견 가능해야 합니다.
- Runtime runbook은 `docs/current/PROJECT_MAP.md`의 runbook link와 일치해야 합니다.
