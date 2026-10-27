# Runbook: rollback

## Role

중앙·Seoul의 Docker Compose, Osaka·Osaka2 AP의 native systemd runtime rollback과 contract/document rollback 판단 기준입니다. Collector의 paired issuer 복원 절차는 [collector runbook](youtube-collector.md#rollback)을 따릅니다.

## Before Rollback

- Identify the changed runtime, config, contract, or document gate.
- Preserve relevant logs and DLQ samples.
- Check whether rollback would break a newer provider/consumer contract.

## Runtime Rollback

API/alarm rollback의 단위는 **이전 image + 정확한 worker profile + 보존 Compose 입력**입니다.
image tag만 바꾸거나 stack-secrets master에 삭제된 settings를 다시 넣지 않습니다.
먼저 pairing/preflight baseline을 운영에 배포·수용해야 하며, 첫 adoption에는 현재
실행 중인 바이너리의 인증된 loaded-profile hash 증거가 필요합니다.

보존 backup 디렉터리로 검사한 뒤, 승인된 창에서 같은 pair를 적용합니다.

```bash
export COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/compose.env
sudo -n ./scripts/deploy/compose-rollback-service.sh <backup-directory> --check
# 별도 운영 rollback 승인 후에만 실행합니다.
sudo -n ./scripts/deploy/compose-rollback-service.sh <backup-directory> --apply
```

인자를 생략한 mode는 `--check`입니다. 도구는 `hololive-api`와
`hololive-alarm-worker`만 받으며 이전 image/profile checksum, 보존 Compose 구성,
**이전 image의 실제 `--check-worker-profile`**을 검증합니다. 적용 시 원래 실행 중이던
writer만 기동하며, 이전에 정지해 있던 writer는 정지 상태로 복원합니다. DB migration,
schema/data rollback, secret mirror/master 복원은 수행하지 않습니다.

profile pair 검증은 DB 호환성 검사가 아닙니다. 아래 durable/receipt 경계와 현재 schema,
admission·send-unit·projection 계약을 먼저 확인합니다. pair가 없거나 손상됐거나
호환성을 입증할 수 없으면 임의로 profile을 합성하지 말고 current revision을 fix-forward합니다.
이전 ref를 재빌드해야 한다면 별도 clean worktree와 [`release.md`](release.md)의
전송 절차를 사용하되 원래 paired profile과 revision/architecture를 함께 검증합니다.

복원 후 container image ID/revision, 실제 loaded profile hash, health, `StartedAt`,
`RestartCount`를 확인합니다. desired source는 최신 상태로 유지하므로 rollback 뒤
`profile_file_changed`는 정상적인 차이 표시이며 reload/fallback 사유가 아닙니다.
backup pair와 실패한 release는 원인 분석이 끝날 때까지 보존합니다.

Collector Compose/native는 [collector runbook](youtube-collector.md#rollback)의
binary/image·Node helper·issuer·프로필 짝을 사용합니다. PO issuer는
[`RestartCount`가 아닌 issuer 수용 기준](youtube-collector.md#issuer-generation-교체와-재시작-카운트)으로 판정합니다.

delivery digest의 content-sensitive identity를 도입한 revision은 기존 period-only identity와
rollback 호환되지 않습니다. v3 handoff(`DELIVERY_OUTBOX_V3_HANDOFF_MODE`)는
`DEC-20260926-hololive-outbox-v3-convergence`로 삭제되어 현재 `hololive-api`는 v3 digest row를
만들지 않고 digest를 v2 `notification_delivery_outbox`에만 적재합니다. 그래도 rollback 전에는
authoritative DB에서 아래 read-only preflight가 `0`인지 확인하고 `hololive-alarm-worker`를 먼저
전환한 뒤 `hololive-api`를 전환합니다.

```sql
SELECT count(*)
FROM alarm_dispatch_events
WHERE category = 'delivery_digest'
   OR payload ->> 'source_kind' = 'delivery_digest';
```

content-sensitive delivery digest row가 한 건이라도 생성된 뒤에는 구 period-only API 또는
alarm-worker image로 되돌리지 않습니다. 구 producer는 같은 kind/period/room을 새 key로
인식하지 못해 재발송할 수 있고, 구 worker는 메시지가 다른 row를 같은 group으로 합칠 수
있습니다. 이 경우 API의 scheduler·run-now ingress를 중지하고 active send unit을 drain하며
current revision을 fix-forward합니다. rollback image 사용은 최초 v3 digest 생성 전으로만
제한합니다. handoff mode 키를 다시 넣어 v3 digest 생성을 켜는 절차는 없으며, 현재 revision은
그 키가 있으면 기동을 거절합니다.

Runtime service names:

- `hololive-api`
- `hololive-alarm-worker`
- `youtube-collector`

`youtube-collector` rollback은 Go binary/image, bundled Node helper, worker profile, Compose base/AP overlays 또는 native release, paired issuer를 함께 보존한 revision 단위입니다. Binary-only rollback과 schema/data rollback은 허용하지 않습니다. migration 218 이후에는 typed `last_failure_*`를 쓰지 않는 pre-177 collector image가 rollback 대상이 아닙니다. 실제 fleet/DB 계약을 만족하는 pair만 사용하며, 이 문서의 로컬 준비가 운영 canary·rollback 실행을 뜻하지 않습니다.

`hololive-api`가 durable bot admission migration 123~136을 적용하고 traffic을 수락한 뒤에는 이전 image나 구 bot runtime이 `bot_webhook_inbox`/`bot_reply_outbox`를 소비하지 못합니다. 따라서 `docs/current/runbooks/hololive-api.md`의 rollback 절차가 우선하며, ingress quiescence와 zero-backlog preflight가 성공하지 않으면 image rollback 대신 현재 durable runtime을 fix-forward합니다.

### 이전 Valkey 축소 revision과의 경계

migration 231~233 이전 image의 image-only rollback, session 일괄 삭제, systemd
비활성화 절차는 현재 운영 경로가 아닙니다. 당시 분석은
[Valkey 축소 계획](../plans/2026-09-28-valkey-dependency-reduction.md#2차-rollback-규칙migration-231233-적용-뒤)과 Git 이력에 보존합니다.

현재 schema·migration ledger·방 이름 정본을 이해하지
못하는 이전 image는 pair가 있더라도 적용하지 않습니다. 구 migrator 실행이나
`--no-deps` 누락으로 dependencies를 다시 기동하지 않으며, 실패한 migration은
소유자의 검증·복구 절차로 처리합니다. 데이터/세션 파괴나 서비스 자동 기동 정책 변경을
routine rollback에 묶지 않습니다. 호환성이 입증되지 않으면 현재 revision으로 fix-forward합니다.

## Contract Rollback

- HTTP contract rollback must preserve route constants expected by deployed consumers.
- Queue envelope rollback must keep consumers able to read already-enqueued messages.
- Pub/Sub rollback must tolerate missed messages and trigger startup refresh where needed.
- Iris boundary rollback must verify cert/token/transport compatibility.

## Post-Rollback Smoke Tests

```bash
./scripts/smoke/smoke-compose-config.sh
./scripts/smoke/smoke-runtime-health.sh
```

## Related documents

- `release.md`
- `../CONTRACT_MAP.md`
- `../QUEUE_AND_PUBSUB_CONTRACTS.md`
