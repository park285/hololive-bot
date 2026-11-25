# Runbook: rollback

## Role

중앙·iris-seoul(collector b)의 Podman Quadlet, Osaka·Osaka2 AP의 native systemd runtime rollback과 contract/document rollback 판단 기준입니다. Collector의 paired issuer 복원 절차는 [collector runbook](collector.md#rollback)을 따릅니다.

## Before Rollback

- Identify the changed runtime, config, contract, or document gate.
- Preserve relevant logs and DLQ samples.
- Check whether rollback would break a newer provider/consumer contract.

## Runtime Rollback

API/alarm rollback의 단위는 **이전 image + 정확한 worker profile + 그 image가 실행되던 unit 정의**입니다.
image tag만 바꾸거나 stack-secrets master에 삭제된 settings를 다시 넣지 않습니다.
먼저 pairing/preflight baseline을 운영에 배포·수용해야 하며, 첫 adoption에는 현재
실행 중인 바이너리의 인증된 loaded-profile hash 증거가 필요합니다.

중앙 API·worker의 이전 pair는 배포 helper(`quadlet-redeploy-service.sh`)가 배포마다
`/opt/hololive-bot/compose/current/artifacts/worker-profile/releases/<SERVICE>-<UTC>-<suffix>/previous`에 보존합니다.
`SERVICE`는 `hololive-api` 또는 `hololive-alarm-worker`이고 unit과 container 이름은 서비스 이름과 같습니다.
배포 중 활성화 뒤 실패(`[OUTCOME] failed_after_activation`)도, 수용한 뒤의 rollback도 helper가 자동으로 되돌리지 않습니다.
배포 트리의 rollback helper로 먼저 검사하고, 스키마·admission 호환성을 포함한 별도 운영 rollback 승인 뒤에만 적용합니다.

`--check-worker-profile`은 profile만이 아니라 같은 runtime 환경과 로컬 CA·DB 옵션도 검사합니다. Pair 검사 helper는 보존 unit의 환경 파일·사용자·읽기 전용 mount를 전달하고 네트워크를 차단합니다. Native 검사는 기존 runtime envfile과 로컬 파일을 사용합니다. 필수 파일·환경을 읽을 수 없으면 검사를 거부하며 임시 기본값이나 직접 DB 연결로 대체하지 않습니다.

중앙 API·worker·collector c의 파생 env는 `quadlet-prestart.sh --render-env`가 현재 `[Service]` 환경과 정상 prestart의 매핑·검증으로 다시 만듭니다. 검사별 새 0700 디렉터리에 0600 파일을 만들고 검사 종료 시 제거하므로 `/run/hololive-bot/quadlet-env`의 누락·stale 파일을 읽거나 덮어쓰지 않습니다. API retained pair의 `APP_VERSION`은 보존한 값을 사용합니다. 다른 `EnvironmentFile`의 순서와 명시적 `Environment` 우선순위는 유지하며, 선택 변수 생략은 빈 값으로 확장하되 필수값 거부는 그대로 적용합니다.

Quadlet `.volume`은 설치된 선언의 `VolumeName`과 Podman native inspect로 기존 저장 경로를 확인해 읽기 전용 bind로 전달합니다. 볼륨 생성·copy-up을 하지 않으며 idmap·UserNS를 보존합니다. 이 검사는 서비스를 시작하거나 tailnet·firewall 상태를 변경하지 않으며 원격 DB 인증·TLS 검증을 대신하지 않습니다.

```bash
rollback=/opt/hololive-bot/compose/current/scripts/deploy/quadlet-rollback-service.sh
release=/opt/hololive-bot/compose/current/artifacts/worker-profile/releases/<SERVICE>-<UTC>-<suffix>
# 검사: checksum·image identity 대조와 이전 image의 실제 --check-worker-profile(network none)
sudo "$rollback" "$release" --check
# 별도 운영 rollback 승인 후에만 실행합니다.
sudo "$rollback" "$release" --apply
```

`--apply`는 배포 때 pair 디렉터리에 만든 rollback drop-in(이전 image identity `Image=sha256:<id>`와 보존 profile,
`hololive-api`는 그 image의 `APP_VERSION`도 고정)을 `/etc/containers/systemd/hololive-bot/<SERVICE>.container.d/`에
게시하고 `daemon-reload` 뒤 unit을 재시작합니다. 그 뒤 실행 중 profile(`verify-live`)과 `NRestarts=0`을 확인합니다.
primary fence 표식이 있으면 적용하지 않습니다. 정지해 있던 writer를 정지 상태로 되돌려야 하면 이 helper 대신
`worker-profile-pair.py`의 `verify`·`check`·`activate`와 `daemon-reload`만 실행하고 unit을 시작하지 않습니다.
런타임 peer는 `Wants=`라 멈춰 있는 peer를 함께 시작하므로(`hololive-api` 시작은 `hololive-alarm-worker`도 시작) 정지 상태를
유지해야 하는 writer가 있으면 순서를 먼저 확인합니다. unit 정의 자체가 바뀐 revision이면 이전 revision의
clean checkout에서 `sudo ./scripts/deploy/sync-opt-current.sh`로 배포 트리와 unit을 다시 설치한 뒤(이 도구는
checkout의 HEAD만 배포합니다) 위 순서를 따릅니다. DB migration, schema/data rollback, secret mirror/master 복원과
migrator drop-in(`hololive-db-migrate.container.d/50-migrator-image.conf`) 복원은 수행하지 않습니다.

profile pair 검증은 DB 호환성 검사가 아닙니다. 아래 durable/receipt 경계와 현재 schema,
admission·send-unit·projection 계약을 먼저 확인합니다. pair가 없거나 손상됐거나
호환성을 입증할 수 없으면 임의로 profile을 합성하지 말고 current revision을 fix-forward합니다.
이전 ref를 재빌드해야 한다면 별도 clean worktree와 [`release.md`](release.md)의
전송 절차를 사용하되 원래 paired profile과 revision/architecture를 함께 검증합니다.

복원 후 container image ID/revision, 실제 loaded profile hash, health, `StartedAt`,
unit의 `NRestarts`(`systemctl show <SERVICE>.service -p NRestarts`)를 확인합니다. desired source는 최신 상태로 유지하므로 rollback 뒤
`profile_file_changed`는 정상적인 차이 표시이며 reload/fallback 사유가 아닙니다.
backup pair와 실패한 release는 원인 분석이 끝날 때까지 보존합니다.

Collector Quadlet(c·b)·native(a)는 [collector runbook](collector.md#rollback)의
binary/image·Node helper·issuer·프로필 짝을 사용합니다. PO issuer는
[`RestartCount`가 아닌 issuer 수용 기준](collector.md#issuer-generation-교체와-재시작-카운트)으로 판정합니다.
kapu Quadlet d는 [kapu README](../../../deploy/podman/kapu/README.md)의 `SHA256SUMS` release profile과
`runtime-before-<날짜>` image tag로 되돌립니다.


migration 006(`idx_bot_reply_outbox_message` 삭제)과 007(발송 route CHECK를 `text`·`richtext`로 좁히고
`alarms.alarm_types` 빈 배열 거절)은 v15.0.0 image와 함께 동작합니다. v15.0.0은 그 인덱스를 조회 계획에만 쓰고,
`markdown`·`sender` route나 빈 `alarm_types`를 저장하지 않습니다. 이 두 migration은 image rollback 때 되돌리지 않습니다.

Runtime service names:

- `hololive-api`
- `hololive-alarm-worker`
- `collector`

`collector` rollback은 Go binary/image, bundled Node helper, worker profile, Quadlet drop-in(c·b) 또는 native release, paired issuer를 함께 보존한 revision 단위입니다. Binary-only rollback과 schema/data rollback은 허용하지 않습니다. migration 218 이후에는 typed `last_failure_*`를 쓰지 않는 pre-177 collector image가 rollback 대상이 아닙니다. 실제 fleet/DB 계약을 만족하는 pair만 사용하며, 이 문서의 로컬 준비가 운영 canary·rollback 실행을 뜻하지 않습니다.

`hololive-api`가 durable bot admission 스키마(epoch-4 baseline 포함)를 적용하고 traffic을 수락한 뒤에는 이전 image나 구 bot runtime이 `bot_webhook_inbox`/`bot_reply_outbox`를 소비하지 못합니다. 따라서 `docs/current/runbooks/hololive-api.md`의 rollback 절차가 우선하며, ingress quiescence와 zero-backlog preflight가 성공하지 않으면 image rollback 대신 현재 durable runtime을 fix-forward합니다.

### RichText 저장 요청과의 경계

새 bot outbox 및 alarm/notification/YouTube 고정 요청은 `richtext` kind/route를 사용합니다.
`richtext` 저장 경로 제약은 epoch-4 baseline에 포함되어 있으며 기존 `text` 행은 바꾸지 않습니다.
새 RichText 요청을 저장한 이후에는 해당 타입을 모르는 구형 API/alarm writer로 롤백하지 않습니다.
구형 writer는 저장 요청을 거부하거나 같은 ID에 다른 타입을 보내므로 현재 revision을 forward-fix합니다.
`markdown` route·kind 재생 경로는 퇴역했으며(#763), 남은 `markdown` 요청은 Iris를 호출하지 않고 지원하지 않는 형식으로 거절됩니다.
이 변경의 로컬 DB·HTTP 검증은 운영 migration·배포·rollback 승인이나 완료를 뜻하지 않습니다.

### 이전 Valkey 축소 revision과의 경계

migration 231~233 이전 image의 image-only rollback, session 일괄 삭제, systemd
비활성화 절차는 현재 운영 경로가 아닙니다. 당시 분석은
[Valkey 축소 계획](../plans/2026-09-28-valkey-dependency-reduction.md#2차-rollback-규칙migration-231233-적용-뒤)과 Git 이력에 보존합니다.

현재 schema·migration ledger·방 이름 정본을 이해하지
못하는 이전 image는 pair가 있더라도 적용하지 않습니다. 구 migrator(`hololive-db-migrate.service`) 실행이나
정지한 peer unit의 연쇄 기동으로 dependencies를 다시 기동하지 않으며, 실패한 migration은
소유자의 검증·복구 절차로 처리합니다. 데이터/세션 파괴나 서비스 자동 기동 정책 변경을
routine rollback에 묶지 않습니다. 호환성이 입증되지 않으면 현재 revision으로 fix-forward합니다.

### 퇴역한 pooler 경로로의 복구

이전 image/profile만으로는 접속 경로를 복구할 수 없습니다. 중앙·b·native helper는 현재 env/drop-in의 effective host·port와 복구점의 값을 비교하고, 다르면 재기동 전에 거절합니다. 자동 복원 중 교차 경로를 발견한 경우 collector/issuer 정지와 복구 입력을 유지합니다.

PgBouncer 퇴역 전환 뒤 pooler 경로로 되돌리는 rollback은 지원하지 않습니다. 이전 HBA·pooler unit·인증 객체를 다시 세워야 하므로 호환 후보로 fix-forward합니다. 임시 pooler 허용·우회 flag를 추가하지 않습니다. [상세 계약](../../../deploy/quadlet/README.md#복구와-자격-증명-변경)을 확인합니다.

## Contract Rollback

- HTTP contract rollback must preserve route constants expected by deployed consumers.
- Queue envelope rollback must keep consumers able to read already-enqueued messages.
- Pub/Sub rollback must tolerate missed messages and trigger startup refresh where needed.
- Iris boundary rollback must verify cert/token/transport compatibility.

## Post-Rollback Smoke Tests

```bash
# 중앙: 설치된 unit 정의가 생성되는지, unit이 active이고 container health가 healthy인지
sudo /usr/libexec/podman/quadlet -dryrun >/dev/null
systemctl is-active hololive-api.service hololive-alarm-worker.service hololive-collector-c.service
for c in hololive-api hololive-alarm-worker hololive-collector-c; do
  sudo podman inspect "$c" --format '{{.Name}} {{.State.Health.Status}}'
done
```

## Related documents

- `release.md`
- `../CONTRACT_MAP.md`
- `../QUEUE_AND_PUBSUB_CONTRACTS.md`

**20.0.0 원장:** 완료된 epoch-3 032는 32쌍을 그대로 읽을 뿐이다. 기존/신규 어느 DB에도 과거 migrator 자동실행을 붙이지 않는다. 이미지·프로필 rollback은 실제 writer 호환성 검증과 별개이며, DB를 되돌릴 때는 전환 전 백업을 별도 복원하거나 fix-forward한다.
