# 멤버 표시명 정본 미준수와 API 후속 작업 인계

2026-10-05 조사 결과를 다음 작업자에게 넘긴다. 코드는 바꾸지 않았고, 운영 DB는 `transaction_read_only=on` 가드로만 조회했다.

## 목표와 제약

- 사용자에게 보이는 멤버 이름을 정본 규칙 하나로 맞춘다. `!라이브`와 `!예정`이 같은 멤버를 다른 이름으로 표시한다는 사용자 보고가 출발점이다.
- 정본 계약은 [alarm 계약](../contracts/alarm.md)의 "멤버 표시명 예외 계약"이다. 알림은 `members.short_korean_name` → `korean_name` → `misc/vtuber_fallback` 순서를 쓴다. 담당은 `alarm.Repository.GetMemberName`(`queries/repository_0155_07.sql`)과 alarm-worker `format.DisplayMemberName`이다.
- 명령 응답에는 아직 명시된 계약이 없다. 이미 정본을 쓰는 방송기록(`broadcast_history_repository_0179_01.sql`)·캘린더(`calendarMemberDisplayName`)·관리 목록(`alarmcache.FirstMemberName`)은 `short_korean_name` → `korean_name` → `english_name` 순서다. 이 순서를 명령 응답 규칙으로 확정하고 계약 문서에 적는 것을 권한다.
- 식별·dedup·라우팅은 `channel_id`로 한다. 표시명은 표시 전용이며 저장 사본을 새로 만들지 않는다.
- 운영 배포·Git 발행은 별도 승인이 필요하다.

## 운영 데이터 예

`members`의 이름 컬럼은 `english_name`, `korean_name`, `short_korean_name`, `japanese_name`이다. 같은 멤버가 "Tokino Sora", "토키노 소라", "소라", "ときのそら"로 저장된다. 현역 123명 중 `short_korean_name`이 빈 행은 2개, `korean_name`이 빈 행은 1개다.

코드의 `domain.Member.Name`은 `english_name`이다(`hololive-shared/pkg/service/member/repository_scan.go:378`). `NameKo`는 `korean_name`, `NameJa`는 `japanese_name`이다.

## 공통 원인

명령의 멤버 검색 matcher가 돌려주는 `domain.Channel.Name`이 `english_name`이다. 해당 위치는 `matcher_search.go`의 `memberToChannel`과 `matcher_candidate.go`의 `candidateFromMember`이다. 여러 handler가 이 값을 표시명으로 그대로 쓴다. 또 `domain.Channel.GetDisplayName()`은 Holodex `EnglishName`을 우선한다.

## A. 사용자 표시가 정본과 다른 경로

경로는 `hololive-api/internal/planes/bot/internal/` 기준이다(1번의 shared 경로 제외).

| # | 경로 | 현재 표시 | 위치 |
| --- | --- | --- | --- |
| 1 | `!예정` 목록(전체·멤버) | Holodex 채널 제목(예: "Sora Ch. ときのそら"). 공식 일정 fallback은 탤런트 일본어 이름 | `hololive-shared/internal/service/holodex/provider/streammapping/mapper.go:57`, `htmlscraper/stream_mapper.go:135`, `command/handlers/handler_upcoming.go` |
| 2 | `!라이브` 전체 | `korean_name` → `english_name` → `channel_id`. `short_korean_name`을 쓰지 않음 | `service/livequery/queries/snapshot.sql:6` |
| 3 | `!라이브 <멤버>` 목록과 "방송 없음" 문구 | `english_name`(matcher 결과로 덮어씀) | `service/livequery/repository.go:140`, `command/handlers/handler_live.go:95` |
| 4 | `!예정 <멤버>`의 "예정 없음" 문구 | `english_name` | `command/handlers/handler_upcoming.go:169` |
| 5 | `!일정` 제목 | `english_name`(`Channel.GetDisplayName()`) | `adapter/messaging/formatter/formatter_streams.go:171` |
| 6 | 알람 추가·해제 응답 | `english_name` | `command/handlers/alarm/alarm.go:259`, `:399` |
| 7 | 방송기록 필터 표시("멤버: …") | `english_name`. 같은 메시지의 항목은 정본이라 섞임 | `command/handlers/handler_broadcast_history.go:191` |
| 8 | 동명이인 후보 목록 | "English (Org)"(`Member.GetDisplayName()`) | `adapter/messaging/formatter/formatter_alarm_milestone.go:63` |

## B. 정본 사본·대체 경로

평소에는 가려지고 특정 상황에서만 드러난다.

| # | 내용 | 위치 |
| --- | --- | --- |
| 9 | 알람 추가가 `alarms.member_name`에 이름 사본을 저장한다(현재 코드는 matcher의 `english_name`). 운영 25행 중 21행은 Holodex 채널 제목(예: "Miko Ch. さくらみこ"), 4행은 일본어 이름이다. 알람 목록은 정본 캐시 이름을 쓰지만 캐시에 없으면 이 사본을 보여 준다. | api `command/handlers/alarm/alarm.go:242`, worker `internal/service/alarm/subscriptions/alarm_cache_view.go:51-53` |
| 10 | live·upcoming 알림 checker는 이름 캐시(Valkey `alarm:member_names`)에 값이 없으면 이름 적용을 건너뛰어 Holodex 채널 제목으로 발송한다. 계약의 종단 문구와 다르다. | worker `internal/service/alarm/checker/checking/common.go:151-157`, `internal/egress/alarmdispatch/alarm_dispatch_render.go:271-274` |
| 11 | X 스페이스 대상 이름을 `members`가 아닌 `X_SPACES_CONFIG_FILE`의 `member_name`에서 가져온다. | worker `internal/service/xspaces/config.go:21` |
| 12 | mekPark 호스트는 코드 고정 표(`mekparkhost.SubscriptionMember`)를 이름 출처로 쓴다. `members` 밖 대상이라 의도된 별도 출처로 보이며 변경 대상이 아니다. | shared `mekparkhost` |

## C. 정리 대상

- 13: 봇 formatter의 `AlarmNotification`·`AlarmNotificationGroup`은 운영 호출자가 없다. 비정본 이름 로직(`alarmBaseChannelName`이 `Channel.GetDisplayName()` 우선)을 담고 남아 있다. 위치는 `formatter_alarm.go`, `formatter_alarm_notification.go`이다. 사용처가 없음을 확인한 뒤 삭제한다.

## 정본을 지키는 경로

YouTube 알림 발송(`GetMemberName`), 생일·기념일 알림(`celebration_runner.go:421-426`), 캘린더, 방송기록 항목, 알람 목록(캐시 적중 시), 관리 목록이다. 멤버 목록과 프로필은 여러 이름을 함께 보여 주는 화면이라 대상이 아니다.

## 다른 세션의 API 작업(완료, 미커밋)

2026-10-05 13:25 UTC의 projection 테이블 VACUUM FULL 중 API가 한 번 재시작했다. 관측 consume과 그 Retry가 `acquire DB slot: context deadline exceeded`로 실패했고, worker가 이를 프로세스 종료로 키웠다. 근거는 [DB hot path 계획](2026-10-05-db-hotpath-optimization.md)에 있다. 다른 세션이 이 수정을 마쳤다.

- 위치: `/home/kapu/work/iris-stack/hololive-bot` main checkout의 미커밋 변경. 수정은 `internal/planes/youtube/runtime/supervisor.go`이고, 테스트는 신규 `supervisor_failure_test.go`, `supervisor_failure_integration_test.go`이다.
- 내용: `retryAndForget`·`deadLetterAndForget`에서 Retry·DeadLetter 기록이 `retryableObservationError`로 분류되는 일시 오류로 실패하면 ERROR 로그와 기존 `retry_error`·`dead_letter_error` 지표만 남기고 worker를 계속 돌린다. 행은 `PROCESSING`으로 남고 lease 만료 뒤 claim이 회수한다. 일시 오류가 아닌 실패는 계속 프로세스를 종료한다. 새 재시도 경로는 없다.
- 테스트: `TestFailureRecordingControlsWorkerExit`, `TestWorkerContinuesAfterRetryDBSlotTimeout`, `TestFailureRecordingTimeoutRecoversExpiredLease`(실제 DB lease 만료 회수).
- 남은 일: 리뷰, `-race` PostgreSQL 테스트와 lint, 커밋·PR, 중앙 `hololive-api` 배포(승인 필요). runbook의 `hololive-api runtime error` 항목에 "일시 오류로 실패한 기록은 종료하지 않고 lease 회수에 맡긴다"는 문장을 함께 갱신한다.
- 이 인계 문서는 origin/main 기준 별도 worktree에서 작성했다. main checkout의 미커밋 변경은 건드리지 않았다.

## 권장 순서

1. 명령 응답 표시명 함수 하나를 정한다(`short_korean_name` → `korean_name` → `english_name`). 위치 후보는 `domain.Member` 메서드 또는 member 서비스다. [alarm 계약](../contracts/alarm.md)이나 메시지 문서에 명령 응답 규칙을 적는다.
2. matcher의 `memberToChannel`·`candidateFromMember`가 이 함수로 `Channel.Name`을 채우게 한다. 3~7번이 함께 해소된다. 검색 일치 판단에 쓰는 이름(정규화 키)은 바꾸지 않는다.
3. `!라이브` SQL(2번)을 같은 순서로 바꾼다.
4. `!예정`(1번)은 표시 직전에 `channel_id`로 members 표시명을 결합한다. members에 없는 채널만 지금처럼 응답 이름을 쓴다. 기존 동작 유지이며 새 fallback이 아니다. Holodex 캐시 데이터는 바꾸지 않는다.
5. 8번 후보 목록과 13번 죽은 코드를 정리한다.
6. alarm-worker의 9~11번은 별도 작업으로 다룬다. 9번은 `alarms.member_name`을 표시에 쓰지 않도록 하고 쓰기도 멈출지 결정한다(컬럼 삭제는 migration). 10번은 캐시 미스에서 계약의 종단 문구나 DB 조회로 맞출지 결정한다. 11번은 설정 이름 대신 `channel_id`로 members를 조회할지 결정한다.

## 검증 기준

- 같은 멤버에 대해 `!라이브`(전체·멤버), `!예정`(전체·멤버·없음 문구), `!일정`, 알람 추가·해제·목록, 방송기록이 같은 이름을 내는 회귀 테스트.
- `short_korean_name`이 빈 멤버와 members에 없는 채널의 표시 경로 테스트.
- 변경 모듈의 `-race` 테스트, golangci-lint, staticcheck, NilAway, `./build-all.sh --build-only --no-bump`.

## 필요한 승인

- 위 API 변경과 다른 세션 작업의 커밋·PR·중앙 API 배포
- 9번에서 `alarms.member_name` 컬럼을 지운다면 migration 적용

## 진행 기록(2026-10-05, `fix/member-display-name-ssot-20261005`)

권장 순서 1~6과 사용자 추가 요청(쓰지 않는 표시 자산 정리)을 구현했다. 6은 아래처럼 결정했다.

- 1~5(명령 응답): `domain.Member.DisplayName`(`short_korean_name`→`korean_name`→`english_name`)을 정본으로 두고 [alarm 계약](../contracts/alarm.md)에 "명령 응답 멤버 표시명"을 적었다. matcher는 검색 키와 표시명을 분리해 3~7을 해소한다. `!라이브` SQL은 `short_korean_name`을 먼저 쓰고, `!예정`은 표시 직전 `channel_id`로 members 표시명을 결합한다. 동명이인 후보는 정본 표시명으로 보이고 복사 예시는 검색 키(`QualifiedName`)를 유지한다. 13번 죽은 formatter와 `Channel.GetDisplayName`을 지웠다.
- 9: `alarms.member_name`을 읽고 쓰지 않는다. 알람 목록은 이름 캐시 미스를 members로 채우고, 멤버 뉴스도 members만 읽는다. 컬럼은 2단계 migration으로 지운다.
- 10: 방송 알림 이름을 alarm dispatch 렌더 때 members로 정한다(계약 순서와 종단 문구, `hololive_alarm_dispatch_member_name_missing_total`). checker의 Valkey 이름 덮어쓰기를 지웠다.
- 11: X 스페이스 설정·payload에서 이름을 지우고 렌더 때 members로 정한다. 운영 설정의 `member_name`은 같은 배포에서 지운다(알 수 없는 필드는 시작 오류).
- 정리: 렌더 경로가 없던 `CMD_ALARM_NOTIFICATION`·`CMD_ALARM_LIVE_STARTED`·`CMD_ALARM_NOTIFICATION_GROUP`을 268 migration과 코드에서 지웠다. 읽지 않게 된 `misc/alarm_unknown_member`는 코드에서 지우고 DB 행은 2단계에서 지운다(구 버전 worker가 기동 때 필수로 검사하므로 롤백 지점을 지키기 위함).

배포 순서:

1. 1단계: 위 코드와 268을 배포한다. 268은 구 버전이 읽지 않는 템플릿 행만 지우므로 롤백해도 안전하다.
2. 2단계: 1단계 확인 뒤 `alarms.member_name` 컬럼과 `misc/alarm_unknown_member` 행을 지우는 migration을 배포한다. 이후 롤백 지점은 1단계 이미지다.

1단계 검증(마지막 코드 수정 뒤): `bash scripts/ci/local-ci.sh` 통과(vet, staticcheck, golangci-lint, NilAway, Go test, `-race`; integration tag 테스트는 기본값대로 생략). 운영 DB 읽기 전용 점검에서 X 스페이스 대상 5개와 알람 구독 채널 21개 모두 한국어 표시명이 있어 종단 문구로 바뀌는 채널은 없다.
