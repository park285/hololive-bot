# Runbook: rollback

## Role

중앙·Seoul의 Docker Compose, Osaka·Osaka2 AP의 native systemd runtime rollback과 contract/document rollback 판단 기준입니다. Collector의 paired issuer 복원 절차는 [collector runbook](youtube-collector.md#rollback)을 따릅니다.

## Before Rollback

- Identify the changed runtime, config, contract, or document gate.
- Preserve relevant logs and DLQ samples.
- Check whether rollback would break a newer provider/consumer contract.

## Runtime Rollback

중앙 runtime image는 빌드 호스트에서 만들고 런타임 호스트로 전송합니다. 정상 release는
새 image를 `prod`로 승격하기 전에 기존 `<service>:prod`를
`<service>:rollback-<UTC timestamp>`로 보존합니다. 따라서 1차 code rollback은
보존 tag를 `prod`로 되돌린 뒤 runtime host에서 `--no-build --no-deps`로 recreate하는
것입니다. 필요한 tag가 없거나 손상된 경우에만 이전 git ref를 빌드 호스트의 별도 clean
worktree에서 rebuild하고, [`release.md`](release.md#compose-service-재배포)의 전송·무빌드
cutover 절차를 따릅니다. 통합 전 per-runtime image(구 bot/admin/llm)로 되돌리는 5→3 긴급
rollback은 `DEC-20260926-hololive-retired-rollback-tooling`으로 종료했습니다. durable migration 123~136이
epoch-2 baseline에 포함되고 중앙에 퇴역 runtime image·컨테이너가 없으므로(T18 2026-09-26) 실행할 수 없는
절차였고, 이전 기록은 git 이력(이 파일의 2026-09-26 이전 revision)에만 남깁니다.

```bash
# 중앙 runtime host에서 rollback tag와 revision을 먼저 확인합니다.
sudo -n docker image inspect <service>:rollback-<UTC timestamp> \
  --format '{{index .Config.Labels "org.opencontainers.image.revision"}}'
sudo -n docker tag <service>:rollback-<UTC timestamp> <service>:prod

export COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/compose.env
sudo -n ./scripts/deploy/compose.sh \
  -f deploy/compose/docker-compose.prod.yml \
  -f deploy/compose/docker-compose.live-compat.yml \
  up -d --no-build --no-deps <service>
```

rollback 후에는 대상 container의 `StartedAt`, health, `RestartCount`, image revision을
확인하고 rollback tag와 실패한 release image를 원인 분석이 끝날 때까지 보존합니다.
PO issuer는 `RestartCount` 대신 [youtube-collector 런북의 issuer 수용 기준](youtube-collector.md#issuer-generation-교체와-재시작-카운트)으로 판정합니다.

`hololive-alarm-worker`를 Stack Worker Contract v1 이전 image로 되돌리는 것은 단일
image rollback이 아닙니다. 해당 image와 함께 보존한 repository revision, profile,
Compose/config, 그리고 stack-secrets backup을 하나의 승인된 rollback package로
복원해야 합니다. 현재 config를 유지한 채 이전 image만 재기동하는 경로는 없습니다.
paired package가 없거나 active send-unit/DB schema 호환성이 입증되지 않으면 rollback을
중단하고 current revision을 fix-forward합니다.

`DEC-20260926-hololive-delivery-telemetry-single-path`를 반영한 revision보다 앞선
`hololive-alarm-worker` image로 되돌릴 때도 image만 바꾸면 기동하지 않습니다. 그 image의
exact-key worker profile 디코더는 `youtube_delivery.telemetry_backfill_batch`를 요구하므로,
운영 alarm-worker profile(`/etc/stack-secrets/hololive-bot/worker-profiles/alarm-worker.json`과
stack-secrets master)에 제거 전 값을 다시 넣는 작업과 image 전환을 같은 유지보수 단계에서
합니다. 되돌린 image를 다시 current revision으로 올릴 때는 이 키를 다시 지웁니다.

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

`youtube-collector` Valkey/config projection rollback is an exact repository revision: Go binary/image, bundled Node helper, Compose base and AP overlays, host-native generator/wrapper, and the collector runbook together. See [`youtube-collector.md`](youtube-collector.md#rollback). Binary-only Valkey rollback is forbidden. Schema/data rollback is none. Mixed-version boundaries follow the hardening contract v6 §20.16: Go binary and Node helper stay lockstep in the same image/native artifact; fleet mixed binaries are allowed only among collectors that write typed `last_failure_*` (migration 218 removed the `legacy_collector` trigger, so pre-177 collector images are no longer a rollback target); old/new phase env dual-render is allowed for one release. This paragraph is a change-record draft. Production canary and rollback were not executed.

`hololive-api`가 durable bot admission migration 123~136을 적용하고 traffic을 수락한 뒤에는 이전 image나 구 bot runtime이 `bot_webhook_inbox`/`bot_reply_outbox`를 소비하지 못합니다. 따라서 `docs/current/runbooks/hololive-api.md`의 rollback 절차가 우선하며, ingress quiescence와 zero-backlog preflight가 성공하지 않으면 image rollback 대신 현재 durable runtime을 fix-forward합니다.

### Valkey 2차 축소(migration 231~233) 이후 rollback

migration 231(`auth_users.session_generation`)·232(`alarm_room_display_names`)·233(알람 템플릿 다음 방송 분기 제거)을
적용한 뒤 v6.0.x image로 되돌릴 때의 규칙입니다. 전체 절차와 근거는
[Valkey 축소 계획](../plans/2026-09-28-valkey-dependency-reduction.md#2차-rollback-규칙migration-231233-적용-뒤)에 있습니다.

- image만 되돌립니다. 되돌리기 전에 현재 7.0.0 `:prod` 두 개(`hololive-api`, `hololive-alarm-worker`)를
  `<service>:v7.0.0`처럼 별도 tag로 보존합니다. 그다음 두 서비스의 rollback tag를 `:prod`로 되돌리고 아래 명령으로만 교체합니다.

  ```bash
  sudo -n ./scripts/deploy/compose.sh \
    -f deploy/compose/docker-compose.prod.yml \
    -f deploy/compose/docker-compose.live-compat.yml \
    up -d --no-build --no-deps hololive-alarm-worker hololive-api
  ```

- 구 image의 `hololive-db-migrate`는 절대 실행하지 않습니다. 구 runner는 manifest 밖 ledger 행(092~094)을 이유로
  실패하고, `--no-deps` 없는 `up`은 이 실패 때문에 API·worker·collector 기동까지 막습니다. schema는 되돌리지
  않습니다(구 SQL은 열을 명시하고 새 열은 DEFAULT가 있으며, 233 본문은 구 코드에서도 같은 출력을 냅니다).
- `hololive-db-migrate`도 `hololive-api:prod` image를 쓰므로 retag 순간부터 migrate는 구 runner입니다. 중앙 host가
  재부팅되면 `hololive-compose.service`가 `systemd-compose-up.sh`로 `up -d --no-build`를 `--no-deps` 없이 실행해 이
  구 runner를 돌리고, 위 실패로 API·alarm-worker·중앙 collector가 기동하지 못합니다. 그래서 rollback 창 동안에는
  중앙 host를 재부팅하지 않고, retag 전에 `sudo systemctl disable hololive-compose.service`로 부팅 자동 실행을 끕니다
  (standby host의 disabled unit과 같은 방식, [`DEPLOYMENT_BASELINE.md`](../DEPLOYMENT_BASELINE.md)). 수동 전체 `up`과
  `--no-deps` 없는 `compose.sh up`도 같은 이유로 금지합니다. unit은 아래 재전진을 마친 뒤 `enable`로 되돌립니다.
- 재부팅이나 전체 `up`으로 이미 기동이 막혔다면 7.0.0으로 재전진해 복구합니다. 보존한 `<service>:v7.0.0`을 두 서비스
  모두 `:prod`로 되돌리고 revision label을 확인한 뒤, 새 runner로 migrate를 실행하고(231~233은 이미 기록돼 적용 없이
  exit 0) 전체를 기동합니다. 기동 뒤 C5 수용 판정을 다시 수행합니다.

  ```bash
  sudo -n docker tag hololive-api:v7.0.0 hololive-api:prod
  sudo -n docker tag hololive-alarm-worker:v7.0.0 hololive-alarm-worker:prod
  export COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/compose.env
  sudo -n ./scripts/deploy/compose.sh \
    -f deploy/compose/docker-compose.prod.yml \
    -f deploy/compose/docker-compose.live-compat.yml \
    run --rm hololive-db-migrate
  sudo systemctl reset-failed hololive-compose.service
  sudo systemctl enable --now hololive-compose.service
  ```

- image만 되돌린 동안에도 중앙 트리의 `hololive/hololive-api/VERSION`은 7.0.0이므로, 이후 `compose.sh`로 재생성된
  hololive-api는 `APP_VERSION=7.0.0`을 보고합니다. rollback 판정은 `APP_VERSION`이 아니라 실행 중 container image의
  `org.opencontainers.image.revision`·`org.opencontainers.image.version` label로 합니다([`release.md`](release.md#버전-관리)).
- 교체 직전에 `SCAN 0 MATCH auth:sess:* COUNT 1000`을 반복하며 `UNLINK`해 모든 관리자 세션을 끊고 재로그인시킵니다.
  새 코드가 발급한 세션은 `auth:user_sessions:*` 인덱스에 없어 구 reset이 폐기하지 못하고, 구 코드는
  `session_generation`을 비교하지 않아 새 코드에서 reset으로 무효화된 세션을 다시 받아들입니다. 세션 key는 사용자별로
  거를 수 없으므로 `session_generation > 0` 사용자만 고르는 대신 전체를 지웁니다. 값은 로그에 남기지 않습니다.
- `auth:user_sessions:*`는 rollback 창이 닫힐 때까지 지우지 않고 TTL(8일)로 만료시킵니다.
- rollback 기간에도 Console 방 이름 변경 동결을 유지합니다. 구 worker의 rename은 Valkey `alarm:room_names`에만
  남으므로, 동결을 풀었다면 재전진 전에 계획의 관리자 방 별칭 이관 절차를 다시 수행합니다.
- 구 image의 알람 upsert는 기존 행의 `room_name`을 바꿀 때 `alarms.room_name_updated_at`을 갱신하지 않으므로, 재전진 뒤
  관리 목록의 Kakao 대표 이름은 그 방의 다음 알람 upsert 전까지 rollback 이전 기준일 수 있습니다(관리 화면 표시만 영향).
- 폐기 key를 회수한 뒤 rollback하면 구 worker가 회수한 key를 PG에서 다시 만들므로, 재전진 뒤 계획의 2차 폐기 key 회수 표
  1~8을 같은 순서로 다시 수행합니다.
- C4 db-migrate에서 233이 거절되면 231·232만 적용된 상태입니다. 계획의 migration 233 사전 점검 쿼리로 찾은 본문에서
  `NextStream` 참조를 지우고 새 image의 db-migrate를 다시 실행하거나, 위처럼 `--no-deps`로 구 image를 되돌립니다.

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
