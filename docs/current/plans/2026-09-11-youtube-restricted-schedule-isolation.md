# YouTube 접근 제한 영상의 일정 수집 격리

**Decisions:** `DEC-20260911-youtube-restricted-schedule-isolation` (governing), `DEC-20260829-hololive-live-start-evidence-admission` (constraint), `DEC-20260826-hololive-youtubejs-bounded-read-retry` (constraint)

## Execution capsule

**Goal:** 접근 제한 영상의 일정 부재가 같은 채널의 정상 방송과 채널 정보 수집을 중단시키지 않도록 수정한다.
**Context:** collector의 channel RPC가 live와 metadata 작업을 함께 수집하며 하나의 UPCOMING 시각 부재를 전체 parser_drift로 전파한다.
**Constraints:** 시각 추정·과거 시각 재사용·추가 provider·새 의존성·운영 DB 변경·메시지 재전송을 하지 않는다. 운영 배포와 Git 발행은 별도 권한 범위다.
**Evidence:** 2026-09-11 공개 player/watch/next/ANDROID 응답의 ijnXjcoquTY는 멤버십 UNPLAYABLE이며 두 기계가독 시각이 없다. 같은 채널의 yK62q7V_JvE는 공개 목록에서 LIVE이지만 DB는 UPCOMING이다.
**Success:** 접근 제한만 격리하고 유효 영상은 PARTIAL observation으로 반영한다. 미확인 영상은 종료·취소 근거가 되지 않으며 metadata 수집은 live 요청을 하지 않는다. 회귀·typecheck·관련 계약 검사가 통과한다.
**Output:** collector helper/RPC/Go adapter, 회귀 테스트, 운영·계약 문서와 이 계획의 검증 근거.

## 구현 범위

Collector-owned helper가 raw player identity·boolean·시각 검증을 계속 소유한다. `UNPLAYABLE`이며 관측된 `playerLegacyDesktopYpcOfferRenderer`가 있는 입력에서 UPCOMING/live-content가 확인되고 시각이 없을 때만 `access_restricted`로 분류한다. 알려지지 않은 UNPLAYABLE, 잘못된 schema/identity/time, 모순된 시각, transport 실패는 기존 typed failure를 유지한다.

Channel RPC는 필수 `kind=live|metadata`를 받아 요청에 필요한 수집만 수행한다. live 응답은 제한된 영상 ID·채널 ID·사유 목록을 포함하고 유효 sessions와 분리한다. 제한 목록은 최대 32개이며 중복·교차 채널·정상 목록과의 중첩을 거부한다. helper는 공개 식별자와 고정 사유만 구조화된 WARN에 기록한다.

Go adapter는 제한 목록이 존재하면 live observation의 completeness를 PARTIAL로 설정한다. 알려진 영상의 positive만 전달하고 제한 영상의 기존 canonical 상태는 유지한다. 정상 poll을 완료하여 다음 scheduled slot에서 다시 관측하며 같은 slot 무한 defer나 추가 재시도를 만들지 않는다. 기존 `youtube_collection_completeness_total`이 불완전성을 기록한다. PARTIAL은 완전 수집이나 영상 부재 근거가 아니다. 공용 payload generation과 DB schema는 변경하지 않는다.

작업 소유자는 본 세션이며 source worktree는 `.tmp/youtube-schedule-recovery-20260911/stack/hololive-bot`, branch는 `fix/youtube-schedule-isolation-20260911`이다. 다른 checkout의 변경을 가져오거나 통합하지 않는다.

### T01 Helper와 RPC의 수집 범위 분리

raw metadata adapter에 제한 판정을 추가하고 channel 요청 kind·응답 제한 목록을 JS/TS/Go에 일치시킨다. metadata는 streams/player를 호출하지 않고 live는 about을 호출하지 않는다. AC01과 V01을 충족한다.

### T02 불완전 관측의 저장 경계 검증

Go channel runner가 kind를 지정하고 제한 응답을 엄격히 검증한 뒤 PARTIAL observation을 만든다. 정상 LIVE/upcoming의 반영, 제한 행 보존, partial absence의 종료 금지를 검증한다. T01 이후 수행하며 AC02와 V02를 충족한다.

### T03 문서와 실제 응답 검증

기존 전체 실패 계약을 관측된 접근 제한 예외로 한정해 갱신한다. 로컬에서 실제 공개 채널을 읽고 helper/RPC/collector 관련 검사와 스택 영향 계약 검사를 완료한다. 운영 배포 절차와 남은 외부 제한을 보고한다. T01·T02 이후 수행하며 AC03과 V03을 충족한다.

### AC01 제한 영상과 독립 수집

유효 예정 두 개·제한 한 개·LIVE 한 개가 섞인 응답에서 정상 세 개를 유지하고 제한 한 개의 ID/사유를 보존한다. 제한 영상에도 정확한 시각이 있으면 정상 경로를 사용한다. metadata는 streams/player 호출 없이 성공하며 미지의 drift는 실패한다.

### AC02 저장과 종료의 정확성

제한 목록이 있으면 PARTIAL observation으로 정상 영상만 발행한다. 제한 영상만 있는 빈 sessions도 PARTIAL이며 complete-empty로 처리하지 않는다. 정상 영상과 제한 ID 중첩·중복·다른 채널·미지 사유를 거부한다. 기존 LIVE는 partial absence 때문에 종료되지 않는다.

### AC03 검증과 운영 설명

기존 dependency 버전과 공용 payload generation을 유지하고 로컬 회귀·typecheck·계약 검사를 통과한다. 공개 원천의 숨겨진 일정은 미확인으로 남기며 운영 배포·재전송을 수행한 것으로 보고하지 않는다.

### V01 Helper 검사

`node --test --test-concurrency=1 src/live-metadata.test.mjs src/fetch-channel.test.mjs src/rpc-validation.test.mjs`를 먼저 실행한다. 이후 helper 전체 `npm test`와 `npm run typecheck`를 실행한다.

### V02 Go와 consumer 검사

collector 모듈의 `go test ./internal/runtime/youtubejs ./internal/runtime/youtubejscollector ./internal/runtime/collectorruntime` 및 관련 package의 race 검사를 실행한다. 공용 live reducer의 partial/positive 보존 회귀 검사를 실행하며 필요하면 격리 DB harness로 persist를 확인한다.

### V03 계약과 실제 입력 검사

`bash scripts/architecture/check-contract-map.sh`, stack의 `bash tools/checks/check-stack-retry-contract.sh`, `bash tools/checks/check-stack-worker-contract.sh`, `bash tools/checks/check-decision-catalog.sh`를 실행한다. 실제 채널 probe는 kapu의 최대 60초 transient cgroup에서 읽기 전용으로 수행하고 task-owned process가 종료됐는지 확인한다. 최종 diff를 검토한다.

## 검증 근거

조사 시각 2026-09-11 13:40 KST. 기존 helper 회귀 29개가 통과했으며 실제 channel 입력에서 전체 parser_drift를 재현했다. 중앙 DB의 `transaction_read_only=on`을 확인한 뒤 조회했으며 live job 마지막 완료는 2026-09-10 09:57:38 UTC였다. Holodex의 해당 video 조회는 404였고 공식 일정에도 없었다. collector의 공식 YouTube Data API 키는 구성되어 있지 않아 그 경로의 복구 여부는 미검증이다.

2026-09-11 14:06 KST 수정본의 실제 공개 channel RPC는 HTTP 200으로 UPCOMING `Spraq2szAMA`/`dAOcenyS3n8`의 엄격한 시각과 LIVE `yK62q7V_JvE`를 반환했다. `ijnXjcoquTY`는 정상 sessions에서 분리되고 `access_restricted` 사유와 ID를 응답 및 구조화 WARN에 보존했다. live는 browse 두 번·player 세 번, metadata는 browse 두 번만 호출했고 player 요청은 없었다. 운영 DB에는 쓰지 않았다.

Helper 전체 `npm test` 148개와 `npm run typecheck`가 통과했다. Go collector 전체 `go test ./...`가 통과했고 youtubejs/youtubejscollector/collectorruntime/live reducer 네 package의 race 검사도 통과했다. 새 회귀는 제한 행만 있는 PARTIAL, 유효 방송 보존, unknown identity/reason·중복·중첩·상한 거부, 다음 poll 재확인, PARTIAL에서 정상 LIVE/명시적 ENDED 처리와 제한 영상의 상태·last_seen 보존을 확인했다. 기존 opt-in 실제 Go↔Node roundtrip test의 누락된 MaxInflight 설정을 보완한 뒤 같은 채널을 사용한 공개 roundtrip도 3.245초에 통과했다. 모든 실제 원천 probe는 60초 이하 transient cgroup에서 종료됐다.

수정 package의 golangci-lint는 0 issues, NilAway는 exit 0이다. 기존 contract-map/runbook-coverage/collector-hardening 검사와 스택 retry/worker 계약 검사가 통과했다. worker 계약은 14개 invalid fixture 거부, 20 metrics 및 기존 hash 일치를 확인했다. dependency와 lockfile은 변경하지 않았으며 2026-09-11 npm registry와 upstream release에서 youtubei.js 최신 정식 버전이 현재와 같은 18.0.0임을 확인했다. 격리 worktree의 helper 의존성은 기존 lockfile로 `npm ci --offline --ignore-scripts`하여 준비했다.

Fallback delta: 관측된 paid-access schedule 부재에 한해 PARTIAL observation을 발행하는 예외 1개. 시각 추정·stale cache 재사용·추가 provider·추가 transport retry·수동 재전송은 없다. 기존 payload generation과 consumer 구현은 그대로이며 source observation의 PARTIAL 및 helper WARN이 미확인 상태를 나타낸다. 운영 배포와 그 이후 실제 DB·알림 상태 검증은 이 구현 검증의 완료 주장에 포함하지 않는다.

## 인계

기존 재시도와 알림 중복 방지 계약은 유지한다. 구현 검증 완료 후 배포가 필요하면 검증된 collector binary/helper를 함께 교체하는 네 slot의 대상·검증·rollback을 구체화하고 현재 승인 범위를 확인한다.
