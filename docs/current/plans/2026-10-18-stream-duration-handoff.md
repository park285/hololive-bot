# 방송 시간 표시와 방송 시각 확정 인계 - 2026-10-18

**상태** (2026-10-19 08:58Z, 미시도 0·완료 후 검증 통과, 재시도 꼬리 진행 중, **K=200**)
- 08:51:45Z 표본에서 backlog 9,745건이 모두 첫 결과를 냈습니다(미시도 0). **확정 9,426·충돌 35·exhausted 8·pending 276건**이며 pending은 모두 재시도 대기입니다. 충돌 35건 모두 원래 시각이 보존되었습니다.
- 무작위 확정 20건 공개 watch 대조는 **20/20 일치(최대 차이 0초)**, 최근 30일 `!방송기록` 길이 표시는 **ENDED 전체 2,341건 중 2,313건(98.8%)**, 방송하지 않은 13건을 빼면 2,328건 중 2,313건(99.4%)입니다(7절 기록).
- 남은 일은 재시도 꼬리의 최종 분포 기록입니다. 재시도는 첫 시도 시각 기준 30분·3시간·24시간에 하므로, 마지막 24시간 재시도는 2026-10-20 09:00Z 전후로 끝날 것으로 봅니다(추정). 세션 소유의 60초 읽기 전용 감시가 계속 실행 중입니다.
- 03:52Z 새 영상 identity_missing 34/42(81%) 중단 뒤, 사용자 결정("1번으로 진행해")으로 30분을 기다려 04:23:27Z `108ecb2e6` image에서 K=200·`hololive-api` 단독 재생성으로 재개했습니다.
- 운영 `hololive-api` image는 다른 작업(#662 워커·DB 전환)이 배포한 `108ecb2e6`이며 `f089d9a64`를 포함합니다. 사용자가 이 revision에서의 재개를 선택했습니다.
- 04:46Z 사용자 결정으로 identity_missing 중단 기준을 뺐습니다. 몰림이 거의 매시간 반복되어 80% 기준으로는 남은 재개 1회 안에 끝낼 수 없기 때문입니다. 나머지 중단 기준은 그대로입니다.
- 04:23Z 재생성 직후 API의 Iris 연결이 약 3분 30초 재시도한 뒤 04:26:57Z에 연결됐습니다. 이 작업 범위 밖이라 Iris는 건드리지 않았습니다.
- 조건부 재개는 **7/8회 사용, 잔여 1회, 기한 2026-10-20 14:35:50Z**입니다. 인계 중단 뒤의 재개도 1회로 셉니다.
- 00:14Z 인계 중단 기록은 아래 7절에 그대로 둡니다.
- I4는 완료했습니다. 최초 신규 종료 30건을 관찰해 공개 26/26이 시간 내 확정되어 표본 기준 통과했고, 멤버십 1건도 시간 내 확정했습니다. 미확정 3건은 공개 페이지에서 비공개 2·삭제 1로 확인했습니다.
- I8은 사용자 선택으로 보류합니다. 카카오 방 접근·명령 발송·화면 열기는 하지 않았습니다. 00:14Z 인계 전 세션의 감시는 K=0 검증 후 종료했습니다.

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
- 2026-10-19 00:14Z 인계 지시: "핸드오프해서 다른세션에서 진행하자". 무감시 처리를 피하도록 K=0으로 일시 중단하고 확정값·진행 행을 보존했습니다. 다음 세션은 남은 I5와 완료 후 검증만 이어갑니다. 조건부 반복 재개 승인(7절)은 5/8 사용·잔여 3회·동일 기한이며 초기화하지 않습니다.
- 2026-10-19 다섯 번째 세션: 사용자가 이 인계 문서의 진행을 요청했습니다. 기존 승인 범위(조건부 재개·K=0 중단·docs 게시)만 사용합니다. 새 운영 승인은 받지 않았고, 검증 시점에 대한 사용자 결정(8절 4)만 추가되었습니다.

## 2. 작업 트리 상태

| 항목 | 값 |
|---|---|
| worktree | `/home/kapu/work/iris-stack/hololive-bot-stream-duration` |
| branch / HEAD | `feat/stream-duration`. 이번 세션은 PR #657(`427eebc19`)에서 시작했고 이후 변경은 이 문서와 계획 문서뿐입니다. 실행 기록은 docs PR #658에 게시합니다. 원격 squash SHA와 로컬 문서 커밋 SHA는 다를 수 있으므로 다음 세션에서 실제 ref·clean 상태를 확인합니다. |
| 원 checkout | `/home/kapu/work/iris-stack/hololive-bot`. 이번 세션 시작 시 main `6ecd2ed44`, 다른 작업의 tracked 변경 295개·untracked 29개를 관찰했습니다. 이후 상태를 추정하지 않으며 이동·stash·reset·pull로 건드리지 않습니다. |
| 다른 worktree | `hololive-bot-build`, `-dead-audit`, `-docs2`, `-remaining-limits`. 모두 다른 작업 소유입니다. |
| 인계용 임시 자원 | `/tmp/stream-duration-i5-validation-vffun_ii/hololive-bot`는 게시 전용 clone(`docs/stream-duration-i5`)입니다. 형제 clone은 `shared-go` `6ed91f9`·`iris-client-go` `9621f18`로 고정했습니다. 원 checkout의 형제 미커밋 변경 때문에 gate가 실패하여 격리했으며 관련 작업은 변경하지 않았습니다. 읽기 전용 감시 코드 `/tmp/stream-duration-i5-watch-20261018.py`와 시각 알림 `/tmp/stream-duration-i5-resume-window-next.py`는 다음 세션 참고용으로 보존합니다. 실행 중인 감시 프로세스는 없습니다. 2026-10-19 확인: kapu의 tmpfiles 설정이 `/tmp`와 `~/.cache/tmp`(세션 scratchpad 포함)에서 1일 지난 파일을 지우므로(`Q /tmp 1777 root root 1d`, 매시간 실행), 이 clone은 작업 파일이 지워져 더 쓸 수 없습니다. 이후 게시는 `/home/kapu/work/iris-stack` 아래 작업 소유 worktree에서 하고, 오래 둘 감시 스크립트는 1일 안에 다시 읽히게 둡니다. |

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

**진행 상태 (2026-10-19 00:35Z 갱신)**

| # | 상태 | 근거·남은 일 |
|---|---|---|
| I1 | 완료·배포 | #651(`51c0519cc`)에 포함, 03:35Z 배포. |
| I2 | 완료·배포 | #651 A → `hololive-api` 03:35Z, #652 B → migration 322 적용·04:03Z. 전역 본문 md5 `abab0db8…` 확인. 게시마다 push hook의 `pre-push-gate.sh`가 격리 작업 공간에서 통과했고 CI 11개 check가 모두 통과했습니다. |
| I3 | 완료·배포 | 적대적 검토(5개 영역) 지적 3건(backlog 재시도 누적으로 target 상한 초과, 중지 뒤 묶음 일괄 due, 늦게 감지한 방송의 시작 충돌)을 반영한 뒤 #653(`7bd38b05b`)으로 병합, migration 323 적용·04:30Z 배포. |
| I4 | 완료·표본 기준 통과 | 최초 신규 종료 30건의 1시간 관찰 완료 후 공개 26/26(100%)이 관측 종료·provider 종료 양쪽 기준 시간 내 확정됐습니다. provider 종료 기준 최대 지연 34.499분입니다. 멤버십 1건도 확정, 미확정 3건은 공개 페이지로 비공개 2·삭제 1을 확인했습니다. 전체 표본 확정률 27/30(90%)과 구분합니다. 신규 queue conflict 0, 05:19Z 이후 p99 4.641초·4.934초입니다. 장기 모집단 97%의 통계적 보장은 아닙니다. |
| I5 | 검증 통과, 재시도 꼬리 진행 중 (조건부 재개 7/8, K=200) | 08:51:45Z 미시도 0. 확정 9,426·격리 충돌 35·exhausted 8·pending 276건이며 허용 외 충돌 사유·원래 시각 변경·신규 queue conflict는 0입니다. 무작위 확정 20건 대조 20/20 일치, 30일 표시 비율 98.8%(방송하지 않은 기록 제외 99.4%)입니다(8절 4). I5 최종 완료는 남은 재시도가 모두 최종 상태가 된 뒤 분포를 기록할 때로 봅니다. 마지막 24시간 재시도는 2026-10-20 09:00Z 전후로 끝날 것으로 봅니다(추정). |
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
- 중단 기준(두 번째 재개 때 사용자 승인. identity_missing 항목은 2026-10-19 02:33Z 교체, 04:46Z 해제, 아래 기록): p99 >10초, 5분 `request_failed` 3건 이상이면서 시도 대비 5% 이상, 이전 48시간 공개 확인 집합의 `identity_missing` 3건 이상이면서 그 대상의 5분 시도 대비 5% 이상, **직전 30분 최종 결과 대비 conflict 비율 0.5% 이상이면서 3건 이상**입니다. conflict만 최초의 5분·단발 중단에서 조정했습니다. 작은 이상 징후의 중단이 늦어질 수 있다는 한계를 보고하고 승인받았습니다. 공개 이력이 없는 UNKNOWN은 봇 확인으로 단정하지 않으며 제품의 시각·identity 검증 규칙은 그대로입니다.
- K=0은 다음 projection의 backlog 첫 시도와 재시도 대상을 제거합니다. 신규 종료와 이미 발행된 관측 정산은 계속되므로 `pending=0`을 중단 성공 조건으로 쓰지 않습니다. 시각 롤백 SQL은 실행하지 않았습니다.
- 실행 중 오류: 일반 SSH 사용자의 deploy 디렉터리 진입 권한 때문에 첫 recreate 명령은 실제 실행 전에 실패했고, 상태 불변을 확인한 뒤 `sudo bash`로 실행했습니다. distroless 컨테이너에 `printenv`가 없어 설정 확인은 docker inspect의 해당 키만 추출했습니다. 감시 초회에는 PostgreSQL JSON의 여러 줄 출력을 한 줄로 가정하여 중단했으며, 집계 보호를 유지한 채 framing을 수정하고 실제 표본 수신으로 재개했습니다.
- 08:02:04Z 중단: 직전 5분 최종 결과 49건 중 `conflict(channel_mismatch)` 1건(2.04%)으로 중단 기준을 넘었습니다. 당시 p99는 4.615초·4.685초, request_failed는 0건입니다. 08:02:45Z K=0 API 단독 재생성 후 healthy·재시작 0, health/readiness 통과, 다른 컨테이너 불변, 마스터/미러 일치·0640 유지, 현재 backlog target 0건을 확인했습니다.
- 08:03Z 격리 결과: 확정 259·충돌 1·pending 9,485건(미시도 9,416·identity_missing 재시도 69). 충돌 행은 10월 12일 `UNRESOLVABLE_VIDEO`로 끝난 `observed` 세션입니다. 응답의 영상 identity·공개·종료·양쪽 시각은 확인되지만 채널이 저장 채널과 다르고 운영 roster 밖입니다. 저장 시각은 사본과 동일하여 보정되지 않았습니다. 보존된 해당 영상 확인은 이번 1건뿐이므로 과거 채널 귀속 원인은 확정하지 못했습니다.
- 사용자 재개 결정: **격리 유지 후 재개**를 선택했습니다. 충돌 행·원래 시각은 변경하지 않고 중단 지표가 정상 범위인 것을 확인한 뒤 K=200·API 단독 재생성으로 나머지를 재개합니다. 동일 중단 조건을 유지하며 시각 롤백·채널 수정·conflict 재큐잉은 승인하거나 실행하지 않았습니다.
- 08:17Z 재개 사전 집계: 직전 5분 conflict 0건, p99 4.632초·4.585초, video_live_check 신규 시도 0건으로 중단 후 정지 상태를 확인했습니다. scoped sync dry-run에서 compose.env 외 파일 변경·삭제가 없음을 확인하고 apply했습니다. 08:17:50Z 동일 image로 API만 재생성했고 K=200·healthy·재시작 0·health/readiness·다른 컨테이너 ID 불변을 확인했습니다. 새 투입 SQL은 실행하지 않았습니다.
- 08:18:06Z 재개 표본: backlog 확정 296·격리 충돌 1·pending 9,448건(identity_missing 32·미시도 9,416). 5분 p99 4.840초·4.688초, request_failed·새 conflict 0건입니다. 재개 직후 밀린 재시도는 기존 일정대로 처리됐으며 첫 시도 pacing 규칙은 변경하지 않았습니다.
- 문서 게시: 동일 문서 커밋과 고정 sibling SHA의 격리 checkout에서 push hook·pre-push gate를 통과하여 draft PR #658을 만들었습니다. 첫 커밋 기준 GitHub check 11개가 통과했습니다. 원 worktree의 push 2회는 형제 shared-go 미커밋 변경에 따른 tidy 불일치로 차단됐으며 다른 작업을 수정하거나 hook을 우회하지 않았습니다.
- 10:21:45Z 두 번째 중단: `start_mismatch` 1건이 직전 5분 최종 결과 52건의 1.92%였습니다. p99 4.647초·5.238초, request_failed 0건입니다. K=0·10:22:20Z API 단독 재생성 뒤 health/readiness·재시작 0·현재 backlog target 0건을 확인했습니다. 확정 1,275·충돌 2·pending 8,468건을 보존했습니다.
- 두 번째 충돌 근거: 9월 30일 `EXPLICIT_END`로 끝난 `observed` 세션이며 영상·채널·종료 시각은 일치합니다. provider 시작 시각이 저장값보다 28.464분 늦어 기존 허용폭 15분을 넘었습니다. 세션 시각은 원래 사본과 같으며 코드의 정상적인 격리입니다. 시각 허용폭을 바꾸거나 재큐잉하지 않았습니다.
- 사용자 결정과 두 번째 재개: **30분 창·3건 기준으로 재개**를 선택했습니다. 11:10Z 사전 p99 4.223초·4.766초, 직전 30분 conflict 0건을 확인했습니다. dry-run에서 compose.env만 변경됨을 확인한 뒤 마스터 K=200·scoped sync·11:11:00Z API 단독 재생성을 실행했습니다. 마스터/미러 일치·동일 image·K=200·healthy·재시작 0·health/readiness·다른 컨테이너 불변을 확인했습니다. 감시의 conflict 창·최소 건수만 바꾸었고 다른 중단 조건·제품 재시도·첫 시도 pacing은 그대로입니다.
- 11:11:11Z 실제 재개 표본: backlog 확정 1,288·격리 충돌 2·pending 8,455건(identity_missing 20·login_required_unclassified 1·미시도 8,434), p99 4.701초·4.798초, request_failed·직전 30분 새 conflict 0건입니다.
- 13:57Z 세 번째 중단: 30분 conflict 3/203(1.48%), p99 3.945초·5.170초, request_failed 0건입니다. K=0·13:58:09Z API 단독 재생성 뒤 health/readiness·재시작 0·동일 image·다른 컨테이너 불변·backlog target 0을 확인했습니다. 확정 2,536·충돌 8·pending 7,201건을 보존했습니다.
- 누적 충돌 8건은 채널 불일치 2·시작 불일치 6이며 모두 observed 시각이 사본과 같습니다. 시작 불일치의 provider 시작 차이는 +16.217~+97.206분입니다. 누적 충돌률은 8/2,544(0.31%)이나 최근 30분 집중률은 1.48%였습니다. 사용자는 **현재 기준으로 재개**를 선택하여 누적률 전환을 하지 않았습니다.
- 14:28Z 30분 conflict 0건·p99 4.586초/4.565초 확인 후 마스터 K=200·scoped sync·14:29:35Z API 단독 재생성을 완료했습니다. 컨테이너 K=200·동일 image·health/readiness·재시작 0·다른 컨테이너 불변·마스터/미러 일치를 확인했습니다. 14:29:47Z 확정 2,552·충돌 9·pending 7,184건, p99 4.701초/4.958초입니다. 기존 재시도에서 start_mismatch 1건이 추가 격리됐으며 30분·3건 조건에는 이르지 않았습니다.
- I4 판정 근거: 최초 30건의 마지막 관찰 창 종료는 14:23:37Z이고 14:27:51Z에 전부 경과를 확인했습니다. 공개 26건의 최대 지연은 관측 종료 30.029분·provider 종료 34.499분, 멤버십 1건은 각각 3.048분·5.530분입니다. identity_missing 3건은 공개 watch 3회로 비공개 2·삭제 1을 확인했습니다. DB 사유를 변경하지 않았고 전체 27/30(90%)와 공개 26/26(100%)를 구분합니다.
- 14:32:48Z 네 번째 중단: 30분 conflict 3/57(5.26%), p99 5.582초·5.749초, request_failed 0건입니다. K=0·14:34:21Z API 단독 재생성 뒤 동일 image·healthy·재시작 0·health/readiness·다른 컨테이너 불변·backlog target 0을 확인했습니다. 중단 완료 시 확정 2,596·충돌 12(channel_mismatch 3·start_mismatch 9)·pending 7,137건이며 12/12의 원래 시각이 보존되었습니다.
- 조건부 반복 재개 승인: 이 작업의 9,745건에 한해 **최대 8회, 2026-10-20 14:35:50Z까지**입니다. 매회 K=0 중단 후 충돌이 `start_mismatch`·`channel_mismatch`뿐이고 원래 시각 보존·현재 backlog target 0·수집 지표 정상·직전 30분 새 conflict 0을 확인한 다음 K=200으로 재개합니다. 다른 사유·지연·요청 실패·감시 실패는 재개하지 않고 보고합니다. API 단독 재생성의 짧은 중단이 반복됨을 보고하고 승인받았습니다. 현재 사용 횟수는 **5/8**입니다.
- 조건부 재개 1/8 (15:05Z): 보호된 조회로 허용 외 충돌 사유 0·원래 시각 변경 0·신규 queue conflict 0·현재 backlog target 0·30분 새 conflict 0·p99 4.656초/4.835초·요청 실패 0을 확인했습니다. K=0·기존 revision도 확인한 뒤 마스터 K=200·scoped sync·15:05:08Z API 단독 재생성을 실행했습니다. healthy·재시작 0·health/readiness·다른 컨테이너 불변·마스터/미러 일치가 확인됐습니다. 실제 표본은 확정 2,600·충돌 12·pending 7,133건이며 감시를 다시 시작했습니다.
- 다섯 번째 중단(조건부 재개 1회 뒤, 15:25~15:26Z): 30분 conflict 3/164(1.83%), p99 4.658초/4.692초, request_failed 0건입니다. 15:26:30Z K=0·API만 재생성 후 healthy·재시작 0·health/readiness·동일 image·다른 컨테이너 불변·마스터/미러 일치를 확인했습니다. 보호된 집계에서 충돌 15건 모두 허용된 두 사유·원래 시각 보존, 신규 queue conflict 0·현재 backlog target 0입니다. 확정 2,765·충돌 15·pending 6,965건을 보존했고 다음 30분 창 해소는 15:54:32Z입니다. 조건부 재개 사용 횟수는 아직 1/8입니다.
- 조건부 재개 2/8 (15:55Z): 보호된 조회에서 충돌 15건의 허용 외 사유·원래 시각 변경, 신규 queue conflict·현재 backlog target·30분 새 conflict가 모두 0이고 p99 4.629초/4.395초·요청 실패 0임을 확인했습니다. K=0·동일 revision 확인 뒤 scoped sync·15:55:11Z API 단독 재생성을 완료했습니다. 같은 image·healthy·재시작 0·health/readiness·다른 컨테이너 불변·마스터/미러 일치가 확인됐습니다. 확정 2,765·충돌 15·pending 6,965건으로 감시를 재개했습니다.
- 여섯 번째 중단(조건부 재개 2회 뒤, 16:15~16:16Z): 30분 conflict 3/158(1.90%), p99 4.721초/5.213초, request_failed 0건입니다. 16:16:13Z K=0·API 단독 재생성 뒤 동일 image·healthy·재시작 0·health/readiness·다른 컨테이너 불변·마스터/미러 일치·backlog target 0을 확인했습니다. 확정 2,918·충돌 18·pending 6,809건을 보존했고, 허용 외 사유·원래 시각 변경·신규 queue conflict는 모두 0입니다. 사용 2/8, 다음 30분 창 해소는 16:44:55Z입니다.
- 조건부 재개 3/8 (16:45Z): 보호된 집계에서 허용 외 사유·원래 시각 변경·신규 queue conflict·backlog target·30분 새 conflict가 모두 0이고 p99 4.625초/4.415초·요청 실패 0임을 확인했습니다. K=0·동일 revision 확인 뒤 scoped sync·16:45:19Z API 단독 재생성을 완료했습니다. 같은 image·healthy·재시작 0·health/readiness·다른 컨테이너 불변·마스터/미러 일치가 확인됐고, 확정 2,923·충돌 18·pending 6,804건으로 감시를 재개했습니다.
- 종료 집중 시간대 확인(UTC 12~17시, 17:46Z 집계): 실제 수집 지연 p99는 channel_live_check 4.640초(17,850건)·live_snapshot 4.972초(20,976건)입니다. 해당 구간 video_live_check 1,967회 중 request_failed 0회이며 PUBLIC 1,375·MEMBERS_ONLY 108·identity_missing 482·login_required_unclassified 2회입니다. 호출 횟수이지 영상 수가 아니며, 구간에는 기록한 K=0 중단 시간도 포함됩니다. 17:46Z backlog는 확정 3,345·충돌 21·pending 6,379건, 조건부 재개 사용 3/8입니다.
- 일곱 번째 중단(조건부 재개 3회 뒤, 18:21Z): 30분 conflict 3/271(1.11%), p99 4.202초/4.953초, request_failed 0건입니다. 18:21:31Z K=0·API 단독 재생성 뒤 동일 image·healthy·재시작 0·health/readiness·다른 컨테이너 불변·마스터/미러 일치·backlog target 0을 확인했습니다. 확정 3,639·충돌 25·pending 6,081건을 보존했으며 허용 외 사유·원래 시각 변경·신규 queue conflict는 0입니다. 사용 3/8, 다음 창 해소는 18:50:16Z입니다.
- 조건부 재개 4/8 (18:50Z): 허용 외 사유·원래 시각 변경·신규 queue conflict·backlog target·30분 새 conflict 0, p99 3.806초/4.287초·요청 실패 0, K=0·동일 revision을 확인했습니다. scoped sync·18:50:34Z API 단독 재생성 뒤 같은 image·healthy·재시작 0·health/readiness·다른 컨테이너 불변·마스터/미러 일치가 확인됐습니다. 확정 3,648·충돌 25·pending 6,072건으로 감시를 재개했습니다.
- 여덟 번째 중단(조건부 재개 4회 뒤, 19:23~19:24Z): 30분 conflict 3/242(1.24%), p99 4.640초/4.683초, request_failed 0건입니다. 19:24:21Z K=0·API 단독 재생성 뒤 공통 검증(동일 image·healthy·재시작 0·health/readiness·다른 컨테이너 불변·마스터/미러 일치·backlog target 0)을 통과했습니다. 확정 3,902·충돌 28·pending 5,815건을 보존했고 허용 외 사유·원래 시각 변경·신규 queue conflict는 0입니다. 사용 4/8, 다음 창 해소는 19:53:00Z입니다.
- 조건부 재개 5/8 (19:53Z): 허용 외 사유·원래 시각 변경·신규 queue conflict·backlog target·30분 새 conflict 0, p99 4.011초/4.671초·요청 실패 0, K=0·동일 revision을 확인했습니다. scoped sync·19:53:24Z API 단독 재생성과 공통 검증을 완료했습니다. 재개 표본은 확정 3,907·충돌 28·pending 5,810건입니다.
- 신 lifecycle 첫 확인 완료·legacy 처리 전환(22:25Z): 확정 5,082건(observed 4,101·legacy_unknown 981), 충돌 31건(observed 29·legacy 2), pending 4,632건입니다. 미시도 4,529건은 모두 legacy이며 재시도는 103건입니다. observed 4,209건은 모두 첫 확인을 마쳤습니다. 첫 확인 완료와 최종 상태 완료를 구분하며 기존 3분·30분·3시간·24시간 재시도 일정을 바꾸지 않습니다. 조건부 재개 사용 5/8을 유지합니다.
- 사용자 요청 인계 중단(2026-10-19 00:14Z): 중단 지표 이상이 아니라 세션 이관 때문에 K=0으로 멈췄습니다. 마스터 수정·scoped sync 후 00:14:14Z API만 재생성했고 동일 image·healthy·재시작 0·health/readiness·다른 컨테이너 불변·마스터/미러 일치·root:hololive-runtime 0640·backlog target 0을 확인했습니다.
- 최종 집계(00:14:22Z): 확정 5,856·충돌 31(start_mismatch 23·channel_mismatch 8)·pending 3,858건입니다. pending은 미시도 3,689·identity_missing 159·login_required_unclassified 9·end_time_missing 1건이며 재시도 합계는 169건입니다. 충돌 31/31은 원래 observed 시각을 보존했고 허용 외 사유·신규 queue conflict는 0입니다.
- 인계 직전 5분 p99는 channel_live_check 4.621초·live_snapshot 4.745초, request_failed·이전 공개 확인 대상 identity_missing·30분 새 conflict는 0건입니다. K=0은 backlog 첫 시도·재시도를 다음 projection에서 빼지만 신규 종료와 이미 발행된 관측 정산은 계속됩니다. 시각 롤백 SQL·채널 수정·conflict 재큐잉은 하지 않았습니다.
- `stream-duration-i5-watch-bounded5-20261018` 감시는 K=0 검증 뒤 명시적으로 종료했습니다. 다음 세션은 기존 프로세스나 eval 상태가 살아 있다고 가정하지 말고, 읽기 전용 감시를 자기 소유로 시작합니다. 조건부 재개 사용 횟수는 5/8 그대로이며 제품 재시도·fallback 변경은 없습니다.
- 조건부 재개 6/8 (2026-10-19 00:33~00:35Z, 인계받은 세션): 보호된 집계에서 충돌 31건이 허용된 두 사유뿐이고 원래 시각 변경·신규 queue conflict·현재 backlog target·30분 새 conflict가 0이며 p99 4.222초/4.669초·request_failed 0(K=0이라 직전 5분 영상 확인 0회)임을 확인했습니다. K=0·같은 revision을 확인한 뒤 마스터 K=200, scoped sync dry-run(compose.env만 변경·삭제 없음)과 apply, 00:35:01Z API 단독 재생성을 실행했습니다. 같은 image·K=200·healthy·재시작 0·health/readiness·다른 컨테이너 불변·마스터/미러 일치·0640·기동 로그 WARN/ERROR 0을 확인했습니다. 새 감시는 같은 중단 기준에 더해 허용 외 충돌 사유·충돌 행 시각 변경·신규 queue 충돌이 생기면 멈춥니다. 재개 직후 밀린 재시도가 몰려 5분 영상 확인 71회·p99 5.695초/6.065초였고 request_failed는 0건입니다.
  - 새 감시는 backlog 전량 최종 상태, 중단 조건, 조회 실패, 최대 48시간(2026-10-21 00:35Z) 중 먼저 오는 때 종료합니다. K 변경은 하지 않고 부모 세션이 처리합니다.
  - K=0이면 backlog 재시도도 projection에서 빠지므로 꼬리 도중 멈추면 꼬리도 멈춥니다. 꼬리 완료 전에 세션이 끝나면 무감시 처리를 피하도록 K=0 인계 중단을 먼저 하며, 기한 2026-10-20 14:35:50Z 뒤의 재개는 새 승인이 필요합니다.
- 감시·검증 스크립트 적대적 검토 반영 (2026-10-19 02:12~02:14Z): 중단 기준 판정 결함은 없었습니다. 첫 감시는 출력 파이프 때문에 종료 코드가 항상 0으로 보고되어, 02:13:26Z에 종료하고 02:13:31Z 같은 중단 기준으로 다시 띄웠습니다(공백 약 13초, K 변경 없음). 새 감시는 실패 원인 일부와 참고용 5분 전체 identity_missing 수를 함께 남깁니다. 후보 조건에서 빠져 영원히 pending으로 남는 backlog 행은 0건입니다(02:12Z 읽기 전용 확인). 20건 대조는 수집기와 같은 `ytInitialPlayerResponse` 파싱, `videoDetails.videoId` 일치 확인, redirect 차단 방식으로 고쳤고, 표본 추출은 감시 이벤트 재확인과 정확히 20건을 요구합니다.
  - 한계: "이전 48시간 공개 확인 집합의 identity_missing" 중단 기준은 backlog 첫 확인에 공개 이력이 없어 재개 뒤 표본에서 분모(공개 확인 집합의 시도)가 계속 0이었습니다. 승인된 기준대로 구현되었지만 봇 확인 감지는 사실상 수집 지연·request_failed 기준에 의존합니다. 02:13Z 5분 전체 identity_missing은 4/54회입니다.
- identity_missing 중단 기준 교체 (2026-10-19 02:33Z, 사용자 지시 "전체 identity_missing 비율 기준으로 바꿔줘", 기준값은 선택 질문에서 80% 선택):
  - 새 기준: 직전 5분 video_live_check 중 직전 48시간 안에 이미 identity_missing이었던 영상을 뺀 "새 영상" 시도에서 identity_missing이 **10건 이상이면서 80% 이상**이면 멈춥니다. "이전 48시간 공개 확인 집합" 기준은 뺐고 다른 중단 기준은 그대로입니다.
  - 이미 identity_missing이었던 영상을 빼는 이유: 재시도는 대부분 다시 identity_missing이라 전체 비율을 그대로 쓰면 5분 구간 중앙값 31%, 재시도가 몰리면 100%가 되어 꼬리 구간에서 잘못 멈춥니다.
  - 근거(backlog 시작 07:20Z부터 02:30Z까지 5분 구간 218개, 읽기 전용 집계): 새 영상 기준 비율은 중앙값 5.8%, 상위 10% 53.7%, 시도 20회 이상 구간 최대 72.5%였습니다. 이 기간에 적용했다면 50%는 9회, 60%는 4회, 70%는 1회, 80%는 0회 멈췄습니다(30분 안의 재발은 1회로 셈). 최소 건수 10·15·20건은 결과가 같았습니다.
  - 새로 확인한 사실: 몰림 구간의 identity_missing은 대부분 일시적이었습니다. 21:40~21:55Z 구간의 해당 영상 89건 중 82건, 11:45~11:55Z 구간 53건 중 48건이 30분 재시도에서 확정됐습니다. 따라서 identity_missing은 주로 비공개·삭제가 아니라 몰려서 나타나는 일시 실패이며, 봇 확인일 가능성이 높습니다(추론). 그동안 방송 수집 지연·request_failed는 정상이었습니다.
  - 80%는 지금까지 관찰한 최악보다 나쁜, 거의 전면 차단에 가까운 상황만 잡습니다. 이보다 약한 몰림은 재시도 일정이 흡수하도록 둡니다.
  - 교체 시 경계값을 가짜 표본으로 시험했습니다(10/12·32/40 중단, 10/13·31/40·9/9 계속). 운영에서 질의를 한 번 실행한 뒤 02:33:43Z에 감시를 바꿔 띄웠습니다(공백 약 3초, K 변경 없음).
- 아홉 번째 중단 (2026-10-19 03:52Z, 새 identity_missing 기준):
  - 03:52:11Z 표본에서 새 영상 identity_missing이 34/42(81%)였습니다. 같은 5분 p99는 3.706초/4.586초, request_failed 0건, 30분 conflict 1/151건으로 다른 지표는 정상입니다.
  - 추이(감시 로그): 02:40~02:55Z에 최대 29/39(74%)까지 올랐다가 03:00Z 3/40으로 스스로 회복했습니다. 03:30Z부터 다시 올라 03:40Z 24/41, 03:52Z 34/42가 됐고, 03:49Z부터 3분간 확정 수가 늘지 않았습니다.
  - 처리: 마스터 K=0, scoped sync dry-run(compose.env만 변경·삭제 없음)과 apply, 03:52:38Z `hololive-api` 단독 재생성. 컨테이너 K=0·healthy·재시작 0·health/readiness 통과·마스터/미러 일치·root:hololive-runtime 0640·기동 로그 WARN/ERROR 0을 확인했습니다. 다른 컨테이너의 시작 시각은 모두 03:52Z 이전이라 이번 재생성으로 바뀌지 않았습니다.
  - 03:55:23Z 보호된 집계: 현재 generation(03:52:39Z 활성)의 backlog target 0. backlog는 확정 7,273·충돌 34(start_mismatch 23·channel_mismatch 11)·pending 2,439건이고, pending은 미시도 2,173·identity_missing 251·login_required_unclassified 13·end_time_missing 1건입니다. 허용 외 충돌 사유·충돌 행 시각 변경·신규 queue conflict는 0입니다.
  - 몰림 영상의 결과(집계): 02:38~02:58Z에 identity_missing이었던 backlog 영상 110건 중 87건 확정·1건 충돌·22건 identity_missing 재시도 대기입니다. 03:28~03:53Z의 122건은 30분 재시도 전에 멈춰 모두 identity_missing 재시도 대기입니다. 첫 몰림도 대부분 재시도에서 확정되어 일시 실패로 보입니다(추론).
  - 운영 image: 재생성 전후 모두 `sha256:106fbf94e45c1b3b62a13695ea390e97c57fe9399d3bc13d2921e9adfc204673`, revision `108ecb2e6`(#662)입니다. 제거된 이전 컨테이너의 destroy 이벤트에도 같은 image가 찍혀 있어, 이 변경은 이번 재생성이 아니라 다른 작업의 배포입니다. 같은 시기 holo-postgres(02:12:36Z), collector-c·po-c(02:13:28~29Z), alarm-worker(02:14:29Z)가 재시작됐습니다. `hololive-api`의 정확한 교체 시각은 이벤트 보존 범위 밖이라 확인하지 못했습니다. `108ecb2e6`은 `f089d9a64`를 조상으로 포함합니다.
  - 재개 판단: 이 중단은 허용된 두 충돌 사유가 아니라 identity_missing 기준이므로 조건부 재개 대상이 아닙니다. revision도 바뀌었습니다. 따라서 재개하지 않았고 사용 횟수는 6/8 그대로입니다. 첫 몰림과 #662 배포(02:12~02:14Z)의 시간적 근접은 확인했지만 인과는 확인하지 않았습니다. 이전 몰림(10-18 11:45Z·21:40Z)은 #662 이전에도 있었습니다.
  - 추가 집계: 몰림 영상은 b1·b2 모두 69개 채널에 흩어져 있고 채널당 최대 4~5건이며 전부 2026년 3~4월 legacy입니다. 03:30~03:52Z에는 수집기 a·b·c·d 네 곳이 동시에 identity_missing을 냈습니다(a 14/15, d 65/85, c 37/83, b 4/14). 특정 채널이나 한 호스트 IP의 문제보다 YouTube 쪽 일시 현상일 가능성이 있습니다(추론).
- 조건부 재개 7/8 (2026-10-19 04:23Z, 사용자 결정 "1번으로 진행해"):
  - 사용자에게 보고한 선택지 중 "30분 뒤 회복 확인 후 `108ecb2e6`에서 재개"를 사용자가 골랐습니다. 이 결정은 identity_missing 중단 뒤 재개와 바뀐 revision을 이번 1회에 한해 허용합니다.
  - 회복 판정 한계: K=0에서는 영상 확인이 거의 없어(04:23Z 기준 직전 30분 새 영상 2회, identity_missing 0) 미리 정한 "직전 15분 새 영상 5회 이상·20% 미만" 기준을 판정할 수 없었습니다. 사용자에게 알린 대로 재개 자체를 확인 수단으로 삼았고, 막혀 있으면 기존 80% 기준이 다시 멈추게 했습니다.
  - 04:23:07Z 보호된 집계: 충돌 34건은 허용된 두 사유뿐이며 원래 시각 변경·신규 queue conflict·backlog target·30분 새 conflict가 0입니다. p99 3.821초/4.38초, request_failed 0, K=0·`108ecb2e6`·healthy를 확인했습니다.
  - 처리: 마스터 K=200, scoped sync dry-run(compose.env만 변경·삭제 없음)과 apply, 04:23:27Z API 단독 재생성. 같은 image·K=200·healthy·재시작 0·health/readiness 통과·다른 컨테이너 불변·마스터/미러 일치·root:hololive-runtime 0640을 확인했습니다.
  - 기동 로그 WARN은 모두 Iris 연결 재시도(`iris_ping_retry`, `Iris server not ready, retrying`)였고, 04:26:57Z `Iris server became ready after retry`·`Iris server connected`로 끝났습니다(재생성 뒤 약 3분 30초). 처음에는 호스트에서 `100.100.1.5:3001`로 보낸 curl이 연결 거부여서 Iris가 내려갔다고 보고했지만, 연결된 뒤에도 같은 결과가 나와 유효한 점검이 아니었습니다. 이 판단은 정정합니다. 방송 시각 처리와는 무관하며 Iris는 조사·조치하지 않았습니다.
  - 04:24:01Z 첫 표본: 새 영상 identity_missing 0/6, 밀린 재시도로 5분 영상 확인 74회, request_failed 0, p99 5.727초/6.318초입니다. 1분 사이 확정이 7,273에서 7,334로 늘어 몰림 영상 재시도가 확정되고 있습니다.
  - 밀린 재시도가 몰린 04:25~04:28Z에는 5분 영상 확인이 132~168회, p99가 7.4초/8.5초까지 올랐습니다(중단 기준 10초). 04:41Z에는 4.3초/4.3초로 내려왔습니다.
- identity_missing 중단 기준 해제 (2026-10-19 04:46Z, 사용자 결정 "1번으로 진행해"):
  - 계기: 04:41~04:44Z 새 영상 identity_missing이 16/41(39%)에서 28/43(65%)으로 다시 올랐습니다. 남은 재개가 1회뿐이라 80% 기준에 다시 걸리면 미시도 약 2,000건(약 4.6시간)을 끝낼 수 없었습니다.
  - 근거: 새 영상 비율 50% 이상 몰림은 10-18 09:40·11:40·12:45·16:45·20:40·21:35·22:35·23:40Z, 10-19 00:35·02:40·03:30·04:40Z에 있었습니다. 20:40Z부터는 거의 매시간 반복됐고, #662 배포(02:12Z) 이전부터 있던 패턴입니다. 정점은 72.5%에서 74%·81%로 높아졌습니다. 몰림 동안 수집 지연·request_failed는 정상이었고, 몰림 영상 대부분이 재시도에서 확정됐습니다. 매시간 반복되는 원인은 확인하지 못했습니다.
  - 변경: 감시의 identity_missing 중단 조건만 빼고 비율 기록은 유지했습니다. p99 10초, request_failed 3건·5%, 30분 conflict 3건·0.5%, 허용 외 충돌 사유·충돌 행 시각 변경·신규 queue 충돌 기준은 그대로입니다. 04:46:28Z 감시를 다시 띄웠습니다(공백 약 20초, K 변경 없음).
  - 위험(사용자에게 보고함): 재시도 시각이 몰림과 겹친 일부 행은 4회 UNKNOWN 뒤 `exhausted`로 끝날 수 있습니다. 시각이 틀리게 확정되는 것이 아니라 보정을 받지 못하는 것입니다.
- 미시도 0과 완료 후 검증 (2026-10-19 08:51~08:58Z, 8절 4):
  - 04:46Z 이후 중단 이벤트 없이 진행해 08:51:45Z 표본에서 미시도 0이 됐습니다. 같은 표본의 p99는 3.966초/5.084초, request_failed 0, 30분 새 conflict 0, 허용 외 충돌 사유·충돌 행 시각 변경·신규 queue 충돌 0입니다. 새 영상 identity_missing은 25/41로 몰림 중이었고 기록만 했습니다.
  - 08:58:47Z 보호된 집계: 확정 9,426, 충돌 35(start_mismatch 23·channel_mismatch 12), exhausted(identity_missing) 8, pending 276(identity_missing 258·login_required_unclassified 17·end_time_missing 1)입니다. pending의 시도 횟수는 1회 98건(다음 시도 09:21Z까지), 2회 26건(11:25Z까지), 3회 152건(마지막 시도 2026-10-20 05:46Z까지)입니다. 이 시각에 현재 generation의 backlog target은 276건입니다.
  - 무작위 20건 대조: 08:57Z에 backlog의 finalized·`times_source='provider'` 행에서 무작위로 20건을 한 번 뽑았습니다. 공개 watch 페이지의 `startTimestamp`·`endTimestamp`(수집기와 같은 `ytInitialPlayerResponse` 파싱, redirect 차단, `videoDetails.videoId` 일치 확인)와 저장 시각을 비교해 **20/20 일치, 시작·종료 모두 최대 차이 0초**였습니다. 조회 실패·identity 불일치·시각 누락은 0건이라 교체한 표본이 없습니다.
  - 표시 비율(08:58:31Z): 최근 30일 `ENDED` 2,341건에 `!방송기록` handler의 길이 표시 조건(`trustedBroadcastStartedAt`·`verifiedBroadcastEndedAt`)을 적용하면 **2,313건(98.8%)**이 길이를 표시합니다. 방송하지 않은 기록(`CANCELLED_BEFORE_LIVE` 또는 시작 시각·LIVE 확인 없음) 13건을 빼면 2,328건 중 99.4%입니다.
  - 표시하지 않는 나머지 15건은 backlog `pending(identity_missing)` 13건과 backlog `conflict(channel_mismatch)` 2건입니다. pending 13건은 남은 재시도에서 확정되면 표시되므로 비율은 이후 오를 수 있습니다.

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

게시·배포(I1~I3, I6, I7)와 I4 신규 종료 30건 판정은 완료했습니다(7절, 계획 5단계). 남은 순서입니다.

1. 현재 K=0·같은 운영 revision·healthy·backlog target 0·마스터/미러 일치와 진행 분포를 확인합니다. 기존 9,745건을 다시 투입하지 않습니다. 원 checkout의 다른 작업은 건드리지 않습니다. (2026-10-19 00:33Z 수행 완료)
2. 다음 세션의 감시 소유자·60초 주기·중단 처리를 준비합니다. 7절의 조건부 재개 검증과 기한을 충족하면 기존 승인 범위에서 K=200·API 단독 재생성으로 이어갑니다. (00:35Z에 6/8로 수행 완료. 다음 중단 뒤 재개는 7/8부터 셉니다. 03:52Z identity_missing 중단 뒤 사용자 결정으로 04:23Z에 7/8로 재개했고 남은 재개는 1회입니다.) 다른 사유·지연·요청 실패·감시 실패, 8회 소진 또는 기한 경과는 재개하지 말고 별도 승인 범위를 확인합니다.
3. 중단 기준은 5분 p99 >10초, 5분 request_failed 3건 이상·5% 이상, 30분 최종 결과 중 conflict 3건 이상·0.5% 이상입니다. identity_missing 기준은 2026-10-19 04:46Z 사용자 결정으로 뺐고 비율은 기록만 합니다. 이미 조사한 start_mismatch·channel_mismatch도 terminal 격리하며 시각을 강제로 확정하지 않습니다.
4. I5의 모든 행이 첫 결과를 낸 뒤(미시도 0) 같은 투입 cohort의 finalized·provider 행에서 무작위 20건을 한 번 뽑아 공개 watch의 양쪽 시각을 대조합니다. 조회 실패 표본을 교체하지 않습니다. 최근 30일 ENDED 전체를 분모로 실제 handler의 길이 표시 조건을 적용합니다.
   - 2026-10-19 사용자 결정("1번으로 진행해"): 남은 24시간 재시도 꼬리(identity_missing·login_required·end_time_missing·request_failed 유예 등)를 기다리지 않고 미시도 0 시점에 두 검증을 합니다. 남은 재시도는 감시를 유지한 채 자동으로 끝나게 두고, 최종 상태 분포는 감시가 끝날 때 따로 기록합니다. 서비스 중지·재시도 SQL 앞당기기·6초 간격 코드 변경은 택하지 않았습니다.
   - 한계: 확정 행의 시각은 이후 바뀌지 않습니다(진행 행 갱신은 `outcome='pending'`일 때만, 세션 시각 갱신은 `times_source='observed'`일 때만 일어납니다). 그래서 표본 행의 대조 결과는 꼬리를 기다려도 같습니다. 다만 꼬리에서 새로 확정되는 행은 표본 모집단에서 빠지고(00:35Z 재개 직후에만 28건 이상), 표시 비율은 미시도 0 시점의 값이라 이후 오를 수 있습니다.
   - 2026-10-19 08:57~08:58Z 수행 완료: 20/20 일치(최대 차이 0초), 표시 비율 98.8%(방송하지 않은 기록 제외 99.4%). 다시 뽑지 않습니다. 남은 일은 감시 종료 뒤 최종 분포 기록입니다.
5. I4 완료 판정과 I8 보류를 유지합니다. 단계마다 7절과 계획 6단계에 결과를 갱신하고 승인된 docs PR squash 병합으로 게시합니다. 비밀 파일 전체·주변 문맥을 출력하지 말고 설정 키만 제한적으로 확인합니다.

## 9. 인계 프롬프트

다음 세션에 붙여 넣을 실행 지침입니다.

```text
ultracode로 hololive-bot 방송 시각 확정의 남은 I5 처리와 완료 후 검증을 이어받아 주십시오. 모든 보고는 한국어 합쇼체로 작성합니다.

먼저 읽을 것:
- /home/kapu/work/iris-stack/AGENTS.md
- /home/kapu/work/iris-stack/hololive-bot/AGENTS.md
- /home/kapu/work/iris-stack/.agents/workflows.md
- /home/kapu/work/iris-stack/hololive-bot-stream-duration/docs/current/plans/2026-10-18-stream-duration-handoff.md (1·2·7·8절)
- 같은 디렉터리의 2026-10-18-broadcast-time-finalization.md (5·6단계, 운영 SQL, 남은 일)
- 운영 DB·compose.env는 stack-platform-ops, 재생성은 hololive-bot-ops를 따릅니다.

현재 상태(이 프롬프트를 쓰기 전에 7절 I5 행과 "I5 실행·감시 기록"의 마지막 항목으로 갱신하십시오. 아래는 2026-10-19 08:58Z 값입니다):
- 03:52Z 새 영상 identity_missing 34/42(81%) 중단 뒤 사용자 결정으로 04:23Z 조건부 재개 7/8, K=200·hololive-api 단독 재생성. healthy·재시작 0.
- 운영 hololive-api는 다른 작업(#662)이 배포한 108ecb2e6 image입니다(f089d9a64 포함). 이 revision에서의 재개는 사용자가 선택했습니다. 04:23Z 재생성 직후 API의 Iris 연결은 약 3분 30초 재시도 뒤 연결됐습니다.
- 인계하는 세션은 K=0 인계 중단을 먼저 하고 자기 감시를 종료합니다. 다음 세션은 K=0을 확인한 뒤 시작합니다.
- 08:51:45Z 미시도 0. 투입 9,745건: finalized 9,426, conflict 35, exhausted 8, pending 276(전부 재시도 대기).
- conflict: start_mismatch 23·channel_mismatch 12, 전부 원래 시각 보존.
- 완료 후 검증은 끝났습니다: 무작위 확정 20건 20/20 일치(최대 차이 0초), 30일 표시 비율 98.8%(방송하지 않은 기록 제외 99.4%).
- I4 완료: 최초 신규 종료 30건 중 공개 26/26 시간 내 확정. 전체 27/30이며 나머지는 비공개 2·삭제 1, 멤버십 1은 확정입니다.
- I8 카카오 화면은 사용자 선택으로 보류합니다. 방 접근·발송·화면 열기 금지입니다.

승인과 재개:
- K=200, API 단독 recreate, 기존 backlog 처리와 외부 확인 증가, 중단 시 K=0, docs push·PR·squash 병합은 승인되었습니다. 재투입 SQL은 실행하지 않습니다.
- 조건부 반복 재개는 최대 8회 중 7회 사용, 잔여 1회, 기한 2026-10-20 14:35:50Z입니다(04:23Z 기준, 이후 사용분은 7절에서 확인). 횟수를 초기화하지 않습니다.
- 재개 전 같은 두 충돌 사유뿐인지, 원래 시각 보존·신규 queue conflict 0·backlog target 0·수집 지표 정상·30분 새 conflict 0·K=0·같은 revision을 확인하고 감시 소유권을 인수합니다.
- 마스터 compose.env K=200 → scoped sync dry-run/apply → hololive-api만 --no-build --no-deps --pull never로 재생성 → K·health/readiness·같은 image·다른 컨테이너 불변 확인 → 60초 감시 순서입니다.
- 중단: 5분 p99 >10초, 5분 request_failed 3건 이상·5% 이상, 30분 최종 결과 중 conflict 3건 이상·0.5% 이상. 허용 외 충돌 사유·충돌 행 시각 변경·신규 queue 충돌도 멈춥니다. identity_missing 비율은 사용자 결정(04:46Z)으로 중단 조건이 아니며 기록만 합니다.
- 조건 충족 시 K=0·API 단독 재생성·backlog target 0을 확인합니다. 같은 두 사유의 재개만 남은 횟수·기한·검증 조건 내에서 허용됩니다. 다른 사유·지연·요청 실패·감시 실패는 재개하지 말고 보고합니다.

20건 대조와 표시 비율은 미시도 0 시점에 이미 했으므로 다시 뽑지 마십시오(8절 4). 남은 재시도는 감시를 유지한 채 끝나게 두고 최종 분포를 기록하십시오. 미확정·충돌을 성공으로 바꾸거나 조회 실패 표본을 교체하지 않습니다.
시각 롤백 SQL·채널 수정·conflict 재큐잉·계획 밖 운영 데이터 쓰기·카카오 방 사용은 별도 승인 사항입니다.
운영 조회는 세션마다 읽기 전용 보호를 증명하고 보고·문서는 집계만 남깁니다. 영상 ID·제목·payload·비밀값을 출력하지 마십시오.
원 checkout의 미커밋 작업은 건드리지 말고 기존 작업 공간과 인계 2절의 격리 게시 환경을 확인하십시오. 마지막 결과·한계·남은 승인을 문서와 최종 보고에 남기십시오.
```
