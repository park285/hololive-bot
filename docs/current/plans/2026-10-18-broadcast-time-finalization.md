# 방송 시작·종료 시각 확정과 기존 기록 보정

작성: 2026-10-18, 같은 날 검토를 반영해 개정했다. 브랜치 `feat/stream-duration`(worktree `hololive-bot-stream-duration`)의 `!방송기록`·`!라이브` 표시 변경에서 이어지는 작업이다.

## 목표

- 종료된 방송(`youtube_live_sessions.status='ENDED'`)의 시작·종료 시각을 YouTube 아카이브의 `liveBroadcastDetails.startTimestamp`/`endTimestamp`로 확정한다. 지금은 대부분이 "종료를 감지한 수집 예정 시각"이다.
- 확정하지 못한 방송은 마지막 사유를 남기고, 확정된 것처럼 표시하지 않는다.
- 기존 `ENDED` 10,101건(2026-01-22~)도 같은 경로로 보정한다.
- `!방송기록`은 확정 여부에 따라 정확한 시작~종료·방송 길이, 또는 확인 불가 안내를 표시한다. 정렬(`history_sort_at`)은 보정된 종료 시각을 따른다.

## 확인한 사실 (2026-10-17~18, 운영 DB 읽기 전용 조회, 공개 YouTube 페이지 표본, 코드 대조)

- **원인**: 방송 목록 수집이 종료를 먼저 감지하면(마지막 확인 후 중앙값 1.8분) 세션이 `ENDED`가 되고 `live_check_videos.sql`(LIVE·지난 UPCOMING만)에서 빠진다.
  - 영상 확인은 마지막 확인 후 약 4.5분(신선도 예산 `2×2분+30초`)에야 가능해진다.
  - 최근 4일 `EXPLICIT_END` 334건 중 324건은 종료 전후 영상 확인이 0회였다. 실행된 6건은 모두 YouTube 종료 시각을 받아 0초 차이로 저장됐다.
- **시각 정의**: 종료 근거에 `EndedAt`이 없으면 `ended_at = EffectiveAt`(`reconcile/live/apply.go` `endSession`)이다.
  - 최근 2일 live·채널·영상 관측 36만여 건 중 `source_event_at`이 있는 것은 0건이라, `EffectiveAt`은 항상 `ScheduledFor`(수집 예정 시각)다(`contracts/sourceobservation/envelope_validation.go`).
  - 수집 지연(`observed_at − scheduled_for`)은 p99 약 5초, 최대 약 8분이다.
- **ENDED 세션의 현재 처리**: 이미 `ENDED`인 세션에 영상 확인이 오면 `videoLifecycleGate`가 `KEEP_ENDED`를 반환한다(`sourceobservation/live_check_consumer.go`).
  - 가용성 행만 쓰고, unresolvable 추적·pending·head·세션 쓰기는 없다.
  - 세션 upsert는 `started_at`·`ended_at`을 `COALESCE(기존, 새 값)`로 유지하고, head upsert는 `ended_at`을 덮어쓴다.
- **품질** (최근 30일, 시작 시각 있는 2,283건): `|ended_at − last_live_positive_at| ≤ 5분`이 90.8%다. 나머지는 다음과 같다.
  - `EXPLICIT_END` 공개 아카이브 165
  - `SCOPED_ABSENCE` 25 (방송 길이 중앙값 약 68시간)
  - 멤버십 5
  - 상태 미상·확인 없음 10
  - `UNRESOLVABLE_VIDEO` 5
- **아카이브 표본** (watch 페이지, 영상 ID 미기록):
  - 신 lifecycle 21건: 공개·상태 미상 12/12와 멤버십 2/2 페이지에 `endTimestamp`가 있었다. 비공개 5건·삭제 1건은 없었다.
  - YouTube 종료 시각과 마지막 확인 시각의 차이는 −4.3분~+2.9분이었고, 저장 `ended_at`은 실제보다 5분~19일 늦었다.
  - 아카이브 길이와 실제 방송 길이의 차이는 대부분 ±30초였다. 편집된 아카이브는 −4분이었다.
- **옛 기록** (`lifecycle_origin='legacy_unknown'`, head 없음, 5,536건) 표본 8건:
  - 8/8이 공개이고 `endTimestamp`가 있었다.
  - 저장 `ended_at`은 −16.6분~+22분 차이였다.
  - 저장 `started_at`은 5/8건이 실제보다 1.9~7.2시간 늦었다.
- **lifecycle 분포** (`ENDED` 10,101건):
  - `legacy_unknown`(head 없음) 5,536
  - `observed`이고 방송 확인 있음 4,341
  - `observed`이고 방송 확인·시작 시각 없음 225 (시작 전 취소)
  - 모두 현재 운영 roster 채널이다.
- **비공개·삭제·봇 확인**: 익명 player가 `videoDetails` 없이 응답하면 모두 `UNKNOWN identity_missing`이다(`youtubejs/src/live-check.mjs` `playerIdentity`). 셋을 구분할 입력이 없다.
- **요청 실패**: 쿨다운·속도 제한·timeout·일시 오류도 `UNKNOWN request_failed` 관측으로 발행된다(`youtubejscollector/live_check.go` `liveCheckRequestFailed`).
- **멤버십**: 운영 수집 결과는 `identity_confirmed=true`이지만 시각이 없다. 수집기는 InnerTube player 응답에 `microformat`이 있을 때만 시각을 싣는다(`readBroadcastDetails`).
- **원본 관측 보존**: `video_live_check` 약 5일, `live_snapshot` 약 10일. 저장된 원본만으로 과거를 보정할 수 없다.
- **projection**:
  - 대상 계산은 5초 주기이고 READ COMMITTED에서 statement마다 새 커밋을 본다(`targetprojection/repository.go`).
  - membership이나 scheduling identity(priority·poll interval·enabled)가 바뀌면 generation을 새로 만들고 target 약 600행을 다시 쓴다. 현재 하루 약 197개다.
  - `NotBefore`는 hash에 들어가지 않아(`normalize.go` `hashNormalizedTargets`) 제자리에서 갱신된다.
  - 같은 (subject, kind)가 다른 scheduling 값으로 두 번 들어오면 projection 전체가 거부된다(`acceptTarget`).
  - 상한은 영상 확인 입력 1,000(초과 시 거부), 전체 target 10,000(`MaxProjectionTargetCount`)이다.
- **수집기 예산**: 영상 확인 후보끼리는 priority 순서로 처리한다(`joblease/queries/repository_candidates_0144_02.sql`).
  - YouTube.js 동시 실행 상한(`YouTubeJSMaxInflight`)과 요청 간격(`RequestInterval`, 기본 2초)은 방송 목록·채널 확인 등 모든 YouTube.js 작업이 함께 쓴다(`collectorruntime/scheduler_build.go`).
  - 운영 worker 프로필의 실제 값은 확인하지 않았다.
- **영향 범위** (확인함):
  - 알림 worker 조회(0085, 0132, 0293, upcoming 후보)와 생일 방송 조회는 종료된 세션의 시각을 읽지 않는다.
  - live review 영수증은 append-only이고, `reviewed_at`은 `UPCOMING`에만 계산된다.
  - 시각 UPDATE는 영상별 `refresh_youtube_live_runtime_state`만 발동하며, runtime state에는 `ENDED` 행이 없다.
  - `history_sort_at`은 생성 열이라 자동으로 따라가지만 정렬 색인을 갱신하므로 HOT 갱신이 되지 않는다.
- **규모**: 신규 종료는 하루 약 80~90건, 보정 대상은 약 9,700건이다.
  - 기존 `ended_at < status_observed_at` 추정으로는 "이미 provider 값인 행"을 가릴 수 없다. 영상 확인 없이도 참인 행이 있다.

## 설계 결정

1. **종료 후 확정을 따로 둔다.** 종료 판정 시점과 reducer 상태 기계는 바꾸지 않는다. 종료 뒤 같은 `video_live_check`로 아카이브 시각을 받아 시각만 확정한다. 신규 종료와 과거 보정이 같은 경로를 쓴다.
   - 대안 1: 종료를 영상 확인이 올 때까지 미루는 방식. 종료·pending·finalizer 계약을 바꾸고 과거 보정에 못 쓴다.
   - 대안 2: YouTube Data API. 새 자격 증명이 필요한데 비공개·삭제는 똑같이 얻지 못한다.
   - 대안 3: `lengthSeconds`. 편집 아카이브에서 부정확하고, 시각이 없는 영상에는 이것도 없다.
2. **확정 분기는 consumer에 둔다.** `reconcileVideoLiveCheck`에서 `persistVideoAvailability`의 `applied` 검사 뒤, `videoLifecycleOrUnresolvableFact` 호출 전에 둔다. 조건은 `session.Status == ENDED`다.
   - 이미 잡은 잠금(session→head)으로 전용 UPDATE 하나를 실행하고 reducer는 호출하지 않는다.
   - 대기열 대상이 아닌 `ENDED` 세션의 decision은 기존 `KEEP_ENDED`를 유지한다.
   - 새 decision은 `TIME_FINALIZED`, `TIME_CONFLICT`, `TIME_ATTEMPTED`로 둔다.
3. **시각의 출처.** `youtube_live_sessions.times_source`(`observed` | `provider`)를 둔다.
   - 기존 행은 모두 `observed`로 시작하고 확정 경로가 다시 확인한다. 이미 provider 값인 행도 확인 한 번으로 `provider`가 된다.
   - 종료 순간에는 `pending.EndedAt`이 `video_live_check`의 `VerifiedEndedAt`에서 온 경우만 `provider`다.
   - 세션 upsert는 `ended_at`이 NULL에서 처음 채워질 때만 `times_source`를 쓰고, 그 밖에는 기존 값을 유지한다. 확정 UPDATE만 `provider`로 바꾼다.
4. **진행 상태 표 `youtube_live_time_finalizations`**:
   - `video_id`: PK이며 세션 FK.
   - `outcome`: `pending` | `finalized` | `exhausted` | `conflict` | `members_only` | `reverted`.
   - `reason`: 마지막 UNKNOWN 사유 또는 위반한 규칙 이름.
   - 시도 기록: `attempts`(응답 수준 UNKNOWN), `deferrals`(`request_failed`), `next_attempt_at`, `last_attempt_at`.
   - `observation_id`: `REFERENCES source_observations(id) ON DELETE SET NULL`와 UNIQUE 색인(218 선례). 원본 관측 보존 삭제를 막지 않는다.
   - `observed_started_at`, `observed_ended_at`: 확정 전 값 사본으로 감사·롤백에 쓴다.
   - 이 표가 재시도 계약의 기록이며, 시도 수는 `observation_id`로 중복을 제거해 센다.
5. **확정 규칙.** 공통 조건:
   - `payload.VideoID`와 채널이 세션과 일치한다.
   - `LifecycleFactsTrusted`이다.
   - `start ≤ end`이다. `end ≤ observed_at`은 수집기·계약이 이미 거부한다.
   - 확정 관측이 `CurrentlyLive()`이면 `conflict(provider_live)`로 남긴다. 잘못된 종료를 드러내기 위해서다.

   종료 사유별 경계 검사:

   | 세션 | 상한 | 하한 | 시작 |
   | --- | --- | --- | --- |
   | `observed`, `EXPLICIT_END`·`SCOPED_ABSENCE` | `end ≤ 저장 ended_at + 10분` (수집 지연 최대 약 8분) | `end ≥ last_live_positive_at − 10분` | 저장 시작이 있으면 `\|start − 저장 started_at\| ≤ 15분` |
   | `observed`, `UNRESOLVABLE_VIDEO` | 없음 (저장값이 실제 종료의 하한, `unresolvable.go`) | 위와 같음 | 위와 같음 |
   | 저장 시작 NULL (시작 미관측 종료) | 위와 같음 | 위와 같음 | 검사 생략, provider 값으로 채움 |
   | `legacy_unknown` | 없음 | 없음 | 없음 (저장 시각에 신뢰할 의미가 없음) |

   위반하면 `conflict(<규칙 이름>)`로 남기고 쓰지 않는다.
   - 쓰기: 세션 `started_at`·`ended_at`·`times_source`와 head `ended_at`을 함께 바꾼다(head가 있을 때).
6. **재시도 계약** (`.agents/workflows.md` Failure paths). 근거는 위 4일 재현 데이터다.
   - 시작 조건: `ENDED`, `times_source='observed'`, `end_reason`이 `CANCELLED_BEFORE_LIVE`가 아님, 그리고 시작 시각이나 방송 확인 중 하나가 있음.
   - 응답 수준 UNKNOWN(`identity_missing` 등): 3분, 30분, 3시간, 24시간 뒤에 시도하고 최대 4회다. 신규는 종료 기준, backlog는 투입 기준이다. 4회째에는 `exhausted(<마지막 사유>)`로 끝낸다.
   - `request_failed`(쿨다운·timeout 등): `attempts`를 늘리지 않고 `deferrals`만 늘린 뒤 10분 뒤 다시 시도한다. 최대 12회(약 2시간)이고, 넘으면 `exhausted(request_failed)`로 끝낸다.
   - 멤버십(`MEMBERS_ONLY`이고 시각 없음): 바로 `members_only`로 끝낸다. 7단계 출처가 생기면 다시 대기열에 넣는다.
   - 끝난 뒤에는 시각을 그대로 두고, 표시는 미확정 규칙을 따른다.
   - 지표: 결과·사유별 counter, 종료부터 확정까지의 지연 histogram, 대기열·backlog 길이 gauge.
   - 소유: YouTube plane runtime.
   - 재검토 조건: 7단계 출처 도입, 단계 5 결과로 간격·횟수 조정, 확정률이 목표 아래로 떨어질 때.
   - 배포 보고에는 Fallback delta를 적는다: 신규 재시도 1건(이 계약), 그 밖은 없음.
7. **수집 대상**:
   - 확정 대상 reader는 `live_check_videos.sql`과 한 statement(같은 스냅샷)로 읽는다. 어렵다면 병합할 때 LIVE·UPCOMING 입력을 우선해 같은 영상을 제거한다.
   - **신규 종료** (최근 48시간 안 종료): `pending`인 동안 membership을 유지하고 시도 간격은 `NotBefore = next_attempt_at`으로 표현한다. 종료 시 들어가고 최종 결과 때 나간다.
     - generation 증가는 종료 건마다 약 1회다. 들어가는 변경은 LIVE에서 빠지는 변경과 같은 projection에서 일어난다.
   - **backlog**: 신규 종료 외의 `pending`에서 `LIMIT K`(시작값 200)를 최근 종료부터 고른다.
     - 묶음 안에서는 `NotBefore`를 6초 간격으로 분산해 요청을 분당 약 10건으로 고르게 한다.
     - 다음 묶음은 앞 묶음이 모두 첫 결과를 낸 뒤에 넣는다. 묶음 교체로 늘어나는 generation은 하루 약 70개로 추정한다.
     - 잘림은 대기열 계약이다.
   - reason은 `archive_time_finalization`, priority는 18 이하(UPCOMING 대상의 19보다 낮게)로 둔다.
8. **표시** (`!방송기록`). 판정 순서:
   1. `times_source='provider'`: 정확한 시작~종료·길이.
   2. `observed`이고 `lifecycle_origin='observed'`이고 `|ended_at − last_live_positive_at| ≤ 5분`: 현재 규칙대로 표시.
   3. `exhausted(identity_missing)`: `아카이브를 확인할 수 없어 종료 시각 미확인`. 비공개·삭제·봇 확인을 구분하지 못하므로 사유를 단정하지 않는다.
   4. 그 밖의 `observed`: 신 lifecycle은 시작 시각만, `legacy_unknown`은 `종료`로 표기한 저장 시각만.
   - 옛 기록 판정에는 head 존재나 `last_live_positive_at`이 아니라 `s.lifecycle_origin`을 쓴다. 신 lifecycle 종료는 모두 origin을 `observed`로 바꾼다(`terminateSession`).

## 작업 순서와 의존성

- [x] 0. 미커밋 표시 변경 보완 (선행, 이 브랜치)
  - 조회문 `broadcast_history_repository_0179_01.sql`·`0242_02.sql`에 `s.lifecycle_origin`을 추가한다.
  - `legacy_unknown`은 `started_at`을 시작 시각으로 표시하지 않고 `종료`로 표기한 저장 시각을 쓴다.
  - 시험 사례: `legacy_unknown`, head는 있지만 LIVE 확인이 없는 `ENDED`.
  - 릴리스 A(코드)와 릴리스 B(migration 322)로 나누는 기존 계획은 그대로 둔다.
  - 완료 (2026-10-18): `handlercore.BroadcastHistoryEntry.LifecycleOrigin`과 `trustedBroadcastStartedAt`으로 판정한다. 검증은 bot·template·migrationrunner·db-migrate 시험, handlers·formatter `-race`, dbtest 전체, lint 0 issues, NilAway exit 0, manifest 검사 OK. origin 판정을 끈 overlay로 새 시험 두 개가 실패하는 것을 확인했다.
- [ ] 1. migration 323: `times_source` 열과 `youtube_live_time_finalizations` 표
  - 열: `times_source text NOT NULL DEFAULT 'observed'`. 상수 기본값이라 표를 다시 쓰지 않는다. `SET LOCAL lock_timeout='3s'`(291 선례).
  - 제약: `chk_<table>_<col>_vocab` CHECK.
  - 권한: 312의 조건부 GRANT 패턴.
  - 갱신: schema golden, manifest.
  - dbtest: 경계 fixture, 재적용 멱등성.
- [ ] 2. 정산 (consumer 확정 분기, 전용 UPDATE SQL, 세션 upsert의 `times_source` CASE)
  - 단위·dbtest 확인 항목:
    - 확정
    - 표의 규칙별 `conflict`
    - `provider_live`
    - `legacy_unknown`
    - `identity_missing` 시도 증가와 `exhausted`
    - `request_failed`의 `deferrals`만 증가
    - `members_only`
    - 같은 `observation_id` 재정산 시 시도 수 불변
    - 종료 뒤 title upsert가 `times_source`를 바꾸지 않음
    - runtime state 무변화
    - `history_sort_at` 재정렬
    - live review 영수증 불변
  - 1에 의존한다.
- [ ] 3. 수집 대상 (대기열 reader, target append, 신규·backlog 분리, `NotBefore` 분산)
  - 시험 항목:
    - 1,000·10,000 상한과의 독립성
    - LIVE와 같은 영상의 중복 제거
    - 시도 사이 generation이 바뀌지 않음
    - backlog 묶음 교체 빈도 (`collection_lifecycle_performance_test` 참고)
  - 1에 의존한다.
- [ ] 4. 표시: 설계 8로 바꾼다. 조회문에 `times_source`와 확정 결과를 추가한다. 0·1에 의존한다.
- [ ] 5. 단계적 활성화 (`hololive-bot-ops` 배포)
  - 처음에는 신규 종료만 넣고 backlog는 K=0으로 둔다. 1~2일 관찰한다.
  - 통과 기준:
    - 신규 공개 아카이브의 97% 이상이 종료 1시간 안에 `finalized`
    - `conflict` 0.5% 미만
    - `live_snapshot`·`channel_live_check` 수집 지연 p99가 기준(약 5초)의 2배 이하
  - 이 단계에서 운영 worker 프로필의 `YouTubeJSMaxInflight`·`RequestInterval`을 확인해 backlog K를 정한다.
- [ ] 6. 기존 기록 보정 (backlog)
  - 5를 통과한 뒤 K를 올려 최근 종료부터 처리한다.
  - 약 9,700건, 분당 약 10건이면 약 16시간이다(추정).
  - 중단 조건(하나라도 해당하면 K=0으로 즉시 멈춤):
    - 수집 지연 p99가 기준의 2배를 넘음
    - `request_failed` 비율 급증
    - 이미 공개로 확인된 영상의 `identity_missing` 비율 급증 (봇 확인 의심)
    - `conflict` 급증
  - 롤백:
    - 미리 준비할 SQL: `observed_*` 사본으로 세션 시각을 되돌리고 `times_source='observed'`, `outcome='reverted'`로 둔다.
    - `reverted`는 대기열에서 빠지는 최종 상태다.
    - 운영 실행에는 별도 승인이 필요하다.
- [ ] 7. 멤버십 영상 시각 출처 (독립)
  - InnerTube 다른 client나 watch 페이지 `microformat`에서 시각을 얻을 수 있는지 수집기에서 시험한다.
  - 얻을 수 있으면 `members_only`를 다시 대기열에 넣는다.
  - 최근 30일 약 5건 규모라 1~6과 독립적으로 진행한다.

## 검증

- 각 단계에서 실행할 것:
  - 영향 모듈 `go test`, dbtest 패키지 전체
  - `golangci-lint`(저장소 고정 버전), NilAway, race
  - `check-migration-manifest.sh`
  - `bash tools/checks/check-stack-retry-contract.sh`, `bash tools/checks/check-stack-db-access-policy.sh`
- 게시 전: `scripts/ci/pre-push-gate.sh`
- 운영 확인 (읽기 전용):
  - 결과·사유 분포, 확정률, 종료→확정 지연, 수집 지연 p99
  - 무작위 확정 20건을 공개 watch 페이지의 `start/endTimestamp`와 대조
  - `!방송기록` 표시 비율 재계산. 기대치는 최근 30일 약 99% 이상이고, 남는 것은 `identity_missing` 소진과 멤버십이다.
- 표시: `kakaoformat.Render` 결과 문자열 시험. 실제 카카오톡 화면 확인은 별도로 한다.

## 위험

- **수집 예산 경합**: YouTube.js 동시 실행 상한과 요청 간격을 공유해 방송 목록·채널 확인이 늦어질 수 있다. 대응은 priority 18 이하, 분산된 `NotBefore`, 수집 지연 중단 조건이다.
- **봇 확인 증가**: 요청이 늘면 봇 확인이 늘 수 있다. 봇 확인과 비공개는 같은 `identity_missing`이라 표시 사유를 단정하지 않는다.
- **허용폭의 근거**: 확정 규칙의 허용폭은 표본 35건(아카이브 표본 29 + 운영 provider 종료 6)과 수집 지연 분포에 기반한다. 단계 5의 `conflict` 사유 분포로 조정한다.
- **색인 갱신 비용**: 시각 UPDATE는 정렬 색인을 갱신한다(HOT 아님). backlog는 묶음과 분당 상한으로 나눠 쓴다.

## 필요한 승인

- migration 323의 운영 적용과 YouTube plane 런타임 동작 변경 배포(단계 5): 배포 승인(`hololive-bot-ops`).
- 기존 기록 보정(단계 6): 운영 데이터 쓰기 약 9,700행과 외부 요청 증가. 시작 전 별도 승인이 필요하고, 롤백 SQL 실행도 별도 승인이 필요하다.
- Git: 커밋·push·PR은 요청 시에만 한다.

## 관련 문제

- **멈춘 LIVE 세션** (2026-10-18 사용자 지시로 범위에 포함, 인계 문서 I7):
  - 신선한 확인이 없는 `LIVE` 9건은 모두 `legacy_unknown`이고 head·가용성 기록·종료 후보·pending이 없다. 마지막 기록은 136~146일 전이다.
  - 부재 처리는 positive가 없는 옛 기록의 수명 추적을 시작하지 않으므로(`apply_absence.go`), 어떤 종료 경로로도 끝나지 않는 것으로 보인다(추론).
  - 최근 6일 영상 확인 5,304건이 `UNKNOWN identity_missing`이며, 이 9건과의 관계는 확인이 필요하다.
  - 원인을 조사한 뒤 reconcile을 수정하고, 기존 9건을 정리한다. 정리는 운영 데이터 쓰기라 승인이 필요하다.
  - 정리 결과는 이 계획의 확정 경로와 맞춘다. 예: `exhausted(identity_missing)`.
- **종료 감지 지연**: 10~15분 지연 사례 자체는 확정 경로가 정확도 문제를 대신 해결하므로 이번 범위에서 줄이지 않는다.

## 검토 반영 기록 (2026-10-18)

독립 reviewer의 코드 대조와 추가 운영 조회를 반영했다. 주요 변경은 다음과 같다.

- 비공개·삭제 영상은 `unavailable`에 도달할 수 없어, `exhausted(identity_missing)`과 단정하지 않는 표시로 바꿨다.
- `request_failed`를 `deferrals`로 따로 세도록 했다.
- 묶음 투입을 신규 종료의 상시 membership + `NotBefore`, backlog의 큰 묶음 + 분산 `NotBefore`로 바꿨다.
- 확정 분기 위치를 consumer로 고정했다.
- 옛 기록 판정을 `lifecycle_origin`으로 바꿨다.
- `times_source` upsert 규칙을 정했다.
- 종료 사유별 확정 규칙을 정하고 상한 허용폭을 10분으로 늘렸다.
- migration 요구를 추가했다: vocab CHECK, `lock_timeout`, `ON DELETE SET NULL` FK와 색인, GRANT.
- 재시도 계약 요건을 보완하고 두 check를 추가했다.
- 시작 전 취소를 대기열에서 제외했다.
- 두 입력의 중복 제거를 추가했다.
- `reverted` 최종 상태를 추가했다.
- 중단 조건을 수집 지연과 사유별 비율로 바꿨다.
- 신규 대기열 규모를 하루 약 45건에서 80~90건으로 정정했다.
- 355건 provider 추정을 폐기했다.
