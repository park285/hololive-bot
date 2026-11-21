# Runbook: 릴리즈

## 역할

Runtime, document, and contract changes의 release checklist입니다.

## 버전 관리

저장소 전체 app 릴리즈 버전의 단일 기준은 루트 `VERSION`입니다. 안정 SemVer
`MAJOR.MINOR.PATCH` 형식으로 관리하고 Git tag에는 `r` 접두사를 붙입니다(`r<version>`).
`hololive/hololive-api/VERSION`과 `hololive/hololive-alarm-worker/VERSION`은 각 container
artifact 버전이며 독립 build가 필요하면 서로 달라질 수 있습니다. 저장소 tag는 root
`VERSION`이 가리키는 전체 source snapshot에만 붙입니다.

collector·PO image도 `hololive/hololive-api/VERSION`을 쓰므로 API를 재빌드하지 않는 collector·PO
릴리스에서도 이 파일이 올라갈 수 있습니다. 이때 hololive-api image는 이전 build로 남지만, 중앙의
`hololive-api.service`가 (재)시작될 때마다(재부팅 포함) Quadlet prestart가 배포 트리의 이 파일 값을
`APP_VERSION`으로 넘깁니다. API가 보고하는 `APP_VERSION`은 repo 릴리스와 같아지지만 image의 revision·version
label은 이전 build를 가리킵니다. API source가 바뀌지 않은 릴리스에서만 생기는 식별자 차이이며, API 산출물 판정은
image label로 합니다.

스택 전체 결정으로 버전 번호를 1.0.0부터 다시 시작합니다. 다음 릴리즈에서 루트 `VERSION`과 두 runtime `VERSION`을
모두 1.0.0으로 맞추고 tag `r1.0.0`을 만듭니다. 그때까지 `VERSION`은 직전 릴리즈 값을 그대로 둡니다. 기존 `v<version>`
tag와 `## v<version>` 절은 이전 번호 체계의 기록으로 남기고, 새 tag를 `v` 접두사로 만들지 않습니다. 두 체계의 번호는
겹치므로 버전 숫자만으로 신구를 비교하지 않습니다. 배포 복구 도구는 원래 릴리스 tag로 `r<version>`과 이전 `v<version>`을 모두
받고 tag 이름을 기록된 binding과 bundle의 tag ref로 검증하며, `scripts/publish-release.sh`는 새 Release를 `r<version>` tag로만 만들고, 기존 Release 수정과 비교 기준에는 이전 `v<version>`도 받습니다.

릴리즈할 때는 다음 순서를 지킵니다.

1. 루트 `VERSION`을 변경하고 각 runtime artifact를 함께 릴리즈한다면 해당 runtime의
   `VERSION`도 변경합니다.
2. `CHANGELOG.md`의 `미출시` 항목을 `## r<version> - YYYY-MM-DD` 구간으로 옮기고, 이 버전과 tag가
   붙은 최근 두 버전까지 남기고 그보다 오래된 구간은 지웁니다. 그 사이에 tag 없이 준비만 된 구간은 세지 않고 남깁니다.
3. `bash scripts/check-release-version.sh`와 pre-push gate를 `FULL_PRE_PUSH=true`로 통과시킵니다.
4. 검증된 릴리즈 commit을 `main`에 반영하고 반영된 `main` commit의 SHA를 기록합니다. squash 병합은 SHA가
   달라지므로 그 commit의 tree가 검증한 tree와 같은지 확인하고, 다르면 그 commit에서 2단계부터 다시 진행합니다.
5. 4단계에서 기록한 `main` commit에 annotated tag `r<version>`을 만들어 별도 승인된 publish 절차로 push합니다.

버전과 tag는 실제로 배포하거나 게시할 artifact를 만들 때만 올리며 PR을 병합할 때마다 올리지 않습니다.
병합한 변경은 `미출시`에 누적하고, 배포할 때 직전 릴리즈 이후의 변경 전체를 한 버전으로 묶습니다.
같은 배포에서 버전을 연달아 만들지 않으며, tag 전 검증에서 찾은 결함은 같은 버전 안에서 고칩니다.
배포 artifact가 달라지지 않는 변경(문서, 시험, CI, 개발 도구)은 버전을 올리지 않습니다.

`MAJOR`는 스택 밖 사용자나 운영자가 직접 조치해야 하는 비호환 변경(공개 명령·API의 제거·의미 변경,
되돌릴 수 없는 데이터 migration)에만 씁니다. `MINOR`는 기능 추가와 배포 순서로 맞출 수 있는 스택 내부
계약 변경, `PATCH`는 하위 호환 수정·보안·운영 안정화·의존성 갱신에 씁니다. 단계 판단이 갈리면 낮은
단계를 고릅니다. 지운 `CHANGELOG.md` 구간은 Git 이력과 tag에 남으며 추정 버전을 소급하지 않습니다.

### 19.2.1 의존성 수용 기준

- 다섯 모듈을 각각 `GOWORK=off`로 검사합니다. API의 `adapters/valkeydedup/v1.0.0`은 새 모듈 캐시에서 공개 프록시와 `sum.golang.org`로 ZIP·`go.mod`가 모두 기존 `go.sum`과 일치해야 합니다. 형제 checkout이나 오프라인 동기화 결과만으로 릴리스를 수용하지 않습니다.
- Hololive는 `shared-go/postgres`의 `pgxdb`를 쓰지만 `pgstore.Store`는 쓰지 않습니다. API는 `bot_webhook_inbox`와 `bot_webhook_heads`를 소유합니다. 격리 PostgreSQL에서 `internal/planes/bot/internal/durability`의 `TestInboxTransitionsMoveTheOrderingHeadOnlyWhenTerminal`로 재시도 중 head 유지와 종단 뒤 다음 행 처리를 확인합니다. nonce 중복·오류 처리, Gemini 일정표 처리, collector/PO issuer와 worker 회귀도 확인합니다.
- 공용 `pgstore`를 실제로 쓰는 소비자만 [postgres/v1.0.3 전환·롤백 조건](https://github.com/park285/shared-go/blob/postgres/v1.0.3/README.md#inbox-수락-순서)을 따릅니다. `(scope, ordering_key)`별 수락 ID FIFO, 시퀀스 증가폭 1·`CACHE 1`·`NO CYCLE`, 기존 인덱스 보존과 `idx_iris_webhook_inbox_head_by_id` 추가가 전제입니다. 수락·claim·만료 복구를 멈추고 진행 중 트랜잭션이 끝난 뒤 미종단 `pending`·`processing`의 기존/신규 head 불일치 또는 비선두 processing이 0행이어야 합니다. 두 claim 구현을 섞지 않습니다. 이 조건을 Hololive 테이블에 적용하거나 운영 변경 승인으로 해석하지 않습니다.
- 빌드 호스트에서 같은 clean revision과 `19.2.1`로 API·alarm-worker·collector/PO 산출물을 식별합니다. 중앙 API·alarm-worker는 아래 절차로 교체하고, AP는 슬롯별 런북과 승인 범위에 따라 collector·issuer를 한 쌍으로 수용합니다. 소스 게시와 운영 수용은 별도 증거로 남기며 새 DB migration, DB·pooler 재시작이나 승인 없는 rollback은 수행하지 않습니다.

### 11.0.0 worker/DB cutover

먼저 구 API/alarm settings·mode를 받는 **10.2.1 호환 baseline을 실제 운영에서 수용**합니다.
구 프로필 CLI·인증된 loaded hash·atomic source drift·paired rollback 증거 없이 새 settings를
활성화하지 않습니다. 준비 receipt나 source 통합은 이 수용을 대신하지 않습니다.
[worker settings cutover 절차](../../../deploy/compose/README.md#worker-settings-cutover-1100)는
11.0.0의 단일 strict schema, 최초 capture, no-build 후보 검사와 복구 bundle을 정의합니다.
각 module `go.mod`가 고정한 정식 shared-go·iris-client-go pin/sum과 현행 migration manifest 전체
([epoch-3 규약](../../../hololive/hololive-api/scripts/migrations/CONVENTIONS.md))를 유지하며
stream-duration·time-finalization·membership·Node·issuer 산출물을 함께 보존합니다.
이 worker/DB 운영 변경에 data migration을 추가하거나, 이전 manifest를 실행하거나, 적용된 migration을 되돌리지 않습니다.

11.0.0의 API/alarm·collector pair와 standalone Seoul 구성을 검증한 뒤 승인된 순서로 교체합니다.
privileged migrator는 전용 Unix socket·network-none이고 network consumer는 TLS verify-full입니다.
role limit SQL은 배포 wrapper가 실행하지 않습니다. rollout 소유자가 collector 네 곳 모두 max 7
수용을 확인한 **뒤에만** 승인된 admin socket으로 적용합니다. HBA/WAL/server 재생성과
persisted 설정 정리도 각 승인·증거를 남깁니다. Valkey noeviction은 별도 선행 관측이 없으면
활성화하지 않습니다. 이 문서는 새 운영 적용 증거를 주장하지 않습니다.


## 중앙 service 재배포

빌드 호스트와 중앙 런타임 호스트는 서로 다른 머신입니다. 중앙 런타임 호스트에서는
어떤 빌드도 돌리지 않습니다 — 이미지는 빌드 호스트에서 만들어 전송하고, 런타임
호스트는 이미 적재된 이미지로 재기동만 합니다.

모든 build·deploy·restart는 `hololive-bot-ops`로 라우팅하고 각각 필요한 승인을
확인합니다. 검증된 clean revision의 전체 image build-only 진입점은
`./build-all.sh --no-bump --build-only`(`podman build`)입니다. 중앙 service의 실제 배포는 아래의
build host 생성·검증, image와 deploy tree 전송, runtime host Quadlet 기동 세 단계를 분리해 수행합니다.
중앙 unit 정의와 설치는 `deploy/quadlet/README.md`가 소유합니다.

### 1. 빌드 호스트: 이미지 생성과 검증

검토된 revision에서 clean worktree로 빌드하고, 세 항목이 모두 맞는지 확인합니다. x86_64 build host의
arm64 빌드는 등록된 qemu-aarch64 binfmt가 있어야 하며 없으면 `build-all.sh`가 빌드 전에 멈춥니다.

```bash
HOLOLIVE_BUILD_PLATFORM=linux/arm64 ./build-all.sh --no-bump --build-only

podman image inspect localhost/<image>:prod \
  --format '{{.Architecture}} {{index .Config.Labels "org.opencontainers.image.version"}} {{index .Config.Labels "org.opencontainers.image.revision"}}'
```

`arch=arm64`가 아니거나 revision이 `unknown`·short SHA이면 중단합니다.

### 2. 전송

image는 build host에서 `:candidate` 이름으로 저장해 중앙에 적재합니다. 중앙의 `:prod` tag는 옮기지 않습니다.
API·worker의 실행 image는 worker-profile drop-in이, infra image는 host `compose.env`의 identity가 정합니다
([image identity 소유](../../../deploy/quadlet/README.md#이미지)). collector c와 PO issuer는 kapu의
`scripts/deploy/po-central-cutover.sh c`가 build·전송·교체를 함께 소유합니다. iris-seoul collector b는 같은
release의 c 배포 뒤 `po-central-cutover.sh b`로 배포합니다([iris-seoul README](../../../deploy/podman/iris-seoul/README.md#배포)).

```bash
podman tag localhost/<image>:prod localhost/<image>:candidate
podman image inspect localhost/<image>:candidate --format 'sha256:{{.Id}}'    # build host identity를 기록
podman save --format docker-archive localhost/<image>:candidate | gzip -1 \
  | ssh <central-host> 'gunzip | sudo -n podman load'
ssh <central-host> "sudo -n podman image inspect localhost/<image>:candidate --format 'sha256:{{.Id}}'"
```

`build-all.sh`는 `--format docker`로 빌드합니다. `oci-archive`로 옮기면 config가 OCI 형식으로 바뀌어
수신측 `.Id`가 build host와 달라지므로 `docker-archive`를 씁니다(2026-11-04 리허설 VM 실측). 두 identity가 다르면
배포하지 않습니다.

rollback 기준은 다음과 같습니다. API·worker는 배포 helper가 실행 중이던 image·profile pair를 release 디렉터리와
`hololive-worker-profile:pair-*` tag로 보존합니다. infra image는 바꾸기 전 `compose.env` 값(이전 identity)과 그 image이며,
수용 전에는 이전 image를 지우지 않습니다. 직전 배포 트리(`/opt/hololive-bot/compose/current.bak-*`)도 함께 보존합니다.

### 3. 런타임 호스트: Quadlet 기동

배포할 commit의 clean checkout에서 `sudo ./scripts/deploy/sync-opt-current.sh`로 `/opt/hololive-bot/compose/current`와
unit을 갱신합니다. 이 도구는 배포 트리에 그 commit을 `.source-revision`으로 남기고, host `compose.env`의 PostgreSQL
연결 예산을 확인하지 못하면 아무것도 설치하지 않습니다. unit은 빌드하지 않으며(`Pull=never`), prestart가 bind 주소·비밀
파일·방화벽을 확인합니다.
기존 runtime data/config/권한 변경을 승인하지 않은 갱신은 `--preserve-runtime`을 명시합니다.
새 고유 exec-tree backup만 만들며 과거 backup을 덮거나 자동 정리하지 않습니다.
이미 `--schema-only` migrator를 수용했고 host 정의가 바뀌지 않는 앱 갱신에는 `--source-only <현재 설치된 40자리 SHA>`를 사용합니다. 빌드 호스트에서 검증한 annotated tag의 commit과 중앙의 고유 root 소유 staging checkout이 일치하는지 확인한 뒤 **그 checkout의** `scripts/deploy/sync-opt-current.sh`를 실행합니다. 설치된 구 helper를 수정하거나 새 helper만 따로 복사해서 실행하지 않습니다. `--application-only`는 구 `--pool-only=auth`에서 카드 migrator로 전환하는 창 전용입니다.

`--source-only`는 writer lock 아래 설치 stamp·구 소스 전체와 후보 계보·깨끗한 checkout을 대조합니다. 아카이브를 풀기 전에 Git 객체의 경로·모드와 예약된 stamp 경로를 검사하며 기존 일반 파일의 내용 변경만 허용합니다. 설치 coordinator와 실행 의존성은 루트까지 경로 전체가 root 소유이고 그룹·기타 쓰기, 추가 POSIX ACL, 심볼릭 링크나 일반 파일의 하드 링크가 없어야 합니다. 이 모드는 호스트 권한을 조정하거나 Quadlet·방화벽·pooler·runtime 경로를 재설치하거나 daemon-reload하지 않습니다.

구 `current.bak-*`를 검증·보존하고 stamp를 먼저 `incomplete`로 내구성 있게 바꿉니다. 검토된 파일(이번에는 helper 자신과 CHANGELOG 포함)을 교체한 뒤 전체 후보와 대조하고 온전한 SHA를 마지막에 게시합니다. 중간 실패 시 혼합 트리에 구 이미지나 새 이미지를 활성화할 수 없습니다. 자동 재실행·복원하지 말고 실제 파일·backup·기존 pair를 대조합니다. 이후 각 앱 교체는 아래 설치된 coordinator가 수행합니다.

기존 19.2.1 설치의 보호 bind와 `current`를 그대로 두고 epoch 4 앱을 배포할 때는 위 세 가지 동기화 모드 대신 [보호된 소스 게시](../../../deploy/quadlet/README.md)의 `--protected-source <설치 SHA> <후보 SHA> <도구 SHA> <후보 clone>`를 사용합니다. 소스 게시 성공은 앱 기동 성공이 아닙니다. 중앙 API·worker용으로 검증해 전송한 이미지와 **같은 commit에 이 coordinator가 포함되어야** 합니다. 과거 릴리스의 이미지와 후속 도구를 섞지 않습니다.

```bash
helper="/opt/hololive-bot/compose/app-sources/${reviewed_sha}/scripts/deploy/quadlet-redeploy-service.sh"
sudo -n "$helper" hololive-api "$approved_api_image_id"
sudo -n "$helper" hololive-alarm-worker "$approved_worker_image_id" "$approved_api_image_id"
```

각 호출은 별도로 승인합니다. `reviewed_sha`는 승인된 40자리 SHA, image ID는 `sha256:`과 64자리 hex입니다. `active`를 따라가서 SHA를 고르거나, 검증 실패 후 `current`의 구 helper를 실행하지 않습니다. 기존 `--schema-only` base를 재설치하지 않고 migrator drop-in만 `--check`와 후보 API 이미지에 고정합니다. 후보의 실제 종료·`applied=0`, 이전 pair, 완전한 계보와 pooled SQL, 대상의 H3 준비 상태와 loaded profile을 확인합니다. 완료 epoch 3이면 32개 영수증을 그대로 사용하며, 신규 기준선 영수증을 쓰지 않습니다. peer의 컨테이너·invocation·이미지·endpoint와 보호 bind의 양쪽 inode가 바뀌면 중단합니다. 실패 시 소스 세대·release·migrator 기록을 보존하며 자동으로 재시도하거나 원복하지 않습니다.

순서는 API 다음 worker입니다. 최초 API 교체에는 게시 영수증의 predecessor worker가 실행 중이어야 합니다. worker 교체에는 같은 SHA의 API `accepted-writer` 영수증과 그 세대에서 실제 로드된 pair가 필요합니다. 양쪽 모두 기존 설치 pair가 없는 경우 새로 만들어 우회하지 않습니다. `outcome_unknown`이면 release의 `activation.intent`, `schema-job`, `writer-job`과 실제 실행 세대를 보존하고 동일 명령을 다시 보내지 않습니다.

보호 소스 저장소가 없는 일반 설치에서는 배포 트리의 helper로 API·worker를 교체합니다. helper가 이전 pair 보존, candidate 검사, 활성화 전
`hololive-db-migrate`(같은 revision의 API image로 고정), drop-in 활성화와 restart, `verify-live`·`NRestarts` 확인을 한
번에 합니다. 활성화 뒤 실패해도 자동으로 이전 pair를 복원하지 않습니다. 따로 `systemctl start hololive-db-migrate.service`나
`systemctl restart`를 실행하지 않습니다. 단계와 결과 줄은 [배포 helper](../../../deploy/quadlet/README.md#배포-helper)에 있습니다.

```bash
helper=/opt/hololive-bot/compose/current/scripts/deploy/quadlet-redeploy-service.sh
sudo "$helper" hololive-api sha256:<api-id>
sudo "$helper" hololive-alarm-worker sha256:<worker-id> sha256:<api-id>
```

Iris와 인증 계약을 함께 전환할 때는 위 단일 서비스 호출 대신
[동시 전환의 `--quiesce`/`--resume`](../../../deploy/quadlet/README.md#iris-소비자-동시-전환)을 사용합니다.
정지 완료와 배포 완료를 구분하고 두 후보 drop-in을 모두 설치하기 전에는 peer를 기동하지 않습니다.

마지막 줄이 `[OUTCOME] deployed`가 아니면 실패입니다. `not_activated`는 실행 중 pair를 바꾸지 않았다는 뜻입니다(migration은
적용됐을 수 있음). `failed_after_activation`은 candidate가 활성화된 채 남았다는 뜻이며 helper는 자동으로 되돌리지 않습니다.
release 디렉터리를 보존하고, 스키마·admission 호환성을 승인받은 뒤에만 [rollback 런북](rollback.md)의 `--check`·`--apply`로
이전 pair를 되살립니다. 호환되지 않으면 fix-forward합니다. helper는 실패한 단계를 재시도하지 않습니다.

일반 설치의 runtime peer는 `Wants=`라 한 unit의 restart가 다른 unit을 재시작하지 않습니다. 다만 멈춰 있는 peer는 함께 시작되므로
`hololive-api.service`를 시작하면 `hololive-alarm-worker.service`도 시작됩니다. main-host collector-c는
`hololive-youtube-collector-c.service`이며 `po-central-cutover.sh`가 PO issuer를 먼저 교체·검증한 뒤 collector를
재시작합니다. infra image는 `compose.env`의 identity를 바꾼 뒤 승인된 창에서 해당 unit만 재시작합니다.

**컷오버 직후**에는 app unit이 아직 drop-in 없이 `:prod`·compose.env 값을 따르므로, 첫 배포 전에
[첫 고정](../../../deploy/quadlet/README.md#컷오버-직후-첫-고정) 절차로 세 app 서비스의 drop-in을 만듭니다.

### 4. 수용 증거

변경 시작 시각을 기록해 두고 다음을 확인합니다.

- helper의 `[OUTCOME] deployed` 줄(서비스, revision, image identity, release 디렉터리)
- 대상 container의 `StartedAt`이 변경 시작 이후이고 health가 `healthy`
- 실행 중 image identity(`sha256:` + `podman inspect --format '{{.Image}}' <container>`)와 revision label이 검토된 SHA와 일치
- `systemctl show <unit>.service -p NRestarts --value`가 `0`. 명시적 restart가 0으로 되돌리므로 이후 값은 자동
  재시작(unhealthy 정지·crash) 횟수입니다. Podman은 기동마다 새 container(`--replace --rm`)라 container `RestartCount`는
  늘 0이어서 기준으로 쓰지 않습니다.

건강한 container가 다른 image ID로 돌고 있으면 실패한 rollout입니다. 이전 pair release 디렉터리와 직전 배포 트리는
수용될 때까지 보존합니다. PO issuer(`youtube-po-b`, `youtube-po-c`, native `hololive-youtube-po.service`)의 재시작
카운트는 generation 교체 횟수이므로 `NRestarts=0` 대신 [youtube-collector 런북의 issuer 수용 기준](youtube-collector.md#issuer-generation-교체와-재시작-카운트)을 따릅니다.

> 중앙 Postgres publish 주소·인증서 mount는 `holo-postgres.container`와 app unit이 정의하고 기동 전에 `quadlet-prestart`가
> topology 정본·Tailscale identity와 대조한다. Docker 시절 live-compat overlay와 그 cert mount 검사는 컷오버로 없어졌다.

Current Go runtime services:

- `hololive-api`
- `hololive-alarm-worker`
- `youtube-collector` on the main host; `youtube-collector-b` uses the AP host wrapper.

## 필수 검사

```bash
./scripts/architecture/ci-boundary-gate.sh
```

## PostgreSQL hot path plan gate

Before releasing PostgreSQL GUC, migration, alarm dispatch, or YouTube outbox claim changes, capture fresh plan snapshots from the target environment. On production, run the script without `--analyze` in a read-only session: the two claim UPDATEs are only planned with `EXPLAIN (SETTINGS)`, never executed, and the server rejects `--analyze` there. `PGOPTIONS` makes the session read-only only when neither `DATABASE_URL` nor a connection service entry (`service` or `PGSERVICE`) sets `options`, because that value replaces `PGOPTIONS`; otherwise add `-c default_transaction_read_only=on` to that `options` value. The stack PostgreSQL rule (`.agents/skills/pg-optimization/references/diagnostics.md` in iris-stack) forbids `EXPLAIN ANALYZE` of writes, queue claims, and `FOR UPDATE` on production because `ROLLBACK` does not make external side effects or sequence use safe, and measures writes with `EXPLAIN` without `ANALYZE` or in a local fixture DB. The stats window statement holds a snapshot for the whole window, which delays vacuum cleanup, so keep the default 60 seconds on production.

```bash
PGOPTIONS='-c default_transaction_read_only=on' DATABASE_URL=... ./scripts/runtime/pg-hotpath-explain-snapshot.sh \
  --stats-window-seconds 60 \
  --output-dir artifacts/pg-hotpath-explain/$(date -u +%Y%m%dT%H%M%SZ)
```

The script accepts `DATABASE_URL` or standard `PG*` variables and does not embed secrets. `DATABASE_URL` is passed to `psql` as a process argument, so it must be a `postgres://` or `postgresql://` URI that cannot carry a secret: a URL with a `:` before its first `@` (a user info password, including one with an unencoded `/`), a control character, or a query key outside the script's libpq allowlist is rejected. The allowlist covers connection, TLS, keepalive, and session keys such as `sslmode`, `sslrootcert`, `application_name`, `connect_timeout`, and `target_session_attrs`; `password`, `sslpassword`, `oauth_*`, `scram_*`, `replication`, `sslkeylogfile`, and JDBC aliases such as `ssl` are rejected. Supply the password through `PGPASSWORD` or a password file. The YouTube claim snapshot mirrors the current runtime defaults (`LockTimeout=5m`, `ClaimFreshnessWindow=2h`). It writes:

- `invalid-indexes.txt`
- `target-indexes.txt`
- `mvcc-database-state.txt`
- `dead-tuples-autovacuum.txt`
- `claim-statement-window.txt`
- `alarm-dispatch-claim-explain.txt`
- `youtube-outbox-claim-explain.txt`

Required index contracts:

- `alarm_dispatch_deliveries` claim: `idx_alarm_dispatch_deliveries_due`,
  `idx_alarm_dispatch_deliveries_send_unit`,
  `idx_alarm_dispatch_deliveries_send_unit_due`
- `youtube_notification_outbox` claim: `idx_yno_pending_due_created_id`

The script requires the PostgreSQL 18 `pg_stat_statements` schema. It verifies that the required indexes exist, are ready and valid, and match the required table, key order, and partial predicate. PostgreSQL may choose another valid index for a small relation, so plan selection is recorded separately from the catalog contract. `claim-statement-window.txt` takes ordered `pg_stat_statements` snapshots around a bounded observation interval (60 seconds by default), joins exact `(dbid, userid, queryid, toplevel)` identities, and evaluates only the interval deltas for `calls` and `total_exec_time`. Claim classification fingerprints the runtime CTE, `UPDATE`, and `RETURNING` structure from `claim_due.sql` and `fanout_claim.sql`; alarm maintenance claims and the YouTube revive query are not performance evidence. Ambiguous fingerprints are ignored. Other statements in the window that `UPDATE` exactly one of the two claim tables with `SKIP LOCKED` are written as `unmatched` records; they never count as claims or affect the window status or the 5ms limit. When a required hot path has no matching claim but has `unmatched` records, the failure message reports how many ran per hot path as a clue for telling a stale fingerprint from missing claim traffic; maintenance statements of the same shape also appear there, so inspect the recorded SQL. The observer SQL excludes its own `hololive-pg-hotpath-stats-observer` marker explicitly. Lifetime means do not affect the gate.

The artifact retains the complete whitespace-normalized `pg_stat_statements` representative SQL for those two static runtime claims and for `unmatched` records so the fingerprint can be revalidated; it does not truncate the query text. Bind values and constants remain parameter placeholders and the owned SQL contains no dynamic comments or identifiers, so application values and credentials are not expected in this artifact. An `unmatched` record can also be a manual statement, including its SQL comments. It still exposes internal table and column names: keep it as internal operational evidence, review it before external sharing, and never place secrets in SQL comments or identifiers.

The stats window is fresh only when at least one `alarm_dispatch_deliveries` claim and one `youtube_notification_outbox` claim complete during the interval. No matching call for either required hot path, a global `pg_stat_statements` reset, entry deallocation, or a `stats_since` change or decreasing counter on a matching claim statement makes the result inconclusive and fails the gate. This also rejects a statement reset whose counters recover past the starting values before the second snapshot. Rerun during a representative active window instead of treating missing evidence as a pass. The script also fails when the fresh delta mean exceeds 5ms, any index contract is broken, or any invalid index exists. The dead-tuple snapshot records heap, TOAST, and index statistics for a fixed hot-path table list and remains review evidence rather than a fixed threshold. Executed claim plans are measured only with `--analyze` on a local fixture DB: a disposable PostgreSQL 18 with `pg_stat_statements` preloaded, the migrations applied, representative rows, and claim traffic during the stats window. There the claim UPDATEs run under `EXPLAIN (ANALYZE, BUFFERS)` inside a rolled-back transaction, and the script also fails when `Rows Removed by Filter` exceeds 1000. Without `--analyze` it prints `[SKIP]` for that check. The repository has no fixture with representative rows and claim traffic, and an `--analyze` run on empty tables passes that check trivially, so the production gate rests on the 5ms delta mean and the recorded plans.

## 계약 변경 릴리즈 규칙

- Keep provider and consumer compatible during rollout.
- Use dual-read/dual-write or additive fields for queue/envelope changes.
- Update `CONTRACT_MAP.md`, matching `contracts/*.md`, and runbook impacts before release.
- Include rollback notes for old and new contract versions.

## 릴리즈 노트

GitHub Release 제목은 tag와 같은 `r<version>`으로 쓰고, 본문은
[`v3.5.0`](https://github.com/park285/hololive-bot/releases/tag/v3.5.0)처럼 GitHub가
생성한 `What's Changed`, PR 제목·작성자·링크, `Full Changelog` 형식을 사용합니다.
수동 운영 요약이나 `CHANGELOG.md` 복사본으로 대체하지 않습니다.

`previous_tag`는 직전 게시 릴리즈로 명시하여, tag만 있고 Release가 없는 중간 버전의
변경도 누락하지 않습니다. 첫 게시 릴리즈는 직전 app tag를 기준으로 합니다.
게시·본문 수정의 단일 진입점은 `scripts/publish-release.sh`입니다. 기본 mode는
`preview`이며 대상 tag·직전 게시 릴리즈·자동 생성 본문을 확인합니다. GitHub API의
자동 생성이 실패하거나 결과가 비면 중단하며 수동 본문으로 대체하지 않습니다.
직전 게시 릴리즈와 다른 기준은 거부합니다. 첫 게시 릴리즈는 직전 app tag를
명시합니다. 기존 릴리즈 수정 시에는 해당 릴리즈의 게시 시각 이전을 기준으로 합니다.

```bash
bash scripts/publish-release.sh "$tag" "$previous_tag"
# 기존 검증과 main 반영, annotated tag push 및 게시 승인을 마친 뒤:
bash scripts/publish-release.sh "$tag" "$previous_tag" create
# 승인된 기존 본문 교정:
bash scripts/publish-release.sh "$tag" "$previous_tag" edit
```

스크립트는 tag를 생성하거나 push하지 않으며 전체 pre-push gate를 대신하지 않습니다.
`create`는 원격 annotated tag를 요구합니다. `edit`는 원문을 로컬 임시 디렉터리에
보존한 뒤 제목·본문만 수정하고 tag·첨부물·draft/prerelease 상태 보존을 확인합니다.
기록 경로는 실행 시 출력합니다. 생성 본문과 게시 결과가 다르면 실패로 보고하며
자동 재시도하지 않습니다. 임의 `gh release create/edit`나 웹 수동 요약으로 우회하지 않습니다.

배포·검증·롤백 근거는 `docs/runbook_execution/RELEASE_NOTES_TEMPLATE_20260303.md`와
`scripts/architecture/render-release-notes.sh`를 이용해 별도 운영 기록으로 유지합니다.
이 양식은 공개 GitHub Release 본문 생성기가 아닙니다.

## Smoke test

다음 확인은 이미지를 다시 빌드하거나 unit을 재시작하지 않습니다. 중앙 호스트에서 실행 중인 컨테이너를 대상으로 합니다.

```bash
sudo podman exec hololive-api ./bin/healthcheck https://127.0.0.1:30001/health
sudo podman exec hololive-api ./bin/healthcheck https://127.0.0.1:30006/health
sudo podman exec hololive-alarm-worker ./bin/healthcheck https://127.0.0.1:30007/health
sudo podman exec hololive-youtube-collector-c ./bin/healthcheck https://127.0.0.1:30025/health
```

## 관련 문서

- `../DEPLOYMENT_BASELINE.md`
- `pgo.md`
- `rollback.md`
