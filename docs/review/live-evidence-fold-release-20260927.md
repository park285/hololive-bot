# 라이브 확인·목록 접기 운영 전환 근거

- Plan: `PLN-20260927-live-evidence-fold-release`
- Decisions: `DEC-20260926-hololive-live-absence-evidence`, `DEC-20260927-live-check-slot-isolation`, `DEC-20260926-hololive-list-reply-fold-default`, `DEC-20260814-hololive-youtube-three-provider-convergence-v2`.
- 권한: 사용자 “필요한건 전부 승인”에 따른 중앙/API·worker·collector 및 AP a/b/c/d 전환. Git publication·secret 조회/변경·unrelated runtime 변경·rollback 자료 정리는 수행하지 않는다.
- Local acceptance: [진단·접기 검증](live-diagnostics-list-fold-20260927.md).

## T01 / V01 — Artifact와 전환 준비

검증한 코드 SHA는 `d0f8a2feccbb47001ba7193d0df641c9d803cbbc`다. 별도 clean worktree `fix/live-evidence-fold-20260927`은 기존 운영 mekpark 수정도 포함한다. 전체 `build-all.sh --build-only --no-bump`/local CI는 1112.125초, exit 0이었다.

`test-three-runtime-topology.sh`, `test-compose-services.sh`, `ap-deploy-version_test.sh`, `ap-host-native-deploy_test.sh`, `systemd-compose-up_test.sh` 5개 static contract → exit 0. 절차 이탈: 이 추가 5개 검사 묶음 직전 selected gate를 다시 실행하지 않았다. 기존 gate 통과를 소급 준수로 주장하지 않는다. 실제 운영 mutation은 각각 직전 release plan gate를 실행했다.

kapu의 `kapu-multiarch` builder, `--platform linux/arm64 --provenance=false --sbom=false --load`, 정확한 VERSION/REVISION 인수로 빌드했다. 중앙 원격 build는 없다.

| image | version | image ID | 빌드 초 |
|---|---|---|---:|
| hololive-api:rel-d0f8a2feccbb | 4.0.1 | sha256:40f024784593d679f926e4865471b6f9bc35f754c72fa897e67fc6845fb4f49f | 28.203 |
| hololive-alarm-worker:rel-d0f8a2feccbb | 3.2.6 | sha256:19c9f505ea334c7ef3aba35a2448ea442c823bb65c71de947f4fc3d445b6acde | 11.269 |
| hololive-youtube-collector:rel-d0f8a2feccbb | 4.0.1 | sha256:fa5b8a84e0b6d824ae0e510ed5caa46cec5feab1e00923feb18e91cc39bc9c5e | 8.921 |

`docker save | gzip | ssh docker load`는 exit 0(20.055초). 원격에서도 세 ID·linux/arm64·full revision이 로컬과 일치했다. 검증 후에만 :prod로 retag했다.

### 사전 운영 상태와 파일 범위

중앙 `100.100.1.8`, aarch64. API/worker는 `6b6f99af196ac1cec6944916304c364109e4dba4`, collector c는 `476a15009ebdef07d1243d389358a4e60b98728a`, 각 RestartCount=0이었다. API/worker의 prod/live-compat/admin-web/x-spaces overlay와 c의 main-ap 두 overlay를 보존한다.

`PGOPTIONS='-c default_transaction_read_only=on -c statement_timeout=5000'`, `SHOW transaction_read_only=on`을 먼저 확인했다. ledger는 217까지이며 checksum `d122e701adfe6a91d31c7b82bf4de0da78ce8681d678dd1a7ba0a902a8bd955e`가 로컬과 같다. 60초 이상 transaction은 0, 기존 부분 index는 존재하고 새 channel evidence 표는 없었다.

공개 tracked runtime 파일 222개를 hash 비교했다. 차이는 migrations 217~220/manifest와 중앙에서 실행하지 않는 AP test/rsync 목록 두 파일뿐이었다. HBA/ingress/Compose drift는 없었다. 중앙에는 migration 파일 5개만 61,440-byte archive로 설치하고 root:root/0644를 적용했다. AP test/rsync 파일은 중앙에서 교체하지 않았다. SOURCE_REVISION/.build-revision은 이번 운영 artifact의 source SHA로 기록했다.

### Rollback 지점

`/opt/hololive-bot/compose/rollbacks/live-evidence-fold-20260927-d0f8a2fec/`에 prechange runtime metadata, added-paths 목록, 기존 manifest/SOURCE_REVISION/.build-revision을 보관했다. `previous-deploy-files.tar` SHA-256: `89a41a6f813d8cb65b53c08dbb2c3208ee77bad5f81b295b3dfa5dba05f48123`.

| rollback tag suffix | runtime | 이전 image ID |
|---|---|---|
| rollback-live-evidence-fold-20260927-d0f8a2fec | API | sha256:b359f32097e6953ced35043afdcc7c024c634fdbfc6bdb0af73166affead6f4b |
| 동일 | worker | sha256:2d59f435643a2846eac66693b3a20254893113aabe2e23887cc7174d1653cbba |
| 동일 | collector c | sha256:33ed7de52596d1c63d3996037fc97e0a3cdba847f4e51b5d7317dd87387ce360 |

구 API rollback은 219 index 재생성과 새 kind backlog/decoder 경계를 해결한 뒤에만 가능하다. 구 migrator는 재실행하지 않는다. 이 절은 준비 증거이며 중앙/AP 재기동 성공을 뜻하지 않는다.

## T02 — 중앙 전환 결과

change_started_at은 `2026-09-26T19:34:16Z`(KST 09-27 04:34:16)다. 구 API를 정지한 뒤 이미 전송한 API image의 migration one-shot을 `run --rm --no-deps --pull never -T -e POSTGRES_ADMIN_PASSWORD=`로 실행했다. 결과는 **applied=3, skipped=78, total=81, exit 0**이었다. 218~220 checksum은 아래와 같고 원본과 일치한다.

- 218: `62ec5f9871d0964a7318b8917254be9702f088b0c0c1a719c7778bf3400ed67b`
- 219: `a447b71d77c6c697ba72b57ca3eedb4d8b372589028df20b0db2b081d6409d12`
- 220: `16d87345f5dc275f8ac2e076326b1b9c3cb839bd66e6a08ebd7dea0462999633`

`up -d --no-build --no-deps --pull never`로 API → worker → c를 전환했다(c는 기존 main-ap overlay 두 개와 --force-recreate). API가 새 table/decoder/target을 준비한 뒤 collector를 시작했다.

| runtime | StartedAt UTC | 상태 |
|---|---|---|
| API | 2026-09-26T19:34:24.753796088Z | healthy, restarts=0 |
| worker | 2026-09-26T19:36:29.732357946Z | healthy, restarts=0 |
| c | 2026-09-26T19:36:39.424392563Z | healthy, restarts=0 |

모두 준비한 image ID/full SHA와 일치했다. API의 세 endpoint(30001 /health, 30003 /internal/ready, 30006 /health), worker 30007 /health, c 30025 /ready를 실행했다. c는 instance_id=youtube-collector-c, helper=ok, first_success=true, handoff_status=PROCESSED였다.

guarded DB에서 두 canonical table 존재, 219 index 부재, runtime SELECT/INSERT/UPDATE 권한과 scraper SELECT 거부를 확인했다. 새 target은 channel_live_check 119개, video_live_check 17개였다. 과거 문서의 74는 Hololive 조회 fixture 수이고 현재 전체 collection target 수를 고정하지 않는다.

진단 명령 1회는 API healthcheck가 지원하지 않는 `--body` 인수로 실패했다. API는 healthy였으며, 실제 지원 인수로 세 endpoint를 다시 실행해 exit 0을 확인했다. Compose의 기존 정지 `hololive-x-space-login` orphan 경고는 cleanup 지시로 해석하지 않았고 해당 container를 건드리지 않았다.

## T03 — AP fleet 전환 결과

각 apply는 검증한 clean SHA에서 기존 스크립트를 실행했고 원격 빌드는 하지 않았다.

| AP | 경로 | 시작 UTC | 결과 |
|---|---|---|---|
| a / osaka | ap-host-native-deploy.sh | 2026-09-26 19:40:38 | exit 0, 41.788초, active/running, NRestarts=0 |
| d / osaka2 | ap-host-native-deploy.sh | 2026-09-26 19:41:44 | exit 0, 45.401초, active/running, NRestarts=0 |
| b / Seoul | ap-deploy.sh | 2026-09-26T19:42:46.502851827Z | exit 0, 31.917초, healthy, restarts=0 |

a/d는 version 4.0.1, linux/amd64 v1, Node v24.21.0이다. manifest source_revision은 동일 SHA이고 collector binary SHA-256은 두 호스트 모두 `037f991baf1493efeac73c601ad69d454cbe8051098f5e9daa55d4d4da26d756`이다. 실제 MainPID의 /proc/exe가 새 release의 binary를 가리켰다. release 디렉터리는 각각 `live-evidence-fold-20260927-d0f8a2fec-osaka`, `...-osaka2`이며 previous는 `20260925-grpc-security-476a15009-osaka`, `...-osaka2`를 보존한다.

b는 linux/arm64/full SHA를 확인했다. backup은 `backups/seoul-collector-20260926T194238Z`, rollback tag는 `hololive-youtube-collector:rollback-20260926T194238Z`다. AP_ROLLBACK_TAG_KEEP를 2147483647로 지정했고 기존 rollback tag 7개가 새 것 포함 8개가 되어 삭제가 없음을 확인했다.

b의 Docker image ID는 `sha256:4955532ffb6d0e44ccab4c4110b81250ecebf592ce8763e24f2b19dfdadd4ddc`로 kapu의 OCI manifest ID와 표시 형식이 달랐다. **로컬 docker-save archive의 해당 OCI manifest에서 config.digest가 이 ID와 정확히 같음**, image Config·Created·모든 RootFS diffID도 같음을 확인했다. 다른 artifact를 허용한 것이 아니다.

네 collector 모두 strict readiness의 helper=ok, first_success=true, handoff_status=PROCESSED/handoff_processed=true를 통과했다. a/d/b 배포 스크립트의 completion gate도 통과했다. native unit/host env와 기존 파일 권한 검사는 기존 스크립트가 수행했다. 일부 조회의 queue_full/discovery_truncated는 관측값 그대로이며 임의로 false로 정규화하지 않았다.

## T04 / AC01~AC03 / V02 — 운영 관측과 한계

중앙의 change_started_at 이후 error marker를 집계했다. 처음에는 대소문자 무시 `OOM`이 `rooms_loaded` 등에 맞아 INFO 3행을 오류로 잘못 셌다. 해당 행의 level/message를 확인하고 단어 경계를 사용한 정확한 검사에서 API/worker/c의 ERR/ERROR/FATAL/panic/permission denied/x509/no such file/OOM은 각각 **0**이었다. Postgres/Valkey 초기화 marker와 readiness는 관측했다. 변경 경로 밖 CLIProxy 실제 요청은 실행하지 않았으며 성공으로 주장하지 않는다.

중앙의 ingress/deunhealth/docker-proxy/Valkey/Postgres/Alloy container ID는 전환 전후 동일하다. BOT_SEE_MORE_FOLD의 명시적 environment override는 API/worker 모두 없으며 코드 계약의 기본 true를 사용한다. 원본 사용자 보호 파일 5개의 SHA-256은 최종 비교에서도 동일했다. custom template/override 또는 pending 데이터에 별도 쓰기/정리를 하지 않았다.

guarded DB에서 전환 뒤 새 kind가 각 instance에서 소비된 표본 집계:

| instance | channel_live_check | video_live_check |
|---|---:|---:|
| a | 53 | 11 |
| b | 42 | 6 |
| c | 146 | 54 |
| d | 28 | 8 |

후속 snapshot에서 channel canonical 119개 모두 **scheduled_for/effective_at/observed_at/received_at 네 시각이 270초 안**이었다. outcome은 CHANNEL_PAGE 52, UPCOMING_VIDEO 61, UNKNOWN 6이었다. check job은 ACTIVE 5/IDLE 114, 모두 최근 270초 내 완료 이력이 있었고 video job은 IDLE 17/최근 완료 17이었다. 이는 독립 check 슬롯의 운영 진행 증거이며 모든 외부 판정이 알려졌다는 뜻은 아니다.

### 외부 응답 미확정

영상 availability 17개는 `UNKNOWN(identity_missing)`이었다. 앞서 공개 종료 사실을 얻었던 영상 `H9Sutl-r6YY`를 **운영 c container의 배포된 helper/transport로 공개 player 요청 1회** 측정했다. proxy_enabled=false, playabilityStatus=LOGIN_REQUIRED, videoDetails/videoId/channelId/endTimestamp 모두 부재였다. parser 결과는 UNKNOWN/identity_missing이었다. 원시 response 전체·credentials는 출력하지 않았고 운영 DB를 직접 수정하지 않았다.

이 한 표본으로 17개 모두의 upstream 상태나 robot/private 여부를 단정하지 않는다. status-only로 종료/공개 불가를 만들지 않는 계약이므로 미확정 후보는 차단을 유지한다. 추가 인증·HTML fallback·원인 없는 재시도·부재 자동 종료를 도입하지 않았다. 실제 upstream 사실이 없는 영상의 자동 해소는 이번 운영 검증으로 입증되지 않는다.

### 실제 카카오 UI 대기

사용자 지정 테스트 chatId 또는 해당 방의 `!라이브` 실행 KST 시각이 아직 없다. 따라서 운영 예시 발송·방 열기·스크린샷은 수행하지 않았다. 최종 payload의 접기/본문 보존은 로컬 실제 renderer/egress 경로에서 입증했지만 Kakao client가 화면에서 접는 동작은 미검증이다. 기존 기본 수신방이나 과거 일회성 방을 재사용하지 않는다. 지정 후 예시 1건과 방 열기/읽음 영향을 포함한 별도 증명 항목을 재개한다.

Fallback delta: none. 새 호환 shim/retry/증거 기준 완화 없음. code release는 로컬 커밋으로 보존했고 Git publication은 수행하지 않았다. 실제 UI 대기와 upstream UNKNOWN을 기능 검증 성공에 숨기지 않는다.
