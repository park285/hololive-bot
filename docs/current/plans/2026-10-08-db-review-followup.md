# DB 리뷰 후속: 스키마 수렴, live 상태 적재 축소와 운영 메트릭 개선

2026-10-07 DB 리뷰에서 권고한 항목 중 [PostgreSQL 계측 확장](2026-10-07-postgres-extensions.md)만 운영에
반영됐다. 남은 구조 개선·기술부채 항목을 구현하고, 운영 메트릭을 다시 측정해 찾은 개선을 더해 v7.2.5로 릴리스한다.
2026-10-08 사용자 요청으로 커밋·게시·운영 배포(migration 271–278 적용 포함)까지 진행한다. 운영 측정은 읽기 전용
가드를 적용한 카탈로그·통계·집계 조회로 한정했다.

## 브랜치

통합 브랜치 `perf/db-review-followup-20261008`은 로컬 main `9bbe24939`(미게시 계측 확장 커밋 7개 포함) 위에 아래
커밋을 차례로 둔다. 각 변경은 별도 작업트리에서 구현·검토한 뒤 가져왔다.

| 커밋 | 내용 |
|---|---|
| 게이트 복구 | 계측 확장 통합 뒤 실패하던 아키텍처 게이트 두 건 |
| 스키마 수렴 | migration 271–276, 운영 형상 업그레이드 시험, members writer 정리 |
| live 적재 축소 | live snapshot 적재 범위 축소, 무시 부재 이력의 미적재 타입, 벤치마크 복구 |
| pending 재기록 중단 | 저장된 `ENDED` 세션의 반복 종료·취소를 pending으로 다시 기록하지 않음 |
| retention·통계 | migration 277(retention 함수 `jit = off`), 278(heads 배열 통계 끄기) |
| LIVE 고착 수정 | 지연 positive 때문에 검증된 영상 종료를 거부하던 consumer 비교 범위 축소 |
| 릴리스 | VERSION 7.2.5(API 7.2.5, alarm-worker 6.1.2), CHANGELOG, 이 문서 |

게이트 복구: 계측 확장 준비 커밋 `3c2b20305`(main 통합은 `20ed0842f`)가 허용 위치 밖에 실험용 SQL을 추가했고,
PostgreSQL Dockerfile의 multi-stage `AS` stage를 런타임 계약 검사가 인식하지 못했다. 실험 SQL은
`scripts/experiments/postgres-extensions/testqueries/`로 옮기고 와일드카드 조회를 명시 열로 바꿨다. 검사는 postgres
이미지를 이름으로 참조하는 모든 `FROM`(`--platform`·registry 접두사·대소문자 포함)이 같은 digest 고정 18.6 이상
이미지일 때만 통과한다. ARG로 이미지를 고르는 `FROM`은 해석하지 않는다.

## 스키마 수렴 (migration 271–276)

운영 base 테이블은 레거시 애플리케이션이 먼저 만들어 epoch-1의 `CREATE TABLE IF NOT EXISTS`가 적용되지 않았고,
fresh 재생과 모양이 달랐다. 서울 운영 카탈로그를 golden 직렬화와 같은 방식으로 비교한 결과 의미 있는 차이는
15건이었다. 공통 인덱스·트리거·사용자 함수는 같았고, 운영에만 있는 인덱스 두 개(`idx_acl_settings_key`,
`ux_alarm_dispatch_outbox_dedupe`)는 위 차이에 속한다. CHECK 15건은 괄호·캐스트 표기만 달라 플래너 기준으로 같았다.
GRANT/ACL은 fresh에 운영 role이 없어 비교하지 않았다. 운영 데이터 중 fresh 정의를 위반하는 값은 없었다.

| 파일 | 내용 |
|---|---|
| 271 | `idle_in_transaction_session_timeout=5min` DB 기본값 재선언. 183은 적용 기록이 있으나 운영에 값이 없다. |
| 272 | 백필 전용 `idx_source_application_live_origin` 삭제(`CONCURRENTLY` 단일 문장) |
| 273 | `acl_settings`: `key` NOT NULL·`acl_settings_key_key` UNIQUE 제약, fresh의 `id`·시퀀스를 운영과 같은 bigint로 확대 |
| 274 | `members`: 이름 열 varchar(200), `is_graduated`·`aliases` NOT NULL, `chk_members_aliases_shape`, 운영 전용 CHECK 2개와 `created_at`·`updated_at` 삭제 |
| 275 | fresh `notification_delivery_outbox.attempt_count` bigint, 운영 전용 `youtube_notification_outbox` CHECK·`dispatched_at` 삭제, template revision FK |
| 276 | 운영 전용 빈 테이블 `streams`·`alarm_dispatch_outbox` 삭제 |

- 각 파일은 현재 카탈로그를 확인한 뒤 바꾸며, 같은 이름의 객체가 다른 정의로 있으면 중단한다. NOT NULL은
  CONVENTIONS의 NOT VALID CHECK 순서를 따르고, 행 검증이 실패하면 열·제약을 지우기 전에 멈춘다.
- 삭제 대상에 값이나 행이 생겼으면(`dispatched_at`, 두 테이블) 지우지 않고 실패한다. 고아 revision은 지우지 않고
  FK 검증이 실패한다.
- 코드: `CreateMember`는 별명이 없으면 빈 ko·ja 별명을 저장한다. 제약으로 불필요해진 `COALESCE`와 int4 열 전용
  `math.MaxInt32` 가드를 제거했다.
- 설정 소유자는 183과 같은 DB 수준으로 유지했다. compose `-c`로 옮기는 대안은 운영 PostgreSQL 재시작이 필요하다.

### 운영 적용 전 확인할 사항

- 274는 운영 `members` 135행의 `created_at`·`updated_at` 값을 지운다. 마지막 전체 DB 복원 검증은 2026-10-07이고 그
  뒤 백업은 중단됐다([DEPLOYMENT_BASELINE](../DEPLOYMENT_BASELINE.md)). 2026-10-07 19:12 UTC 읽기 전용 세션에서
  `id, slug, created_at, updated_at` 135행을 build-control host의
  `~/.local/share/hololive-db-backup/manual/20261007T191238Z-members-timestamps-pre-274/`에 CSV와 sha256으로 보관했다.
  두 열을 쓰는 trigger·view·함수와 운영 문장은 없었다(`pg_stat_statements`, 형제 저장소 검색).
- 272 대상 인덱스의 누적 `idx_scan`은 5813이고 마지막 스캔(2026-10-06 07:35:59 UTC)의 주체는 확인하지 못했다. 코드에는
  이 부분 술어를 쓰는 조회가 없다. 적용 직전에 일정 구간의 `idx_scan` 증가분이 0인지 다시 확인한다.
- 274의 이름 열 typmod 변경 뒤 `cache_statement` 연결에서 이름 열을 반환하는 문장이 한 번씩
  `cached plan must not change result type`(0A000)로 실패한다. db-migrate 직후 `hololive-api`와
  `hololive-alarm-worker`를 순차 재기동한다. `compose-redeploy-service.sh hololive-api`도 전체 migration을 먼저
  실행하므로 alarm-worker 재기동을 따로 한다.
- 271은 새 세션부터 적용되므로 애플리케이션 pool을 순차 재기동한 뒤 새 세션의 `SHOW`로 확인한다. 논리 복원의
  DB 수준 설정 확인 절차는 [postgres-replication runbook](../runbooks/postgres-replication.md)에 추가했다.
- 운영 drift는 이 migration으로 의미상 수렴한다. 적용 뒤 운영 카탈로그를 golden 직렬화로 다시 비교하면 물리 열
  순서(`major_events`·`members`·`youtube_notification_outbox`·`youtube_videos`)와 CHECK 15건의 표기 차이는 남는다.
  열 순서는 테이블을 다시 쓰지 않는 한 바뀌지 않는다.

## Live 상태 적재 축소

2026-10-08 운영 읽기 전용 측정(통계 기준 2026-09-06 이후): heads `FOR UPDATE` 조회가 평균 9.43ms, 275만 회,
누적 약 7.2시간으로 측정한 live 상태 조회 중 비용이 가장 컸다. 배열 열이 없던 이전 문장은 평균 0.17ms였다.
`ignored_absence_scheduled_for` 원소 1,578만 개 중 84.1%가 `ENDED` 세션에 있고, 채널당 `ENDED` 세션 중앙값은
77개, 비`ENDED`는 2개다. 종료 후보가 남은 `ENDED` 세션은 0건이었다.

- 채널 범위 세션 조회는 payload 영상, `UPCOMING`·`LIVE`, 종료 후보(`next_end_check_at`)가 남은 `ENDED`만 잠근다.
  payload 밖 `ENDED`가 결정에 주는 영향은 종료 후보 정리 하나뿐이어서 결정은 같다.
- session 행이 있는 `ENDED` 세션은 heads 조회에서 배열을 읽지 않는다. `IgnoredAbsenceHistory`의 0값은 "미적재"이며,
  reducer가 미적재 이력을 읽어야 하면 결정 없이 오류를 반환한다. 저장 시 미적재는 SQL NULL로 보내 기존 배열과
  TOAST 값을 유지하고, 적재된 빈 이력은 기존처럼 배열을 지운다.
- `BenchmarkLiveIgnoredAbsenceHistory`는 metadata-only 세션 fixture 때문에 실패했다. 초기 세션을 `Reduce`로 만든다.

## 운영 메트릭 기반 추가 개선

2026-10-07 18:34–19:04 UTC 운영 읽기 전용 구간 측정(`pg_stat_statements`·`pg_stat_kcache`·`pg_wait_sampling` 스냅숏
차이, `pg_stat_user_tables`, Prometheus·Loki)에서 호스트 여유는 충분했다(2 vCPU 중 약 0.45코어, 앱 질의 CPU 약
0.09코어, iowait 0.2%). 그 안에서 비용이 크거나 결함인 항목을 고쳤다.

- **pending 재기록 중단:** `youtube_live_pending_ends`는 초당 약 24행이 HOT 없이 갱신됐고 84%가 이미 `ENDED`인 세션의
  행이었다. 저장된 `ENDED` 세션은 positive로 되살아나지 않고 모든 판정 경로가 그 pending을 읽지 않으므로, session 행이
  있는 `ENDED` 세션의 반복 종료·취소는 다시 보관하지 않는다. 기존 D2 진단 행은 동결되고, session 행이 없거나 head만
  남은 영상은 그대로 보관한다. due가 아닌 종료 후보가 남은 `ENDED` 세션에서 head 후보 FK 위반(23503)으로 관측 처리가
  실패하던 경로도 함께 사라진다.
- **277 retention 함수 JIT 끄기:** 두 retention 함수의 내부 문장은 `generate_subscripts` 행 추정과 정책별 LIMIT이
  곱해져 추정 비용이 수백만이 되고, 누적 실행 시간의 61–77%(최근 구간 85–96%)가 JIT 컴파일이었다. 시험 DB에서 함수
  내부 실행이 939.7/131.8 ms에서 624.7/13.9 ms로 줄었고 계획 노드와 비용은 같았다.
- **278 heads 배열 통계 끄기:** heads autoanalyze가 평균 384 ms(26,174회)였다. 술어에 쓰이지 않는
  `ignored_absence_scheduled_for` 배열을 매번 detoast했기 때문이다.
- **LIVE 고착 수정:** LIVE 세션 22개 중 21개가 6시간–7일 동안 positive 없이 LIVE였다(17개 채널). video live check는
  identity·채널을 확인한 종료 시각을 보고했지만 마지막 positive(20건이 Holodex `live_snapshot`)가 그보다 0.5–3.3분
  늦어, consumer가 하루 9,475번 `INVALID_END_TIMELINE`으로 거부했다. Holodex positive의 EffectiveAt은 수집 예정
  시각이다. `ended_at`과 positive의 직접 비교는 grace 없이 끝내는 시작 미관측 terminal 경로에만 두고, LIVE positive
  clock이 있는 세션은 schema 1과 snapshot 종료처럼 reducer의 일반 명시적 종료 계약(관측 시각 기준 positive 비교와
  grace)을 따른다. 종료 경로는 알림 outbox·egress를 만들지 않는다. 고착 세션은 배포 뒤 다음 영상 확인에서 실제 종료
  시각으로 끝나며, 저장된 `ended_at`이 `last_live_positive_at`보다 이를 수 있다.

## 검증

- 스키마 수렴: 270까지 재생한 DB에 운영 drift 15건을 재현한 뒤(열 순서·CHECK 표기 차이는 재현하지 않음) 271–276을 적용하면
  fresh 재생과 직렬화가 같고 행 값·xmin·relfilenode가 유지되며 재적용은 no-op이다. 잘못된 행·고아 revision·값이 있는
  `dispatched_at`·행이 있는 운영 전용 테이블에서 각각 멈추는 것도 실제 PostgreSQL로 확인했다.
- Live 상태: 전체 적재와 축소 적재의 결정 동일성, 미적재 이력의 오류 반환과 저장 시 보존을 단위·PostgreSQL 시험으로
  확인했다.
- pending 재기록 중단: ENDED 세션의 종료·취소 표 시험, 세션 없는 영상·head만 남은 영상 대조군, 기존 pending 행의
  xmin 유지와 FK 회귀를 단위·PostgreSQL 시험으로 확인했다. 가드를 되돌린 변이에서 새 시험이 실패한다.
- 277·278: 기존 retention 보호·한도 시험, 실제 manifest 재생, golden(두 함수 CONFIG의 `jit=off`만 변경)을 통과했다.
- LIVE 고착 수정: 지연 positive 뒤 검증된 종료(LIVE·UPCOMING positive), grace 안의 END_CANDIDATE와 finalizer 정산,
  확인 시각 positive의 보존, 시작 미관측 경로의 거부를 PostgreSQL 시험으로 확인했다. 각 핵심 비교를 지운 변이에서
  해당 시험이 실패한다.
- 각 작업트리에서 대상 패키지 `go test`(race 포함), golangci-lint, migration manifest·SQL 소유권 검사를 통과했다.
  게이트 복구·스키마 수렴·live 적재 축소 브랜치는 `./build-all.sh --build-only --no-bump`(아키텍처 게이트·staticcheck·
  golangci-lint·NilAway·race·이미지 빌드)를 통과했다. 통합 브랜치의 전체 게이트와 게시 게이트 결과는 배포 기록에 적는다.

## 남은 후보 (이번 범위 밖)

- `youtube_live_pending_ends`: 저장된 `ENDED` 세션의 pending `FOR UPDATE` 적재와 값이 같은 upsert 호출(초당 약 17–21건)은
  남는다. 저장 경로의 삭제 keep-list 의미를 바꿔야 하므로 이번 변경의 효과를 측정한 뒤 정한다.
- `video_live_check`의 `IDENTITY_UNCONFIRMED`: 24시간 17,349건(57개 영상, 대부분 예정 시각이 7일 넘게 지난 UPCOMING)이
  반복된다. backoff 상한과 검토 영수증 흐름과의 관계를 설계해야 한다.
- `live_check_videos.sql`(5초 주기)의 검토 영수증 semantic facts 재계산: DB CPU 약 6%이고 projection guard 잠금 대기의
  원인이다. 새 테이블·trigger·backfill과 review 함수 재작성이 필요하다.
- collector 1초 주기 후보 탐색과 항상 충돌하는 lease INSERT: DB 질의 CPU 약 23%이나 절대량은 약 0.02코어다.
- 보존 기간이 지난 `ignored_absence_scheduled_for` 원소 정리, bot durable inbox/outbox의 개별 유휴 폴링, 은퇴
  projection 세대 보존 기간, `source_observation_queue` retention용 정렬 인덱스, 알림 전달 전이 지표의 빈 sweep 집계.
- finalizer·video live check에서 비`ENDED` head의 배열까지 생략(설계 문서의 2b). 두 경로도 `ENDED` 배열은 이미
  생략하며, 나머지는 단일 영상 조회라 이득이 작아 제외했다.
- 시청자 표본(`youtube_live_viewer_samples`, 2026-10-07 리뷰 측정 약 109만 행·150MiB)의 보존·폐기 결정과 큰
  인덱스의 `REINDEX CONCURRENTLY` 효과 검증.

Fallback delta: 새 재시도·대체 경로를 추가하지 않는다. 미적재 이력과 수렴 전제 위반은 오류로 드러낸다.
