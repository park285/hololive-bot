# SQL 최적화와 호환 경로 fadeout

## 목표와 범위

hololive-bot의 SQL·로직 개선 작업에서 2026-10-12에 확정한 범위를 실행 순서와 조건으로 정리한다. 계획 뒤의 진행·게시·배포 기록은 아래 각 절에 적는다.

- **SQL 최적화:** A1(send unit 조인), A2(alarm dispatch 보존 삭제, A3 포함), B1(코드가 쓰지 않는 인덱스), B2(`youtube_live_sessions` HOT 차단).
- **호환 경로 fadeout:** 전환용 alias·re-export, 호환 shim·fallback, 연결되지 않은 구형 구현, 퇴역 가드, 퇴역 연대기 주석과 문서. 운영 데이터 조건이 걸린 경로는 조건을 확인한 뒤 제거한다.
- **범위 밖(참고):** B3(queue 보존 인덱스)은 B1 migration과 묶을지만 정한다. collector lease 후보 탐색은 [DB 리뷰 후속](2026-10-08-db-review-followup.md)대로 다음 collector 릴리스에서 다룬다. 번호 이름 SQL의 일괄 개명과 `OriginLegacyUnknown` 세션 9건(계약 개정 필요)도 제외한다.

기준과 경계는 다음과 같다.

- 기준은 `main` `0b5955bc1`(#607)이고, 작업 worktree는 `/home/kapu/work/iris-stack-sql-refactor-20261011/hololive-bot`(브랜치 `refactor/sql-logic-20261011`)다. 조사는 `7e8d8dca0`(#605와 같은 트리)에서 했으며, 그 뒤 달라진 점은 아래 "준비 점검"에 있다. 원 checkout의 미커밋 변경은 건드리지 않는다.
- 이전 계획([DB hot path 최적화](2026-10-05-db-hotpath-optimization.md), [DB·런타임 실측 기반 최적화](2026-10-06-db-runtime-optimization.md), [DB 리뷰 후속](2026-10-08-db-review-followup.md))이 반영한 항목은 다시 다루지 않는다.
- 새 재시도·대체 경로를 만들지 않는다. 제거한 경로에 재등장 방지 grep 가드나 이름 검사 테스트를 추가하지 않는다(stack `AGENTS.md`).
- 계획 시험은 2026-10-05 A2와 같은 실행 계획 시험이며 SQL 문자열 검사가 아니다.

## 승인이 필요한 단계

| 단계 | 대상 | 이유 |
| --- | --- | --- |
| 운영 읽기 전용 조회 | 중앙 `holo-postgres` | stack-platform-ops 절차. 2026-10-11 완료. F17은 기다리지 않고 migration 301로 종결("F17 종결" 절) |
| 공개 계약 변경 | 관리 API `/password/reset-request`, `/password/reset` 삭제(F15) | 503 stub이지만 route가 사라진다 |
| 데이터 삭제 | `auth_password_reset_tokens` DROP(F15) | 복구 불가. 사용하지 않는 테이블이어도 데이터가 사라진다 |
| migration 적용·배포 | B1, B2, F15 migration과 api·alarm-worker·collector 배포 | hololive-bot-ops 절차 |
| Git 게시 | 커밋·push·PR | 요청 시에만. `scripts/ci/pre-push-gate.sh` 필수 |

운영 읽기 전용 조회는 2026-10-12 사용자 요청으로 실행했다(아래 "운영 측정 결과").

## 운영 측정 결과 (2026-10-11 16:50~17:00 UTC)

stack-platform-ops 절차로 `hololive-seoul`(`100.100.1.8`)의 `holo-postgres`(PostgreSQL 18.6, DB `hololive`)를 `docker exec`와 `PGOPTIONS='-c default_transaction_read_only=on'`으로 조회했다. 모든 세션에서 `transaction_read_only=on`을 먼저 확인했고, 쓰기·DDL·통계 리셋·EXPLAIN ANALYZE는 하지 않았다. 집계와 카탈로그만 읽었고 행 내용은 읽지 않았다.

- 통계 구간: `pg_stat_database.stats_reset`은 NULL, `pg_stat_statements` 리셋은 2026-08-23 13:24 UTC, postmaster 시작은 2026-10-08 10:47 UTC다. 누적값은 약 49일 구간으로 본다.
- 결론: 앱 테이블이 모두 작아서(대상 테이블 수백~1만 행) A1·A2·B1의 운영 성능 효과는 측정 오차 수준이다. 실제 비용이 확인된 것은 B2다. F18·F19·F20은 제거 조건을 이미 충족했다.

| 항목 | 측정 | 판단 |
| --- | --- | --- |
| A1 | `alarm_dispatch_send_units` 766행(568 kB), 최근 7일 139행 증가. 대상 문장 825회, 평균 4.1~9.6 ms(최대 52 ms). 테이블 seq scan 3,446회 | send unit은 delivery 보존(약 90일)이 지나면 지워지므로 정상 상태에서도 수천 행이다. 성능 효과는 작고, 계획 시험으로 형식 불일치를 고정하는 의미로 진행한다 |
| A2 | `alarm_dispatch_deliveries` 1,274행(`sent` 1,216, `cancelled` 58). 상태별 보존 삭제 문장 각 881·340회, 평균 0.01~2.4 ms, 최대 85 ms | generic plan 전환 징후 없음. 견고성 목적으로 진행한다 |
| B1 | 아래 B1 표 | 7개 모두 런타임 미사용 확인. 합계 약 770 kB이고, 대상 테이블의 HOT 비율은 이미 99.8% 이상이라 효과는 쓰기 증폭 소폭 감소뿐이다 |
| B2 | `youtube_live_sessions` 9,837행(6.8 MB), 누적 UPDATE 475만 건 중 HOT 6.1%, autovacuum 2,689회. 아래 B2 측정 | 진행 |
| F15 | `auth_password_reset_tokens` 0행(24 kB) | DROP으로 잃는 데이터 없음 |
| F17 | `send_unit_id IS NULL` delivery 308건, 모두 종료(`sent` 294, `cancelled` 14), 최신 2026-08-10 | 조건 미충족. `cancelled` 14행의 `cancelled_at`이 2026-09-21이라 보존 기간(90일)이 지난 2026-12-20 이후 다시 확인(2026-10-12 정정). 같은 날 migration 301로 지우고 종결 |
| F18 | 본문 고정 없이 발송을 시도한 send unit 564개, 진행 중 delivery 0건. 마지막 비고정 시도 2026-09-30 12:25, 고정 시도는 2026-09-30 12:54부터 202개 | 충족. 새 행은 생기지 않고, 남은 delivery는 `sent`·`cancelled`라 관리 재처리(`dlq`·`quarantined`만 대상)로 되살아나지 않는다 |
| F19 | `youtube_notification_delivery` 64행, `send_request_id IS NULL` 0행 | 충족 |
| F20 | `notification_delivery_outbox` 0행 | 충족 |

## 준비 점검 (2026-10-12)

코드 단계 착수 전에 작업 환경과 계획 참조를 현재 `main`에 맞춰 확인했다. 코드·migration은 아직 바꾸지 않았다.

- **기준 갱신:** 브랜치에 고유 커밋이 없었고 `7e8d8dca0`은 `main`의 #605(`67328c7c5`)와 트리가 같아, 브랜치를 `0b5955bc1`로 옮겼다(미게시, upstream 없음). 저장소가 추적하는 `go.work`가 `../iris-client-go`, `../shared-go`를 쓰므로 sibling worktree를 stack 포인터와 같은 `iris-client-go` `99265df`(v3.1.0), `shared-go` `e301144`(v2.11.0)로 맞췄다. 운영 빌드(`GOWORK=off`)는 `go.mod`의 `shared-go v2.10.0`을 쓴다.
- **#606·#607 영향:** 계획이 참조하는 파일·SQL·심볼은 모두 남아 있고 최신 migration은 280이라 새 번호는 281부터다. #607은 F20 대상만 바꿨다. legacy 격리 분기는 `notificationdelivery/request.go:43-50`으로 옮겨졌고, payload 형식은 `hololive-shared/pkg/contracts/notificationdelivery`의 `Payload`·`Request`가 정본이 됐다. `known_unsent`는 생산자 rearm(`outbox_enqueue_batch_upsert.sql`)과 준비 실패 표시가 계속 쓰므로 계약은 유지하고, F20은 이 분기와 `outbox_save_request.sql`의 `attempt_count = 0 OR known_unsent` 조건이 같은 legacy 계약인지 확인해 함께 정리한다. F3·F4·F17의 줄 참조는 그대로다.
- **F13과 원 checkout 미커밋 변경의 겹침:** 원 checkout(`/home/kapu/work/iris-stack/hololive-bot`)에 90개 파일의 미커밋 변경(마지막 수정 2026-10-11 19:41 KST)이 있고, 대부분 DEC·stack-audit 이력 주석 축약과 가드 시험·검사 스크립트 삭제다. `DEC-20|PLN-20|stack-audit` 인용은 `main`의 Go 파일 116개 131줄 중 원 checkout이 72개 77줄로 줄였고, 원 checkout이 수정한 Go 파일 47개가 인용 파일이다. F13은 이 변경의 처리 방향이 정해질 때까지 미루고, 나머지 코드 단계 항목부터 진행한다. 사용자가 원 checkout 변경을 합류하기로 해 F13도 진행했다(진행 기록).
- **작업 환경:** 다섯 모듈(`hololive-shared`, `hololive-dbtest`, `hololive-alarm-worker`, `hololive-api`, `hololive-youtube-collector`)의 workspace `go build ./...`와 `go vet ./...`가 통과했다. 계획 시험 선례인 `TestClaimSQLReadsCandidatesWithoutScanningTerminalHistory`가 `-race -tags=integration`으로 통과해 testcontainers PostgreSQL 시험이 동작함을 확인했다. `golangci-lint`는 2.13.1이다. staticcheck·NilAway는 `scripts/ci/local-ci.sh`가 고정 바이너리를 빌드해 쓴다. `GOPATH/bin`의 `deadcode`는 v0.46.0이므로, 조사와 같은 결과를 얻으려면 `go run golang.org/x/tools/cmd/deadcode@v0.51.0`으로 실행한다(실행 확인).

## 진행 기록 (코드 단계, 2026-10-12)

코드 단계의 기록이다. 검증 결과는 각 단계 직후 값이며, 게시와 배포는 아래 "게시와 배포"에 적었다.

- **원 checkout 변경 합류:** 사용자 결정으로 원 checkout의 미커밋 변경 90개 파일을 그대로 가져왔다(원 checkout은 그대로 둠). 주석 축약 외에 게이트 정리(`check-migration-manifest.sh` Python 재작성, `check-shared-go-packages.sh`·`check-pgo-default.sh`·`normalize-epoch2-baseline.py`·`pg-hotpath-explain-snapshot_test.sh` 삭제, 중첩 배포 시험 호출을 `pre-push-gate.sh`의 `run_if_changed`로 이동)와 퇴역 가드 시험 삭제가 들어 있다. 영향 패키지 31개 `-race -tags=integration`, `hololive-dbtest`, migration manifest·경계 게이트·SQL ownership, 배포 스크립트 시험 10개가 통과했다.
- **A1:** recordset의 `send_unit_key`를 `CHARACTER(64)`로 선언했다. `TestInsertDeliveryBatchLooksUpSendUnitsByUnitKey`(send unit 2만 행)는 수정 전 두 모드에서 2만 행 seq scan으로 실패했고 수정 뒤 통과한다. 64자를 넘는 키는 잘리지 않고 `value too long` 오류가 된다. 수천 행에서는 형식이 맞아도 planner가 seq scan을 고를 수 있어 fixture를 2만 행으로 늘렸다.
- **A2:** `0348_04` `fmt` 템플릿을 상태별 SQL 4개(`alarm_dispatch_retention_delete_{sent,dlq,quarantined,cancelled}.sql`)로 나눴다. 상수 상태만으로는 generic plan이 바깥 `DELETE`를 hash join으로 풀어 만료 행 전체(5,100행)를 읽어서, 바깥 `DELETE`를 `d.id = ANY(ARRAY(SELECT id FROM picked))`로 바꿨다. 상태·시각 재검사는 유지했다. 수정 전 SQL은 generic plan에서 상태마다 2만 행, 수정 뒤는 200행 이하다(`TestAlarmDispatchRetentionDeleteReadsOnlyExpiredRowsPerStatus`). requeue 대기 행을 건너뛰는 기존 시험도 통과한다.
- **A3:** `0279_09`, `0301_10`, `0330_11`에 상태 상수를 썼다. `CountByStatus`는 운영에서 FAILED만 세므로 `CountFailed`로 좁히고, 시험의 상태별 집계는 시험 도우미가 직접 센다. 운영 코드에서 쓰이지 않게 된 `deliveryStatusQuarantined`는 시험 파일로 옮기고 QUARANTINED 격리 이유는 `0301_10.sql` 주석으로 옮겼다.
- **F18:** `ErrLegacySendRequest`, `fenceSendRequest`의 attempted 반환, `send_request_members.sql`의 `sending_started_at` 열, runner 분기와 분기 전용 시험 2개를 지웠다. alarm delivery의 `sending_started_at`은 요청 고정 뒤 sending 전이에서만 설정되고 `send_unit_id`·요청 열은 다시 바뀌지 않아, 미고정·시도 조합은 고정 도입 전 행에서만 나온다.
- **F19:** D3 검사는 #559에서, `request_snapshot_allowed` 열은 migration 256에서 이미 지워졌다. 남은 `FreezeRequest` 주석만 현재 계약으로 고쳤다. `send_request_fallback*`은 grouped 확정 미전달 뒤의 현행 경로라 대상이 아니다.
- **F20:** `request.go`의 legacy 격리 분기, `outbox_save_request.sql`의 `attempt_count = 0 OR known_unsent` 조건, 분기 전용 시험을 지웠다. rearm 조건의 같은 legacy 보호는 아래 레거시 정리에서 함께 지웠다.
- **F2:** `alarmdispatch.Sender` alias를 지우고 필드·인자와 `workerapp/build_egress.go`가 `delivery.PreparedMessageSender`를 직접 쓴다.
- **F3:** `validateLiveDispatch`와 `buildLedgerRows`의 빈 `alarm_type` → Live 기본값을 지웠다. 소유자는 `ValidateCanonicalDispatch`이고, 운영 생산자(방송·축하·X Spaces·publisher)는 모두 종류를 채운다. 빈 값은 이제 숨은 기본값이 아니라 오류다(`TestBuildLedgerRows_RejectsEmptyAlarmType`).
- **F4:** readiness 공개 응답의 `egress_flags` 단정을 지우고 시험 이름을 `TestPublicResponseOmitsDependencyDetails`로 바꿨다.
- **F5:** 운영 경로가 쓰지 않는 `youtubedispatch/preparation`의 operation·tracking 파일과 시험, `lifecycle/types.go`의 미사용 타입(NewReason·SendFence·AlarmClaimToken·tracking 요구 타입·ProviderOutcome)과 그 시험을 지웠다.
- **F6:** admin member index 캐시(`stream_handler_member_index.go`, `StreamState`, `getActiveMemberIndex`, 무효화 호출 6곳, 관련 시험)를 지웠다. `memberIndexLoader`는 `api_youtube_ops.go`가 계속 쓴다.
- **F7:** `TemplateRepository`의 `Upsert`·`UpsertWithPreviousBody`·`CreateRevision`·`PruneOldRevisions`와 `revision_insert.sql`을 지웠다. 시험은 `UpsertWithRevision`과 시험용 revision 삽입 도우미로 옮겼고, `UpsertWithRevision` 시험과 같은 단정인 `template_repository_pg18_test.go`는 지웠다.
- **F8:** membernews `FilterCandidates`·`buildFilteredCandidate`·`matchMembers`를 지우고 시험을 `PrepareCandidates(...).Filter`로 옮겼다. 비교 기준인 시험 쪽 `eagerFilterCandidates`는 남겼다.
- **F9:** `buildLLMSchedulerHTTPServer`(단수)와 `buildHealthOnlyRouter`를 지웠다. 복수형 builder의 라우터 구성을 `buildLLMSchedulerRouter`로 분리해 운영 builder와 시험이 같은 경로(API key fail-closed 포함)를 쓴다.
- **F10:** 이름을 든 예와 완전 미사용 함수를 처리했다. `NewYouTubeChecker`, `alarm.NewClient`·`NewClientWithAPIKey`(패키지 안 시험은 `newTestClient`, 바깥 시험은 `NewClientWithAPIKeyStrict`), `NewReplyOutboxRepository`(시험 도우미로 이동), `sourceobservation.NewConsumer`·`NewConsumerWithAbsenceGrace`(시험은 `NewConsumerWithGraces`), `apperrors` 생성자 4개와 미사용 `CircuitOpenError`, `releaseDeliveryClaims`(인터페이스·spy 포함), `DrawCenteredText`, `RetryPolicy.MaxRetries/RetryBackoff`, `StrictEnv.Int64`, `WorkerDuration`, `WriteIrisBaseURLFile`, `AllFailureClasses`, `DecodeEnvelopeStrict`, `AllKeys`, `ScanDeliveryRow`, collector testkit `NewPublishedObservation`을 지웠다.
  - 나머지 정리는 아래 레거시 정리의 F10 잔여 항목에 적었다.
- **F11 (진행하지 않음):** 표의 "runbook 참조 없음" 전제가 틀렸다. `docs/current/runbooks/hololive-api.md`의 manual_review discard 절차가 `MIGRATIONS_DIR=.../durability/queries`와 `-f /migrations/reply_outbox_discard_manual_review.sql`로 이 파일을 직접 실행하므로 현행 운영 artifact다.
- **F12:** `trimLegacyLeading`을 `trimLeadingSpaceAndZeroWidth`로, live snapshot 오류 문구를 `metadata-generation live snapshot cannot carry query proof`로 바꿨다. `LogAndWrapError` 11곳은 이미 helper 결과를 그대로 반환하고 `log and wrap error:` 접두어도 없어 바꿀 것이 없었다.
- **F13:** Go 소스 주석의 `DEC-`·`PLN-`·`stack-audit`·`stack audit`·감사 작업 번호(T11/T17/T18/T19)와 감사 항목 ID 인용을 모두 지웠다. 삭제 연대기만 적은 문장은 현재 계약 문장으로 고쳤고, `dispatchops/model.go`의 제거 조건과 재검토 기한은 날짜 근거와 함께 남겼다.
- **F14:** v3 전환 전 webhook inbox 드레인 절을 `docs/history/runbooks/IRIS_CLIENT_GO_V3_WEBHOOK_INBOX_DRAIN_20260928.md`로 옮기고 history 색인에 올렸다. #542가 들어간 v6.0.0 이후 이미지만 rollback 대상이라 현행 runbook에서는 지웠다.
- **F1:** collector `collecterr`의 type alias 2개·상수 alias 22개를 지우고, 타입 정보로 alias 사용처만 찾아 `contract.*`로 바꿨다(76개 파일). `Error` 타입과 분류 함수는 그대로다.
- **리뷰 후속:** 전체 diff를 다시 읽고 다음을 고쳤다.
  - `pre-push-gate.sh`의 `ap-deploy-cutover-failure_test.sh` 입력이 `ap-deploy.sh`가 source하는 `scripts/deploy/lib/*.sh`를 빠뜨려, lib만 바뀌면 시험을 건너뛰었다. 입력을 `scripts/deploy/lib` 디렉터리로 넓혔다.
  - F13에서 문장 중간을 지워 짧게 끊긴 주석 5곳의 줄을 다시 맞췄다.
  - 원 checkout 변경이 지운 `TestSettingsService_ReadsFileWithRetiredScraperProxyKey`가 덮던 계약("저장 파일의 알 수 없는 필드는 무시하고 Update 때 쓰지 않는다", `settings/service.go` 주석)을 퇴역 키 이름 없이 `TestSettingsService_IgnoresUnknownStoredFieldsAndDropsThemOnUpdate`로 다시 덮었다.
  - 나머지 확인 결과는 다음과 같다. 아키텍처 게이트 재작성은 기존 검사와 동등했다. 상태 상수 SQL은 부분 인덱스 조건과 일치했다. collector 변경은 alias 치환·주석·`NewPublishedObservation` 삭제뿐이었다. 원 checkout 변경이 지운 시험은 퇴역 키·경로 재등장 가드이거나 검사기 자체 시험이었다.
- **레거시 정리 (2026-10-12 추가 요청):**
  - **rearm legacy 보호와 `known_unsent`:** `outbox_enqueue_batch_upsert.sql`의 `request IS NOT NULL OR attempt_count = 0 OR known_unsent` 조건을 지웠다.
    - 현재 코드에서 요청 없이 시도 횟수가 남는 FAILED 행은 payload decode 실패뿐이다. 요청을 저장하지 못하면 발송하지 않으므로, 이 행은 발송된 적이 없어 다시 적재해도 된다.
    - 읽는 곳이 없어진 `known_unsent`도 함께 지웠다. 대상은 `Payload.KnownUnsent`, `outbox_mark_preparation_unsent.sql`, `markPreparationUnsent`다. 준비 실패는 일반 실패 전이로 재시도한다.
    - 생산자 payload 바이트는 바뀌지 않는다(`omitzero`라 false일 때 원래 빠졌다).
    - legacy 보호만 시험하던 `TestGenericLegacyFailedRearmCannotDiscardPriorAttemptEvidence`는 지웠다. `TestEnqueue_FailedRetry`는 시도 3회 행으로 바꿔, 보호 조건이 있으면 실패하는 경우를 덮게 했다.
    - 배포 순서와 무관하게 중복 발송 위험은 없다. 다만 worker만 먼저 바뀐 동안 준비 실패로 FAILED가 된 행은 옛 api가 다시 적재하지 않으므로, api(생산자)도 같은 배포에서 바꾼다.
  - **apperrors:** 운영 코드가 만들지 않는 `APIError`·`KeyRotationError`·`CacheError`·`ValidationError`와 `getErrorMessage`의 네 분기를 지웠다.
    - Holodex 오류는 내부 `apiclient` 타입이라 이 분기에 닿은 적이 없어 응답 문구는 바뀌지 않는다.
    - 문구 key `external_api_call_failed`·`cache_connection_failed`는 Go 상수와 기동 검증 목록에서 뺐다.
    - 시드 행은 이전 이미지로 rollback해도 기동 검증이 통과하도록 다음 배포까지 둔다(269와 같은 2단계). 그 사이 `messages_seed_parity_test.go`의 `seedErrorKeysPendingDeletion`이 두 key를 삭제 대기로 둔다.
    - 이 hololive-api가 운영에 배포된 뒤 두 행을 지우는 migration(새 번호 281 이후)을 추가하면서 목록을 지운다.
  - **F10 잔여:** deadcode 운영 도달 불가 97개를 43개로 줄였다.
    - **함수와 그 자체 시험을 함께 지움:** `util.Unique`·`FormatKoreanNumber`·`FirstNonEmptyString`, `database.IsNoRows`, `middleware.LogDebugf`, `acl.ParseACLMode`, `cleanupctx.Wait`, targetpolicy 미사용 변형 5개, `domain.NewMajorEvent`·`NewMajorNews`·`NewEventRoomSubscription`·`ParseResults` 조회 3개, `CaptionBoldFaceSized`, `HasCommunityShortsOpsRepository`, `OpenAIClient.recordUsage`(운영은 `costTrackerUsageReporter`로 기록), `groupOutboxItems`와 `outboxItemGroup`, `groupAlarmDispatchEnvelopesByKey`, `LiveCoverageCovers*`, alarm key `ChannelContentAlarmTarget*`와 정의 검증, `getRegistryKey`·`channelSubscribersKey`, alarm API `RegisterRoutes`·`Health`·`Ready`(운영은 `RegisterInternalRoutes`), `httpserver.RegisterHealthRoutes`.
    - **운영 변형을 직접 부르게 하고 시험용 wrapper를 지움:** `buildAlarmDispatchGroupView`, `buildDeliveryAuditLogAttrs`, `targetMinutesSnapshot`, `validateMemberLookupDependencies`, `nextDurableIdleDelay`, `replyOutboxRetryAfter`, `replyOutboxSettlementStatus`, `forceOpenedAtForTest`, `allowH3Dial`, `getDomainContext`, `recentlyDispatchedOrNotifiedLiveStreamIDs`. 마지막 두 시험은 운영 경로(`buildMemberFilterSection`, `recentLiveDispatchEvidence`)를 확인하도록 고쳤다.
    - **같은 패키지 시험만 쓰는 보조 함수는 `_test.go`로 옮김:** `isReplyStatusFailed`, `pendingCount`, `concurrentIndexNames`, `deliveryFailureReasonIsPermanent`, `ClassifyBroadcast`.
    - **아무도 import하지 않음:** `hololive-api/internal/service/settings/mocks`.
    - **남은 함수:** 이 단계 뒤 43개였고, 아래 F15에서 비밀번호 재설정 9개를 지워 34개다. 계약 열거 함수 8개, 여러 시험 패키지가 fixture로 쓰는 생성자·도우미(`NewPgxRepositoryFromPool`, `WithWorkerID`, `NewHolodexService`, `observation.NewRepository`, `NewTriggerRuntimeRouter`, `RegisterInternalRoutesWithoutAuth`, `FindMemberOrError` 등), 운영 file override 검증의 엄격 변형 `validateRuntimeIrisBaseURL`이다.
    - 시험 포함 미사용은 인터페이스를 만족시키는 시험 mock 메서드 2개뿐이다.
  - **기타 표식 점검:** 운영 Go·SQL·스크립트의 `legacy`·`구형`·`호환`·`compat`을 다시 훑었다.
    - 남은 것은 모두 현행 계약이거나 거절 경로 설명이다. `legacy_unreviewed` metric label·`legacy_unknown` lifecycle 어휘, migration ledger 잔재 거부, cliproxy 호환 옵션, 구형 저장 형식 거절, `export-full-bundle.sh`가 지금 만드는 manifest를 검증하는 `verify-full-bundle.sh`의 legacy 분기가 여기에 해당한다.
- **구현 리뷰 (2026-10-12, F15·F16 뒤):** 바뀐 운영 경로를 다시 읽고 다음을 고쳤다.
  - F3의 숨은 기본값이 `dedupe_key.go`의 event key 계산에도 남아 있었다(빈 `alarm_type` → `live`). 모든 호출자가 `ValidateCanonicalDispatch`를 먼저 거쳐 닿지 않는 분기라 지웠다. 검증된 입력의 key는 바뀌지 않는다.
  - F16 배포 조건을 운영자가 확인할 수 있는 조회로 구체화했다(아래 F16).
  - 문제없음으로 확인한 것: 준비 실패 경로는 `MarkFailed`의 worker·시도 횟수 fence로 이전 SQL의 lock 조건과 같다. 템플릿·시드·엄격 decode·형제 저장소에 F16 키 참조가 없다. 281은 276과 같은 잠금·실패 형태다.
- **F15·F16 (2026-10-12 사용자 승인 "f16까지 종결"):** 코드 삭제와 migration 파일 작성까지 했다. migration 적용과 배포는 하지 않았다.
  - **F15:** 재설정 route 2개와 핸들러, `service_password_reset.go`(함수 8개), `isPasswordResetRequestRateLimited`, SQL 5개, `ResetTokenTTL`·`PasswordResetRequestRateLimitPerMinute` 설정, `passwordResetTokenModel`, 재설정 상수를 지웠다.
    - 두 경로는 이제 NoRoute 응답(API key 없음 401, 틀림 403, 맞음 404)을 받는다.
    - `281_drop_auth_password_reset_tokens.sql`이 276 선례대로 테이블을 지운다. 행이 있으면 지우지 않고 실패하고, CASCADE를 쓰지 않으며, 재적용은 아무것도 바꾸지 않는다(`TestAuthPasswordResetDropMigrationKeepsNonEmptyTableAndDropsEmptyOne`). schema golden에서는 이 테이블 블록만 빠졌다.
    - api는 기동 때 migration ledger를 검사하지 않고, 이전 이미지도 503 경로에서 이 테이블을 읽지 않는다. 그래서 281 적용 뒤 이전 이미지로 rollback해도 동작은 같다.
    - `auth_users.session_generation`과 Me·Refresh의 세대 비교는 세션 폐기 장치로 남겼다. 이제 코드에서 값을 올리는 곳은 없다. 세대 시험 3개는 reset 대신 SQL로 세대를 올리도록 바꿨다.
    - 재설정 경로의 IP 허용 목록 시험은 `/api/auth` 그룹 연결을 덮던 유일한 시험이라, 지우지 않고 토큰 없는 `/api/auth/me`(401) 대상으로 옮겼다(`TestAPIRouterAuthGroupEnforcesAdminIPAllowlist`).
    - 지운 시험은 재설정 서비스 시험 6개, bcrypt cost 시험 1개, 503 핸들러 시험 1개다.
  - **F16:** `domain.Stream`의 Chzzk·Twitch 필드 10개를 지웠다. 이 필드를 채우는 운영 코드는 없었다.
    - 필드만 설정하던 시험(golden의 Twitch·Chzzk·동시송출 사례 3개, 퇴역 동시송출 링크 재등장 가드 2개, 플랫폼 구분이 사라져 다른 batch 시험과 같아진 notifier 시험 1개)을 지웠다.
    - 엄격 decode 경로 중 `domain.Stream`을 담는 것은 없다(`RejectUnknownMembers` 사용처 확인). 따라서 이전 payload와 캐시는 새 이미지에서도 읽힌다.
    - 형제 저장소(iris-console, chat-bot-go-kakao, iris-bridge, iris-client-go, twentyq-bot, shared-go)에서 지운 키를 읽는 곳은 없다.
    - **배포 조건:** dispatch event `payload_hash`가 바뀌지만 `event_key`는 payload hash를 포함하지 않는다. 배포 경계에서 옛 worker가 만든 event와 같은 `event_key`로 새 delivery가 들어오면, 충돌 경로가 승자 hash와 다른 payload의 delivery를 버리고 `alarm_dispatch_event_collisions`에 기록한다(`assignDeliveryEventIDs`). upcoming 후보도 같은 hash를 비교해 `rejected_collision`으로 끝낸다.
      - `event_key`는 방송·예정 시각·분·종류로 정해지므로, 배포 경계에서 같은 key가 다시 만들어질 수 있는 것은 진행 중이던 fanout의 재시도와 미처리 upcoming 후보로 본다(코드 경로 추론, 운영 측정 아님). 그래서 alarm-worker는 `alarm_upcoming_candidates`에 `outcome = 'pending'` 행이 없을 때 교체한다.
      - 교체 뒤에는 `alarm_dispatch_event_collisions`에 교체 시각 이후 `created_at` 행이 없는지 확인한다. 행이 있으면 그 event의 해당 방 알림이 누락됐을 수 있다.
  - 문서: `docs/current/services/hololive-api.md`의 Password reset API·Stream 필드 문장, `CHANGELOG.md` `미출시`를 갱신했다.
- **Fallback delta:** 새 fallback·retry·기본값 경로는 없다.
  - F3은 숨은 기본값(빈 `alarm_type` → Live)을 오류로 바꿨다.
  - rearm 정리로 요청 없는 FAILED 행이 시도 횟수와 무관하게 생산자 재적재 대상이 됐다. 그런 행은 발송된 적이 없다.
  - `seedErrorKeysPendingDeletion`은 시험의 허용 목록이며 제거 조건이 있다.
- worktree에 `youtubejs/node_modules`가 없어 helper 시험이 실패해, 잠금 파일 그대로 `npm ci --offline`으로 설치했다(추적 파일 변경 없음).
- `scripts/deploy/ap-rsync-files.txt`에서 지운 파일 3개(`alarm/keys/validation.go`, `database/pgx_helpers.go`, `util/math.go`)를 뺐다. `check-ap-rsync-manifest.sh`와 `test-compose-services.sh`가 통과한다.
- **최종 검증(F15·F16까지 마지막 코드 수정 뒤):**
  - `scripts/ci/local-ci.sh` 통과(golangci-lint 0 issues). 범위는 architecture gate(migration manifest·SQL ownership 포함), go mod tidy, `GOWORK=off` canonical vet, integration 태그 vet, staticcheck, golangci-lint, NilAway, 빌드, 전체 race 시험과 non-race 시험, collector·X Spaces helper, 운영 workspace·AP rsync·PostgreSQL 용량·YouTube 성능 예산 gate다. 앞선 실행들의 지적(golangci-lint 17건, 삭제 뒤 형식·unparam·wsl 33건, AP rsync 목록의 지운 파일 경로)은 고친 뒤 처음부터 다시 돌렸다.
  - `RUN_INTEGRATION_TESTS=true scripts/ci/local-ci.sh --integration-tests-only` 통과.
  - `./build-all.sh --build-only --no-bump --skip-local-ci hololive-api hololive-alarm-worker youtube-collector` 통과.
  - 전체 target 빌드는 `deunhealth`가 `cmd/app/main.go:155:3: unknown field BuildDate in struct literal of type gosplash.Settings`로 실패했다. `deploy/`는 이번 작업에서 바뀌지 않았으므로 기존 문제다.
  - deadcode v0.51.0 재실행: 운영 도달 불가 34개, 시험 포함 미사용은 인터페이스 mock 메서드 2개.
  - `scripts/ci/pre-push-gate.sh`는 각 push의 hook에서 통과했다(아래 게시와 배포).

## 게시와 배포 (v9.0.0, 2026-10-12)

사용자 요청("완결해줘 끝까지", "배포까지 끝까지")으로 게시와 중앙 배포까지 진행했다.

- **게시:** 저장소가 squash 병합만 허용해, 코드 삭제와 테이블 DROP을 별도 커밋으로 남기려고 PR을 둘로 나눴다.
  - #608 `8ad084b06`: 코드·문서 정리. #609 `f4828e538`: migration 281과 v9.0.0 버전·CHANGELOG. 두 PR 모두 CI 11개 통과, 병합 tree는 리뷰한 브랜치와 같다.
  - 각 push의 hook에서 `pre-push-gate.sh`가 통과했다(govulncheck 취약점 없음).
  - annotated tag `v9.0.0` → `f4828e5386b10e13d6caa4c952f685b1ca1e671d`. hook이 tag push에 `ALLOW_TAG_PUSH=1` opt-in을 요구해 붙였다.
  - GitHub Release는 `scripts/publish-release.sh v9.0.0 v8.0.0` preview 뒤 `create`로 게시했다(자동 생성 본문).
- **산출물:** kapu의 `kapu-multiarch` buildx로 `linux/arm64`, version 9.0.0, revision `f4828e5…`를 빌드했다.
  - `hololive-api:release-f4828e5386b1-r1-arm64` `sha256:9e47a0cf…`, `hololive-alarm-worker:release-f4828e5386b1-r1-arm64` `sha256:7311dd96…`. 원격 load 뒤 ID·arch·revision이 같았다.
  - 배포 정적 계약 5개(`test-three-runtime-topology`, `test-compose-services`, `ap-deploy-version_test`, `ap-host-native-deploy_test`, `systemd-compose-up_test`)가 통과했다.
- **사전 점검(hololive-seoul, read-only guard `on`):** ledger 141·마지막 280, `auth_password_reset_tokens` 0행, upcoming `pending` 0건, 최근 1일 충돌 0건. 원격 deploy tree에서 바뀌는 15개 파일은 배포 중이던 `0b5955bc1`과 같아 drift가 없었다.
- **전환:**
  - rollback tag `hololive-api:rollback-20261012T045543Z`(`a585e2a8…`, v8.0.0)와 `hololive-alarm-worker:rollback-20261012T045543Z`(`b24ec179…`)를 보존했다.
  - 바뀐 deploy tree 15개 파일을 `/opt/hololive-bot/compose/deploy-backups/pre-v9.0.0-20261012T045543Z/tree-files.tar`로 백업한 뒤 같은 소유자·mode로 교체했고, `compose config --quiet`가 통과했다.
  - 직전 upcoming `pending` 0건을 다시 확인하고 `change_started_at=2026-10-12T04:56:15Z`에 api·alarm-worker를 `up -d --no-build --no-deps`로 함께 교체했다. 두 서비스 모두 health gate를 통과했다.
  - 코드 교체를 확인한 뒤 migration을 따로 적용했다. 직전 0행을 다시 확인했고, `hololive-db-migrate`가 281만 적용했다(applied=1, skipped=141, total=142). 테이블·인덱스가 없어졌고 `auth_users`는 그대로다.
- **수용 증거(교체 후 약 15분):**
  - 두 container가 새 이미지 ID·revision·version으로 실행 중이다. `StartedAt`은 `change_started_at` 이후, healthy, 재시작 0회, OOM 없음.
  - api `/health`·`/ready`, admin `30006`, llm `30003`, worker `/health`·`/ready`가 모두 OK다. Postgres·Valkey 연결 표식이 있고, 교체 이후 로그의 panic·permission denied·x509·no such file·OOM·ERROR 표식은 0건이다.
  - 교체 이후 `alarm_dispatch_event_collisions` 새 행 0건.
  - 교체 직후 notifier 3회는 `sent=0, skipped=2/2/4, failed=0`이었다. 같은 05:00 방송 2건의 5분 전 알림을 이전 worker가 04:54에 보냈고(delivery 2건 SENT), 이후 3분·1분 선정은 중복 방지로 건너뛰었다. 그 upcoming 후보 4건은 이전 날들과 같은 방식으로 `expired`로 끝났다.
  - 교체 뒤 새 dispatch event는 아직 없어 실제 발송 경로는 관측하지 못했다.
- **범위 밖:** 중앙 collector `c`와 AP `a`·`b`·`d`는 동작 변경이 없어(alias 치환) 다시 배포하지 않았다. 중앙 `hololive-youtube-po-c`의 재시작은 이번 작업 전(약 04:43Z)이다.
- **v8.0.0 rollback 경로(v9.0.1 적용 뒤 폐기):** v9.0.0 전환 직후에는 v8.0.0 rollback tag와 tree 15개 파일 백업으로 api·worker를 함께 되돌릴 수 있었다. 이전 이미지의 `db-migrate`는 281이 기록된 ledger를 거부하므로 그때도 이미지만 되돌리는 경로였다.

## 게시와 배포 (v9.0.1, 2026-10-12)

오류 문구 시드 2행 삭제(2단계)를 사용자 요청("끝까지 완결")으로 이어서 진행했다.

- **게시:** #611 `f5fda7953`(migration 282, 시드 parity 시험의 삭제 대기 목록 제거, 루트·hololive-api VERSION 9.0.1). CI 11개 통과, 병합 tree는 리뷰한 브랜치와 같다. annotated tag `v9.0.1`과 GitHub Release(`publish-release.sh v9.0.1 v9.0.0`)를 게시했다. alarm-worker는 9.0.0을 유지한다.
- **사전 검증:** `TestErrorSeedHasNoOrphanKeys`가 282 포함 시 통과하고, manifest에서 282를 뺀 상태에서는 두 key를 orphan으로 잡아 실패함을 확인했다. `hololive-dbtest` 전체, `internal/migrationrunner`, `cmd/db-migrate`, `check-migration-manifest.sh`, `check-release-version.sh`가 통과했다.
- **api 전환:** `hololive-api:release-f5fda7953d18-r1-arm64`(`sha256:e534546f…`, arm64, 9.0.1)를 load해 같은 ID·revision을 확인했다. v9.0.0 api를 `rollback-20261012T055700Z`로 보존하고 VERSION 2개를 백업(`deploy-backups/pre-v9.0.1-20261012T055700Z`)한 뒤 교체했다. `change_started_at=2026-10-12T05:57:06Z`, health gate 통과, `APP_VERSION=9.0.1`, health·ready·admin·llm endpoint OK.
- **migration 282(06:50Z):** 사용자 결정으로 실제 발송 관측을 기다리지 않고 적용했다. 지우기 직전 두 행을 백업의 `message_strings-error-retired.json`(root 600)에 저장했고, `db-migrate`가 282만 적용했다(applied=1, skipped=142, total=143). 두 행 0건, error namespace 29행, api·worker healthy, 이후 오류 표식 0건.
- **rollback 정리:** 282 뒤 v8.0.0은 시드 2행이 없어 기동 검증에 실패하므로 rollback 대상에서 뺐다. v8.0.0 rollback tag 2개와 tree 백업 `pre-v9.0.0-20261012T045543Z`(git `0b5955bc1`의 15개 파일과 같음을 확인)를 지웠다. 이미지 자체는 `release-0b5955bc1646-r1-arm64` tag로 남아 있다. 원격 staging 사본도 지웠다.
- **현재 rollback:** api만 `hololive-api:rollback-20261012T055700Z`(v9.0.0)를 `prod`로 되돌리고 백업 tar로 VERSION 2개를 복원한 뒤 `--no-build --no-deps hololive-api`로 다시 띄운다. v9.0.0도 두 key를 읽지 않으므로 시드 복원과 migration 실행은 필요 없다. worker는 v9.0.0 그대로다.
- **수용 확인(09:36Z 기준):** 06:50Z에는 v9.0.0 전환 뒤 새 dispatch event가 없어 새 worker의 실제 발송을 관측하지 못했다. 이후 LIVE event 5건(07:25Z~08:55Z)이 생겼고 delivery 5건이 모두 `sent`로 끝났다. event 생성에서 발송까지 34~75 ms였고, 전환 뒤 worker notifier 요약의 `failed`는 모두 0이다. 같은 구간 hash 충돌 기록 0건, api·worker 오류 표식 0건, 두 서비스 재시작 0회다.
- **남은 일:**
  - F17은 2026-12-20 이후 다시 확인한다(아래 "정리"의 날짜 정정).
  - 이번 작업 전부터 있던 rollback tag(`hololive-api:rollback-20261008T114613Z`, `*:rollback-20261011-review`, `hololive-api:prod-arm64-a6219fb8`)와 `deploy-backups/pre-v7.2.10-20261008T114613Z`는 현재 ledger보다 오래돼 rollback 대상이 아니다. 이번 작업 산출물이 아니어서 두었다가, 사용자 요청으로 지웠다(아래 "정리").
  - 원 checkout `/home/kapu/work/iris-stack/hololive-bot`의 미커밋 합류분은 로컬 백업 브랜치에 보존한 뒤 main으로 맞췄다. 메타 저장소 hololive-bot 포인터는 v9.0.0 반영 `858b21b36` 뒤 이 기록까지 갱신한다.

## B1·B2 구현 (v9.1.0, 2026-10-12)

첫 단계에서 승인 필요로 빼 두었던 B1·B2를 사용자 결정("B1·B2 모두 진행")으로 진행했다. B3는 범위 밖으로 남겼다.

- **B1:** migration 283~290이 인덱스 8개를 `DROP INDEX CONCURRENTLY` 단일 문장으로 지운다. 확인 필요였던 `idx_alarm_dispatch_deliveries_room_created`도 지웠다. delivery를 room으로 거르는 조회는 관리 목록뿐이고, 이 목록은 상태 조건과 `d.id` 역순으로 읽어 `(room_id, created_at)` 순서를 쓰지 않는다.
- **B2:**
  - 291이 `fillfactor = 90`과 `history_sort_at` 저장 생성 열을 한 `ALTER TABLE`로 적용한다. rewrite는 한 번이고 `lock_timeout`은 3초다. PostgreSQL 18.6 임시 컨테이너에서 같은 형태의 rewrite가 fillfactor를 적용함을 pgstattuple로 확인했다(여유 공간: 열 추가만 0.6%, 묶은 경우 10.6%).
  - 292~295가 `idx_yls_ended_history_sort`, `idx_yls_ended_channel_history_sort`, `idx_yls_channel`, `idx_yls_status`를 만든다. 296~300이 기존 정렬 식 인덱스 2개와 `idx_yls_channel_last_seen`, `idx_yls_status_last_seen`, `idx_yls_ended_cleanup`을 지운다.
  - 방송 이력 목록 SQL(`0179_01`)의 정렬·커서 식을 `history_sort_at`으로 바꿨다. 단건 조회(`0242_02`)는 정렬식을 쓰지 않는다. Go 커서 계산(`broadcastHistorySortTime`)은 생성 열과 같은 우선순위라 주석으로 계약만 남겼다.
  - 세션 테이블을 쓰는 다른 런타임 SQL은 모두 열 이름을 명시한다. 행 전체를 쓰는 review snapshot(`to_jsonb(session)`)은 영수증 비교에 지정 필드만 쓰므로 열 추가가 판정을 바꾸지 않는다. collector는 이 테이블을 직접 쓰지 않는다.
- **시험:**
  - `TestLiveSessionRepeatedObservationIsHeapOnlyUpdate`: 같은 상태의 반복 관측 5회가 모두 HOT이어야 한다. 291~300 없이 실행하면 HOT 0회로 실패하고, 적용하면 통과한다.
  - `TestPgBroadcastHistoryRepositoryPagesAcrossSortTimeFallbacks`: 종료·시작·예정·`last_seen_at` 우선순위, 같은 시각의 `video_id` 역순, 페이지 경계 분할을 확인한다. 수정 전 SQL과 스키마에서도 같은 결과로 통과했다.
  - `TestBroadcastHistoryPageReadsSortIndexInsteadOfEndedHistory`: 종료 세션 2만 행에서 첫 페이지와 다음 페이지가 새 정렬 인덱스를 쓰고 seq scan을 하지 않는지 확인한다. 292·293 없이 실행하면 실패한다. 다음 페이지는 커서 조건이 OR라 커서보다 새 행을 인덱스 앞에서부터 건너뛰며 읽는다. 수정 전 SQL도 같은 성질이라 이 단계에서는 바꾸지 않았고, "방송 이력 커서·채널 조건" 절에서 고쳤다.
- **검증:** `scripts/ci/local-ci.sh`(lint 0건)와 `--integration-tests-only`, `check-migration-manifest.sh`, `check-release-version.sh`가 통과했다. schema golden은 인덱스·`fillfactor`·생성 열만 바뀌었다.
- **배포 조건:** 새 SQL이 생성 열을 읽으므로 migration을 api 교체보다 먼저 적용한다. 기존 인덱스 삭제도 같은 실행에 들어가므로, 교체 전 몇 분 동안 v9.0.1 api의 방송 이력 조회는 정렬 인덱스 없이 실행된다. 종료 세션이 약 1만 행이라 영향은 작다고 판단했다. v9.0.1로 되돌려도 같은 이유로 동작한다.

### 정리 (2026-10-12)

- 사용자 요청("완결할것")으로 이번 작업 전부터 있던 rollback 산출물을 지웠다. 대상은 `hololive-api:rollback-20261008T114613Z`(7.2.9), 같은 이미지인 `hololive-api:rollback-20261011-review`·`hololive-api:prod-arm64-a6219fb8`(7.2.10), `hololive-alarm-worker:rollback-20261011-review`(6.1.2), `deploy-backups/pre-v7.2.10-20261008T114613Z`다. 지우기 전에 이미지 ID와 컨테이너 미사용을 다시 확인했다. v8.0.0 이미지(`release-0b5955bc1646-r1-arm64`)와 collector·po rollback tag는 대상이 아니어서 남겼다(뒤에 별도 승인으로 지웠다. "게시와 배포 (v9.1.1)" 절의 정리 참고).
- 로컬 백업 브랜치 `backup/orig-checkout-merged-20261012`(`a64b893d0`)를 지웠다. main에 합쳐 보면 바뀌는 파일은 그 뒤 main이 의도적으로 고친 3개뿐이었다.
- **F17 날짜 정정:** 대상 308행 중 `sent` 294행의 최신 `sent_at`은 2026-08-10이지만, `cancelled` 14행은 `cancelled_at`이 2026-09-21 07:17Z다. 보존 삭제는 상태별 종료 시각 기준 90일이므로 0건이 되는 것은 2026-12-20 07:17Z 이후다. 코드의 재검토 기한 2026-12-31 안이다.

## 게시와 배포 (v9.1.0, 2026-10-12)

- **게시:** #615 `48f856815`. CI 11개가 통과했고 병합 tree는 리뷰한 브랜치와 같다. annotated tag `v9.1.0`과 GitHub Release(`publish-release.sh v9.1.0 v9.0.1`)를 게시했다.
- **산출물:** `hololive-api:release-48f856815431-r1-arm64`(`sha256:e9da9816…`, arm64, 9.1.0)를 원격에 load해 같은 ID·revision을 확인했다.
- **사전 점검:** 원격 deploy tree 112개 파일 중 v9.1.0과 다른 것은 VERSION 2개와 collector `go.mod`였다. `go.mod`는 2026-10-06 #585 시점 파일이고 v9.0.1과 v9.1.0 사이에 바뀌지 않아, 이번 api 배포 대상이 아니므로 두었다. 5분 넘게 열린 트랜잭션은 0건이었다.
- **전환:**
  - v9.0.1 api를 `hololive-api:rollback-20261012T105146Z`로 보존하고 VERSION 2개를 `deploy-backups/pre-v9.1.0-20261012T105146Z`에 백업한 뒤 교체했다.
  - `db-migrate`가 `hololive-api:prod` 이미지로 실행되므로 후보를 `prod`로 tag한 뒤 migration을 적용했다. 283~300이 11:02:13~11:02:16Z에 적용됐다(applied=18, skipped=143, total=161).
  - `change_started_at=2026-10-12T11:02:16Z`에 api를 `up -d --no-build --no-deps hololive-api`로 교체했고 health gate를 6초에 통과했다. 이전 api가 새 스키마에서 돈 시간은 약 3초다.
- **수용 증거:**
  - api 9.1.0 healthy, 재시작 0회, `APP_VERSION=9.1.0`이다. ledger 161건(마지막 300), invalid 인덱스 0, 지운 인덱스 13개 잔존 0, `fillfactor=90`, 생성 열 값 불일치 0행이다.
  - 운영 DB에서 방송 이력 첫 페이지 계획은 `idx_yls_ended_history_sort`로 101행만 읽고 4.6 ms였다.
  - 교체 이후 api 오류 표식 0건이다. worker 오류 1건은 11:06:25Z의 Holodex 라이브 상태 요청 시간 초과다. 외부 API 경로이고 worker는 교체하지 않았으며, worker 기동(04:56Z) 이후 유일한 건이고 재발하지 않았다.
- **사후 측정(`youtube_live_sessions`):**

| 구간 | UPDATE | HOT | 새 페이지 갱신 | autovacuum |
| --- | ---: | ---: | ---: | --- |
| 배포 전 10:51:46~11:02:00Z | 1,094 | 142 (13.0%) | 29 | 1회(11:01:55, 직전 10:39:54) |
| 배포 후 11:03:26~11:13:30Z | 1,092 | 1,085 (99.4%) | 1 | 0회 |

  - 테이블은 316 페이지에서 385 페이지가 됐다(fillfactor 90과 생성 열). 인덱스를 포함한 전체 크기는 7.0 MB에서 4.9 MB로 줄었다.
  - autovacuum은 배포 전 약 22분 간격이었고, 배포 뒤 측정 구간에는 실행되지 않았다(`n_dead_tup` 66). 간격의 장기 변화는 이 10분 구간으로 판단하지 않았다.
- **현재 rollback:** api만 `hololive-api:rollback-20261012T105146Z`(v9.0.1)를 `prod`로 되돌리고 백업 tar로 VERSION 2개를 복원한 뒤 `--no-build --no-deps hololive-api`로 다시 띄운다. 스키마는 그대로 두며, v9.0.1의 방송 이력 조회는 정렬 인덱스 없이 동작한다.
- **정리:** 수용 뒤 이번 작업이 만든 이전 rollback 산출물(`hololive-api:rollback-20261012T055700Z` tag, `deploy-backups/pre-v9.0.1-20261012T055700Z`)을 지웠다. v9.0.0 api 이미지는 `release-f4828e5386b1-r1-arm64` tag로 남아 있다.
- **남은 일:** F17은 2026-12-20 이후 다시 확인한다.

## 방송 이력 커서·채널 조건 (2026-10-12)

사용자 요청("추가이슈도 수정")으로 v9.1.0 보고에 남긴 후속 항목을 고쳤다.

- **커서 조건:** 다음 페이지 조건 `($3 IS NULL OR 정렬값 < $3 OR (정렬값 = $3 AND video_id < $4))`는 인덱스 시작 위치가 되지 못한다. 그래서 다음 페이지가 커서보다 새 행을 인덱스 앞에서부터 건너뛰며 읽었다(2만 행 시험의 전체 목록 다음 페이지에서 161행). since·커서를 `history_sort_at >= COALESCE($2, '-infinity')`와 행 비교 `(history_sort_at, video_id) < (COALESCE($3, 'infinity'), $4)`로 바꿨다. 두 조건이 정렬 인덱스의 Index Cond가 되어 다음 페이지도 101행만 읽는다. NULL since·커서는 무한대 경계이고, 정렬값이 무한대인 행은 없으므로 결과와 순서는 같다.
- **채널 조건:** `($1 = '' OR s.channel_id = $1)`은 custom plan에서는 값이 접혀 채널 정렬 인덱스를 쓴다. 그러나 generic plan에서는 채널 조건이 Filter로 남아, 전체 정렬 인덱스에서 다른 채널 행을 건너뛰며 읽었다(2만 행·40채널 시험에서 3,927행 제거). 운영 풀은 `cache_statement`라 같은 연결에서 반복 실행되면 generic plan으로 바뀔 수 있다. 전체·채널 목록을 `{{channelPredicate}}`로 나눈 두 문장으로 준비한다. 이 결함은 이번 확인에서 새로 찾았다.
- **시험:** `TestBroadcastHistoryPageReadsSortIndexInsteadOfEndedHistory`를 이름 있는 prepared statement로 바꿨다. 이제 custom·generic plan 모두에서 4개 경우가 정렬 인덱스로 한 페이지만 읽는지 확인한다. 이전 커서 조건으로 되돌리면 다음 페이지 4개가, 이전 채널 조건으로 되돌리면 generic plan의 채널 경우 2개가 실패한다. 페이지 경계 시험(`TestPgBroadcastHistoryRepositoryPagesAcrossSortTimeFallbacks`)은 그대로 통과한다.
- **운영 영향:** 방송 이력 조회는 드물고(2026-10-11 기준 49일 동안 정렬 인덱스 사용 약 1,050회) 종료 세션이 약 1만 행이라, 고치기 전에도 체감 지연은 작았다. migration은 없다.
- **central deploy tree:** collector `go.mod`가 2026-10-06 #585 시점 파일로 남아 실행 중인 `collector-c`(v8.0.0, `0b5955bc1`)와도 맞지 않았다. `0b5955bc1`과 v9.1.0의 내용이 같음을 확인하고 그 내용으로 바꿨다(원본은 `deploy-backups/collector-gomod-20261012T113752Z`). `--no-build` 배포에서는 읽지 않는 파일이다. 교체 뒤 tree 112개 파일이 모두 v9.1.0과 같음을 hash로 확인했다.

## 게시와 배포 (v9.1.1, 2026-10-12)

사용자 결정("v9.1.1 게시·배포까지")으로 위 수정을 운영에 반영했다.

- **게시:** #617 `51990ac5f`. CI 11개가 통과했고 병합 tree는 검증한 브랜치와 같다. annotated tag `v9.1.1`과 GitHub Release(`publish-release.sh v9.1.1 v9.1.0`)를 게시했다.
- **산출물:** `hololive-api:release-51990ac5f4c9-r1-arm64`(`sha256:c919f0fa…`, arm64, 9.1.1)를 원격에 load해 같은 ID·revision을 확인했다.
- **사전 확인(운영 DB, 읽기 전용):** 종료 세션이 가장 많은 채널과 전체 목록 301번째 행의 시각을 커서로 두고, 두 SQL을 prepared statement로 custom·generic plan 모두에서 실행했다.

| 경우 | v9.1.0 SQL이 걸러낸 행(custom / generic) | v9.1.1 SQL |
| --- | ---: | ---: |
| 전체 첫 페이지 | 0 / 0 | 0 / 0 |
| 전체 다음 페이지 | 301 / 301 | 0 / 0 |
| 채널 첫 페이지 | 0 / 4,395 | 0 / 0 |
| 채널 다음 페이지 | 8 / 4,457 | 0 / 0 |

  - v9.1.0 SQL의 generic plan 채널 조회는 전체 정렬 인덱스를 써서 4천여 페이지를 읽었다(5.9~8.2 ms). v9.1.1 SQL은 8개 경우 모두 since·커서·채널이 Index Cond가 되어 95~99 페이지만 읽었다(0.7~2.2 ms).
  - 같은 입력의 결과는 4개 경우 모두 101행으로 두 SQL이 같았다(내용은 저장하지 않고 hash만 비교했다).
- **전환:** v9.1.0 api를 `hololive-api:rollback-20261012T121704Z`로 보존하고 VERSION 2개를 `deploy-backups/pre-v9.1.1-20261012T121704Z`에 백업한 뒤 교체했다. ledger가 161건(마지막 300)임을 확인하고 migration 없이 `change_started_at=2026-10-12T12:17:11Z`에 api를 `up -d --no-build --no-deps hololive-api`로 교체했다. health gate는 6초에 통과했다.
- **수용 증거:** api 9.1.1 healthy, 재시작 0회, `APP_VERSION=9.1.1`, ledger 161건 그대로다. 교체 뒤 api·worker 오류 표식은 0건이다. 배포 뒤 같은 계획 확인에서 8개 경우 모두 걸러낸 행이 없었다. 원격 tree 112개 파일이 v9.1.1과 같다.
- **현재 rollback:** api만 `hololive-api:rollback-20261012T121704Z`(v9.1.0)를 `prod`로 되돌리고 백업 tar로 VERSION 2개를 복원한 뒤 `--no-build --no-deps hololive-api`로 다시 띄운다. 스키마 변경이 없어 다른 단계는 없다.
- **정리:** 수용 뒤 이전 rollback 산출물(`hololive-api:rollback-20261012T105146Z` tag, `deploy-backups/pre-v9.1.0-20261012T105146Z`)을 지웠다. v9.0.1 api 이미지는 `release-f5fda7953d18-r1-arm64` tag로 남아 있다.
- **이미지 정리:** 사용자 승인("이미지 승인")으로 central의 이미지 6개를 지웠다. 대상은 v8.0.0 `hololive-api`·`hololive-alarm-worker:release-0b5955bc1646-r1-arm64`와 `hololive-youtube-collector`·`hololive-youtube-po-sandbox`의 `rollback-20261008T115407Z`(7.2.3)·`rollback-20261011T164739Z`(7.2.10)다. 지우기 직전에 ID·버전 label, 컨테이너 미사용, compose·systemd 설정에서 참조하지 않음을 확인했다. 모두 tag가 하나뿐이라 이미지가 삭제됐다. 실행 중인 collector·po `prod`(8.0.0)는 남겼다. 이제 collector-c·po-c는 central에 rollback 이미지가 없으므로, 되돌리려면 이전 revision에서 다시 build해 옮겨야 한다. Seoul·Osaka 이미지는 확인하지 않았다.
- **남은 일:** F17은 2026-12-20 이후 다시 확인한다.

## F17 종결 (v9.1.2, 2026-10-12)

사용자 결정으로 보존 삭제(2026-12-20 완료 예정)를 기다리지 않고 send unit 없는 legacy delivery를 지우고 분기를 없앴다.

- **운영 조회(읽기 전용, 2026-10-12):** `send_unit_id IS NULL` 행은 `sent` 294(최신 `sent_at` 2026-08-10), `cancelled` 14(최신 `cancelled_at` 2026-09-21)였다. 이 행을 가리키는 관리자 작업은 0건이고 테이블 전체는 1,294행·1.5 MB다.
- **migration 301:**
  - 처음부터 ACCESS EXCLUSIVE를 잡고 `sent`·`cancelled` NULL 행만 지운다. dlq·quarantined NULL 행이 있으면 운영자가 처리할 실패 기록이므로 지우지 않고 실패한다.
  - 이어서 NOT VALID CHECK를 VALIDATE한 뒤 `send_unit_id`를 NOT NULL로 바꾼다. 같은 조건을 포함하게 된 224의 `alarm_dispatch_deliveries_active_send_unit_check`는 지운다.
  - `alarm_dispatch_admin_actions.delivery_id`는 보존 삭제와 같이 ON DELETE SET NULL이 적용된다.
- **코드:**
  - `group.sql`·`group_locked.sql`의 `send_unit_id IS NULL` 단일 행 분기를 지웠다.
  - 네 조회 SQL의 `COALESCE(send_unit_id::text, '')`와 `replayBlock`의 빈 send unit 조건, 제거 조건 주석도 지웠다.
  - `sendUnitId` JSON 필드는 그대로이고 항상 값이 있다. iris-console Android의 빈 값 표시("없음")는 쓰이지 않게 되지만 동작은 같다.
- **시험:**
  - `TestSendUnitRequiredMigrationDeletesSettledLegacyRowsOnly`: 301 전 형태에서 dlq legacy 행이 있으면 실패하고 아무것도 지우지 않는다. 종료 legacy 행만 남으면 그 행만 지운다. 그 뒤 NOT NULL·224 CHECK 삭제·관리자 작업 보존과 재적용 멱등을 확인한다.
  - `TestAlarmDispatchDeliveriesRequireSendUnit`가 224 CHECK 시험을 대신해, 상태와 무관하게 send unit 없는 기록이 23502로 거부됨을 확인한다.
  - send unit 없는 행을 seed하던 dispatchops·worker 보존 삭제 계획·상태 CHECK 시험은 send unit을 쓰게 바꿨다. legacy 행과 비교하지 않음을 지키던 worker 시험(`DoesNotCompareLegacyDedupeKey`)은 그 행이 존재할 수 없게 되어 지웠다.
- **runbook:** `admin-dispatch-operations.md`의 legacy 단일 항목 설명과 NULL 행 확인 조회를 현재 계약으로 바꿨다.
- **배포 조건:** migration을 api 교체보다 먼저 적용한다. v9.1.1 api와 alarm-worker 9.0.0은 NULL 없는 테이블에서 그대로 동작하므로 api만 다시 릴리스한다. 지운 legacy 행은 rollback으로 돌아오지 않는다.

## 게시와 배포 (v9.1.2, 2026-10-12)

- **게시:** #620 `8435b01c6`. CI 11개가 통과했고 병합 tree는 검증한 브랜치와 같다. annotated tag `v9.1.2`와 GitHub Release(`publish-release.sh v9.1.2 v9.1.1`)를 게시했다.
- **산출물:** `hololive-api:release-8435b01c6c73-r1-arm64`(`sha256:972ba82e…`, arm64, 9.1.2)를 원격에 load해 같은 ID·revision을 확인했다.
- **전환:**
  - v9.1.1 api를 `hololive-api:rollback-20261012T141203Z`로 보존하고 VERSION 2개를 `deploy-backups/pre-v9.1.2-20261012T141203Z`에 백업했다.
  - 지울 legacy 308행을 같은 디렉터리의 `legacy-deliveries-without-send-unit.csv`(root 600, sha256 동봉)로 읽기 전용 COPY했다. 내용은 원격에만 두었다.
  - 후보를 `prod`로 tag한 뒤 migration 301을 14:12:13~14:12:16Z에 적용했다(applied=1, skipped=161, total=162). `change_started_at=2026-10-12T14:12:17Z`에 api를 `up -d --no-build --no-deps hololive-api`로 교체했고 health gate를 6초에 통과했다.
- **수용 증거:**
  - 적용 전에는 delivery 1,295행에 legacy가 `cancelled` 14·`sent` 294행이었고 `send_unit_id`는 NULL을 허용했다. 적용 후 987행(정확히 308행 감소), legacy 0행, `send_unit_id` NOT NULL, 224 CHECK 없음, 미검증 제약 0이다.
  - api 9.1.2 healthy, 재시작 0회, `APP_VERSION=9.1.2`다. 교체 뒤 api·worker 오류 표식(23502 포함)은 0건이다.
  - 새 묶음 조회 SQL(`group.sql`)을 운영 DB에서 읽기 전용으로 실행했다. `idx_alarm_dispatch_deliveries_send_unit`로 0.9 ms였다.
  - 원격 tree 112개 파일이 v9.1.2와 같다.
- **현재 rollback:** api만 `hololive-api:rollback-20261012T141203Z`(v9.1.1)를 `prod`로 되돌리고 백업 tar로 VERSION 2개를 복원한 뒤 `--no-build --no-deps hololive-api`로 다시 띄운다. NOT NULL은 v9.1.1과 worker 9.0.0에 영향이 없어 그대로 둔다. legacy 행이 필요하면 백업 CSV에서 되살린다.
- **정리:** 이전 rollback 산출물(`hololive-api:rollback-20261012T121704Z` tag, `deploy-backups/pre-v9.1.1-20261012T121704Z`)을 지웠다. v9.1.0 api 이미지는 `release-48f856815431-r1-arm64` tag로 남아 있다.
- **남은 일:** 이 계획의 F 항목 중 운영 조건을 기다리는 것은 없다. legacy CSV 백업은 필요 없다고 판단되면 지운다.

## B3와 F1 collector 배포 (v9.2.1, 2026-10-13)

사용자 결정으로 범위 밖에 두었던 B3를 추가하고, F1이 들어간 collector를 배포한다. 같은 결정으로 legacy CSV 백업(`pre-v9.1.2-20261012T141203Z/legacy-deliveries-without-send-unit.csv`)을 hash 확인 뒤 지웠고, VERSION 백업 tar는 rollback용으로 남겼다.

- **B3 근거(운영, 2026-10-12):** `source_observation_queue`는 PROCESSED 219,580행·28 MB이고, 보존 삭제 SQL(`repository_retention_delete_queue_0076_76.sql`)은 20,998회 호출에 평균 166 ms·최대 9.2초였다.
- **migration 302·303:** `(processed_at, observation_id) WHERE status = 'PROCESSED'`, `(dead_lettered_at, observation_id) WHERE status = 'DEAD_LETTER'`를 각각 `CREATE INDEX CONCURRENTLY IF NOT EXISTS`로 만든다. SQL은 바꾸지 않는다.
- **시험:** `TestQueueRetentionDeleteReadsOnlyExpiredRows`는 이력 2만 행 중 만료 401행에서 custom·generic plan 모두 두 부분 인덱스를 쓰고 seq scan 없이 만료 행만 읽는지 확인한다. 후보 CTE와 바깥 DELETE가 한 번씩 읽어 802행이다. 302·303 없이 실행하면 두 plan 모두 seq scan으로 실패한다.
- **F1 collector 범위:** 실행 중인 collector(a·b·c·d)는 v8.0.0(`0b5955bc1`)이다. main까지 collector 바이너리에 들어가는 변경은 #608(F1 alias 치환, 쓰지 않는 공유 함수 삭제, 주석, 오류 문구 1건)과 #613(Go builder 이미지 digest, npm 11.21.0)뿐이고 동작 변화는 없다. Go 의존성 갱신(#604)은 이미 v8.0.0에 들어 있다. collector와 PO issuer는 같은 full SHA로 함께 전환한다.
- **다른 세션과의 조정:** v9.2.0(#622, alarm-worker만)을 배포한 세션에 원격 작업이 끝났고 계획도 없음을 확인했다. 그 세션의 `pre-v9.2.0` 백업과 worker rollback tag는 건드리지 않는다.

## 게시와 배포 (v9.2.1, 2026-10-13)

시각은 UTC(2026-10-12)다.

- **게시:** #624 `b482dfd03`. 그 사이 병합된 #623(문서)을 GitHub update-branch로 합친 뒤 CI 11개가 다시 통과했다. 병합 결과와 검증한 커밋의 차이는 #623 문서 하나다. annotated tag `v9.2.1`과 GitHub Release(`publish-release.sh v9.2.1 v9.2.0`)를 게시했다.
- **산출물:** `hololive-api:release-b482dfd03f3f-r1-arm64`(`sha256:f87b4722…`, arm64, 9.2.1)를 원격에 load해 같은 ID·revision을 확인했다. collector·PO issuer는 각 배포 스크립트가 같은 revision으로 kapu에서 빌드했다(c·b는 arm64 이미지, a·d는 amd64 rootfs).
- **api 전환:**
  - 9.1.2 api를 `hololive-api:rollback-20261012T161459Z`로 보존하고 VERSION 2개를 `deploy-backups/pre-v9.2.1-20261012T161459Z`에 백업했다.
  - 5분 넘게 열린 트랜잭션이 없음을 확인하고 migration 302·303을 16:15:08~16:15:12Z에 적용했다(applied=2, skipped=162, total=164). `change_started_at=2026-10-12T16:15:13Z`에 api를 `up -d --no-build --no-deps hololive-api`로 교체했고 health gate를 6초에 통과했다.
- **collector 전환(모두 `b482dfd03`, 9.2.1):**
  - 중앙 `c`: `po-central-cutover.sh deploy`(change `20261012T162312Z`). 16:23:43Z에 issuer 우선 cutover가 검증됐고 백업은 `backups/po-c-20261012T162312Z`다. 새 issuer는 이전 collector 종료 때 exit 0 재시작 1회(`reason=session_closed`)로, runbook의 예상 교체다. 이전 issuer 컨테이너의 exit 143은 recreate의 SIGTERM이다. `/ready`가 성공했고 instance는 `youtube-collector-c`다.
  - Seoul `b`: `ap-deploy.sh seoul --apply`. 16:25:57Z에 기동해 완료 검사를 통과했고 백업은 `backups/seoul-collector-20261012T162535Z`다. collector·issuer 재시작 0회.
  - Osaka `a`: release `20261012T162653Z-b482dfd03f3f-osaka`(previous `20261011T164902Z-0b5955bc1646-osaka`). 16:29:08Z에 기동해 완료 검사를 통과했다.
  - Osaka2 `d`: release `20261012T163017Z-b482dfd03f3f-osaka2`(previous `20261011T165311Z-0b5955bc1646-osaka2`). 16:32:36Z에 기동해 완료 검사를 통과했다.
  - native 두 대의 issuer는 `Result=success`, `ExecMainStatus=0`, `NRestarts=0`이다.
- **수용 증거:**
  - 적용 전 보존 삭제 계획은 custom·generic 모두 `source_observation_queue` Seq Scan이었다. 적용 후 두 인덱스가 valid(PROCESSED 6.9 MB, DEAD_LETTER 8 kB, DEAD_LETTER 행 없음)이고 INVALID 인덱스는 0개이며, 두 계획 모두 두 부분 인덱스의 Bitmap Index Scan이다.
  - 배포 뒤 보존 삭제 6회(16:22~16:33Z, 1,825행 삭제)의 평균은 1.69 ms다. 배포 전 누적 평균은 166 ms(21,020회)였다. 두 인덱스의 `idx_scan`은 16:33Z까지 각각 20회다.
  - api 9.2.1 healthy, 재시작 0회, `APP_VERSION=9.2.1`이다. 16:15:13Z 이후 api·worker·collector-c·po-c 오류 표식은 0건이다.
  - 원격 tree 112개 파일이 v9.2.1과 같다.
- **iris-console:** Android 디코더가 빈 `sendUnitId`를 계약 위반으로 거절하고, 상세 화면의 "없음" 대체 표시를 지웠다(iris-console `cdb32a7`, main 직접 반영, `verify-all.sh` 통과). 앱 릴리스는 하지 않았다.
- **현재 rollback:**
  - api: `hololive-api:rollback-20261012T161459Z`(9.1.2)를 `prod`로 되돌리고 백업 tar로 VERSION 2개를 복원한 뒤 `--no-build --no-deps hololive-api`로 다시 띄운다. 302·303 인덱스는 남겨도 9.1.2에 영향이 없다.
  - collector: c는 `po-central-cutover.sh rollback backups/po-c-20261012T162312Z`, b는 `ap-rollback.sh seoul`, a·d는 `ap-host-native-rollback.sh <ap>`(previous release)다.
- **다른 세션:** iris-console-28 세션이 v9.2.1 위에 v9.2.2(alarm-worker만, migration 없음)를 준비하고 있다. 중앙 작업이 끝났음을 알렸고, 그 세션은 위 rollback 기준점을 건드리지 않는다.
- **남은 일:** 없음. 이전 기준점과 v9.2.1 rollback 산출물은 아래 "후속 정리"에서 지웠다.

### 후속 정리 (2026-10-13)

사용자 결정("기존 이슈도 해결", 정리 승인)으로 진행했다. 시각은 UTC(2026-10-12)다.

- **native AP logrotate:** Osaka·Osaka2 host-native 배포가 설치하는 `/etc/logrotate.d/hololive-bot`은 `daily`·`rotate 14`·`size 10M`을 함께 써서 배포 때 `'size' overrides previously specified 'daily'` 경고가 났다.
  - 처음에는 `daily`가 의도라고 보고 #626 `5addd76ce`에서 `maxsize 10M`으로 바꿔 16:57:30Z에 두 호스트에 반영했다. 이 변경은 v9.2.2에 포함되어 게시됐다.
  - 그 뒤 확인해 보니 collector 로그는 앱(shared-go logging의 lumberjack)이 `LOG_MAX_SIZE_MB`(기본 5, native 호스트는 기본값)마다 회전하고 압축 백업을 같은 `archive`로 옮긴다. 원래 `size 10M`은 앱 회전이 멈췄을 때만 동작하는 안전망이었고, 두 호스트의 archive가 비어 있던 것은 로그가 아직 5 MB에 이르지 않았기 때문이다. `maxsize`·`daily`를 두면 앱 회전과 별도로 매일 copytruncate 회전이 일어난다.
  - 정정: `size 10M`만 남기고 `daily`를 지웠다(`create`는 copytruncate에서 효과가 없어 계속 두지 않는다). 첫 일일 실행(2026-10-13 00:00Z) 전인 17:33:14Z에 두 호스트의 파일을 이 내용(sha256 `dcdb1268…`, root 0644)으로 바꿨다. 이전 파일은 `/var/backups/hololive-bot-logrotate/hololive-bot.20261012T165730Z`(원래 설정)와 `hololive-bot.20261012T173314Z`(maxsize 설정)에 있다.
  - 두 호스트의 `logrotate -d`는 경고 없이 `log does not need rotating (log size is below the 'size' threshold)`이다. kapu logrotate로 3 MB는 회전하지 않고 11 MB는 회전함을 확인했다.
- **central 이전 rollback 기준점:** `hololive-api:rollback-20261012T141203Z` tag를 지웠다(v9.1.1 이미지 `c919f0faade7`은 `release-51990ac5f4c9-r1-arm64` tag로 남는다). `deploy-backups/pre-v9.1.2-20261012T141203Z`(VERSION tar, Git v9.1.1과 같은 내용)를 지웠다. 다른 세션의 `pre-v9.2.0-20261012T145705Z`와 `collector-gomod-20261012T113752Z`는 남겼다. api·worker는 healthy다.
- **native AP 배포 산출물:** [보존 절차](../../../../docs/ops/release-artifact-retention.md)의 후보를 승인받아 `prune-release-artifacts.py --apply`로 호스트마다 4개를 지웠다.
  - Osaka: `/home/ubuntu/.hololive-host-native-osaka-20261006T121252Z-340283120621-osaka`, `...-20261007T020647Z-4a633ab24e8c-osaka`, `releases/20261006T121252Z-340283120621-osaka`, `releases/20261007T020647Z-4a633ab24e8c-osaka`. 사용량 16G→14G.
  - Osaka2: 같은 두 세대(`20261006T121616Z-340283120621`, `20261007T021336Z-4a633ab24e8c`)의 staging·release. 사용량 13G→12G.
  - 남은 release는 current(v9.2.1), previous(v8.0.0), `a6219fb84abe`(7.2.10)다. `.incoming-*`와 소유 불명 파일은 남겼다. 정리 뒤 두 호스트의 완료 검사가 통과했고 collector는 active다.
- **v9.2.1 rollback 산출물 (사용자 승인):** 수용 뒤 정리했다. 이제 9.1.2 api·v8.0.0 collector로 되돌리려면 release tag 이미지를 다시 쓰거나 다시 빌드해야 한다.
  - central: `hololive-api:rollback-20261012T161459Z` tag(9.1.2 이미지는 `release-8435b01c6c73-r1-arm64`로 남는다), `hololive-youtube-collector:rollback-20261012T162312Z`·`hololive-youtube-po-sandbox:rollback-20261012T162312Z`(v8.0.0 이미지 `95644d3038ad`·`3bf902703778` 삭제), `deploy-backups/pre-v9.2.1-20261012T161459Z`, `deploy-backups/collector-gomod-20261012T113752Z`, `po-staging-20261012T162312Z`(이미지 tar와 live `po-current-c`와 같은 영수증 사본)를 지웠다. 영수증이 든 `backups/po-c-*`와 다른 세션의 `pre-v9.2.0`·`pre-v9.2.2` 및 worker rollback tag는 남겼다. 정리 뒤 central 컨테이너 4개가 healthy다.
  - Seoul: `hololive-youtube-collector:rollback-20261012T162535Z`·`hololive-youtube-po-sandbox:rollback-20261012T162535Z`(v8.0.0 이미지)를 지웠다. 현재 issuer 검토 영수증이 든 `backups/seoul-collector-20261012T162535Z`는 남겼다. collector-b·po-b는 9.2.1 healthy다.
  - Osaka·Osaka2의 previous release(v8.0.0)는 보존 절차가 항상 남기므로 그대로 둔다.
  - 범위 밖으로 남긴 것: Seoul에는 2026-06부터 쌓인 백업 디렉터리 100여 개와 이전 rollback 이미지 tag 8개가 있어 디스크 사용률이 60%다. 일부는 issuer 영수증 복구 근거로 쓰이므로, 정리하려면 보존 기준을 따로 정해야 한다.

## 1. SQL 최적화

측정 근거와 조사 방법은 부록에 있다.

### A1. send unit 조인의 형식 불일치

`hololive-alarm-worker/internal/service/alarm/dispatchoutbox/queries/repository_insert_0183_02.sql`은 `alarm_dispatch_send_units.unit_key`(`character(64)`)를 `jsonb_to_recordset`이 만든 `text`와 비교한다. 비교가 `(u.unit_key)::text = x.send_unit_key`가 되어 `UNIQUE (unit_key)`를 쓰지 못하고, delivery를 넣을 때마다 send unit 전체를 읽는다. 10만 행에서 39 ms였고 입력을 `character(64)`로 맞추면 0.5~0.7 ms였다.

- [x] 조인을 `u.unit_key = n.send_unit_key::character(64)`로 바꾸거나 recordset 열을 `CHARACTER(64)`로 선언한다. `unit_key`는 CHECK로 64자리 hex만 허용하므로 일치 의미는 같다.
- [x] send unit 수천 행 fixture에서 `force_custom_plan`·`force_generic_plan` 모두 send unit 쪽이 인덱스 조회이고 읽은 행이 입력 수 이하임을 단정한다. 현재 SQL에서 실패하는지 먼저 확인한다.
- 반영: alarm-worker 재배포. migration 없음.

### A2. alarm dispatch 보존 삭제의 상태 파라미터 (A3 포함)

`alarm_dispatch_maintenance_0348_04.sql`은 상태를 `$1`로 받는다. 보존용 인덱스는 상태 상수를 조건으로 둔 부분 인덱스 4개(`idx_alarm_dispatch_deliveries_{sent,cancelled,dlq,quarantined}_retention`)라서 generic plan은 이를 쓸 수 없고, `(status, created_at)` 인덱스로 해당 상태 전체를 읽고 정렬하게 된다. `plan_cache_mode=auto`가 지금은 custom plan을 유지할 가능성이 크지만 보장되지 않는다.

- [x] `alarmDispatchTerminalTimestampColumn`이 상태마다 시각 열을 고정하는 자리에서 상태 상수도 넣도록 바꾸거나, 상태별 SQL 네 개로 나눈다. 바깥 `DELETE`의 상태·시각 재검사는 유지한다(EvalPlanQual 주석의 계약).
- [x] 종료 이력 수만 행 fixture에서 `force_generic_plan`으로 상태별 보존 인덱스 사용과 읽은 행 상한을 단정한다(2026-10-05 `claim_history_plan_test.go`와 같은 방식).
- [x] A3: `notificationdelivery`의 `outbox_repository_0279_09.sql`, `0301_10`, `0330_11`도 Go가 항상 넘기는 상수 상태를 SQL에 직접 쓴다. 테이블은 digest 4종만 담아 작으므로 성능 시험은 추가하지 않는다.
- 반영: alarm-worker 재배포. migration 없음.

### B1. 코드가 쓰지 않는 인덱스 삭제

런타임 SQL 어디에서도 조건·정렬에 쓰지 않는 인덱스 7개다: `idx_ycat_channel_detected`, `idx_ycat_delivery_status`, `idx_ycat_detected_at`(`youtube_content_alarm_tracking`), `idx_ycsas_delivery_status`, `idx_ycsas_detected_at`(`youtube_community_shorts_alarm_states`), `idx_ycssp_channel_detected`(`youtube_community_shorts_source_posts`), `idx_yv_channel_first_seen`(`youtube_videos`). 확인 필요였던 4개(`idx_alarm_dispatch_deliveries_room_created`, `idx_alarm_dispatch_event_collisions_existing_event`, `idx_yls_ended_cleanup`, `idx_ndo_terminal_cleanup`)는 아래 운영 사용량 표에서 판단했다. 근거와 유지 대상 분류는 부록 B1에 있다.

운영 사용량(2026-08-23 이후 누적):

| 인덱스 | 크기 | `idx_scan` | 마지막 사용 | 판단 |
| --- | ---: | ---: | --- | --- |
| `idx_ycat_channel_detected` | 88 kB | 0 | - | 삭제 |
| `idx_ycat_delivery_status` | 56 kB | 0 | - | 삭제 |
| `idx_ycat_detected_at` | 48 kB | 0 | - | 삭제 |
| `idx_ycsas_delivery_status` | 56 kB | 4 | 2026-09-01 | 삭제 |
| `idx_ycsas_detected_at` | 48 kB | 2 | 2026-09-21 | 삭제 |
| `idx_ycssp_channel_detected` | 112 kB | 0 | - | 삭제 |
| `idx_yv_channel_first_seen` | 360 kB | 4 | 2026-09-21 | 삭제 |
| `idx_alarm_dispatch_deliveries_room_created` | 104 kB | 0 | - | 삭제(290). 관리 목록은 상태 조건과 `d.id` 역순으로 읽어 이 순서를 쓰지 않는다 |
| `idx_alarm_dispatch_event_collisions_existing_event` | 16 kB | 405 | 2026-10-11 | 유지 |
| `idx_yls_ended_cleanup` | 432 kB | 10 | 2026-09-26 | 삭제(300). 코드에 이 술어를 쓰는 조회가 없다 |
| `idx_ndo_terminal_cleanup` | 16 kB | 0 | - | 유지. 테이블이 비어 있고 A3 뒤 보존 삭제가 쓴다 |

- 같은 기간 런타임 role(`hololive_runtime`)은 이 테이블들에 수십만~수백만 번 접근했다(`youtube_videos` 54만, `youtube_content_alarm_tracking` 57만, `youtube_community_shorts_source_posts` 49만 회). 2~4회 사용은 `postgres_admin` 수동 진단이나 `hololive_migrator` 문장에서 나온 것으로 판단한다. `idx_ycsas_detected_at`과 `idx_yv_channel_first_seen`의 마지막 사용 시각이 2026-09-21 12:38:27.956으로 같다.
- 대상 테이블의 HOT 비율은 `youtube_content_alarm_tracking` 99.9%(37.9만 건 중), `youtube_community_shorts_source_posts` 99.8%(290만 건 중)라서, 앞에서 추정한 `delivery_status` 인덱스의 HOT 차단은 운영에서 거의 나타나지 않는다.

- [x] 운영 조회로 `idx_scan`, `last_idx_scan`, 크기, 통계 구간을 확인했다(2026-10-11).
- [x] 삭제 대상 인덱스마다 `DROP INDEX CONCURRENTLY` 단일 문장 migration(281부터)을 만들고 manifest와 schema snapshot을 갱신한다.
- B3는 이번 묶음에 넣지 않았다(범위 밖). 묶는다면 같은 묶음에 `(processed_at, observation_id) WHERE status = 'PROCESSED'`, `(dead_lettered_at, observation_id) WHERE status = 'DEAD_LETTER'`를 `CREATE INDEX CONCURRENTLY`로 추가한다. SQL은 바꾸지 않는다.
- 반영: migration 적용. 앱 재배포 불필요.

### B2. `youtube_live_sessions`의 HOT 차단 해소

live 세션 upsert는 더 늦은 관측마다 `last_seen_at`을 갱신한다. `last_seen_at`은 인덱스 4개에 들어 있다. HOT 판정은 인덱스가 참조하는 열의 값 변화로 정해지고 부분 인덱스 조건으로 행을 거르지 않으므로, ENDED 전용 정렬 인덱스의 식(`COALESCE(ended_at, started_at, scheduled_start_time, last_seen_at)`)까지 바꿔야 HOT이 가능하다.

설계:

1. `history_sort_at timestamptz GENERATED ALWAYS AS (COALESCE(ended_at, started_at, scheduled_start_time, last_seen_at)) STORED` 열을 추가한다. `scheduled_start_time`이 있는 세션은 `last_seen_at`이 바뀌어도 이 값이 그대로라서 HOT 대상이 된다.
2. `(history_sort_at DESC, video_id DESC) WHERE status = 'ENDED'`, `(channel_id, history_sort_at DESC, video_id DESC) WHERE status = 'ENDED'`를 새로 만든다.
3. 방송 이력 SQL(`broadcast_history_repository_0179_01.sql`, `0242_02.sql`)의 정렬·커서 식을 `history_sort_at`으로 바꾼다.
4. `idx_yls_channel_last_seen`을 `(channel_id)`로, `idx_yls_status_last_seen`을 `(status)`로 바꾼다. 두 인덱스를 쓰는 런타임 SQL은 `channel_id`·`status` 조건만 쓴다.
5. 앱 반영 뒤 기존 인덱스 4개를 `DROP INDEX CONCURRENTLY`로 지운다(구현에서는 같은 migration 실행에 넣었다. "B1·B2 구현" 절의 배포 조건 참고).

운영 기준값(2026-10-11):

| 지표 | 값 |
| --- | --- |
| 크기 | 9,837행(ENDED 9,604, UPCOMING 212, LIVE 21), 313 페이지, 전체 6.8 MB, `reloptions` 없음(fillfactor 100) |
| 누적(약 49일) | UPDATE 4,754,820건, HOT 291,279건(6.1%), 새 페이지 갱신 133,099건, autovacuum 2,689회(하루 약 55회) |
| 10분 구간(16:50:35~17:01:00 UTC) | UPDATE 1,046건(초당 1.67건), HOT 91건(8.7%), 새 페이지 갱신 44건, autovacuum 1회(직전 16:35, 이번 16:58) |
| upsert 문장 | 세 버전 합계 약 873만 회 호출, 평균 0.09~0.11 ms |
| 인덱스 사용 | PK 1억 회, `idx_yls_channel_last_seen` 995만 회, `idx_yls_status_last_seen` 56만 회, `idx_yls_live_first_seen` 25만 회, `idx_yls_ended_channel_sort_video` 1,022회, `idx_yls_ended_sort_video` 23회, `idx_yls_ended_cleanup` 10회 |
| 정렬값이 `last_seen_at`에 의존하는 행 | 1행(`scheduled_start_time`·`started_at`·`ended_at`이 모두 NULL) |

- 갱신의 90% 이상이 non-HOT이고 행마다 secondary 인덱스 6개를 새로 쓴다. 정렬값이 `last_seen_at`에 의존하는 행은 1행뿐이므로, 설계대로 바꾸면 `last_seen_at`만 바뀌는 갱신은 대부분 인덱스 key가 그대로 남는다.
- 새 페이지 갱신이 4%라 페이지 여유 공간도 부족하다. 열 추가 rewrite 전에 같은 migration에서 `fillfactor`를 90으로 낮춰 rewrite된 페이지에 여유를 둔다. 313 페이지라 공간 증가는 수십 페이지다.
- 절대 비용은 작다(초당 1.7건, 테이블 6.8 MB). 효과는 non-HOT 인덱스 쓰기와 23~26분마다 도는 autovacuum을 줄이는 것이며, 2026-10-05 B2·B4와 같은 방식으로 적용 후 HOT 비율과 autovacuum 간격을 비교한다.

- [x] 운영 기준값을 확인했다. 갱신이 초당 1행 이상이라 진행 조건을 충족한다.
- [x] migration을 나눈다: `fillfactor` 지정과 열 추가(테이블 rewrite와 ACCESS EXCLUSIVE, 313 페이지라 짧지만 upsert가 초당 수 회이므로 `lock_timeout`을 둔다), 새 인덱스 각각 `CREATE INDEX CONCURRENTLY`, 기존 인덱스 삭제(구현에서는 같은 migration 실행에 넣었다. "B1·B2 구현" 절의 배포 조건 참고).
- [x] `idx_yls_ended_cleanup`(10회, 마지막 2026-09-26)을 쓰는 런타임 문장을 찾지 못했다. 같은 단계에서 코드 경로를 다시 확인하고, 런타임 사용이 없으면 함께 지운다.
- [x] 방송 이력 정렬·페이지 커서 결과가 바뀌지 않음을 기존 시험과 열 추가 전후 비교 시험으로 확인한다. 세션 upsert를 반복한 뒤 `pg_stat_user_tables.n_tup_hot_upd`가 늘어나는지 dbtest에서 확인한다.
- 반영: migration(열·인덱스·기존 인덱스 삭제) → api 재배포. 적용 후 HOT 비율과 autovacuum 간격을 다시 잰다.

## 2. 호환 경로 fadeout

### 코드만으로 제거하는 항목

| ID | 대상 | 근거 | 조치 |
| --- | --- | --- | --- |
| F1 | `collecterr`의 전환용 alias(type alias 2개, 상수 22개) | 2026-08-17 오류 어휘를 `contracts/sourceobservation`으로 옮기며 남긴 별칭. 76개 파일에서 840회 사용(테스트 39개 파일 포함) | 호출부를 `contract.*`로 기계 치환하고 alias 삭제. `Error` 타입·분류 함수는 유지. 동작 변화 없음 |
| F2 | `alarmdispatch.Sender = delivery.PreparedMessageSender` | 다른 패키지 타입의 별칭. 사용처 3곳 | 원래 타입 직접 참조 |
| F3 | `dispatchoutbox/repository_payload.go:49-53`의 빈 `AlarmType`을 Live로 간주하는 기본값 | decoder는 같은 누락을 이미 오류로 바꿨다(`domain/alarm.go:295`) | envelope 생성 19곳이 `AlarmType`을 채우는지 확인한 뒤 오류로 바꾸고 시험 추가 |
| F4 | `readiness_test.go:65-67`의 `egress_flags` 부재 단정 | 삭제된 필드의 재등장 가드(설계 문서 G2) | 단정 삭제. `dependencies` 비노출 단정은 유지 |
| F5 | `youtubedispatch/preparation`(operation.go, tracking.go)과 `lifecycle/types.go`의 미연결 부분 | 2026-08-31 계획 T06의 inactive seam. 운영 경로에서 도달하지 않는 함수 약 55개 | `Resolver`·`DeliverySnapshot` 계열만 남기고 삭제 |
| F6 | 관리 API 멤버 인덱스 캐시 | `GetActiveMemberIndex`·`getActiveMemberIndex` 호출 없음. 무효화만 6곳에서 호출 | 캐시·빌더·무효화 호출 삭제 |
| F7 | 템플릿 저장소 구형 쓰기 경로 | 운영은 `UpsertWithRevision`만 쓴다. `Upsert`, `UpsertWithPreviousBody`, `CreateRevision`, `PruneOldRevisions`와 SQL은 테스트 전용 | 시험을 `UpsertWithRevision` 기준으로 옮기고 삭제 |
| F8 | member-news `FilterCandidates` | 운영은 `PrepareCandidates`. 시험에는 기준 구현 `eagerFilterCandidates`가 이미 있다 | 운영 코드에서 삭제, 동등성 시험은 시험 내 기준 구현으로 유지 |
| F9 | LLM `buildLLMSchedulerHTTPServer`(단수), `buildHealthOnlyRouter` | 운영은 복수형 builder | 시험을 운영 builder로 옮기고 삭제 |
| F10 | 운영 코드의 테스트 전용 생성자·wrapper와 완전 미사용 함수 | deadcode v0.51.0: 운영 도달 불가 192개, 시험 포함 미사용 36개 | 묶음별로 호출부를 다시 확인하고 삭제. 예: `NewYouTubeChecker`, `alarm.NewClient`, `NewReplyOutboxRepository`, `sourceobservation.NewConsumer`, `apperrors` 생성자 4개, `releaseDeliveryClaims`, `DrawCenteredText` |
| F11 | `durability/queries/reply_outbox_discard_manual_review.sql` | psql 변수 문법이고 코드 참조 없음. 단 `docs/current/runbooks/hololive-api.md`의 manual_review discard 절차가 직접 실행한다 | 유지(현행 운영 artifact, 진행 기록 참고) |
| F12 | 이름·문구 | `trimLegacyLeading`은 zero-width 정규화다. live snapshot 2세대는 현행 Holodex 계약인데 오류 문구가 "legacy"라고 한다 | 실제 의미의 이름·문구로 변경. `LogAndWrapError` 11곳(설계 문서 L10)은 이 단계에서 같이 정리 |
| F13 | 소스 주석의 퇴역 연대기 | 71개 파일 88줄이 DEC·PLN·stack-audit 번호와 삭제 이력을 인용한다. stack 규칙은 이력을 Git·과거 문서에 두라고 한다 | 현재 계약·이유만 남기고 이력 문장 삭제. 현행 계약 설명은 유지. 원 checkout 미커밋 변경과 겹치므로 그 처리 방향이 정해진 뒤 진행한다(준비 점검) |
| F14 | `docs/current/runbooks/hololive-api.md`의 "iris-client-go v3 전환 전 webhook inbox 드레인" 절 | v3는 2026-09-28 #542로 도입·운영 반영된 일회성 전환 절차 | `docs/history/`로 옮기고 현행 runbook에서 링크만 남기거나 삭제 |

### 승인이 필요한 항목

- **F15. 관리자 비밀번호 재설정 잔재.** 두 route는 2026-10-02 #559부터 항상 503이고 관리 웹은 호출하지 않는다. route 2개, `service_password_reset.go`의 함수 9개, `isPasswordResetRequestRateLimited`, SQL 5개(`service_password_reset_0052_01`, `0070_02`, `0130_03`, `0177_04`, `0189_05`), 테이블 `auth_password_reset_tokens`와 `idx_auth_reset_tokens_user_unused`를 지운다. 코드 삭제와 테이블 DROP migration을 별도 커밋·별도 적용으로 나눈다. 2026-10-11 운영 조회에서 테이블은 0행(24 kB)이었다. DROP 직전에 한 번 더 확인한다.
- **F16. `domain.Stream`의 Twitch·Chzzk 필드 10개.** Go와 관리 웹 어디에서도 읽지 않는다. 그러나 dispatch `payload_hash`가 envelope 전체 JSON의 SHA-256(`repository_payload.go:67`)이고 `chzzk_live_id`, `is_integrated`, `is_chzzk_only`, `is_twitch_only`는 `omitempty`가 없어 항상 직렬화된다. 배포 경계에서 같은 `event_key`가 옛 버전과 새 버전으로 들어오면 hash가 달라 collision(`event_key payload_hash conflict`)으로 기록된다. 진행 조건은 collision 경로가 알림 누락을 만들지 않음을 시험으로 확인하는 것이며, 그렇지 않으면 진행 중 event가 없는 시점에 alarm-worker를 교체한다. `members`의 `twitch_user_id`·`chzzk_channel_id`는 관리 웹 Android 앱이 표시하는 현행 계약이므로 대상이 아니다.

### 운영 데이터 조건이 걸린 항목

조건은 2026-10-11 운영 읽기 전용 조회로 확인했다. 조회 조건은 각 코드 경로의 분기 조건에서 만들었다.

| ID | 대상 | 확인한 조건 | 상태 |
| --- | --- | --- | --- |
| F17 | `dispatchops/model.go:272-279`의 `send_unit_id IS NULL` 분기 | `alarm_dispatch_deliveries.send_unit_id IS NULL` 행 0건 | 종결. 308건(`sent` 294, `cancelled` 14)은 보존 삭제(2026-12-20 완료 예정)를 기다리지 않고 migration 301로 지웠다("F17 종결" 절) |
| F18 | `dispatchoutbox.ErrLegacySendRequest`(D2)와 runner의 분기 | `request_body_hash IS NULL`인 send unit에 `sending_started_at`이 있는 진행 중 delivery 0건이고, 새 비고정 시도가 생기지 않음 | 충족. 코드 단계로 옮긴다 |
| F19 | `youtubedispatch/store/send_request.go`의 구형 request 증거 경로(D3) | `youtube_notification_delivery.send_request_id IS NULL` 0건 | 충족. 코드 단계로 옮긴다 |
| F20 | `notificationdelivery/request.go:43-50`의 legacy 격리(D1, #607 뒤 위치) | `notification_delivery_outbox` 구형 행(request 없이 시도) 0건 | 충족(테이블 0행). 코드 단계로 옮긴다 |

F18~F20은 삭제할 때 각 분기를 직접 시험하던 테스트도 함께 지운다. 같은 상황을 막는 대체 가드는 추가하지 않는다. 남은 비고정 send unit 564개는 delivery 보존 기간이 지나면 함께 지워진다.

### 유지하기로 확인한 항목

- `RejectStandardOTLPEndpointEnv`: OpenTelemetry SDK가 표준 endpoint env를 자동으로 다른 형식으로 해석하는 것을 막는 현행 계약이다. 주석의 DEC 인용만 F13에서 정리한다.
- live snapshot 2·3세대 decoder: 두 세대 모두 현행 provider가 만든다.
- `bot_webhook_inbox` scrub trigger: migration 230이 이미 지웠다.
- `OriginLegacyUnknown`: 저장된 lifecycle 어휘이며 남은 9건의 종료는 계약 개정이 필요하다.

## 3. 단계와 의존성

1. **코드 단계(운영 접근 없음):** A1, A2(A3 포함), F1~F10, F12~F14, F18~F20을 영역별 커밋으로 진행한다. F11은 현행 runbook artifact라 제외한다. F5~F10 뒤 deadcode를 다시 돌려 새로 도달 불가가 된 함수가 남지 않았는지 확인한다.
2. **운영 읽기 전용 조회:** 2026-10-11에 완료했다. F17은 재조회 대신 migration 301로 종결했다.
3. **migration·계약 단계(승인 필요):** B1 삭제(필요하면 B3 추가), B2 열·인덱스, F15 코드 삭제와 테이블 DROP. F16은 진행 조건 확인 뒤 결정한다.
4. **배포와 사후 측정(hololive-bot-ops):**
   - alarm-worker: A1, A2, A3, F2, F3, F5, F10 일부, F16, F18, F19, F20.
   - api: B2 SQL, F6, F7, F9, F11, F15, F17.
   - collector: F1. collector 바이너리가 바뀌면 AP 포함 4대를 배포해야 하므로 다음 collector 릴리스에 묶는다.
   - B2는 열 추가 migration을 api 재배포보다 먼저 적용한다. 기존 인덱스 삭제는 같은 실행에 넣었다("B1·B2 구현" 절의 배포 조건). B1은 앱과 무관하게 적용할 수 있다.
   - 적용 후 A1 문장의 평균 시간, B1 대상 테이블 쓰기 비용, B2 HOT 비율·autovacuum 간격을 적용 전 값과 비교한다.

## 4. 검증

- 변경 모듈의 Go 테스트와 PostgreSQL 테스트를 `-race`로 실행한다. alarm-worker store 통합 테스트는 `-tags=integration`을 포함한다.
- A1·A2·B2 계획 시험은 수정 전 SQL에서 실패하는지 먼저 확인한다.
- migration 단계는 `scripts/architecture/check-migration-manifest.sh`와 `SCHEMA_SNAPSHOT_UPDATE=1 go test -run TestSchemaSnapshotGolden ./hololive/hololive-dbtest`로 확인한다.
- 변경 패키지의 golangci-lint, staticcheck, NilAway를 실행하고 `./build-all.sh --build-only --no-bump`로 런타임을 빌드한다.
- Git 게시 때 `scripts/ci/pre-push-gate.sh`를 실행한다.

## 부록: 조사 근거

### 조사 방법

- 작업 전용 임시 컨테이너(`postgres:18.6-alpine`, dbtest와 같은 이미지)에 `hololive-dbtest.ApplyMigrations`로 manifest의 migration 141개를 적용했다. migration·testdata·testqueries·init-db를 뺀 SQL 421개에 `enable_seqscan=off`, `EXPLAIN (GENERIC_PLAN)`을 실행해 392개의 계획을 얻었다. 나머지 29개는 조각 SQL 15개, `fmt` 템플릿 5개, `{}` 템플릿 2개, psql 변수 2개, migration ledger 전용 5개다.
- 빈 테이블의 계획은 인덱스 사용 가능성만 보여 준다. 실제 선택은 합성 데이터로 확인했다. 트랜잭션 안에서 `session_replication_role=replica`로 FK 트리거를 끄고 넣은 뒤 되돌렸고, 시간은 같은 조건 3회의 범위다. kapu 로컬의 따뜻한 캐시 기준이므로 상대 비교로만 쓴다.
- 카탈로그로 FK 인덱스, 어떤 계획에서도 선택되지 않은 인덱스, 갱신 열을 포함한 인덱스를 찾았다. 퇴역·미사용 코드는 deadcode v0.51.0(운영 main 기준, 시험 포함 기준 두 번), 설계 문서 4절과 `retired-guards.tsv` 대조, 호환 표지 검색으로 찾았다. 조사 뒤 임시 컨테이너와 임시 프로그램은 지웠다.

### 측정

| 대상 | 조건 | 결과 |
| --- | --- | --- |
| A1 현재 SQL | send unit 10만 행, 입력 10건 | custom 39.0~39.5 ms, generic 38.6~39.0 ms, seq scan |
| A1 `::character(64)` | 같음 | custom 0.53~0.67 ms, generic 0.63~0.68 ms, unique 인덱스 |
| B3 현재 SQL | PROCESSED 23.2만 행, 대상 321행 | custom 25.3~33.3 ms(4,318 buffer), generic 59.0~68.8 ms(5,426 buffer) |
| B3 부분 인덱스 2개 추가 | 같음, SQL 그대로 | 두 plan 모두 0.62~1.27 ms(660 buffer) |
| `repository_target_snapshot_exact_0144_15.sql` | 세대 200개 × target 1,085개 | 0.09 ms PK 상관 조회. 빈 테이블의 hashed SubPlan은 통계 부재 착시 |

### B1 분류

계획에서 선택되지 않은 인덱스(PK·제약 제외) 35개를 다음처럼 나눴다.

| 분류 | 인덱스 | 판단 |
| --- | --- | --- |
| 코드 술어 없음 | B1의 7개 | 삭제 후보 |
| FK 참조 쪽 | `source_observation_collisions`, `youtube_channel_photo_variants`, `youtube_channel_profile_evidence`, `youtube_content_absence_slots`, `youtube_content_evidence_clocks`, `youtube_live_viewer_sample_evidence`의 `observation_id` 계열 | `source_observations` 보존 삭제의 `SET NULL`·`CASCADE`에 필요 |
| 계획을 뽑지 않은 경로에서 사용 | alarm dispatch 보존 4개, `idx_youtube_collection_projection_retired_retention`, `idx_source_observations_payload_id`, `idx_ydt_outbox`, `idx_ndo_kind_content`, `idx_members_slug` | 유지 |
| 작은 테이블 | `members` 5개, `idx_alarms_alarm_types_gin`, `idx_major_events_start_date`, `idx_x_space_login_attempts_started_at`, `idx_bot_reply_outbox_replay_audit_outbox_recorded` | 효과 없음 |
| 확인 필요 | B1의 4개 | 2026-10-11 운영 사용량으로 판단(본문 B1 표) |

`youtube_content_alarm_tracking`은 `(kind, content_id)`나 PK `(kind, canonical_content_id)`로만 찾는다. 관리 telemetry 두 쿼리의 `COALESCE(actual_published_at, detected_at)` 조건은 `detected_at` 인덱스로 처리할 수 없다. `ycsas`는 PK와 `idx_ycsas_kind_content`로만 찾고, `ycssp`는 INSERT만 있으며, `youtube_videos`는 `video_id`나 `(channel_id, is_short)`로 찾는다.

### 개선 대상이 아닌 항목

- FK 인덱스 누락 5건: 4건은 거의 바뀌지 않는 `observation_contract_generations` 참조이고, 1건은 heads PK와 `idx_youtube_live_reconciliation_end_candidate`가 참조 확인을 처리한다.
- 남은 seq scan: `members`, `acl_rooms`, `notification_templates`, `major_events`, `major_event_subscriptions`, `message_strings`, `alarms`의 작은 테이블 조회와 heads 집계다.
- 상태를 파라미터로 받는 SQL 33개 중 A2·A3 외에는 id·PK로 행을 고르거나 상태 선두 일반 인덱스를 쓴다.

### 운영 확인 조회 (2026-10-11 실행)

모두 `transaction_read_only=on`을 먼저 확인한 세션에서 집계·카탈로그만 읽었다. 결과는 본문 "운영 측정 결과"에 있다.

1. A1: `alarm_dispatch_send_units`·`alarm_dispatch_deliveries`의 행 수·크기·생성일 범위, `pg_stat_statements`에서 `JOIN alarm_dispatch_send_units u`를 포함한 delivery INSERT 문장의 `calls`, `mean_exec_time`, `max_exec_time`, 호출당 buffer.
2. B1: 대상 인덱스 11개의 `idx_scan`, `last_idx_scan`, `pg_relation_size`와, 대상 테이블 문장의 role별 `pg_stat_statements` 집계.
3. B2: `youtube_live_sessions` 등 4개 테이블의 `n_tup_upd`, `n_tup_hot_upd`, `n_tup_newpage_upd`, `n_dead_tup`, `autovacuum_count`를 10분 간격으로 두 번, 세션 상태별 행 수, 정렬값이 `last_seen_at`에 의존하는 행 수, upsert 문장 통계.
4. A2: `DELETE FROM alarm_dispatch_deliveries d`를 포함한 보존 삭제 문장의 통계.
5. F15·F17~F20: 본문 표의 조건별 행 수 집계.

## 한계

- 운영 측정은 2026-10-11 한 시점의 누적 통계와 10분 구간 차이다. 누적값은 통계가 유지된 약 49일 구간 기준이다.
- F17은 보존 삭제를 기다리지 않고 migration 301로 legacy 행을 지워 종결했다. 그 행의 발송 기록은 복원하지 않는다.
- 변경 검증 결과는 각 진행 기록 절에 있다.
- 조각·템플릿 SQL 29개는 조립된 최종 문장으로 계획을 보지 않았다. A2는 템플릿을 직접 읽어 판단했다.
- deadcode는 reflection·문자열 등록 경로를 놓칠 수 있으므로 F10은 삭제 전에 호출부를 다시 확인한다.

Fallback delta: 새 재시도·대체 경로를 추가하지 않는다. F3는 숨은 기본값을 오류로 드러내는 변경이다.
