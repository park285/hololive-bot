# 방송 시간 표시와 방송 시각 확정 인계 - 2026-10-18

**상태** (2026-10-18 네 번째 세션, 08:18Z 재개·감시 중)
- PR #657(`427eebc19`)을 기준으로 I5 실행을 이어받았습니다. 원 checkout의 다른 작업은 건드리지 않고 `hololive-bot-stream-duration`을 사용합니다.
- 운영 `hololive-api`는 같은 `f089d9a64` image를 유지합니다. 08:02Z conflict 중단 조건으로 K=0 복귀를 검증한 뒤, 사용자 승인에 따라 08:17:50Z K=200으로 API만 재생성했습니다. healthy·재시작 0과 health/readiness 통과를 확인했습니다.
- I5는 07:21Z 9,745건 투입 후 08:18Z 현재 확정 296건·격리 충돌 1건·pending 9,448건입니다. 충돌 행의 원래 시각은 보존했고 동일 중단 조건으로 나머지를 처리합니다.
- I4 기준 1은 최초 신규 종료 30건의 1시간 관찰 창이 닫힌 뒤 판정합니다. 08:18Z에는 후보 9건, 관찰 창 종료 5건입니다.
- I8은 이번 세션에서 사용자가 **보류**를 선택했습니다. 카카오 방 접근·명령 발송·화면 열기는 수행하지 않습니다.

## 1. 승인 범위

- 사용자가 승인한 작업:
  - `!방송기록`·`!라이브`에 방송 시간 표시 구현 ("최대한 정확하게 표시될 수 있는 방향")
  - 운영 DB 실제 조회. 읽기 전용으로만 수행했습니다.
  - 이득이 있을 때의 서브에이전트 사용
  - 종료 시각 근본 해결과 기존 데이터 보정 계획 수립·검토
- 로컬 커밋 (2026-10-18 사용자 승인: "로컬 커밋 승인")
  - 대상: 이 worktree의 `feat/stream-duration` 브랜치
  - 포함: 릴리스 A·B 분할 커밋과 이후 이슈 작업의 로컬 커밋
  - 제외: push, PR, tag, 로컬 `main` 합류, 다른 checkout·worktree의 커밋
- 승인되지 않은 작업 (별도 승인 필요):
  - push·PR·tag, 로컬 `main` 합류
  - migration 운영 적용, 배포·재시작 (`hololive-bot-ops`)
  - 운영 데이터 쓰기. 계획 6단계 backlog 보정 약 9,700행과 롤백 SQL 실행을 포함합니다.
  - YouTube 대량 조회. backlog 보정에 수반됩니다.
- 2026-10-18 사용자 지시: "모든 이슈를 해결해야지". 다음 세션은 7절의 이슈 전체(I1~I8)를 해결합니다.
  - 이 지시는 코드·문서·시험 작업과 읽기 전용 조사에 적용됩니다.
  - 위 "승인되지 않은 작업"은 각 시점에 대상과 효과를 밝히고 별도로 승인을 받습니다.
- 2026-10-18 세 번째 세션 사용자 승인: "적대적리뷰후 커밋 푸쉬등 전부승인". 적대적 검토와 수정, push·PR·병합, 운영 배포와 migration 322·323·324 적용을 이 승인으로 진행했습니다.
- 2026-10-18 06:00Z 사용자 결정: I4를 1~2일 대기 대신 데이터로 판정하고 I5를 지금 시작하며("지금 I5 시작"), 실행은 다음 세션에서 합니다("대신 핸드오프해서 다른세션에서").
  - 이 결정으로 승인된 것: `compose.env`에 `YOUTUBE_PLANE_TIME_FINALIZATION_BACKLOG_BATCH_SIZE=200` 추가, `hololive-api` recreate, 계획 "운영 SQL 초안"의 backlog 투입 SQL(약 9,700행)과 이에 따른 YouTube 영상 확인 증가. 중단 조건에서 K=0으로 되돌리는 것도 포함합니다.
  - 별도 보고·승인이 필요한 것: 시각 롤백 SQL 실행, 계획에 없는 운영 데이터 쓰기, 카카오 방 사용.
- 2026-10-18 네 번째 세션: 사용자가 위 인계 작업의 실행과 docs PR squash 병합을 다시 요청했습니다. I8 선택 질문에는 **보류**를 선택했습니다. 이는 I5·I4 승인 철회가 아니며 카카오 화면만 제외합니다.

## 2. 작업 트리 상태

| 항목 | 값 |
|---|---|
| worktree | `/home/kapu/work/iris-stack/hololive-bot-stream-duration` |
| branch / HEAD | `feat/stream-duration`. 생성 시 `6ecd2ed44`에서 시작했습니다. 세 번째 세션에서 병합된 main(`7e60b2061`) 위로 rebase해, 남은 로컬 커밋은 문서 커밋뿐입니다. 게시한 코드는 PR #651~#655입니다. 아래 릴리스 A·B 목록은 #651·#652의 내용입니다. |
| 원 checkout | `/home/kapu/work/iris-stack/hololive-bot` (`main` `6ecd2ed44`). 다른 작업의 미커밋 변경이 약 100개 있습니다(`CHANGELOG.md`, alarm-worker 시험 등). 이 작업과 무관하므로 건드리지 않습니다. |
| 다른 worktree | `hololive-bot-build`, `-dead-audit`, `-docs2`, `-remaining-limits`. 모두 다른 작업 소유이므로 건드리지 않습니다. |
| 임시 자원 | 게시 gate·image build는 `/tmp/stream-duration-scratch/gate/`의 격리 작업 공간(detached worktree, `shared-go` `6ed91f9`·`iris-client-go` `9621f18` clone)에서 실행했고, 작업이 끝난 뒤 제거합니다. dbtest PostgreSQL 컨테이너 잔여는 없습니다. |

변경 파일은 경로 접두어 `hololive/hololive-api/internal/planes/bot/internal/`을 `bot/`로 줄여 적습니다.

**릴리스 A (코드, 먼저 배포)**
- `!방송기록`
  - 조회문: `bot/command/handlers/queries/broadcast_history_repository_0179_01.sql`, `0242_02.sql`. `youtube_live_reconciliation_heads`를 LEFT JOIN해 `h.last_live_positive_at`을 읽습니다.
  - `bot/command/handlers/handlercore/command.go`: `BroadcastHistoryEntry.LastLivePositiveAt`
  - `bot/command/handlers/broadcast_history_repository.go`: scan
  - `bot/command/handlers/handler_broadcast_history.go`: `maxBroadcastEndUncertainty`(5분), `verifiedBroadcastEndedAt`, `broadcastHistoryFallbackTime`(`종료`/`예정`/`마지막 확인` 표기)
  - `bot/adapter/messaging/formatter/formatter_broadcast_history.go`: `broadcastHistoryTimeSpan`, `broadcastHistoryEndTime`
  - 시험: `handler_broadcast_history_test.go`, `broadcast_history_repository_db_test.go`, `formatter_broadcast_history_test.go`
- 공용
  - `bot/adapter/messaging/formatter/formatter_duration.go`: `formatBroadcastDuration`, 분 단위 내림
  - 시험: `formatter_duration_test.go`
- `!라이브`
  - `bot/adapter/messaging/formatter/formatter_streams.go`: `liveStreamView.Elapsed`, `liveElapsed`(`AsOf − StartedAt`)
  - `formatter_live_query.go`: `result.AsOf` 전달
  - `formatter_streams_directory_test.go`: 시그니처 변경을 따라 수정
- 템플릿
  - `hololive/hololive-shared/internal/service/template/sampledata/template_sample_data_core.go`: `Elapsed` 표본
  - `hololive/hololive-shared/pkg/service/template/renderer_message_layout_test.go`, `renderer_readability_test.go`: fixture에 `Elapsed: ""`
  - `renderer_seed_render_test.go`의 `fieldElapsed` 상수와 fixture 변경

**릴리스 B (A 배포 확인 뒤)**
- `hololive/hololive-api/scripts/migrations/322_live_streams_elapsed.sql`과 `manifest.txt`의 `183 322_live_streams_elapsed.sql` 줄
  - 220 표준 본문과 같을 때만 바꾸는 CAS UPDATE입니다. old md5는 `b306166c7ae45035185ef79b21cea25f`로 운영 전역 본문과 같고, 채널 override는 없습니다. new md5는 `abab0db8d544c64ee59c98fa0c1d6a3d`입니다.
- `hololive/hololive-dbtest/live_streams_elapsed_migration_test.go`
- `bot/adapter/messaging/formatter/formatter_live_query_test.go`: seed 본문을 쓰므로 322가 있어야 통과합니다.
- `renderer_seed_render_test.go`의 `TestSeedTemplates_LiveStreamsAppendElapsedToChannelLine` 함수
- 나누는 이유: CONVENTIONS의 "바이너리 선배포가 필요한 개정은 PR을 나눈다"에 해당합니다. 322 본문은 이전 바이너리에 없는 `.Elapsed`를 참조하므로, 322가 먼저 적용되면 이전 바이너리에서 `!라이브`가 렌더링에 실패합니다.
- 롤백 주의: 322 적용 뒤 A보다 이전 버전으로 되돌리면 전역 본문을 220 본문으로 되돌려야 합니다.

**문서 (커밋 대상)**
- `docs/current/plans/2026-10-18-broadcast-time-finalization.md`
- 이 문서

## 3. 완료된 검증

모두 마지막 코드 편집 뒤에 실행했습니다. 그 뒤로 바뀐 것은 문서뿐입니다. worktree 루트에서 실행했습니다.

- 시험 (모두 ok)
  - `go test -count=1 ./hololive/hololive-api/internal/planes/bot/... ./hololive/hololive-api/internal/migrationrunner/... ./hololive/hololive-api/cmd/db-migrate/... ./hololive/hololive-shared/pkg/service/template/... ./hololive/hololive-shared/internal/service/template/...`
  - `go test -count=1 ./hololive/hololive-dbtest/...`: ok (97.9초)
  - `go test -race -count=1`: formatter와 handlers 패키지 ok
- 정적 검사
  - `/home/kapu/go/bin/golangci-lint run -c .golangci.yml <영향 패키지>`: 0 issues. `scripts/ci/go-tooling.sh`가 고정한 v2.14.0입니다. PATH의 2.13.1은 Go 1.27.2 export data를 읽지 못합니다.
  - NilAway (`go vet -vettool=<scripts/ci/nilaway-models 빌드 결과>`, formatter·handlers·sampledata): exit 0, 진단 0
  - `bash scripts/architecture/check-migration-manifest.sh`: OK
- 릴리스 분할 확인: 임시 worktree에 릴리스 B를 뺀 변경만 적용했습니다. build, formatter·handlers·template·migrationrunner 시험, manifest 검사가 모두 통과했습니다.
- 실행하지 않은 검증 (게시 단계에서 필요):
  - `./build-all.sh --build-only --no-bump`
  - `scripts/ci/pre-push-gate.sh`
  - 전체 `local-ci.sh`
  - 실제 카카오톡 화면 확인. 지금은 `kakaoformat.Render` 결과 문자열 시험으로만 확인했습니다.

## 4. 알려진 결함: 릴리스 A 전에 수정 (계획 0단계) — 2026-10-18 수정 완료

**문제**
- `!방송기록` 시간 줄을 시작 시각 기준으로 바꿨는데, 옛 기록(`lifecycle_origin='legacy_unknown'`, 5,536건, 약 75일 이전)의 `started_at`은 표본 8건 중 5건이 실제보다 1.9~7.2시간 늦습니다.
- 지금 상태로는 `!방송기록 전체` 같은 조회에서 틀린 시작 시각이 표시됩니다.

**수정 방향** (검토 반영)
- `0179_01`·`0242_02` 조회문에 `s.lifecycle_origin`을 추가하고 `handlercore.BroadcastHistoryEntry`에 담습니다.
- `legacy_unknown`이면 `StartedAt`을 시작 시각으로 쓰지 않고 `broadcastHistoryFallbackTime`(저장 `ended_at`을 `종료`로 표기)을 씁니다.
- 판정에 `LastLivePositiveAt == nil`이나 head 존재를 쓰지 않습니다. 신 lifecycle 종료는 모두 origin을 `observed`로 바꿉니다(`reconcile/live/apply.go` `terminateSession`). head는 있지만 LIVE 확인이 없는 정상 종료도 있습니다(현재 225건, 모두 시작 시각 없음).
- 시험 사례: `legacy_unknown`, head는 있지만 LIVE 확인이 없는 `ENDED`.
- 수정 뒤 3절 검증 중 영향받는 것을 다시 실행합니다. 최소한 handlers·formatter 시험, dbtest의 방송기록 DB 시험, lint, NilAway입니다.

## 5. 근본 해결 계획 요약

전문과 근거 수치는 [계획](2026-10-18-broadcast-time-finalization.md)에 있습니다.

**원인**
- 방송 목록 수집이 종료를 먼저 감지하면 세션이 `ENDED`가 되어 영상 확인 대상에서 빠집니다.
- YouTube 종료 시각(`endTimestamp`)은 영상 확인으로만 얻을 수 있습니다. 최근 4일 `EXPLICIT_END` 334건 중 324건은 영상 확인이 0회였습니다.

**해결**
- 종료 뒤 같은 `video_live_check`로 아카이브 시각을 받아 확정합니다. 확정 분기는 consumer에 두고 reducer는 거치지 않습니다.
- 기록: `times_source`, 확정 진행 표, 재시도 계약
- 대상 투입: 신규 종료는 상시 대상으로 두고 재시도는 `NotBefore`로 표현합니다. backlog는 묶음과 분산 `NotBefore`로 넣습니다.
- 확정할 수 없는 기록: 비공개·삭제·봇 확인은 구분할 수 없어 `exhausted(identity_missing)`로 남깁니다.

**단계**: 0 결함 수정 → 1 migration 323 → 2 정산 → 3 수집 대상 → 4 표시 → 5 단계적 활성화 → 6 backlog 보정 → 7 멤버십 출처(독립)

**남은 확인**: 운영 worker 프로필의 `YouTubeJSMaxInflight`·`RequestInterval`. 5단계에서 확인해 backlog K를 정합니다.

## 6. 운영 조회 방법

- 운영 DB 조회는 `stack-platform-ops`의 PostgreSQL runbook만 사용합니다.
  - Hololive 대상: `hololive-seoul` `100.100.1.8`, 컨테이너 `holo-postgres`, DB `hololive`, 역할 `postgres_admin`
  - 세션마다 `PGOPTIONS='-c default_transaction_read_only=on'`을 걸고, 첫 문장으로 `show transaction_read_only`가 `on`인지 확인합니다.
- 이전 세션의 보조 스크립트(scratchpad `roq.sh`)는 세션 전용이라 다시 쓸 수 없습니다. 같은 보호를 걸고 새로 만듭니다.
- 영상 ID, 제목, payload 내용은 보고·문서에 적지 않고 집계만 남깁니다.

## 7. 해결할 이슈 전체 (사용자 지시: "모든 이슈를 해결")

다음 세션은 아래를 모두 해결합니다. "승인 필요"로 표시한 행동은 준비와 검증을 끝낸 뒤 사용자 승인을 받고 실행합니다. 승인이 없으면 그 행동과 그에 의존하는 일만 멈추고, 독립적인 일은 계속합니다.

| # | 이슈 | 해결 방법 | 완료 기준 | 승인 필요 |
|---|---|---|---|---|
| I1 | 옛 기록의 틀린 시작 시각 표시 (4절) | 계획 0단계 | 시험·lint·NilAway 통과 | 없음 |
| I2 | 릴리스 A·B 게시 | 커밋 분할, `pre-push-gate.sh`, `./build-all.sh --build-only --no-bump`, PR | gate 통과, A 배포 확인 뒤 B | push·PR, 배포 (로컬 커밋은 승인됨) |
| I3 | 종료 시각이 감지 시각으로 저장됨 (근본 원인) | 계획 1~4단계 (migration 323, consumer 확정, 수집 대상, 표시) | 계획의 단위·dbtest 목록 통과 | migration 운영 적용, 배포 |
| I4 | 신규 종료의 확정 운영 확인 | 계획 5단계 | 계획 5단계 통과 기준 | 배포 |
| I5 | 기존 기록 약 9,700건 보정, `!방송기록` 정렬 오류 포함 | 계획 6단계. `history_sort_at`이 따라오므로 정렬도 해결 | 결과 분포·무작위 20건 대조·표시 비율 약 99% | 운영 데이터 쓰기, 외부 요청 증가, 롤백 실행 |
| I6 | 멤버십 영상 시각 출처 없음 | 계획 7단계 | 출처를 찾으면 `members_only` 재확정, 없으면 근거 기록 | 수집기 배포 |
| I7 | 멈춘 `LIVE` 세션 9건 (아래) | 원인 조사 → reconcile 수정 → 기존 9건 정리 | 정상 경로로 종료되고 반복 영상 확인이 사라짐 | 정리용 운영 데이터 쓰기, 배포 |
| I8 | 미확인 항목 | 운영 worker 프로필의 `YouTubeJSMaxInflight`·`RequestInterval`, Iris Console 템플릿 변수 안내에 `Elapsed` 반영 여부, 실제 카카오톡 화면 확인 | 값·결론을 계획·이 문서에 기록 | 카카오 화면 확인은 운영 방 사용 시 승인 |

**I7 출발점** (2026-10-18 읽기 전용 조회)
- 신선한 확인이 없는 `LIVE` 세션 9건은 모두 다음 상태입니다.
  - `lifecycle_origin='legacy_unknown'`
  - head 없음, 가용성 기록 없음
  - 종료 후보·pending 없음
  - 마지막 기록이 136~146일 전
- 원인 추정: 부재 처리는 옛 기록이 positive 없이 부재만 있으면 수명 추적을 시작하지 않습니다(`reconcile/live/apply_absence.go` `applyAbsenceToKnownSession`). 이 세션들은 어떤 종료 경로로도 끝나지 않는 것으로 보입니다(추론).
- 확인할 것:
  - 최근 6일 `UNKNOWN identity_missing` 영상 확인 5,304건 중 이 9건이 차지하는 비율. 일부라도 반복 확인이면 수집 예산 낭비입니다.
  - 이 9건이 `live_check_videos.sql` 대상에 들어가는지
  - unresolvable 추적에 들어가지 못하는 이유(`live.UnresolvableTrackable`)
- 수정 방향은 조사 뒤에 정합니다. 기존 9건 정리는 운영 데이터 쓰기이므로 승인 필요입니다. 정리 결과도 계획의 확정 경로(`exhausted(identity_missing)`)와 맞아야 합니다.

**진행 상태 (2026-10-18 네 번째 세션, 08:18Z)**

| # | 상태 | 근거·남은 일 |
|---|---|---|
| I1 | 완료·배포 | #651(`51c0519cc`)에 포함, 03:35Z 배포. |
| I2 | 완료·배포 | #651 A → `hololive-api` 03:35Z, #652 B → migration 322 적용·04:03Z. 전역 본문 md5 `abab0db8…` 확인. 게시마다 push hook의 `pre-push-gate.sh`가 격리 작업 공간에서 통과했고 CI 11개 check가 모두 통과했습니다. |
| I3 | 완료·배포 | 적대적 검토(5개 영역) 지적 3건(backlog 재시도 누적으로 target 상한 초과, 중지 뒤 묶음 일괄 due, 늦게 감지한 방송의 시작 충돌)을 반영한 뒤 #653(`7bd38b05b`)으로 병합, migration 323 적용·04:30Z 배포. |
| I4 | 기준 2·3 충족, 기준 1 관찰 중 | 이전 06:00Z 판정은 유지합니다. 배포 이후 `observed` 신규 종료를 시각순으로 고정하고 취소·legacy 정리 행을 제외합니다. 진행 행은 LEFT JOIN하여 미시도 행도 분모에 남깁니다. 최초 30건 각각의 종료 관측 후 1시간이 지난 뒤 공개 여부와 관측 종료·provider 종료 기준 지연을 구분합니다. 08:18Z 후보 9건, 1시간 경과 5건입니다. |
| I5 | 중단 검증 후 승인 재개·감시 중 | 08:02Z conflict(channel_mismatch) 1건으로 K=0 복귀·backlog target 0을 검증했습니다. 사용자가 격리 유지 재개를 승인했고, 지표 정상 범위 확인 후 08:17:50Z K=200으로 API만 재생성했습니다. 08:18Z 확정 296·격리 충돌 1·pending 9,448건입니다. 전량 완료·20건 대조·표시 비율 판정은 아직입니다. |
| I6 | 완료·배포 | #655(`7e60b2061`), 수집기 c·b·a·d 05:10~05:18Z. `members_only` 행이 0건이라 재확정 SQL은 실행하지 않았고, 멤버십 종료 세션 33건은 I5 backlog에서 새 수집기로 처음 확인합니다. |
| I7 | 완료·배포 | #654(`f089d9a64`), migration 324 적용·04:56Z. 9건 `ENDED` → 5분 안에 8건 `finalized`, 1건 `pending(identity_missing)`(조사 때 `LOGIN_REQUIRED`였던 1건으로 보임, 24시간 일정 뒤 `exhausted` 예상). roster 밖 head 없는 legacy LIVE는 0건입니다. |
| I8 | 사용자 선택으로 보류 | worker 프로필 값과 템플릿 안내 확인은 완료 상태를 유지합니다. 현재 시험 방 승인이 없음을 확인하고 선택 질문을 했으며, 사용자가 카카오 화면 검증 보류를 선택했습니다. 방 접근·발송·읽음 변경을 하지 않았습니다. |

**I5 실행·감시 기록**
- 사전 읽기 전용 보호: `PGOPTIONS='-c default_transaction_read_only=on -c statement_timeout=15000'`, 각 조회의 첫 `SHOW transaction_read_only=on` 확인. 보고에는 집계만 남깁니다.
- 대상: 9,745건, `legacy_unknown` 5,536건. 사전 30분 실제 수집 지연(`observed_at-scheduled_for`) p99는 `channel_live_check` 4.508초, `live_snapshot` 4.765초입니다. RPC 실행 시간 histogram과 구분합니다.
- 설정: 마스터 `hosts/hololive-seoul/hololive-bot/compose.env`에 K=200을 추가하고 `sync-host.sh hololive-seoul --stack hololive-bot` dry-run에서 해당 파일만 바뀌고 삭제가 없음을 확인한 뒤 apply했습니다. 마스터·미러 일치, 원격 root:hololive-runtime 0640 유지, 컨테이너 K=200을 확인했습니다.
- 설정 복구본: 로컬 `/home/kapu/work/stack-secrets/compose.env.pre-stream-duration-i5-20261018T071756Z`, 원격 `/opt/hololive-bot/compose/deploy-backups/compose.env.pre-stream-duration-i5-20261018T071756Z`. 두 파일은 0600이며 보존합니다. 정기 백업은 재개하지 않았습니다.
- API image: `sha256:5c268d05d19ef60238d6c581b8f6a6b37372ae9a43e4491acdc580829af04fa2`, revision `f089d9a64136f1b5322d1a96fee4d68412433cee`, arm64. 기존 compose wrapper의 `config --quiet`와 `up -d --no-build --no-deps --pull never hololive-api`만 실행했습니다. migration·image 교체·다른 서비스 재시작은 하지 않았습니다.
- 감시: 부모 세션이 kapu에서 60초마다 읽기 전용 집계로 직전 5분 p99·요청 사유·진행 분포를 확인합니다. 임시 감시 프로세스는 모든 backlog의 최종 상태 도달, 중단 조건, 조회 실패, 최대 48시간 중 먼저 발생하는 때 종료하여 부모에게 결과를 전달합니다. 운영 자동화나 서비스로 설치하지 않았습니다.
- 보수적 중단 수치화: p99 >10초, 5분 `request_failed` 3건 이상이면서 시도 대비 5% 이상, 이전 48시간에 공개가 확인된 대상의 `identity_missing` 3건 이상이면서 그 대상 시도 대비 5% 이상, 최근 최종 결과 중 conflict 비율 0.5% 이상이면 즉시 K=0 복귀 대상으로 판정합니다. 사전 30분 request_failed와 conflict는 0건입니다. 공개 이력이 없는 영상의 UNKNOWN을 봇 확인으로 단정하지 않습니다.
- K=0은 다음 projection의 backlog 첫 시도와 재시도 대상을 제거합니다. 신규 종료와 이미 발행된 관측 정산은 계속되므로 `pending=0`을 중단 성공 조건으로 쓰지 않습니다. 시각 롤백 SQL은 실행하지 않았습니다.
- 실행 중 오류: 일반 SSH 사용자의 deploy 디렉터리 진입 권한 때문에 첫 recreate 명령은 실제 실행 전에 실패했고, 상태 불변을 확인한 뒤 `sudo bash`로 실행했습니다. distroless 컨테이너에 `printenv`가 없어 설정 확인은 docker inspect의 해당 키만 추출했습니다. 감시 초회에는 PostgreSQL JSON의 여러 줄 출력을 한 줄로 가정하여 중단했으며, 집계 보호를 유지한 채 framing을 수정하고 실제 표본 수신으로 재개했습니다.
- 08:02:04Z 중단: 직전 5분 최종 결과 49건 중 `conflict(channel_mismatch)` 1건(2.04%)으로 중단 기준을 넘었습니다. 당시 p99는 4.615초·4.685초, request_failed는 0건입니다. 08:02:45Z K=0 API 단독 재생성 후 healthy·재시작 0, health/readiness 통과, 다른 컨테이너 불변, 마스터/미러 일치·0640 유지, 현재 backlog target 0건을 확인했습니다.
- 08:03Z 격리 결과: 확정 259·충돌 1·pending 9,485건(미시도 9,416·identity_missing 재시도 69). 충돌 행은 10월 12일 `UNRESOLVABLE_VIDEO`로 끝난 `observed` 세션입니다. 응답의 영상 identity·공개·종료·양쪽 시각은 확인되지만 채널이 저장 채널과 다르고 운영 roster 밖입니다. 저장 시각은 사본과 동일하여 보정되지 않았습니다. 보존된 해당 영상 확인은 이번 1건뿐이므로 과거 채널 귀속 원인은 확정하지 못했습니다.
- 사용자 재개 결정: **격리 유지 후 재개**를 선택했습니다. 충돌 행·원래 시각은 변경하지 않고 중단 지표가 정상 범위인 것을 확인한 뒤 K=200·API 단독 재생성으로 나머지를 재개합니다. 동일 중단 조건을 유지하며 시각 롤백·채널 수정·conflict 재큐잉은 승인하거나 실행하지 않았습니다.
- 08:17Z 재개 사전 집계: 직전 5분 conflict 0건, p99 4.632초·4.585초, video_live_check 신규 시도 0건으로 중단 후 정지 상태를 확인했습니다. scoped sync dry-run에서 compose.env 외 파일 변경·삭제가 없음을 확인하고 apply했습니다. 08:17:50Z 동일 image로 API만 재생성했고 K=200·healthy·재시작 0·health/readiness·다른 컨테이너 ID 불변을 확인했습니다. 새 투입 SQL은 실행하지 않았습니다.
- 08:18:06Z 재개 표본: backlog 확정 296·격리 충돌 1·pending 9,448건(identity_missing 32·미시도 9,416). 5분 p99 4.840초·4.688초, request_failed·새 conflict 0건입니다. 재개 직후 밀린 재시도는 기존 일정대로 처리됐으며 첫 시도 pacing 규칙은 변경하지 않았습니다.
- 문서 게시: 동일 문서 커밋과 고정 sibling SHA의 격리 checkout에서 push hook·pre-push gate를 통과하여 draft PR #658을 만들었습니다. 첫 커밋 기준 GitHub check 11개가 통과했습니다. 원 worktree의 push 2회는 형제 shared-go 미커밋 변경에 따른 tidy 불일치로 차단됐으며 다른 작업을 수정하거나 hook을 우회하지 않았습니다.

**커밋 운용** (로컬 커밋 승인됨, 1절)

이후 단계는 릴리스 A가 바꾼 파일(`handler_broadcast_history.go` 등)을 다시 고칩니다. 그래서 I1을 끝내고 검증한 직후, 다른 작업보다 먼저 릴리스 A·B를 커밋으로 나눕니다.

1. **커밋 전 확인** (`.agents/workflows.md` Git writes)
   - Git 프로세스, ref·index lock, `git reflog -5`를 확인합니다. 소유가 모호하면 멈춥니다.
   - 신원은 설정된 값(`git config user.name`)을 그대로 씁니다. AI 표기는 넣지 않습니다.
   - hook을 실행합니다. `--no-verify`는 쓰지 않습니다.
2. **커밋 1: 릴리스 A**
   - 대상 파일:
     - 2절 "릴리스 A" 파일과 I1 수정
     - `CHANGELOG.md` `## 미출시`의 항목 하나 (굵은 제목 + 한 문단 형식, `!방송기록` 방송 길이와 `!라이브` 경과 시간)
     - 문서 두 개 (계획, 이 문서)
   - `renderer_seed_render_test.go`는 A와 B의 변경이 섞여 있습니다. 앞의 두 hunk(`fieldElapsed` 상수, fixture)가 A이고, 마지막 hunk(`TestSeedTemplates_LiveStreamsAppendElapsedToChannelLine`)가 B입니다.
     - 대화형 `git add -p`는 쓸 수 없습니다. 그래서 `git diff -- <파일>`에서 마지막 hunk를 뺀 패치를 scratchpad에 만들고 `git apply --cached`로 올립니다.
   - `manifest.txt`의 변경은 `183` 줄 하나뿐이라 파일 전체가 B입니다.
   - 커밋 전에 `git diff --cached --stat`로 B 파일이 섞이지 않았는지 확인합니다.
   - 메시지 예: `feat(bot): !방송기록 방송 길이와 !라이브 경과 시간 표시`
3. **A 단독 검증**
   - 커밋 1을 scratchpad에 `git worktree add --detach`로 꺼냅니다.
   - 그곳에서 bot·template·migrationrunner 시험, build, `check-migration-manifest.sh`를 실행하고 worktree를 제거합니다.
   - stash·reset으로 B를 치우지 않습니다.
4. **커밋 2: 릴리스 B**
   - 대상 파일:
     - 2절 "릴리스 B" 파일
     - `CHANGELOG.md` 항목 하나 (migration 322의 적용 조건: A 배포 확인 뒤 적용, 롤백 시 220 본문 복원)
   - 메시지 예: `feat(template): !라이브 템플릿에 경과 시간 표시 추가 (migration 322)`
5. **이후 작업**
   - 배포 순서가 다른 묶음은 커밋을 나눕니다. 예: migration 323 → 323을 읽는 코드, I7 수정, I6 수집기.
   - 게시할 때 묶음별로 PR을 나눌 수 있게 하기 위해서입니다. 게시는 squash 병합이므로 커밋 하나가 PR 하나에 대응하게 둡니다.
   - 아직 게시하지 않은 로컬 커밋은 고쳐도 됩니다. 게시한 커밋은 amend·force-push하지 않습니다.
6. **합류 주의**
   - 원 checkout의 `CHANGELOG.md`에 다른 작업의 미커밋 변경이 있습니다.
   - 게시 전에 `origin/main`과 rebase할 때 `## 미출시`에서 충돌할 수 있습니다. 원 checkout의 변경은 건드리지 않습니다.

## 8. 다음 단계 순서

게시·배포(I1~I3, I6, I7)는 세 번째 세션에서 끝났고, I4 기준 2·3은 데이터로 판정했습니다(7절 "진행 상태", 계획 "배포 결과"·5단계 "데이터 판정"). 남은 순서입니다.

1. I5 처리 감시: K=200 활성화와 9,745건 투입은 07:21Z 완료했습니다. 재투입하지 말고 현재 진행 분포와 감시 소유자를 먼저 확인합니다. 7절의 중단 수치에 해당하면 마스터 K=0 → scoped sync → API 단독 recreate → 다음 projection의 backlog 대상 제거를 확인합니다. UTC 12~17시도 같은 조건으로 감시합니다.
2. I4 기준 1: 04:30Z 이후 실제 신규 종료 최초 30건 각각의 1시간 관찰 창이 닫힌 뒤 공개 여부와 `finalized` 비율(≥97%)을 판정합니다. 관측 종료와 provider 종료 기준 지연을 구분합니다.
3. I5의 모든 행이 최종 상태가 된 뒤 무작위 확정 20건을 대조하고 최근 30일 `!방송기록` 표시 비율을 계산합니다. 표본 조회 실패를 교체 표본으로 숨기지 않습니다.
4. I8은 사용자 선택으로 보류합니다. 새 요청·승인 전에는 시험 방을 찾거나 카카오 화면을 열지 않습니다.
5. 각 단계가 끝나면 이 문서의 7절 표와 계획 문서의 체크박스·검증 결과를 갱신합니다.

## 9. 인계 프롬프트

세션이 예기치 않게 중단되었을 때만 사용하는 재개 지침입니다. 7절의 최신 실행 상태를 먼저 확인합니다.

```text
hololive-bot 방송 시각 확정의 진행 중인 I5와 I4 기준 1을 이어받아 주십시오. I8 카카오 화면은 사용자가 보류했습니다. 모든 보고는 한국어 합쇼체로 작성합니다.

먼저 읽을 것:
- /home/kapu/work/iris-stack/AGENTS.md, /home/kapu/work/iris-stack/hololive-bot/AGENTS.md
- /home/kapu/work/iris-stack/.agents/workflows.md (Git writes, Failure paths, Validation)
- 인계 문서: hololive-bot/docs/current/plans/2026-10-18-stream-duration-handoff.md (7절 "진행 상태", 8절)
- 계획: 같은 디렉터리의 2026-10-18-broadcast-time-finalization.md (5·6단계, "운영 SQL", "배포 결과", "남은 일")
- 배포·재시작은 hololive-bot-ops, 운영 DB 조회와 compose.env는 stack-platform-ops 스킬

현재 운영 상태 (2026-10-18):
- hololive-api f089d9a64 (migration 322·323·324 적용), 07:20:46Z K=200 API 단독 재생성. 07:21Z backlog 9,745건 투입 완료. 이후 중단 여부는 반드시 최신 기록과 실제 K 값으로 확인합니다.
- 확정 진행 표 youtube_live_time_finalizations, 지표 hololive_youtube_collection_time_finalization_*.
- I4 기준 2·3은 2026-10-18 06:00Z 데이터로 충족했습니다(계획 5단계 "데이터 판정").

승인 범위: K=200 활성화, 9,745건 backlog 투입과 외부 조회 증가, 중단 조건 시 K=0 복귀, docs PR squash 병합은 승인되었습니다. 활성화와 투입은 이미 실행했으므로 반복하지 않습니다. 시각 롤백 SQL·계획 밖 데이터 쓰기는 별도 승인 사항입니다.

진행 순서 (인계 문서 8절):
1. I5: 감시 소유자·실제 K·진행 분포를 확인하고 6단계 중단 조건 감시를 이어갑니다. 중단 조건이면 K=0 복귀·API 단독 recreate 뒤 backlog projection 제거를 확인합니다.
2. I4 기준 1: 04:30Z 이후 실제 신규 종료 최초 30건 각각의 1시간 관찰 창이 닫힌 뒤, 공개 여부와 관측 종료·provider 종료 기준 확정 지연을 구분하여 판정합니다.
3. I5 전량 최종 상태 뒤 무작위 확정 20건을 공개 watch 시각과 대조하고 최근 30일 !방송기록 표시 비율을 계산합니다.
4. I8은 보류를 유지하며 카카오 방 접근·발송·화면 열기를 하지 않습니다.

진행 기록: 단계마다 인계 문서 7절 표와 계획 문서 5·6단계에 결과를 남기고, 문서 변경은 docs PR(squash 병합)로 게시합니다. push·PR·병합은 승인되었습니다.

별도 보고·승인이 필요한 것: 시각 롤백 SQL 실행, 계획에 없는 운영 데이터 쓰기, 카카오 방 사용.
운영 DB 조회는 읽기 전용 보호를 증명한 뒤 집계만 하고, 영상 ID·제목·payload 내용은 보고와 문서에 넣지 마십시오.

보고 내용: 이슈별 상태, 실행한 검증과 결과, 실행하지 못한 검증과 이유, 필요한 승인, 남은 한계.
```
