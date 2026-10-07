# Current Runbooks

현재 운영 runbook의 루트 인덱스입니다.

## Runtime Runbooks

| Runtime | Runbook |
|---|---|
| `hololive-api` | `hololive-api.md` |
| `alarm-worker` | `alarm-worker.md` |
| `youtube-collector` | `youtube-collector.md` |

Collector runbook은 중앙 `c`·Seoul `b`의 Compose와 Osaka `a`·Osaka2 `d`의 native systemd 절차를 함께 소유합니다.
통합 관리자 웹은 iris-seoul의 `iris-console.service`가 소유하며, 이 저장소의 연결·shortlink 경계는
[`admin-dashboard.md`](admin-dashboard.md)를 따릅니다.

## Infra And Release

- `dlq-replay.md` - alarm dispatch DLQ 확인/재처리 기준
- `alarm-dispatch-quarantine-closeout.md` - 검토한 격리 send unit의 재발송 없는 종료 영수증
- `release.md` - release checklist
- `rollback.md` - rollback 기준
- `postgres-replication.md` - Osaka single-primary 운영 기준과 명시적 재승인 뒤 사용하는 Seoul physical standby/failover 재구축 참고 절차
- `postgres-observability.md` - 선택적 PostgreSQL CPU·대기 계측 확장의 검증·활성화·복구
- `integration-tests.md` - opt-in integration 테스트 주기 실행 경로
- `member-cache-v2-rollout.md` - durable epoch 기반 member cache expand/rollback 절차
- `../../runbook_execution/RELEASE_NOTES_TEMPLATE_20260303.md` - release notes template
- `../../history/host-migration/host-migration-root-to-kapu.md` - root → kapu 호스트 계정 풀 마이그레이션 절차 (완료, history)

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
