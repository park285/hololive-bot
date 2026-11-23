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

스택 전체 결정으로 버전 번호를 1.0.0부터 다시 시작했습니다. `r1.0.0`에서 루트 `VERSION`과 두 runtime `VERSION`을
모두 1.0.0으로 맞췄습니다. 기존 `v<version>`
tag와 `## v<version>` 절은 이전 번호 체계의 기록으로 남기고, 새 tag를 `v` 접두사로 만들지 않습니다. 두 체계의 번호는
겹치므로 버전 숫자만으로 신구를 비교하지 않습니다. 배포 복구 도구는 원래 릴리스 tag로 `r<version>`과 이전 `v<version>`을 모두
받고 tag 이름을 기록된 binding과 bundle의 tag ref로 검증하며, `scripts/publish-release.sh`는 새 Release를 `r<version>` tag로만 만들고, 기존 Release 수정과 비교 기준에는 이전 `v<version>`도 받습니다.

릴리즈할 때는 다음 순서를 지킵니다.

1. 루트 `VERSION`을 변경하고 각 runtime artifact를 함께 릴리즈한다면 해당 runtime의
   `VERSION`도 변경합니다.
2. `CHANGELOG.md`의 `미출시` 항목을 `## r<version> - YYYY-MM-DD` 구간으로 옮기고, 이 버전과 tag가
   붙은 최근 두 버전까지 남기고 그보다 오래된 구간은 지웁니다. 그 사이에 tag 없이 준비만 된 구간은 세지 않고 남깁니다.
3. 통합할 commit의 전체 pre-push 게이트를 통과시킵니다. 그 commit을 먼저 **승인된 브랜치 push**로 검증한다면 `FULL_PRE_PUSH=true`를 그 push에 설정합니다. 훅이 세 `VERSION` 형식 검사까지 실행하므로 직전에 같은 게이트를 수동 실행하지 않습니다. 게시 없이 검증할 때는 `FULL_PRE_PUSH=true bash scripts/ci/pre-push-gate.sh`를 사용합니다. 별도 tag push의 훅은 생략하지 않습니다.
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

### Iris webhook nonce 저장소 전환

구 Valkey 판정과 신규 PostgreSQL 판정은 한 요청을 각각 처음 보는 것으로 처리할 수 있습니다. 키 복사나 이중 쓰기 대신, 승인된 정비 창에서 다음 순서로 서명 유효창을 닫습니다. 각 collector의 정지·재개는 준비한 후보로의 교체이므로, 무엇이든 멈추기 전에 모든 입력을 갖춥니다.

1. 정지·재개 입력을 승인 기록에서 확보합니다. 하나라도 없거나 확인되지 않으면 창을 열지 않습니다. 네 도구는 직접 TLS 원점(a·b·d `100.100.1.8:5433`, c `holo-postgres:5432`)만 받으므로, 네 슬롯이 모두 이 원점에서 실행 중인지도 확인합니다.
   - d: kapu에 푼 카드 산출물 트리 `release`, 새 `hold` 경로, 다섯 인수 `candidate`([kapu 절차](../../../deploy/podman/kapu/README.md))
   - b: 다섯 인자 `B_ARGS=(<clean-candidate-tree> <trusted-receipt-dir> <b-staging-timestamp> <owner-window-id> <trusted-origin-pair-dir>)`([iris-seoul 절차](../../../deploy/podman/iris-seoul/README.md)). kapu의 영수증 디렉터리(실행 사용자 소유 0700, 심볼릭 링크 없는 절대 경로로 지정)에 b staging이 남긴 `arm64/` archive와 `b.stage.<b-staging-timestamp>.finished`(`<revision> <seal> <b-staging-timestamp>` 한 줄), 승인 주체가 쓴 정지 grant `b.current-grant`가 있어야 합니다. 승인 주체는 grant를 게시하거나 회수할 때 5단계처럼 영수증 디렉터리의 `receipts.lock`을 `flock -n`으로 잡습니다. `b quiesce`·`b resume`은 같은 잠금을 잡은 뒤 grant를 다시 검증하고 소비합니다. 영수증 파일은 모두 실행 사용자 소유 0600 일반 파일입니다. grant는 하드 링크 1개인 한 줄 `PO_B_PAIR_V1 iris-seoul <owner-window-id> quiesce <b-staging-timestamp> <tool-revision> <revision> <seal> <origin-revision> <origin-collector-id> <origin-issuer-id> <origin-digest> <expires>`입니다. 재개 grant는 `quiesce`만 `resume`으로 바꾼 같은 줄이어야 하므로 `expires`(Unix 초)는 첫 정지부터 5단계의 b 재개까지(4단계 대기와 앞선 슬롯 재개 포함)를 덮어야 합니다. 재개 grant는 `B_RESUME_GRANT` 파일로 따로 준비해 두고 5단계에서만 게시합니다.
   - a: `OLD`, `RELEASE_ID`, `HOLO_BOT_VERSION`, `ARTIFACT_DIR`, 승인 기록의 `HOLO_NATIVE_PAYLOAD_SHA256`, `I_APPROVE_OSAKA_ACTIVE_ACTIVE_DEPLOY=true`([W3 정지 창](collector.md#osaka-a-분리-정지-창))
   - c: 검증한 pair 디렉터리 `C_PAIR`, 승인된 transport SHA-256 `C_SEAL`, 승인된 SSH 대상 `C_HOST`
   - 중앙: 같은 revision으로 설치한 API·alarm-worker 후보 image ID `API_ID`, `WORKER_ID`(`sha256:<64자리>`)

   무엇이든 멈추기 전에 b staging 후보와 c 후보가 같은지 이 release의 clean checkout에서 대조합니다. b는 staging한 revision·seal과 `B_ARGS[0]` checkout으로, c는 이 checkout의 revision과 `C_PAIR`·`C_SEAL`로 재개되므로 하나라도 다르면 c·a를 재개한 뒤에야 b 재개가 거절됩니다. `c verify-pair`는 c `quiesce-verified`와 같은 기준(seal, 전송할 파일이 모두 심볼릭 링크가 아닌 일반 파일인지, 두 image archive의 체크섬과 원격이 받는 체크섬 줄 형식, arm64 manifest, 이 checkout의 VERSION 형식과 source 목록)으로 `C_PAIR`를 읽기만 하며 SSH를 열지 않습니다. `b preflight`는 같은 다섯 인자로 `b quiesce`가 grant를 소비하기 전까지의 검사(두 checkout, 영수증 디렉터리 경로·권한, staging 영수증, 정지 grant와 그 만료, 이 stage·창의 정지 요청 표식이 없는지, W2 pair, `${B_ARGS[1]}/arm64`가 심볼릭 링크 없는 일반 파일인지와 seal·체크섬, source 목록, VERSION)를 그대로 실행하고, grant를 소비하거나 SSH를 열지 않고 끝납니다. 이전 창의 `c.accepted`가 영수증 디렉터리에 남아 있으면 그 창의 b 재개 완료 영수증 `b.<이전-owner-window-id>.resume.finished`를 확인한 뒤 `ln -T -- c.accepted c.accepted.<이전-b-staging-timestamp>`로 보존하고 원래 이름을 지웁니다. 완료 영수증이 없으면 창을 열지 않습니다. 아래 검사는 `c.accepted`가 없어야 통과합니다. 정지 grant의 `expires`가 지금부터 1시간 이상 남았는지도 확인합니다. 재개 grant는 같은 `expires`를 써야 하므로, 4단계의 10분 초과 대기와 3·5단계의 앞선 슬롯 정지·재개를 마치고 b를 재개할 때까지 grant가 유효해야 합니다. 남은 시간이 1시간보다 짧으면 승인 주체에게서 새 grant를 받은 뒤 창을 엽니다.

   ```bash
   (
     set -euo pipefail
     . scripts/deploy/lib/source-revision.sh
     read -r b_revision b_seal b_change <"${B_ARGS[1]}/b.stage.${B_ARGS[2]}.finished"
     [[ $b_revision == "$(deploy_source_revision .)" && $b_revision == "$(deploy_source_revision "${B_ARGS[0]}")" ]]
     [[ $b_seal == "$C_SEAL" && $b_change == "${B_ARGS[2]}" && ! -e ${B_ARGS[1]}/c.accepted && ! -L ${B_ARGS[1]}/c.accepted ]]
     read -r -a quiesce_grant <"${B_ARGS[1]}/b.current-grant"
     (( ${#quiesce_grant[@]} == 13 && quiesce_grant[12] - EPOCHSECONDS >= 3600 ))
     scripts/deploy/po-central-cutover.sh c verify-pair "$C_PAIR" "$C_SEAL"
     PO_B_SSH_TARGET=iris-seoul scripts/deploy/po-central-cutover.sh b preflight "${B_ARGS[@]}"
   )
   ```
2. 송신을 일시 중단하고 `/webhook/iris`의 모든 유입 경로(직접 H3 포함)를 닫습니다. 재시작·자동 복구·다른 슬롯을 통한 구 API 재기동도 차단합니다. Valkey는 그대로 둡니다.
3. 검증한 복구점을 확보합니다. [writer 정지·재개 계약](../../../deploy/quadlet/README.md#writer-정지재개-계약)에 따라 collector를 d→b→a→c 순서로 정지한 뒤 [동시 전환](../../../deploy/quadlet/README.md#iris-소비자-동시-전환)으로 실행 중인 구 API·alarm-worker pair를 함께 정지하고 영속 mask합니다. 각 명령이 성공하고 출력과 영수증을 확인한 뒤에만 다음 명령으로 넘어갑니다.

   kapu에서 d는 root가, b·a·c는 이 release의 clean checkout이 실행합니다.

   ```bash
   sudo -n python3 -I -B "$release/scripts/deploy/kapu-d-cutover.py" quiesce "$hold" "${candidate[@]}"
   PO_B_SSH_TARGET=iris-seoul APPROVE_PO_B_DEPLOY=true scripts/deploy/po-central-cutover.sh b quiesce "${B_ARGS[@]}"
   scripts/deploy/ap-host-native-deploy.sh osaka --quiesce W3 "$OLD"
   PO_C_SSH_TARGET="$C_HOST" APPROVE_PO_C_DEPLOY=true scripts/deploy/po-central-cutover.sh c quiesce-verified "$C_PAIR" "$C_SEAL"
   ```

   그다음 `hololive-seoul`에서 중앙 helper를 실행합니다.

   ```bash
   sudo /opt/hololive-bot/compose/current/scripts/deploy/quadlet-redeploy-service.sh --quiesce "$API_ID" "$WORKER_ID"
   ```

   a가 출력한 nonce를 `NONCE`로, c가 출력한 `backup=` 경로(`card-po-c-<timestamp>`)를 `C_BACKUP`으로, 중앙 `[OUTCOME] quiesced`가 보존한 두 release 경로를 `API_RELEASE`·`WORKER_RELEASE`로 기록합니다. 구 API 원래 invocation의 정지 journal 시각을 마지막 드레인 시각으로 기록합니다. 다른 구 API 인스턴스가 남았거나 구 세대가 마지막으로 승인할 수 있었던 시각이 확인되지 않으면 창을 열지 않습니다.
4. 마지막 드레인 확인 이후 **10분을 초과**해 기다립니다. 서명 timestamp는 당시 시각보다 최대 5분 앞설 수 있고 그 시각 이후 5분까지 유효하므로, 5분 대기만으로는 부족합니다. 송·수신 시계 동기화와 인스턴스 간 시각 오차를 확인하고 오차만큼 여유를 둡니다. 시계가 역행하거나 구 writer가 다시 떴으면 hold를 유지하고 기준점을 다시 잡습니다.
5. 같은 정지 작업으로 중앙 pair를 재개한 뒤 collector를 c→a→b→d 순서로 재개합니다. 이번에도 각 명령의 성공을 확인한 뒤 다음으로 넘어갑니다. 먼저 `hololive-seoul`에서 실행합니다.

   ```bash
   sudo /opt/hololive-bot/compose/current/scripts/deploy/quadlet-redeploy-service.sh --resume "$API_RELEASE" "$WORKER_RELEASE"
   ```

   중앙 재개에서 가드가 writer 차단 게이트를 설치·확인하고 `--schema-only` migrator로 006을 적용한 뒤 두 후보를 엽니다([적용 조건](../../../deploy/quadlet/README.md#postgresql-직접-tls-접속)). `[OUTCOME] deployed`를 확인한 뒤 kapu에서 c·a를 재개합니다.

   ```bash
   PO_C_SSH_TARGET="$C_HOST" APPROVE_PO_C_DEPLOY=true scripts/deploy/po-central-cutover.sh c resume "$C_BACKUP"
   scripts/deploy/ap-host-native-deploy.sh osaka --resume W3 "$OLD" "$NONCE"
   ```

   c가 실제로 재개한 후보를 `$C_BACKUP`의 `candidate.revision`·`transport.sha256`에서 읽어 b staging 영수증과 같은지 승인 주체가 확인하면, 그 값으로 `c.accepted`를 게시하고 준비한 재개 grant를 게시한 뒤 b·d를 재개합니다. `b quiesce`가 정지 grant를 소비해 지웠으므로 재개 grant는 새 파일이어야 합니다. 두 파일은 같은 디렉터리의 임시 파일에 한 줄을 완성한 뒤 하드 링크로 게시하므로 기존 파일을 덮어쓰지 않고 빈 파일도 남기지 않습니다. `c.accepted`가 이미 있으면 실행 사용자 소유 0600 일반 파일이고 같은 줄일 때만 그대로 씁니다. 같은 창에서 재개 grant 게시가 실패한 뒤 다시 실행하는 경우입니다. `b resume`이 이미 재개 grant를 소비했으면(`b.<owner-window-id>.resume.used`나 `b.resume.<b-staging-timestamp>.requested`가 있으면) `c.accepted`가 있든 없든 아무것도 게시하지 않고 멈춥니다. 이 확인과 두 게시는 영수증 디렉터리의 `receipts.lock`을 잡은 채 하고, `b quiesce`·`b resume`도 grant를 소비하는 동안 같은 잠금을 잡으므로, 다른 실행이 잠금을 쥐고 있으면 기다리지 않고 실패합니다. 내용이 다르거나 재개 grant가 이미 있으면 기존 파일을 보존한 채 b 재개를 멈추고 누가 언제 썼는지 대조합니다. 블록 안의 명령 하나라도 실패하면 뒤의 b·d 재개는 실행되지 않습니다.

   ```bash
   (
     set -euo pipefail
     umask 077
     publish_new() {
       local target=$1 tmp status=0
       tmp=$(mktemp "$target.XXXXXX") || return 1
       { cat >"$tmp" && [[ $(wc -l <"$tmp") == 1 ]] && ln -T -- "$tmp" "$target"; } || status=1
       rm -f -- "$tmp"
       return "$status"
     }
     c_revision="$(ssh "$C_HOST" "sudo -n cat '$C_BACKUP/candidate.revision'")"
     c_seal="$(ssh "$C_HOST" "sudo -n cat '$C_BACKUP/transport.sha256'")"
     read -r b_revision b_seal _ <"${B_ARGS[1]}/b.stage.${B_ARGS[2]}.finished"
     [[ $c_revision == "$b_revision" && $c_seal == "$b_seal" ]]
     accepted="${B_ARGS[1]}/c.accepted"
     (
       flock -n 9
       for consumed in "b.${B_ARGS[3]}.resume.used" "b.resume.${B_ARGS[2]}.requested"; do
         [[ ! -e ${B_ARGS[1]}/$consumed && ! -L ${B_ARGS[1]}/$consumed ]]
       done
       if [[ -e $accepted || -L $accepted ]]; then
         [[ -f $accepted && ! -L $accepted && $(stat -c '%u:%a' "$accepted") == "$EUID:600" ]]
         cmp -s -- "$accepted" <(printf '%s %s\n' "$c_revision" "$c_seal")
       else
         printf '%s %s\n' "$c_revision" "$c_seal" | publish_new "$accepted"
       fi
       publish_new "${B_ARGS[1]}/b.current-grant" <"$B_RESUME_GRANT"
     ) 9>>"${B_ARGS[1]}/receipts.lock"
     PO_B_SSH_TARGET=iris-seoul APPROVE_PO_B_DEPLOY=true scripts/deploy/po-central-cutover.sh b resume "${B_ARGS[@]}"
     sudo -n python3 -I -B "$release/scripts/deploy/kapu-d-cutover.py" resume "$hold" "${candidate[@]}"
   )
   ```

   `--check`로 `006_epoch4_bot_webhook_nonces.sql`까지 양쪽 원장·권한·실물 검사를 통과하는지 확인합니다. 준비 상태와 실행 revision, 구 세대 차단 상태를 확인한 뒤 ingress와 송신을 엽니다. 같은 서명은 한 번만 승인되고 판정 불능은 503이어야 합니다. 운영 계정·ACL을 임시로 넓히거나 기존 원장 및 cache 키를 지우지 않으며, Valkey의 nonce를 조회하거나 복원하지 않습니다.

**롤백:** 이 전환 뒤에는 구 세대(Valkey nonce 판정)를 다시 열지 않으며, 도구에도 그 경로가 없습니다. `--quiesce`는 설치된 source revision의 image만 후보로 받고, `--resume`은 같은 정지 작업이 보존한 후보만 엽니다. hold가 남아 있는 동안 단일 `rollback --apply`는 거부됩니다. 5단계의 중앙 `--resume`이 `[MIGRATE]` 전에 사전 검사에서 실패하면 hold를 유지한 채 원인을 해결하고 같은 보존 pair로 다시 재개합니다. release 디렉터리에 `migration/`이 남았는데 `migration/completed`가 없으면(`previous migration failed or its outcome is unknown; not retrying`), 또는 `opening/`이 남았으면(`previous release opening is incomplete`) 결과 불명이므로 도구가 재시도를 거부합니다. 이때는 hold와 두 표식을 그대로 두고, DB 원장(`--check`), `hololive-db-migrate` 실행 기록, 두 writer의 job·실행 세대를 대조하는 별도 승인 복구로 넘깁니다. 표식을 지우고 같은 명령을 다시 실행하지 않습니다. 재개 뒤 신규 API에 결함이 있으면 2단계처럼 유입을 다시 닫고, 006 원장을 쓰는 수정 revision으로 1~5단계를 다시 수행해 교체합니다. 신규와 수정 후보는 같은 PostgreSQL nonce 원장을 쓰므로 4단계의 대기는 필요 없습니다. 휘발·축출 가능한 cache 설정을 쓰는 구 코드로 되돌리지 않으며, DB 원장·migration을 되돌리거나 삭제하지 않습니다. PG 복구로 최신 nonce의 보존 여부가 불명이면 2~4단계와 같은 격리 창이 필요합니다.

### 19.2.1 의존성 수용 기준

- 다섯 모듈을 각각 `GOWORK=off`로 검사합니다. 릴리스 빌드는 형제 checkout이 아니라 각 모듈의 `go.mod`와 `go.sum`에 고정된 공개 의존성을 사용합니다.
- Hololive는 `shared-go/postgres`의 `pgxdb`를 쓰지만 `pgstore.Store`는 쓰지 않습니다. API는 `bot_webhook_inbox`와 `bot_webhook_heads`를 소유합니다. 격리 PostgreSQL에서 `internal/planes/bot/internal/durability`의 `TestInboxTransitionsMoveTheOrderingHeadOnlyWhenTerminal`로 재시도 중 head 유지와 종단 뒤 다음 행 처리를 확인합니다. nonce 중복·오류 처리, Gemini 일정표 처리, collector/PO issuer와 worker 회귀도 확인합니다.
- 공용 `pgstore`를 실제로 쓰는 소비자만 [postgres/v1.0.3 전환·롤백 조건](https://github.com/park285/shared-go/blob/postgres/v1.0.3/README.md#inbox-수락-순서)을 따릅니다. `(scope, ordering_key)`별 수락 ID FIFO, 시퀀스 증가폭 1·`CACHE 1`·`NO CYCLE`, 기존 인덱스 보존과 `idx_iris_webhook_inbox_head_by_id` 추가가 전제입니다. 수락·claim·만료 복구를 멈추고 진행 중 트랜잭션이 끝난 뒤 미종단 `pending`·`processing`의 기존/신규 head 불일치 또는 비선두 processing이 0행이어야 합니다. 두 claim 구현을 섞지 않습니다. 이 조건을 Hololive 테이블에 적용하거나 운영 변경 승인으로 해석하지 않습니다.
- 빌드 호스트에서 같은 clean revision과 `19.2.1`로 API·alarm-worker·collector/PO 산출물을 식별합니다. 중앙 API·alarm-worker는 아래 절차로 교체하고, AP는 슬롯별 런북과 승인 범위에 따라 collector·issuer를 한 쌍으로 수용합니다. 소스 게시와 운영 수용은 별도 증거로 남기며 새 DB migration, DB 재시작이나 승인 없는 rollback은 수행하지 않습니다.

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
이미 `--schema-only` migrator를 수용했고 host 정의가 바뀌지 않는 앱 갱신에는 `--source-only <현재 설치된 40자리 SHA>`를 사용합니다. 빌드 호스트에서 검증한 annotated tag의 commit과 중앙의 고유 root 소유 staging checkout이 일치하는지 확인한 뒤 **그 checkout의** `scripts/deploy/sync-opt-current.sh`를 실행합니다. 설치된 구 helper를 수정하거나 새 helper만 따로 복사해서 실행하지 않습니다. `--application-only`는 관리자 bootstrap migrator(`--configure-login-roles`)가 설치된 창에서 카드 migrator로 전환할 때만 씁니다.

`--source-only`는 writer lock 아래 설치 stamp·구 소스 전체와 후보 계보·깨끗한 checkout을 대조합니다. 아카이브를 풀기 전에 Git 객체의 경로·모드와 예약된 stamp 경로를 검사하며 기존 일반 파일의 내용 변경만 허용합니다. 설치 coordinator와 실행 의존성은 루트까지 경로 전체가 root 소유이고 그룹·기타 쓰기, 추가 POSIX ACL, 심볼릭 링크나 일반 파일의 하드 링크가 없어야 합니다. 이 모드는 호스트 권한을 조정하거나 Quadlet·방화벽·runtime 경로를 재설치하거나 daemon-reload하지 않습니다.

구 `current.bak-*`를 검증·보존하고 stamp를 먼저 `incomplete`로 내구성 있게 바꿉니다. 검토된 파일(이번에는 helper 자신과 CHANGELOG 포함)을 교체한 뒤 전체 후보와 대조하고 온전한 SHA를 마지막에 게시합니다. 중간 실패 시 혼합 트리에 구 이미지나 새 이미지를 활성화할 수 없습니다. 자동 재실행·복원하지 말고 실제 파일·backup·기존 pair를 대조합니다. 이후 각 앱 교체는 아래 설치된 coordinator가 수행합니다.

API·worker는 배포 트리의 배포 helper로 교체합니다. helper가 이전 pair 보존, candidate 검사, 활성화 전
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

런타임 peer는 `Wants=`라 한 unit의 restart가 다른 unit을 재시작하지 않습니다. 다만 멈춰 있는 peer는 함께 시작되므로
`hololive-api.service`를 시작하면 `hololive-alarm-worker.service`도 시작됩니다. main-host collector-c는
`hololive-collector-c.service`이며 `po-central-cutover.sh`가 PO issuer를 먼저 교체·검증한 뒤 collector를
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
카운트는 generation 교체 횟수이므로 `NRestarts=0` 대신 [collector 런북의 issuer 수용 기준](collector.md#issuer-generation-교체와-재시작-카운트)을 따릅니다.

> 중앙 Postgres publish 주소·인증서 mount는 `holo-postgres.container`와 app unit이 정의하고 기동 전에 `quadlet-prestart`가
> topology 정본·Tailscale identity와 대조한다. Docker 시절 live-compat overlay와 그 cert mount 검사는 컷오버로 없어졌다.

Current Go runtime services:

- `hololive-api`
- `hololive-alarm-worker`
- `collector` on the main host; `collector-b` uses the AP host wrapper.

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

The script requires the `pg_stat_statements` schema of PostgreSQL 18 or later. It verifies that the required indexes exist, are ready and valid, and match the required table, key order, and partial predicate. PostgreSQL may choose another valid index for a small relation, so plan selection is recorded separately from the catalog contract. `claim-statement-window.txt` takes ordered `pg_stat_statements` snapshots around a bounded observation interval (60 seconds by default), joins exact `(dbid, userid, queryid, toplevel)` identities, and evaluates only the interval deltas for `calls` and `total_exec_time`. Claim classification fingerprints the runtime CTE, `UPDATE`, and `RETURNING` structure from `claim_due.sql` and `fanout_claim.sql`; alarm maintenance claims and the YouTube revive query are not performance evidence. Ambiguous fingerprints are ignored. Other statements in the window that `UPDATE` exactly one of the two claim tables with `SKIP LOCKED` are written as `unmatched` records; they never count as claims or affect the window status or the 5ms limit. When a required hot path has no matching claim but has `unmatched` records, the failure message reports how many ran per hot path as a clue for telling a stale fingerprint from missing claim traffic; maintenance statements of the same shape also appear there, so inspect the recorded SQL. The observer SQL excludes its own `hololive-pg-hotpath-stats-observer` marker explicitly. Lifetime means do not affect the gate.

The artifact retains the complete whitespace-normalized `pg_stat_statements` representative SQL for those two static runtime claims and for `unmatched` records so the fingerprint can be revalidated; it does not truncate the query text. Bind values and constants remain parameter placeholders and the owned SQL contains no dynamic comments or identifiers, so application values and credentials are not expected in this artifact. An `unmatched` record can also be a manual statement, including its SQL comments. It still exposes internal table and column names: keep it as internal operational evidence, review it before external sharing, and never place secrets in SQL comments or identifiers.

The stats window is fresh only when at least one `alarm_dispatch_deliveries` claim and one `youtube_notification_outbox` claim complete during the interval. No matching call for either required hot path, a global `pg_stat_statements` reset, entry deallocation, or a `stats_since` change or decreasing counter on a matching claim statement makes the result inconclusive and fails the gate. This also rejects a statement reset whose counters recover past the starting values before the second snapshot. Rerun during a representative active window instead of treating missing evidence as a pass. The script also fails when the fresh delta mean exceeds 5ms, any index contract is broken, or any invalid index exists. The dead-tuple snapshot records heap, TOAST, and index statistics for a fixed hot-path table list and remains review evidence rather than a fixed threshold. Executed claim plans are measured only with `--analyze` on a local fixture DB: a disposable PostgreSQL 19 with `pg_stat_statements` preloaded, the migrations applied, representative rows, and claim traffic during the stats window. There the claim UPDATEs run under `EXPLAIN (ANALYZE, BUFFERS)` inside a rolled-back transaction, and the script also fails when `Rows Removed by Filter` exceeds 1000. Without `--analyze` it prints `[SKIP]` for that check. The repository has no fixture with representative rows and claim traffic, and an `--analyze` run on empty tables passes that check trivially, so the production gate rests on the 5ms delta mean and the recorded plans.

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
sudo podman exec hololive-collector-c ./bin/healthcheck https://127.0.0.1:30025/health
```

## 관련 문서

- `../DEPLOYMENT_BASELINE.md`
- `pgo.md`
- `rollback.md`
