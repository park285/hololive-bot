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
  - 운영 값(2026-10-18 확인, stack-secrets worker 프로필·Prometheus): AP 4대(`youtube-collector-a~d`) 모두 `RequestInterval` 1초, `YouTubeJSMaxInflight` 4, worker 4. 7일 최대 helper 동시 실행은 4/3/4/3이다.
  - 수요: 필요한 RPC는 최근 1시간 약 2.36/s, 7일 최고 2.96/s로 fleet 상한 4/s(85% 경계 3.4/s) 안이다. `video_live_check`는 약 0.016/s다. backlog는 첫 시도를 한 번에 한 영상씩 직전 결과 6초 뒤에 열어 분당 10건을 넘지 않으므로 많아야 0.17/s가 늘어 7일 최고 기준 3.13/s다. 그래서 backlog 시작값 K=200을 유지한다.
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
   - 세션 upsert는 `times_source`를 쓰지 않는다(구현 2026-10-18). 종료 순간의 `pending`에는 근거 관측 kind가 없어 `provider`를 가릴 수 없고, 세션 upsert는 `started_at`·`ended_at`을 `COALESCE(기존, 새 값)`로 지켜 확정 시각을 되돌리지 않는다. 그래서 영상 확인으로 끝난 종료도 확정 확인을 한 번 거쳐 `provider`가 된다. 확정 UPDATE만 `provider`로 바꾼다.
4. **진행 상태 표 `youtube_live_time_finalizations`**:
   - `video_id`: PK이며 세션 FK.
   - `outcome`: `pending` | `finalized` | `exhausted` | `conflict` | `members_only` | `reverted`.
   - `reason`: 마지막 UNKNOWN 사유 또는 위반한 규칙 이름.
   - 시도 기록: `attempts`(응답 수준 UNKNOWN), `deferrals`(`request_failed`), `next_attempt_at`, `last_attempt_at`.
   - `observation_id`: `REFERENCES source_observations(id) ON DELETE SET NULL`와 UNIQUE 색인(218 선례). 원본 관측 보존 삭제를 막지 않는다.
   - `observed_started_at`, `observed_ended_at`: 확정 전 값 사본으로 감사·롤백에 쓴다.
   - 이 표가 재시도 계약의 기록이며, 시도 수는 `observation_id`로 중복을 제거해 센다.
   - 구현에서 더한 열: `queue`(`new`·`backlog`), `retry_base_at`(재시도 기준: 신규는 세션 `status_observed_at`, backlog는 첫 결과 관측 시각), `enqueued_at`, `completed_at`(최종 결과 시각), `updated_at`. 결과 전 backlog만 `next_attempt_at` 없이 `pending`일 수 있고, 최종 상태는 `completed_at`을 가진다(CHECK). 같은 관측의 재정산은 가용성 upsert(`(effective_at, observation_id)`가 더 새로울 때만 반영)가 먼저 거른다.
   - 세션 표에 확정 대상 색인을 두지 않는다. `status_observed_at`은 LIVE·UPCOMING의 반복 관측마다 바뀌어 색인 열이면 HOT 갱신이 막힌다(`TestLiveSessionRepeatedObservationIsHeapOnlyUpdate`). 운영 세션 표는 423쪽·약 10,300행이라 5초 projection마다 순차로 읽는다.
5. **확정 규칙.** 공통 조건:
   - `payload.VideoID`와 채널이 세션과 일치한다.
   - `LifecycleFactsTrusted`이다.
   - `start ≤ end`이다. 계약 검증(`validateLifecycleFacts`)이 `started_at ≤ ended_at ≤ observed_at`을 이미 강제하므로 consumer는 다시 검사하지 않는다.
   - 확정 관측이 `CurrentlyLive()`이면 `conflict(provider_live)`로 남긴다. 잘못된 종료를 드러내기 위해서다.

   종료 사유별 경계 검사:

   | 세션 | 상한 | 하한 | 시작 |
   | --- | --- | --- | --- |
   | `observed`, `EXPLICIT_END`·`SCOPED_ABSENCE` | `end ≤ 저장 ended_at + 10분` (수집 지연 최대 약 8분) | `end ≥ last_live_positive_at − 10분` | 저장 시작이 있으면 `start ≤ 저장 started_at + 15분` (아래 참고) |
   | `observed`, `UNRESOLVABLE_VIDEO` | 없음 (저장값이 실제 종료의 하한, `unresolvable.go`) | 위와 같음 | 위와 같음 |
   | 저장 시작 NULL (시작 미관측 종료) | 위와 같음 | 위와 같음 | 검사 생략, provider 값으로 채움 |
   | `legacy_unknown` | 없음 | 없음 | 없음 (저장 시각에 신뢰할 의미가 없음) |

   시작 검사는 한쪽만 본다(적대적 검토 2026-10-18). 시작 시각 없는 첫 LIVE 관측은 감지 시각을 시작으로 저장한다(`newLiveSession`). 최근 30일 `observed` 2,294건 중 1,124건이 `live_first_seen_at`과 5초 안이라 이 경우로 보이고, 시작 시각이 있던 세션의 감지 지연은 p50 56초·p90 4.4분·p99 10분·최대 2.5시간이다. 그래서 저장 시작은 실제 시작보다 늦을 수만 있고, 늦게 감지한 방송은 대칭 15분 규칙에서 `start_mismatch`가 된다. 아카이브 시작이 저장 시작보다 15분 넘게 늦을 때만 충돌로 둔다.

   위반하면 `conflict(<규칙 이름>)`로 남기고 쓰지 않는다. 규칙 이름: `channel_mismatch`, `provider_live`, `end_after_stored_end`, `end_before_live_positive`, `start_mismatch`. 종료 시각이 없으면 `end_time_missing`, 시작 시각이 없으면 `start_time_missing`을 응답 수준 UNKNOWN 사유로 센다.
   - 쓰기: 세션 `started_at`·`ended_at`·`times_source`와 head `ended_at`을 함께 바꾼다(head가 있을 때). 진행 행 갱신과 한 statement다(`repository_live_time_finalization_write.sql`).
   - 시도 시각 전에 온 관측(예: 종료 전 예정 slot의 확인이 늦게 정산된 경우)은 확정만 반영하고 UNKNOWN·충돌은 쓰지 않는다. 종료 전 LIVE 상태를 보고했을 수 있기 때문이다.
6. **재시도 계약** (`.agents/workflows.md` Failure paths). 근거는 위 4일 재현 데이터다.
   - 시작 조건: `ENDED`, `times_source='observed'`, `end_reason`이 `CANCELLED_BEFORE_LIVE`가 아님, 그리고 시작 시각이나 방송 확인 중 하나가 있음.
   - 응답 수준 UNKNOWN(`identity_missing` 등): 3분, 30분, 3시간, 24시간 뒤에 시도하고 최대 4회다. 신규는 종료 기준, backlog는 투입 기준이다. 4회째에는 `exhausted(<마지막 사유>)`로 끝낸다.
   - `request_failed`(쿨다운·timeout 등): `attempts`를 늘리지 않고 `deferrals`만 늘린 뒤 10분 뒤 다시 시도한다. 최대 12회(약 2시간)이고, 넘으면 `exhausted(request_failed)`로 끝낸다.
   - 멤버십(`MEMBERS_ONLY`이고 시각 없음): 바로 `members_only`로 끝낸다. 7단계 출처가 생기면 다시 대기열에 넣는다.
   - 끝난 뒤에는 시각을 그대로 두고, 표시는 미확정 규칙을 따른다.
   - 지표(구현 2026-10-18): consumer는 관측 트랜잭션 안에서 돌고 Prometheus가 없어, YouTube plane observability 주기에 DB에서 계산한 gauge로 둔다. `hololive_youtube_collection_time_finalization_outcomes{outcome,reason}`(보존 행 수), `..._queue_length{queue}`(new: 최종 결과 전 후보, backlog: pending), `..._recent_finalized{queue,le}`(최근 24시간 finalized 중 지연 ≤ le초 누적 수, 경계 180·1800·3600·10800·86400·+Inf). 누적 event counter·histogram은 아니다.
   - 소유: YouTube plane runtime.
   - 재검토 조건: 7단계 출처 도입, 단계 5 결과로 간격·횟수 조정, 확정률이 목표 아래로 떨어질 때.
   - 배포 보고에는 Fallback delta를 적는다: 신규 재시도 1건(이 계약), 그 밖은 없음.
7. **수집 대상**:
   - 확정 대상 reader는 `live_check_videos.sql`과 다른 statement로 읽고, 병합할 때 LIVE·UPCOMING 입력을 우선해 같은 영상을 제거한다(구현 2026-10-18, `time_finalization_videos.sql`). 기존 입력 상한·결과 형식을 유지하기 위해서다. 운영 roster로 거르지 않는다(I7 정리 대상이 roster 밖 채널이다).
   - **신규 종료** (최근 48시간 안 종료): `pending`인 동안 membership을 유지하고 시도 간격은 `NotBefore = next_attempt_at`으로 표현한다. 종료 시 들어가고 최종 결과 때 나간다.
     - generation 증가는 종료 건마다 약 1회다. 들어가는 변경은 LIVE에서 빠지는 변경과 같은 projection에서 일어난다.
   - **backlog**: 신규 종료 외의 `pending`을 최근 종료부터 K개(시작값 200)씩 묶는다.
     - 첫 시도는 한 번에 한 영상만 연다. `NotBefore`는 마지막 backlog 결과(없으면 투입) 6초 뒤이고, 결과가 들어온 다음 projection에서 다음 영상으로 넘어간다. 요청은 분당 10건을 넘지 않고, 중지·재시작·수집 중단 뒤에도 한꺼번에 due가 되지 않는다.
     - 다음 묶음은 앞 묶음이 모두 첫 결과를 낸 뒤에 넣는다. 묶음 교체로 늘어나는 generation은 하루 약 70개로 추정한다.
     - 재시도 중인 행과 활성 묶음을 합쳐 2,000개를 넘기면 다음 묶음을 열지 않는다. 앞 묶음의 재시도 행은 최종 결과 때까지 대상에 남고(UNKNOWN은 첫 결과 뒤 최대 약 27시간), 봇 확인이 몰리면 이 행이 쌓여 전체 target 상한 10,000에 닿을 수 있다. 상한을 넘으면 projection 전체가 거부되어 모든 수집이 멈추므로 backlog 몫을 고정한다(적대적 검토 2026-10-18).
     - 잘림은 대기열 계약이다.
     - 구현: K는 `YOUTUBE_PLANE_TIME_FINALIZATION_BACKLOG_BATCH_SIZE`(기본 0, 0~1000)다. 후보 조건을 잃은 pending을 뺀 backlog 행을 `(observed_ended_at DESC, video_id)` 순위로 묶음 번호를 매기고, 결과 전 행이 남은 가장 앞 묶음이 활성이다. 활성 묶음의 행은 결과와 무관하게 membership을 유지한다(최종 결과 행은 `completed_at+30일`, 차례를 기다리는 행은 `enqueued_at+365일`로 잠재움). 재시도 중인 행은 묶음과 무관하게 최종 결과 때 빠진다. 예산 2,000은 `roster.go`의 상수다.
   - 신규 상한은 최근 종료 순 300개다.
   - reason은 `archive_time_finalization`, priority는 18 이하(UPCOMING 대상의 19보다 낮게)로 둔다.
8. **표시** (`!방송기록`). 판정 순서:
   1. `times_source='provider'`: 정확한 시작~종료·길이.
   2. `observed`이고 `lifecycle_origin='observed'`이고 `|ended_at − last_live_positive_at| ≤ 5분`: 현재 규칙대로 표시.
   3. `exhausted(identity_missing)`: `아카이브를 확인할 수 없어 종료 시각 미확인`. 비공개·삭제·봇 확인을 구분하지 못하므로 사유를 단정하지 않는다. 신뢰할 시작이 없으면(옛 기록) 저장 시각에 `종료` 대신 `기록`을 붙인다(`… 기록 · 아카이브를 확인할 수 없어 종료 시각 미확인`). 종료를 확인했다는 뜻과 모순되지 않게 하기 위해서다.
   4. 그 밖의 `observed`: 신 lifecycle은 시작 시각만, `legacy_unknown`은 `종료`로 표기한 저장 시각만.
   - 옛 기록 판정에는 head 존재나 `last_live_positive_at`이 아니라 `s.lifecycle_origin`을 쓴다. 신 lifecycle 종료는 모두 origin을 `observed`로 바꾼다(`terminateSession`).

## 작업 순서와 의존성

- [x] 0. 미커밋 표시 변경 보완 (선행, 이 브랜치)
  - 조회문 `broadcast_history_repository_0179_01.sql`·`0242_02.sql`에 `s.lifecycle_origin`을 추가한다.
  - `legacy_unknown`은 `started_at`을 시작 시각으로 표시하지 않고 `종료`로 표기한 저장 시각을 쓴다.
  - 시험 사례: `legacy_unknown`, head는 있지만 LIVE 확인이 없는 `ENDED`.
  - 릴리스 A(코드)와 릴리스 B(migration 322)로 나누는 기존 계획은 그대로 둔다.
  - 완료 (2026-10-18): `handlercore.BroadcastHistoryEntry.LifecycleOrigin`과 `trustedBroadcastStartedAt`으로 판정한다. 검증은 bot·template·migrationrunner·db-migrate 시험, handlers·formatter `-race`, dbtest 전체, lint 0 issues, NilAway exit 0, manifest 검사 OK. origin 판정을 끈 overlay로 새 시험 두 개가 실패하는 것을 확인했다.
- [x] 1. migration 323: `times_source` 열과 `youtube_live_time_finalizations` 표
  - 열: `times_source text NOT NULL DEFAULT 'observed'`. 상수 기본값이라 표를 다시 쓰지 않는다. `SET LOCAL lock_timeout='3s'`(291 선례).
  - 제약: `chk_<table>_<col>_vocab` CHECK.
  - 권한: 312의 조건부 GRANT 패턴.
  - 갱신: schema golden, manifest.
  - dbtest: 경계 fixture, 재적용 멱등성.
  - 완료 (2026-10-18): `323_youtube_live_time_finalization.sql`, manifest `184`, schema golden. 시험 `TestTimeFinalizationMigrationGuardsProgressShapeAndReplays`(진행 행 모양 CHECK 10개 거절, 재적용 시 행 버전 불변, vocab 검증 완료). 세션 표 색인은 두지 않는다(설계 4).
- [x] 2. 정산 (consumer 확정 분기, 전용 UPDATE SQL)
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
  - 완료 (2026-10-18): `sourceobservation/live_time_finalization.go`, `repository_live_time_finalization_{state,write}.sql`. 세션 upsert는 바꾸지 않는다(설계 3). 시험 `live_time_finalization_persist_test.go` 8개(하위 22): 확정과 head·`history_sort_at`, 확정 뒤 upsert가 시각·출처를 되돌리지 않음, 규칙별 `conflict`·상한 없는 `UNRESOLVABLE_VIDEO`·시작 채움·`legacy_unknown`·`provider_live`·채널 불일치, `identity_missing` 4회 일정과 이른 관측 무시·`exhausted`, 이른 확정 반영, `request_failed` 12회 연기 뒤 `exhausted`, `members_only`, replay 시 시도 수 불변, 비대상 `KEEP_ENDED`. 경계 검사·시도 시각·48시간 창을 끈 overlay에서 각각 시험이 실패했다. runtime state는 `ENDED`에 행이 없고 live review 영수증은 append-only trigger로 바뀔 수 없어 따로 시험하지 않는다.
- [x] 3. 수집 대상 (대기열 reader, target append, 신규·backlog 분리, `NotBefore` 분산)
  - 시험 항목:
    - 1,000·10,000 상한과의 독립성
    - LIVE와 같은 영상의 중복 제거
    - 시도 사이 generation이 바뀌지 않음
    - backlog 묶음 교체 빈도 (`collection_lifecycle_performance_test` 참고)
  - 1에 의존한다.
  - 완료 (2026-10-18): `runtime/queries/time_finalization_videos.sql`, `runtime/roster.go` `TimeFinalizationVideos`, `targetprojection/types.go` `appendTimeFinalizationVideos`, 설정 `YOUTUBE_PLANE_TIME_FINALIZATION_BACKLOG_BATCH_SIZE`, 지표 `runtime/time_finalization_metrics.go`. 시험 `targetprojection/time_finalization_test.go`, `runtime/time_finalization_projection_test.go`(후보 제외, 재시도 사이 generation 유지, 묶음 교체·분산·K=0, 10,350행 조회 예산), `runtime/time_finalization_metrics_test.go`, `config/youtube_plane_time_finalization_test.go`.
- [x] 4. 표시: 설계 8로 바꾼다. 조회문에 `times_source`와 확정 결과를 추가한다. 0·1에 의존한다.
  - 완료 (2026-10-18): 조회문 두 개에 `s.times_source`, `f.outcome`, `f.reason`(LEFT JOIN). `handler_broadcast_history.go`의 판정과 formatter `ArchiveUnavailable`. handler 16개 사례와 formatter 사례를 `kakaoformat.Render` 결과로, DB 시험으로 LEFT JOIN NULL과 목록·단건 일치를 확인한다.
- [x] 5. 단계적 활성화 (`hololive-bot-ops` 배포)
  - 처음에는 신규 종료만 넣고 backlog는 K=0으로 둔다. 1~2일 관찰한다.
  - 통과 기준:
    - 신규 공개 아카이브의 97% 이상이 종료 1시간 안에 `finalized`
    - `conflict` 0.5% 미만
    - `live_snapshot`·`channel_live_check` 수집 지연 p99가 기준(약 5초)의 2배 이하
  - 운영 worker 프로필 값은 2026-10-18에 확인했다(위 "수집기 예산"). backlog 시작값 K=200을 유지한다.
  - 진행 (2026-10-18 04:30Z 배포, K=0, 운영 읽기 전용 집계):
    - 배포 직후 48시간 창의 종료 162건 중 161건이 몇 분 안에 `finalized`, 1건은 `pending(identity_missing)`으로 재시도 일정에 있다. `conflict`·WARN·ERROR는 0건이다.
    - 확정으로 바뀐 폭: 종료는 저장값보다 0~7.9분 이르다(중앙값 2.3분). 시작은 0~2.0분 이르다. 아카이브 종료와 마지막 LIVE 확인의 차이는 −5.3~+1.9분이다. 161건 중 156건의 시각이 바뀌었다.
    - 수집 지연 p99는 첫 일괄 확인이 몰린 04:30~04:33Z에만 `channel_live_check` 11.6초, `live_snapshot` 9.3초로 올랐고, 그 전후 3분 단위 구간은 4.2~4.8초다. 같은 때 `hololive_youtube_collection_oldest_due_age_seconds`가 두 표본 동안 약 42시간으로 나왔다. 오래전에 종료된 후보의 첫 시도 시각이 이미 지나 있었기 때문이며, 이 지표에 걸린 경보 규칙은 없다.
    - 05:20Z까지 배포 뒤 새로 종료된 방송은 1건이고 `finalized`다.
  - 데이터 판정 (2026-10-18 06:00Z, 사용자 결정: 시간 대신 데이터로 판정하고 6단계를 바로 시작):
    - 1~2일은 표본을 모으려는 대리 기준이었다. `conflict` 0건으로 0.5% 미만을 95% 신뢰로 보이려면 약 600건(최근 3일 종료 249건, 하루 약 83건 기준 7일 남짓)이 필요해, 1~2일을 더 기다려도 통계적 근거는 크게 늘지 않는다.
    - 기준 2 충족: 확정 행 172건 중 `conflict` 0건.
    - 기준 3 충족: 수집기 교체 뒤(05:19Z~) p99 `channel_live_check` 4.65초, `live_snapshot` 5.49초(기준 2배 10초 이하). 배포 때 2분 동안 `video_live_check` 163건(분당 약 80건)이 몰렸을 때도 11.0~11.5초였다. backlog 첫 시도는 6초에 하나(분당 10건 이하)라 그 8분의 1 이하다.
    - 기준 1 일부: 확정 가능률은 신 lifecycle 첫 시도 161/162(99.4%)다. "종료 1시간 안" 부분의 근거인 종료 직후 아카이브 준비 시간은 표본이 적다(종료 1시간 안 시도 2/2, 조사 때 종료 전후 영상 확인 6/6). 이 불확실성은 신규 종료 경로에만 해당하고, backlog와 같은 성격인 종료 6시간 이상 지난 아카이브는 151/152가 확정됐다.
    - 남은 판정: 기준 1은 배포 뒤 신규 종료 30건으로 6단계와 병행해 판정한다. 06:00Z에 3건이고, 최근 3일 시간대별 종료 수로 보면 오늘 UTC 13~14시 무렵 30건이 된다. 종료는 UTC 12~17시에 몰리므로 그 시간대 수집 지연은 6단계 중단 조건으로 감시한다.
  - 네 번째 세션 판정 방법 보완 (07:23Z): 배포 뒤 `lifecycle_origin='observed'`인 실제 신규 종료를 `status_observed_at,video_id`순으로 고정하고 취소·시작/LIVE 근거 없는 행·migration 324의 legacy 정리 행은 제외한다. 진행 행은 LEFT JOIN하여 아직 확인하지 않은 행도 분모에 남긴다. 최초 30건 각각의 종료 관측 후 1시간이 지나야 판정하며, 공개 여부 미확인은 별도로 집계한다. 성공 여부는 관측 종료→확정과 provider 종료→확정을 구분해 기록한다. 07:23Z에는 후보 5건·관찰 창 종료 3건으로, 아직 기준 1을 판정하지 않는다.
  - 기준 1 최종 표본 판정 (2026-10-18 14:28~14:32Z):
    - 04:30Z 이후 실제 신규 종료 최초 30건의 마지막 1시간 관찰 창은 14:23:37Z에 닫혔고 14:27:51Z 집계에서 30/30을 확인했다. 공개 26건·멤버십 1건·identity_missing 3건이다.
    - 공개 아카이브는 26/26(100%)이 관측 종료와 provider 종료 양쪽 기준으로 1시간 안에 finalized되어 **표본 기준 통과**다. 공개 최대 지연은 관측 종료 기준 30.029분, provider 종료 기준 34.499분이다. 멤버십 1건도 양쪽 기준 시간 내 확정했다.
    - 미확정 3건은 별도 공개 watch 조회 3회로 비공개 2건(LOGIN_REQUIRED)·삭제 1건(ERROR)을 확인했다. 식별자·제목·payload는 기록하지 않았다. 이 3건을 공개 아카이브 성공률의 분모에서 제외하지만 전체 표본에서는 남긴다. 전체 종료 30건의 시간 내 확정률은 27/30(90%)로 별도 보고한다.
    - 기준 2·3도 유지된다. 14:32Z 신규 queue 209건은 finalized 204·pending 4·exhausted 1·conflict 0이다. 05:19Z 이후 실제 수집 지연 p99는 channel_live_check 4.641초(32,919건)·live_snapshot 4.934초(38,108건)다.
    - 한계: 공개 26건의 관측 표본에 대한 판정이며 장기 모집단의 97%를 통계적으로 보장하지 않는다. 비공개·삭제는 응용 프로그램의 identity_missing 계약을 그대로 유지하고, 공개 페이지 조사 결과로 운영 사유를 덮어쓰지 않았다.
- [ ] 6. 기존 기록 보정 (backlog)
  - 5단계 기준 2·3의 데이터 판정 뒤 K를 올려 최근 종료부터 처리한다(2026-10-18 사용자 결정, 기준 1은 병행 판정).
  - 약 9,700건이다. 첫 시도는 직전 결과 6초 뒤에 하나씩 열려 분당 10건이 상한이고, 수집·정산 지연을 더하면 분당 약 5~8건, 하루 안팎으로 본다(추정).
  - 중단 조건(하나라도 해당하면 K=0으로 즉시 멈춤):
    - 수집 지연 p99가 기준의 2배를 넘음
    - `request_failed` 비율 급증
    - 이미 공개로 확인된 영상의 `identity_missing` 비율 급증 (봇 확인 의심)
    - `conflict` 급증
  - 롤백:
    - 미리 준비할 SQL: `observed_*` 사본으로 세션 시각을 되돌리고 `times_source='observed'`, `outcome='reverted'`로 둔다(아래 "운영 SQL 초안").
    - `reverted`는 대기열에서 빠지는 최종 상태다.
    - 운영 실행에는 별도 승인이 필요하다.
  - 멈춤: K를 0으로 두고 API를 재시작하면 다음 projection에서 backlog 대상이 빠진다. 진행 행은 그대로 남아 K를 다시 올리면 이어진다.
  - 실행 (2026-10-18 07:20~07:23Z):
    - 기존 `f089d9a64136f1b5322d1a96fee4d68412433cee` arm64 image를 유지한 채 K=200으로 API만 07:20:46Z 재생성했다. healthy·재시작 0, bot/admin health·LLM readiness 통과, 다른 컨테이너 ID 불변을 확인했다.
    - `compose.env` 마스터를 먼저 수정하고 scoped sync의 dry-run·apply·마스터/미러 일치·root:hololive-runtime 0640을 확인했다. 설정 복구본은 인계 문서 "I5 실행·감시 기록"에 적었다.
    - 07:21:26~30Z 아래 투입 SQL을 1,000건 9회·745건 1회 실행했다. 매회 읽기 전용 사전 count와 별도 `BEGIN READ WRITE; SET LOCAL lock_timeout='3s'` 트랜잭션을 사용했고, 각 COMMIT을 확인했다. 삽입 합계 9,745건, 미투입 후보 0건이다. 시각 롤백·멤버십 재투입 SQL은 실행하지 않았다.
    - 사전 30분 실제 수집 지연(`observed_at-scheduled_for`) p99는 `channel_live_check` 4.508초·`live_snapshot` 4.765초였다. 07:22:51Z 직전 5분은 3.938초·4.616초, backlog 확정 10건·pending 9,735건이다. 완료 검증과 혼동하지 않는다.
    - 부모가 kapu의 임시 감시 프로세스로 60초마다 집계한다. 현재 사용자 승인 중단 기준은 5분 p99 >10초, request_failed 3건 이상·5% 이상, 이전 48시간 공개 확인 집합의 5분 identity_missing 3건 이상·5% 이상, **직전 30분 최종 결과 대비 conflict ≥0.5%이면서 3건 이상**이다. conflict만 최초 5분·단발 중단에서 두 번째 재개 때 조정했다. 작은 이상 징후의 중단은 늦어질 수 있으나 단발 충돌은 계속 terminal 격리되며 제품의 시각·identity 검증은 바꾸지 않았다. 공개 이력 없는 UNKNOWN은 봇 확인으로 단정하지 않는다.
    - 중단·조회 실패·최대 48시간·backlog 전량 최종 상태 중 먼저 발생하는 때 감시가 부모에게 결과를 전달한다. K=0 복귀는 다음 projection에서 backlog 첫 시도·재시도를 제거하지만 신규 종료·이미 발행된 관측 정산은 멈추지 않는다. 진행 행과 확정한 시각은 유지한다.
  - 중단·재개 승인 (08:02~08:17Z):
    - `conflict(channel_mismatch)` 1건이 직전 5분 최종 결과 49건의 2.04%여서 K=0으로 복귀했다. API만 08:02:45Z 재생성했고 health/readiness·재시작 0·같은 image·현재 backlog target 0건을 확인했다. 당시 p99 4.615초·4.685초, request_failed 0건이다.
    - 08:03Z 분포는 확정 259·충돌 1·pending 9,485건(미시도 9,416·identity_missing 69)이다. 충돌은 공개 응답의 영상 identity는 맞지만 채널이 저장 채널과 다른 경우이며, 해당 세션의 시각·observed 출처가 그대로 보존되었다. 과거 채널 귀속 경위는 보존된 관측만으로 확정하지 못했다.
    - 사용자는 충돌 행을 terminal 상태로 격리한 채 나머지를 재개하는 것을 승인했다. 중단 지표의 정상 범위 복귀를 확인한 뒤 K=200·API 단독 재생성하며 동일 중단 조건을 유지한다. 채널 수정·conflict 재큐잉·시각 롤백은 하지 않는다.
  - 재개 (08:17~08:18Z): 사용자 승인 후 직전 5분 conflict 0건·p99 4.632초/4.585초를 확인했다. 마스터 K=200·scoped sync·08:17:50Z API 단독 재생성 뒤 health/readiness·재시작 0·동일 image·다른 컨테이너 불변을 확인했다. 새 SQL 투입은 없다. 08:18:06Z 확정 296·격리 충돌 1·pending 9,448건이며 p99 4.840초/4.688초·request_failed/새 conflict 0건이다. 같은 감시 조건을 유지한다.
  - 두 번째 중단·기준 조정·재개 (10:21~11:11Z):
    - 10:21Z `start_mismatch` 1건이 5분 최종 결과 52건의 1.92%여서 K=0·10:22:20Z API 단독 재생성을 실행했다. health/readiness·재시작 0·backlog target 0건을 확인했다. 확정 1,275·격리 충돌 2·pending 8,468건을 보존했다.
    - 새 충돌은 영상·채널·종료 시각이 일치하지만 provider 시작이 저장값보다 28.464분 늦어 허용폭 15분을 넘는 `observed`·`EXPLICIT_END` 사례다. 원래 시각은 바뀌지 않았다. 시각 검증 완화·채널 수정·재큐잉은 하지 않았다.
    - 사용자가 conflict 중단만 30분 창·3건 이상·0.5% 이상으로 조정하고 K=200 재개하는 것을 승인했다. 사전 p99 4.223초/4.766초·30분 conflict 0건을 확인한 뒤 마스터·미러 sync와 11:11:00Z API 단독 재생성을 완료했다. 동일 image·K=200·health/readiness·재시작 0·다른 컨테이너 불변을 확인했다.
    - 11:11:11Z 확정 1,288·격리 충돌 2·pending 8,455건, p99 4.701초/4.798초, request_failed·30분 새 conflict 0건이다. I4는 신규 후보 14건 모두 1시간을 관찰했지만 30건에 못 미쳐 계속 대기한다.
  - 세 번째 중단·동일 기준 재개 (13:57~14:29Z):
    - 직전 30분 최종 결과 203건 중 conflict 3건(1.48%)으로 K=0·13:58:09Z API 단독 재생성을 실행했다. 동일 image·health/readiness·재시작 0·다른 컨테이너 불변·현재 backlog target 0을 확인했다.
    - 중단 뒤 확정 2,536·충돌 8·pending 7,201건이다. 채널 불일치 2건·시작 불일치 6건 모두 원래 시각이 보존되었다. 시작 불일치 6건의 provider 시작 차이는 +16.217~+97.206분이다. 누적 최종 결과 충돌률 8/2,544(0.31%)와 30분 집중률을 구분해 보고했다.
    - 사용자는 **현재 기준으로 재개**를 선택했다. conflict 기준을 누적률로 바꾸지 않았다. 14:28Z 30분 conflict 0건·p99 4.586초/4.565초를 확인하고 scoped sync·14:29:35Z API 단독 재생성을 완료했다. K=200·동일 image·health/readiness·재시작 0·다른 컨테이너 불변·마스터/미러 일치를 확인했다.
    - 14:29:47Z 재개 표본은 확정 2,552·충돌 9·pending 7,184건, p99 4.701초/4.958초, request_failed 0·30분 새 conflict 1건이다. 재개 때 기존 재시도에서 start_mismatch 1건이 추가로 격리되었으며 30분·3건 중단 조건은 충족하지 않는다.
  - 네 번째 중단·조건부 반복 재개 승인 (14:32~14:35Z):
    - 30분 conflict 3/57(5.26%), p99 5.582초/5.749초로 K=0·14:34:21Z API 단독 재생성을 완료했다. 건강·준비 상태·재시작 0·동일 image·다른 컨테이너 불변·현재 backlog target 0을 확인했다. 확정 2,596·충돌 12·pending 7,137건을 보존했고 충돌 12/12의 원래 시각이 유지되었다.
    - 사용자가 이번 9,745건에 한해 최대 8회·2026-10-20 14:35:50Z까지 조건부 반복 재개를 승인했다. 같은 start_mismatch·channel_mismatch뿐이고 원래 시각 보존·현재 backlog target 0·수집 지표 정상·30분 새 conflict 0을 확인한 뒤 K=200·API 단독 재생성한다. 다른 사유·지연·요청 실패·감시 실패는 재개하지 않는다. 30분·3건·0.5% 중단 기준은 바꾸지 않는다. 승인된 재개 사용 횟수는 5/8이다.
  - 조건부 재개 1/8 (15:05Z): 허용 외 충돌 사유·원래 시각 변경·신규 queue conflict·현재 backlog target·30분 새 conflict가 모두 0이고 p99 4.656초/4.835초·request_failed 0임을 확인했다. K=0·기존 revision 확인 뒤 마스터 K=200·scoped sync·15:05:08Z API 단독 재생성을 완료했다. 건강·준비 상태·재시작 0·다른 컨테이너 불변·마스터/미러 일치가 확인됐다. 재개 표본은 확정 2,600·충돌 12·pending 7,133건이다.
  - 다섯 번째 중단 (15:25~15:26Z): 30분 conflict 3/164(1.83%)로 K=0·15:26:30Z API 단독 재생성을 실행했다. 동일 image·건강/준비 상태·재시작 0·다른 컨테이너 불변·마스터/미러 일치·backlog target 0을 확인했다. 확정 2,765·충돌 15·pending 6,965건이며 허용 외 사유·원래 시각 변경·신규 queue conflict가 모두 0이다. 조건부 재개 사용 1/8을 유지하고 15:54:32Z 이후 새 사전 검증으로 2회차를 판단한다.
  - 조건부 재개 2/8 (15:55Z): 허용 외 사유·원래 시각 변경·신규 queue conflict·backlog target·30분 새 conflict 0, p99 4.629초/4.395초·request_failed 0, K=0·동일 revision을 확인했다. scoped sync·15:55:11Z API 단독 재생성 뒤 같은 image·건강/준비 상태·재시작 0·다른 컨테이너 불변·마스터/미러 일치가 확인됐다. 재개 표본은 확정 2,765·충돌 15·pending 6,965건이다.
  - 여섯 번째 중단 (16:15~16:16Z): 30분 conflict 3/158(1.90%)로 K=0·16:16:13Z API 단독 재생성을 완료했다. 동일 image·건강/준비 상태·재시작 0·다른 컨테이너 불변·마스터/미러 일치·backlog target 0을 확인했다. 확정 2,918·충돌 18·pending 6,809건이며 허용 외 사유·원래 시각 변경·신규 queue conflict는 0이다. 사용 2/8, 16:44:55Z 이후 새 사전 검증으로 3회차를 판단한다.
  - 조건부 재개 3/8 (16:45Z): 허용 외 사유·원래 시각 변경·신규 queue conflict·backlog target·30분 새 conflict 0, p99 4.625초/4.415초·request_failed 0, K=0·동일 revision 확인 뒤 scoped sync·16:45:19Z API 단독 재생성을 완료했다. 같은 image·건강/준비 상태·재시작 0·다른 컨테이너 불변·마스터/미러 일치를 확인했다. 재개 표본은 확정 2,923·충돌 18·pending 6,804건이다.
  - 종료 집중 시간대 확인(UTC 12~17시, 17:46Z 집계): channel_live_check p99 4.640초(17,850건), live_snapshot p99 4.972초(20,976건), video_live_check 1,967회 중 request_failed 0회다. 이 구간에는 K=0 중단 시간도 포함되며 연속 K=200 관찰로 해석하지 않는다. 17:46Z backlog는 확정 3,345·충돌 21·pending 6,379건, 조건부 재개 사용 3/8이다.
  - 일곱 번째 중단 (18:21Z): 30분 conflict 3/271(1.11%)로 K=0·18:21:31Z API 단독 재생성을 완료했다. 동일 image·건강/준비 상태·재시작 0·다른 컨테이너 불변·마스터/미러 일치·backlog target 0을 확인했다. 확정 3,639·충돌 25·pending 6,081건이며 허용 외 사유·원래 시각 변경·신규 queue conflict는 0이다. 사용 3/8, 18:50:16Z 이후 4회차 사전 조건을 판단한다.
  - 조건부 재개 4/8 (18:50Z): 허용 외 사유·원래 시각 변경·신규 queue conflict·backlog target·30분 새 conflict 0, p99 3.806초/4.287초·request_failed 0, K=0·동일 revision 확인 뒤 scoped sync·18:50:34Z API 단독 재생성을 완료했다. 같은 image·건강/준비 상태·재시작 0·다른 컨테이너 불변·마스터/미러 일치를 확인했다. 재개 표본은 확정 3,648·충돌 25·pending 6,072건이다.
  - 여덟 번째 중단 (19:23~19:24Z): 30분 conflict 3/242(1.24%)로 K=0·19:24:21Z API 단독 재생성을 완료했다. 동일 image·건강/준비 상태·재시작 0·다른 컨테이너 불변·마스터/미러 일치·backlog target 0을 확인했다. 확정 3,902·충돌 28·pending 5,815건이며 허용 외 사유·원래 시각 변경·신규 queue conflict는 0이다. 사용 4/8, 19:53:00Z 이후 5회차 사전 조건을 판단한다.
  - 조건부 재개 5/8 (19:53Z): 허용 외 사유·원래 시각 변경·신규 queue conflict·backlog target·30분 새 conflict 0, p99 4.011초/4.671초·request_failed 0, K=0·동일 revision 확인 뒤 scoped sync·19:53:24Z API 단독 재생성과 공통 검증을 완료했다. 재개 표본은 확정 3,907·충돌 28·pending 5,810건이다.
  - 신 lifecycle 첫 확인 완료(22:25Z): observed 4,209건 모두 첫 확인을 마쳤다. 전체 확정 5,082(observed 4,101·legacy 981)·충돌 31·pending 4,632건이며, 미시도 4,529건은 모두 legacy이고 재시도는 103건이다. 마지막 재시도까지 최종 결과가 필요하므로 첫 확인 완료를 I5 완료로 처리하지 않는다. 조건부 재개 사용 5/8이다.
  - 사용자 요청 인계 중단(2026-10-19 00:14Z): "핸드오프해서 다른세션에서 진행하자"에 따라 무감시 처리를 막도록 K=0·00:14:14Z API 단독 재생성을 완료했다. 같은 image·건강/준비 상태·재시작 0·다른 컨테이너 불변·마스터/미러 일치·0640·backlog target 0을 확인했다. 이후 세션 감시 프로세스를 종료했다.
    - 00:14:22Z 확정 5,856·충돌 31(start_mismatch 23·channel_mismatch 8)·pending 3,858건이다. 미시도 3,689·재시도 169(identity_missing 159·login_required_unclassified 9·end_time_missing 1)이며 충돌 31/31의 원래 시각이 보존되었다. 신규 queue conflict 0, p99 4.621초/4.745초, request_failed·30분 새 conflict 0이다.
    - 조건부 재개 사용 5/8·잔여 3회·기한 2026-10-20 14:35:50Z를 유지한다. 인계 중단은 재개 횟수를 소비하지 않았다. 다음 세션은 자기 소유 감시와 동일 검증 조건을 갖춘 뒤 기존 승인 범위에서 K=200으로 이어가며 재투입 SQL은 실행하지 않는다. 20건 대조와 표시 비율은 전량 최종 상태 뒤에 수행한다. 제품 재시도·fallback 변경은 없다.
  - 조건부 재개 6/8 (2026-10-19 00:35Z, 인계받은 세션): 허용 외 충돌 사유·원래 시각 변경·신규 queue conflict·backlog target·30분 새 conflict 0, p99 4.222초/4.669초·request_failed 0(K=0이라 직전 5분 영상 확인 0회), K=0·동일 revision을 확인했다. 마스터 K=200·scoped sync·00:35:01Z API 단독 재생성과 공통 검증(동일 image·건강/준비 상태·재시작 0·다른 컨테이너 불변·마스터/미러 일치·0640)을 완료했다. 00:35:47Z 확정 5,887·충돌 31·pending 3,827건(미시도 3,686)이다. 마지막 첫 시도 뒤 24시간 재시도까지 기다려야 하므로 전량 최종 상태는 2026-10-20 10시(UTC) 전후로 본다(추정).
  - identity_missing 중단 기준 교체 (2026-10-19 02:33Z, 사용자 결정): "이전 48시간 공개 확인 집합" 기준은 backlog 첫 확인에 공개 이력이 없어 분모가 늘 0이었다. 직전 48시간 identity_missing 이력이 없는 새 영상의 5분 identity_missing이 10건 이상·80% 이상이면 멈추도록 바꿨다. backlog 기간 5분 구간 218개에서 이 비율은 중앙값 5.8%, 최대 72.5%(시도 20회 이상)였고, 몰림 구간의 해당 영상 대부분(89건 중 82건, 53건 중 48건)이 30분 재시도에서 확정되어 일시 실패로 보인다(봇 확인 추정). 80%는 관찰한 최악보다 나쁜 거의 전면 차단만 잡는다. 근거 수치는 인계 문서 "I5 실행·감시 기록"에 있다.
  - 아홉 번째 중단 (2026-10-19 03:52Z): 새 영상 identity_missing 34/42(81%)로 K=0·03:52:38Z API 단독 재생성과 공통 검증(healthy·재시작 0·건강/준비 상태·마스터/미러 일치·0640·다른 컨테이너 불변·backlog target 0)을 완료했다. p99 3.706초/4.586초·request_failed 0으로 다른 지표는 정상이었다. 03:55:23Z 확정 7,273·충돌 34·pending 2,439건(미시도 2,173)이며 허용 외 사유·원래 시각 변경·신규 queue conflict는 0이다. 02:40~02:55Z의 74% 몰림은 스스로 회복했고 해당 영상 110건 중 87건이 재시도에서 확정됐다. 운영 API는 다른 작업(#662)이 배포한 `108ecb2e6` image였다. 이 중단은 조건부 재개 대상이 아니어서 재개는 사용자 결정을 기다린다(사용 6/8).
- [x] 7. 멤버십 영상 시각 출처 (독립)
  - InnerTube 다른 client나 watch 페이지 `microformat`에서 시각을 얻을 수 있는지 수집기에서 시험한다.
  - 얻을 수 있으면 `members_only`를 다시 대기열에 넣는다.
  - 최근 30일 약 5건 규모라 1~6과 독립적으로 진행한다.
  - 조사 (2026-10-18, 외부 요청 10회, 영상 ID 미기록): 멤버십 watch HTML 4/4에 시작·종료 시각·영상·채널 identity·`isLiveNow=false`가 있었다. `WEB_EMBEDDED` player 0/2, WEB `/next` 0/2. 공개 대조 1건은 watch와 WEB player의 시각이 같았다.
  - 완료 (2026-10-18, 수집기): `youtubejs/src/live-check.mjs`가 멤버십이고 시각이 빠진 종료 영상만 같은 RPC 슬롯에서 watch를 한 번 더 읽는다. 영상·채널 identity, `isLiveNow=false`, 양쪽 시각, player 시각과의 일치를 요구하고 가용성·player 사실은 그대로 둔다. 조회·검증에 실패하면 기존 관측(시각 없음)을 보낸다. 시험: `live-check.test.mjs`, `youtubejscollector/live_check_test.go`(발행된 멤버십 시각이 `LifecycleFactsTrusted`·`VerifiedEndedAt()`를 만족). 계약 검증은 이미 MEMBERS_ONLY와 시각의 조합을 허용한다.
  - 수집기 배포 뒤 `members_only` 행을 다시 대기열에 넣는다(아래 SQL, 승인 필요). 조사 때 MEMBERS_ONLY 가용성이고 후보 조건을 만족하는 세션은 34건(최근 30일 31건)이었다.
  - 배포 (2026-10-18 05:10~05:18Z, #655): 그때 `members_only` 행이 0건이라 재확정 SQL은 실행하지 않았다("배포 결과").

## 운영 SQL (backlog 투입은 2026-10-18 실행, 나머지는 미실행·별도 승인 필요)

모두 `SET LOCAL lock_timeout='3s'` 트랜잭션에서 실행하고, 먼저 같은 조건의 `count(*)`를 확인한다.

- backlog 투입(단계 6): 1,000행씩 반복한다. 신규 창(48시간) 안의 종료는 넣지 않는다. 첫 시도는 한 번에 하나씩 열리므로 K를 켜기 전후 어느 때 투입해도 된다. 2026-10-18 07:21Z 실행 결과는 6단계에 기록했다.

  ```sql
  INSERT INTO youtube_live_time_finalizations (video_id, queue, outcome, observed_started_at, observed_ended_at)
  SELECT s.video_id, 'backlog', 'pending', s.started_at, s.ended_at
  FROM youtube_live_sessions s
  LEFT JOIN youtube_live_reconciliation_heads h USING (video_id)
  WHERE s.status = 'ENDED' AND s.times_source = 'observed'
    AND COALESCE(h.end_reason, '') <> 'CANCELLED_BEFORE_LIVE'
    AND (s.started_at IS NOT NULL OR h.last_live_positive_at IS NOT NULL)
    AND (s.status_observed_at IS NULL OR s.status_observed_at < statement_timestamp() - interval '48 hours')
    AND NOT EXISTS (SELECT 1 FROM youtube_live_time_finalizations f WHERE f.video_id = s.video_id)
  ORDER BY s.video_id
  LIMIT 1000
  ON CONFLICT (video_id) DO NOTHING;
  ```

- 롤백(단계 6): 확정한 backlog 행의 세션·head 시각을 사본으로 되돌린다. 범위를 줄이려면 `completed_at` 조건을 더한다.

  ```sql
  WITH reverted AS (
      UPDATE youtube_live_time_finalizations f
      SET outcome = 'reverted', completed_at = statement_timestamp(), updated_at = statement_timestamp()
      WHERE f.outcome = 'finalized' AND f.queue = 'backlog'
      RETURNING f.video_id, f.observed_started_at, f.observed_ended_at
  ), sessions AS (
      UPDATE youtube_live_sessions s
      SET started_at = r.observed_started_at, ended_at = r.observed_ended_at, times_source = 'observed'
      FROM reverted r
      WHERE s.video_id = r.video_id AND s.times_source = 'provider'
      RETURNING s.video_id, r.observed_ended_at
  )
  UPDATE youtube_live_reconciliation_heads h
  SET ended_at = sessions.observed_ended_at, updated_at = statement_timestamp()
  FROM sessions
  WHERE h.video_id = sessions.video_id;
  ```

- 멤버십 재확정(단계 7, 수집기 배포 뒤):

  ```sql
  UPDATE youtube_live_time_finalizations f
  SET outcome = 'pending', reason = NULL, attempts = 0, deferrals = 0,
      retry_base_at = statement_timestamp(), next_attempt_at = statement_timestamp(),
      last_attempt_at = NULL, observation_id = NULL, completed_at = NULL, updated_at = statement_timestamp()
  FROM youtube_live_sessions s
  WHERE f.video_id = s.video_id AND f.outcome = 'members_only'
    AND s.status = 'ENDED' AND s.times_source = 'observed';
  ```

- 322 되돌리기(A보다 이전 image로 되돌릴 때만): `322_live_streams_elapsed.sql`의 같은 `VALUES`를 쓰고 방향만 바꾼 CAS다. 전역 본문 md5가 `abab0db8d544c64ee59c98fa0c1d6a3d`일 때만 바꾸고, 결과 md5는 `b306166c7ae45035185ef79b21cea25f`여야 한다.

  ```sql
  UPDATE notification_templates AS target
  SET body = seed.old_body, updated_at = now()
  FROM (VALUES ('CMD_LIVE_STREAMS', $old$…322의 $old$ 본문…$old$, $new$…322의 $new$ 본문…$new$))
      AS seed(template_key, old_body, new_body)
  WHERE target.template_key = seed.template_key
    AND target.channel_id IS NULL
    AND target.body = seed.new_body;
  ```

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
- 1~4·7단계 로컬 결과 (2026-10-18, 마지막 코드 편집 뒤, worktree 루트):
  - `go test -count=1 ./hololive/hololive-api/... ./hololive/hololive-dbtest/... ./hololive/hololive-youtube-collector/internal/runtime/youtubejscollector` 통과. 수집기 전체는 `collectorruntime`만 형제 `shared-go` 미커밋 작업 때문에 빌드되지 않고 `GOWORK=off`로 통과했다.
  - 변경 패키지 8개 `-race` 통과, golangci-lint 0 issues, NilAway exit 0, `check-migration-manifest.sh` OK, youtubejs `npm test`(372)·`npm run typecheck` 통과, `check-stack-retry-contract.sh`·`check-stack-db-access-policy.sh` 통과.
  - 확정 시험은 경계 검사·시도 시각·48시간 창을 끈 overlay에서 각각 실패했다.
  - `scripts/ci/pre-push-gate.sh` 통과(격리 작업 공간, 수집기 커밋까지 포함한 HEAD, 624초). SQL ownership 검사에 맞춰 확정 조회문의 열 이름을 명시한 뒤 runtime·sourceobservation 시험을 다시 통과했다.

## 위험

- **수집 예산 경합**: YouTube.js 동시 실행 상한과 요청 간격을 공유해 방송 목록·채널 확인이 늦어질 수 있다. 대응은 priority 18 이하, 한 번에 한 영상씩 여는 backlog 첫 시도, 수집 지연 중단 조건이다.
- **봇 확인 증가**: 요청이 늘면 봇 확인이 늘 수 있다. 봇 확인과 비공개는 같은 `identity_missing`이라 표시 사유를 단정하지 않는다.
- **허용폭의 근거**: 확정 규칙의 허용폭은 표본 35건(아카이브 표본 29 + 운영 provider 종료 6)과 수집 지연 분포에 기반한다. 단계 5의 `conflict` 사유 분포로 조정한다.
- **색인 갱신 비용**: 시각 UPDATE는 정렬 색인을 갱신한다(HOT 아님). backlog는 묶음과 분당 상한으로 나눠 쓴다.
- **`!라이브` 경과 시간의 기준**: 경과는 저장 `started_at` 기준이다. 시작 시각 없이 처음 감지한 방송(최근 30일 약 49%)은 이 값이 감지 시각이라 실제보다 짧게 보인다. 감지 지연 분포로 보면 보통 1분 안팎(p90 4.4분, p99 10분)이고, 수집이 멈췄다 재개되면 더 커진다. LIVE 중에 영상 확인이 실제 시작을 주어도 세션 upsert가 기존 시작을 지키므로(`COALESCE`) 고쳐지지 않는다. 시작 출처를 세션에 기록해야 해결되며, reducer·세션 upsert의 시작 시각 계약을 바꾸고 이 값을 읽는 다른 경로를 모두 다시 확인해야 하므로 이번 범위에 넣지 않았다. 종료 뒤에는 확정 경로가 아카이브 시작으로 바꾼다.

## 배포 결과 (2026-10-18, 사용자 승인 "커밋 푸쉬등 전부승인")

`hololive-bot-ops` deployment 절차를 따랐다.

- 공통 절차:
  - 묶음마다 PR을 squash 병합하고, 병합한 `main` revision(40자)의 깨끗한 격리 worktree에서 kapu가 `DOCKER_DEFAULT_PLATFORM=linux/arm64 ./build-all.sh --build-only --no-bump hololive-api`(local CI 포함)로 image를 만들었다. 원격 build는 하지 않았다.
  - 전송 전과 중앙 load 뒤에 `arm64`와 revision을 확인하고, 교체 전 중앙 `hololive-api:prod`를 `hololive-api:rollback-stream-duration-<묶음>-<UTC>` tag로 남겼다.
  - migration 파일과 `manifest.txt`는 중앙 deploy tree에 넣고 sha256을 git과 대조했다. 이전 manifest는 `/opt/hololive-bot/compose/deploy-backups/manifest.txt.pre-<번호>-<UTC>`에 있다. `hololive-db-migrate` one-shot 뒤 `up -d --no-build --no-deps hololive-api`로만 바꿨다.
  - 이전 image의 db-migrate는 더 새로운 ledger를 거절한다. 그래서 migration 적용 뒤 image를 되돌릴 때는 앱 컨테이너만 되돌린다.
- 변경 binary: A·B·323·324 묶음은 `hololive-api`만 바꿨다. `hololive-alarm-worker`는 바뀐 표본 데이터(`sampledata`)를 링크하지만 템플릿 관리 경로를 쓰지 않아 교체하지 않았다(revision `f8ce7354…` 유지).
- 재시도·보조 경로 변화(release 기록용):
  - 확정 시도 일정 1개가 새로 생겼다. 일정은 [3분, 30분, 3시간, 24시간]이고, `request_failed`는 10분씩 12회까지 연기한다.
  - I6는 멤버십 종료 영상에 한해 watch 조회 1회를 더한다. 재시도는 없다.

| 순서 | PR · 병합 revision | 서비스 · 교체 시각 | migration | 확인 결과 | 롤백 지점 |
|---|---|---|---|---|---|
| 1 | #651 A `51c0519cc` | `hololive-api` 03:35Z | 없음 | healthy, 재시작 0, WARN·ERROR 0 | `rollback-stream-duration-a-20261018t033506z`(`f8ce7354…`) |
| 2 | #652 B `5d1d405ab` | `hololive-api` 04:03Z | 322 (`applied=1 skipped=182`) | 전역 본문 md5 `b306166c…` → `abab0db8…`, 채널 override 없음, 기동 로그 WARN·ERROR 0 | `…-b-20261018t040324z`(A image). A보다 이전으로 되돌릴 때만 "운영 SQL 초안"의 322 되돌리기 |
| 3 | #653 323·확정 `7bd38b05b` | `hololive-api` 04:30Z | 323 (`applied=1 skipped=183`) | 5단계 "진행" 기록, `hololive_youtube_collection_time_finalization_*` 지표 수집 | `…-c-20261018t043000z`(B image). 323 열·표는 이전 binary에 영향이 없다. 시각 원복은 "운영 SQL 초안" 롤백 |
| 4 | #654 324 `f089d9a64` | `hololive-api` 04:56Z | 324 (`applied=1 skipped=184`) | 대상 9건 `ENDED`, 5분 안에 8건 `finalized`, 1건 `pending(identity_missing)`(조사 때 `LOGIN_REQUIRED`였던 1건으로 보임, 24시간 일정 뒤 `exhausted` 예상). roster 밖 head 없는 legacy LIVE 0건 | `…-d-20261018t045615z`(323 image) |
| 5 | #655 수집기 `7e60b2061` | c 05:10Z(`po-central-cutover.sh deploy`), b 05:11Z(`ap-deploy.sh seoul --apply`), a 05:11~05:14Z·d 05:14~05:18Z(`ap-host-native-deploy.sh`, `HOLO_BOT_VERSION=10.1.0`) | 없음 | 네 곳 모두 collector·issuer 짝이 같은 revision이다. c는 `central collector/issuer completion verified`, a·b·d는 `ap-completion-check.sh` exit 0(a·d `READY`·`first_success`·`NRestarts=0`, b healthy·재시작 0). 교체 뒤 수집 지연 p99 4.6~4.9초 | c `/opt/hololive-bot/compose/backups/po-c-20261018T050931Z`, b `backups/seoul-collector-20261018T051047Z`, a·d `previous` release(`f8ce7354…`) |

- 순서 조정: 3→4는 원래 1~2일 관찰 뒤였다. 배포 직후 48시간 창 162건 중 161건 확정·`conflict` 0건으로 확정 경로가 실제 응답에서 동작함을 확인하고 4를 진행했다. 324 대상 9건은 같은 신규 대기열로 들어갈 뿐이다.
- `members_only` 재확정 SQL: 수집기 배포 시점에 `members_only` 행이 0건이라 실행하지 않았다. 확정 대상 조건을 만족하는 멤버십 종료 세션 33건(조사 때 34건)은 모두 48시간 창 밖이고 확정 행이 없다. 이들은 6단계 backlog에서 새 수집기로 처음 확인한다.
- kapu 로컬 `hololive-api:prod`와 `hololive-youtube-collector:prod`는 이번에 배포한 image다.

## 남은 일

- 5단계는 신규 종료 30건 판정으로 완료했다. 공개 26/26의 시간 내 확정, 비공개 2·삭제 1·멤버십 1의 별도 분류와 표본 한계는 5단계에 기록했다.
- 6단계 backlog: 2026-10-19 00:35Z 조건부 재개 6/8 뒤 03:52Z identity_missing 중단 기준으로 K=0 정지했다. 재개는 사용자 결정을 기다린다(잔여 2회, 기한 2026-10-20 14:35:50Z). 재투입하지 않는다.
- backlog 첫 결과 완료(미시도 0) 뒤: 같은 투입 cohort의 finalized·provider 행에서 무작위 20건을 한 번 뽑아 공개 watch의 양쪽 시각과 대조한다. 조회 실패 표본을 교체하지 않는다. 최근 30일 ENDED 전체를 분모로 실제 handler의 길이 표시 조건을 분자로 계산한다. 이 두 검증은 아직 실행하지 않았다.
  - 2026-10-19 사용자 결정: 24시간 재시도 꼬리를 기다리지 않고 미시도 0 시점에 검증한다. 남은 재시도는 감시를 유지한 채 자동 종료되게 두고 최종 분포는 따로 기록한다. 꼬리에서 나중에 확정되는 행은 표본 모집단에서 빠지고 표시 비율은 그 시점 값이다(인계 문서 8절 4).
  - 첫 시도는 코드상 직전 backlog 결과 6초 뒤 한 영상씩이라 상한이 분당 10건이다. 실측 분당 약 7.2건(건당 약 8.3초)은 6초에 수집·정산 지연이 더해진 값이므로, 다른 부하를 줄여도 크게 빨라지지 않는다(추론). 꼬리는 24시간 재시도 일정이 정한다.
- 실제 카카오톡 화면: 네 번째 세션에서 사용자가 I8 **보류**를 선택했다. 방 접근·발송·화면 열기는 하지 않으며 문자열 시험을 실제 화면 검증으로 대체하지 않는다.

## 관련 문제

- **멈춘 LIVE 세션** (2026-10-18 사용자 지시로 범위에 포함, 인계 문서 I7):
  - 신선한 확인이 없는 `LIVE` 9건은 모두 `legacy_unknown`이고 head·가용성 기록·종료 후보·pending이 없다. 마지막 기록은 136~146일 전이다.
  - 원인 (2026-10-18 조사, 읽기 전용): 9건은 5개 채널에 속하고, 이 채널들은 `members`에 행이 없다(졸업 표시가 아니라 roster 밖). 그래서 채널 수집(`operational_channel_ids.sql`: `is_graduated=false`)과 영상 확인(`live_check_videos.sql`의 roster 조건, `appendLiveCheckVideos`의 roster 검사)이 모두 이 세션을 대상으로 삼지 않는다. head가 없어 부재 판정도 적용되지 않는다. 이 채널의 알림·채널 확인·발송·pending은 0건이고, 다른 조회(LiveQuery·observability)도 roster로 걸러 보이지 않았다.
  - 최근 6일 영상 확인 중 이 9건은 0건이다. `identity_missing` 5,372건은 head가 있는 UPCOMING 42건의 설계된 해소 불가 backoff(약 1시간 간격)에서 왔다.
  - 공개 watch 페이지 확인(9회, 영상 ID 미기록): 8건은 공개 아카이브이고 시작·종료 시각과 채널이 맞았다. 1건은 `LOGIN_REQUIRED`였다.
  - 수정: migration 324가 roster 밖 채널의 head·pending 없는 `legacy_unknown` LIVE를 `ENDED`(종료 시각은 증거가 없어 `last_seen_at`, origin 유지)로 정리하고 `status_observed_at`을 적용 시각으로 둔다. 그러면 신규 확정 대기열(48시간 창, roster 무관)이 아카이브 시각을 확인한다. 예상 결과는 8건 `finalized`, 1건은 응답에 따라 `members_only`·`exhausted`다. 최종 결과 뒤 대상에서 빠지므로 반복 확인이 남지 않는다.
  - roster 밖 LIVE를 영상 확인 대상에 넣는 방안은 택하지 않았다. 수집 대상 계약(운영 roster)을 넓히고, 아카이브를 확인할 수 없는 영상(1건)을 끝낼 경로가 없어 2분마다 반복 확인이 남는다. 신 lifecycle 세션은 head가 있어 같은 경로로 멈추지 않는다. 남는 재발 경로는 방송 중에 채널이 roster에서 빠지는 경우뿐이다(추론).
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

적대적 검토(2026-10-18, reviewer 5개 영역)에서 다음을 반영했다.

- backlog 재시도 행이 쌓여 전체 target 상한을 넘는 경로를 막았다. 재시도 중인 행과 활성 묶음을 합쳐 2,000개로 제한한다.
- backlog 중지·재시작 뒤 지난 기준 시각 때문에 묶음 전체가 한꺼번에 due가 되는 경로를 없앴다. 첫 시도는 한 번에 하나씩 연다.
- 시작 검사를 한쪽으로 바꿨다. 늦게 감지한 방송의 정상 아카이브 시작이 `start_mismatch`가 되지 않게 하기 위해서다.
- 322 되돌리기 SQL을 추가했다.
- 남긴 한계: `!라이브` 경과의 시작 기준(위험 절), watch 조회가 일시 실패하면 멤버십 영상은 `members_only`로 끝나고 재확정 SQL로만 다시 시도한다.
