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

## 게시와 운영 배포 기록

- 통합 브랜치 `5c195b50c`에서 `./build-all.sh --build-only --no-bump`(10분 38초)와 pre-push 게이트를 통과했고, PR #590의
  CI 11개 항목이 모두 통과했다. squash 병합 커밋 `2ed1a8b63982516cd2aaf9d8d870db843618a2ff`의 트리는 `5c195b50c`와 같다.
- 병합 커밋의 clean worktree에서 `kapu-multiarch`로 arm64 이미지를 빌드했다. `hololive-api` 7.2.5
  `sha256:ebe3045272976cdda87fb5b95ec993d218b2fc3c10e66709648ba43984076384`, `hololive-alarm-worker` 6.1.2
  `sha256:cc7f3ef40835ac74b7fa3ce44a322b12382434bb2bfa857e431a3257cf682d7c`이며 두 이미지의 revision label은 병합 커밋이다.
  `hololive-seoul`에 `docker load`한 뒤 ID·arch·revision을 다시 확인하고 `:prod`로 승격했다. 원격 빌드는 하지 않았다.
- 롤백 기준점(change_started_at 2026-10-07T20:10:52Z): 이전 이미지 `hololive-api:rollback-20261007T201052Z`
  (`b35ca2aaf5fb`, 7.2.3, `4a633ab24`)와 `hololive-alarm-worker:rollback-20261007T201052Z`(`471a7f38702b`, 6.1.1),
  배포 트리 사본 `/opt/hololive-bot/compose/deploy-backups/pre-v7.2.5-20261007T201052Z/`(data·logs·backups 제외).
  이전 이미지는 삭제된 열·테이블을 읽거나 쓰지 않으므로 앱만 되돌릴 수 있다. DB 변경은 되돌릴 수 없다.
- 배포 트리에서는 VERSION 파일 세 개만 바꿨다. migration은 이미지에 내장된 `migrations.FS`를 쓰며, compose가 mount하는
  배포 트리의 migrations 디렉터리는 비어 있다.
- 적용 직전 확인(20:11 UTC): 272 대상 인덱스 `idx_scan`이 29분 동안 5813으로 변하지 않았고, `dispatched_at` 값과 운영 전용
  두 테이블의 행은 0, ledger는 131개, 1분 넘게 열린 트랜잭션은 없었다.
- `hololive-db-migrate`는 20:11:25–20:11:29 UTC에 `applied=8 skipped=131 total=139`로 끝났다. 곧바로
  `hololive-alarm-worker`(20:11:38)와 `hololive-api`(20:11:49)를 `up -d --no-build --no-deps`로 차례로 재생성했고 둘 다
  health gate를 통과했다(재시작 0). 재생성 뒤 두 서비스와 collector `c` 로그에 ERROR·WARN·cached plan 오류는 없었다.
- DB 확인: 새 세션 `idle_in_transaction_session_timeout=5min`, ledger 139(마지막 278), `members` 시각 열 삭제·이름 열
  varchar(200)·NOT NULL, `chk_members_aliases_shape`·`acl_settings_key_key` 검증 완료, 운영 전용 제약·테이블·인덱스
  삭제, 두 retention 함수 `jit=off`, heads 배열 `attstattarget=0`.
- 효과(배포 뒤 약 6분, 읽기 전용):
  - `youtube_live_pending_ends` 갱신이 초당 약 24행에서 3.6행으로 줄었다(306초에 1,100행).
  - heads autoanalyze 1회가 24 ms였다(이전 평균 384 ms). 새 heads `FOR UPDATE` 문장은 512회 평균 3.33 ms였다(이전 9.4–18 ms).
  - LIVE 세션이 30개에서 13개로 줄었다. 영상 확인 판정은 `ENDED` 17건이며, 종료 시각은 마지막 positive보다 29초–3분 16초
    이르고 10시간–7일 전이다. 배포 뒤 `youtube_notification_outbox` 행은 0건이다.
  - 남은 `INVALID_END_TIMELINE` 3건은 모두 LIVE positive clock이 없는 UPCOMING 세션(시작 미관측 경로의 의도된 거부)이다.
  - 남은 LIVE 13개 중 4개는 positive clock이 있으나 6시간 넘게 갱신되지 않았으며 YouTube identity 미확인이다. 9개는
    head 없는 `legacy_unknown` 기록으로 시작한 지 126–136일 지났고 이번 변경 전부터 있었다.

## v7.2.6: 재측정으로 추가한 개선

v7.2.5 배포 뒤 23:02–23:32 UTC 구간 재측정에서 네 개선이 모두 확인됐다. retention 내부 문장 JIT 0(평균 543→77 ms,
184→4.5 ms), pending 갱신 24→3.9행/s, heads `FOR UPDATE` 평균 17.4→3.3 ms, heads autoanalyze 384→22 ms, 앱 DB CPU
0.087→0.059코어, API DB 수신 2.29→0.40 MB/s. 그 위에서 남은 비용 두 가지를 줄인다.

- live 적재는 종료 후보가 없는 저장된 `ENDED` 세션의 pending을 읽거나 잠그지 않는다. 그 행은 어떤 판정 경로도 읽지
  않고 세션이 dirty가 되지 않아 삭제 keep-list와 무관하므로, 이전에 적어 둔 "keep-list 의미 변경 필요" 전제는 성립하지
  않았다. 운영에서 pending `FOR UPDATE`가 초당 약 25행, 값이 같은 upsert가 호출의 84%였다.
- migration 279는 검토 영수증의 의미 사실을 기록 시 저장하고 판정 함수가 저장값과 비교한다. `live_check_videos.sql`
  27.3 ms 중 24.5 ms가 영수증 쪽 재계산이었고, 시험 DB(영수증 53건)에서 중앙값 24.5→4.0 ms, 판정 53/53 동일이었다.

### v7.2.6 게시와 운영 배포 기록

- 통합 브랜치 `e9f01bd26`에서 `./build-all.sh --build-only --no-bump`(11분 2초)와 pre-push 게이트를 통과했고, PR #592의
  CI 11개 항목이 모두 통과했다. squash 병합 커밋은 `67d4ab9d92c89b39d6368f0bff3f7251ae212558`이다.
- 병합 커밋의 clean worktree에서 arm64 `hololive-api` 7.2.6
  `sha256:ccacfc0303d7627bb458cf3196c4265d6a10ad4da217704fc58726fd7697d34a`를 빌드해 `hololive-seoul`에 적재하고 ID·arch·
  revision을 다시 확인한 뒤 `:prod`로 승격했다. alarm-worker(6.1.2)와 collector는 바꾸지 않았다.
- 롤백 기준점(change_started_at 2026-10-08T02:38:56Z): `hololive-api:rollback-20261008T023856Z`(`ebe304527297`, 7.2.5)와
  배포 트리 사본 `/opt/hololive-bot/compose/deploy-backups/pre-v7.2.6-20261008T023856Z/`. 279는 추가형이고 판정 함수
  시그니처가 같아 앱만 7.2.5로 되돌릴 수 있다.
- `hololive-db-migrate`는 02:39:08–02:39:11 UTC에 `applied=1 skipped=139 total=140`으로 끝났고, `hololive-api`는 02:39:22에
  재생성되어 health gate를 통과했다(재시작 0, 이후 10분 ERROR·WARN 0).
- DB 확인: ledger 140(마지막 279), `youtube_live_review_receipt_facts` 53행 = 영수증 53건, 면제 영상 53개로 배포 전과 같다.
  runtime은 새 표 SELECT만 가능하고 INSERT 불가, scraper는 조회 불가다.
- 효과(배포 뒤 10분 구간 02:39:46–02:50:01 UTC, 읽기 전용): pending upsert 호출 25.1→3.8회/s(갱신 행 3.8/s와 같아져 값이
  같은 no-op이 사라짐), pending `FOR UPDATE` 적재 평균 0.318→0.056 ms, `live_check_videos.sql` 평균 25.7→8.5 ms,
  heads `FOR UPDATE` 3.3 ms 유지.

## v7.2.7: `!라이브` 고착 원인 추적과 판정 개정

2026-10-08 14:17 KST 운영 스냅샷(roster 74채널)은 covered 69, stale 3채널(LIVE 세션 4건), confirming_end 1채널,
incomplete 1채널(방송 중)이었다. 방송 중인 멤버가 없어지면 전체 `!라이브`가 "현재 방송 상태를 확인할 수 없습니다."로
답하는 상태였고, 두 가지 원인을 운영 데이터와 코드로 확인했다.

- **비공개·삭제 영상의 LIVE 세션 4건:** YouTube 익명 조회에서 3건은 `LOGIN_REQUIRED`(비공개 동영상), 1건은 `ERROR`
  (재생 불가)였고, player 응답에 `videoDetails`와 `isPrivate`가 없어 영상 확인은 2분마다 `identity_missing` UNKNOWN만
  남겼다. 계약이 정한 유일한 면제 경로 `PUBLIC_UNAVAILABLE(player_private)`는 익명 수집에서 도달하지 않는다(계약 v2의
  "별도 실측 대상"이 이 결과다). 부재 종료(`SCOPED_ABSENCE`)도 불가능하다. 채널별 youtubejs 스냅샷은 `ENDED`까지 scope에
  넣어 첫 페이지에 continuation이 남으면 PARTIAL이라 최근 1시간 기준 COMPLETE 채널은 8개뿐이었고(PARTIAL 102채널),
  Holodex 스냅샷은 전부 PARTIAL이었다. 최근 30일 `SCOPED_ABSENCE` 종료 69건은 모두 그 소수 채널에서 나왔고, 고착 3채널은
  24시간 동안 부재 slot이 하나도 없었다. 즉 대부분 채널에서 종료 전에 비공개로 바뀐 방송은 어떤 경로로도 끝나지 않는다.
- **UPCOMING 세션의 `EXPLICIT_END` 보류 1건:** 해당 영상은 8-20에 1초간 송출된 뒤 끝난 공개 VOD였다. 공급자 positive
  (UPCOMING, 수집 예정 시각이 EffectiveAt)가 실제 `ended_at`보다 1분 늦었고, 시작 미관측 경로의 신선도 없는 `ended_at`
  직접 비교가 9-14부터 매 확인마다 `INVALID_END_TIMELINE`으로 거부해 보류가 영구히 남았다. 검토 영수증은 UPCOMING 전용이고
  `snapshot.sql`은 영수증을 읽지 않으므로, 영수증을 기록해도 pending 기반 `confirming_end`는 풀리지 않는다(영수증 53건의
  영상에는 pending 행이 없다).

적용한 변경은 다음과 같다.

- `live.TerminalEndBlockedByPositive`가 시작 미관측 종료의 positive 판정을 소유한다. `ended_at` 이후의 positive는
  관측(seen) 시각 + grace(운영 2분)가 지나지 않은 동안만 종료를 막고, 그 뒤에는 공급자 지연으로 보고 upstream `ended_at`으로
  끝낸다. consumer와 reducer가 같은 술어를 쓴다. 관측 시각 이후 positive(`NEWER_END_RETAINED`)와 더 새로운 pending 거부는
  그대로다. 시작·positive clock·알림은 만들지 않는다.
- `snapshot.sql`의 stale LIVE 제외 근거에 신선한 `identity_missing` UNKNOWN(identity 미확인)을 추가했다. 기존
  `PUBLIC_UNAVAILABLE`과 같은 시각 경계(`scheduled/effective/observed/received`가 영상 확인 validity 안, 마지막 positive
  이후, 신선한 positive 없음)를 적용하며, `identity_mismatch`·`request_failed` 등 다른 UNKNOWN 사유와 만료는 계속 차단한다.
  로봇 확인이 player를 막아도 채널 `/live` 확인이 음성이면 채널은 방송 중이 아니므로 결과는 바뀌지 않고, 채널 확인이
  UNKNOWN이면 coverage가 없어 여전히 "확인할 수 없습니다"다.
- 계약 v2(시작 미관측 종료 규칙, `player_private` 실측, LiveQuery 제외 근거), services/hololive-api.md를 갱신했다.

바꾸지 않은 것: 비공개·삭제 영상의 LIVE 세션은 수명 상태가 LIVE로 남고 영상 확인도 2분 cadence로 계속된다(LIVE 4건과
UPCOMING 85건이 `identity_missing`). 채널 스냅샷 scope와 검토 영수증 범위도 그대로다.

### v7.2.7 게시와 운영 배포 기록

- 작업 브랜치 `c648d1e55`에서 `./build-all.sh --build-only --no-bump`(LOCAL CI 47단계)와 pre-push 게이트를 통과했고, PR #594의
  CI 11개 항목이 모두 통과했다. squash 병합 커밋은 `d0472782478a41031d07c7a2a720ebfd5f25611b`이다.
- 병합 커밋의 clean worktree에서 arm64 `hololive-api` 7.2.7
  `sha256:bcd350e8da4ddfebf9402d91c1a9f063cad0224d74318533f0ecd209418c5b91`를 빌드해 `hololive-seoul`에 적재하고 ID·arch·
  revision을 다시 확인한 뒤 `:prod`로 승격했다. alarm-worker(6.1.2)와 collector, DB migration은 바꾸지 않았다.
- 롤백 기준점(change_started_at 2026-10-08T06:14:48Z): `hololive-api:rollback-20261008T061448Z`(`ccacfc0303d7`, 7.2.6)와
  배포 트리 사본 `/opt/hololive-bot/compose/deploy-backups/pre-v7.2.7-20261008T061448Z/`. DB 변경이 없으므로 앱만 되돌리면 된다.
  되돌리면 `!라이브`는 다시 stale 3채널 때문에 빈 결과를 확정하지 못하고, 이미 `ENDED`로 정리된 세션은 그대로 남는다.
- `hololive-db-migrate`는 06:15:08–06:15:11 UTC에 `applied=0 skipped=140 total=140`이었고, `hololive-api`는 06:15:22에
  재생성되어 health gate를 통과했다(재시작 0, 06:20까지 ERROR·WARN 0, 알림 outbox 0).
- 효과(읽기 전용 스냅샷 SQL): 배포 직전 06:14:31 UTC covered 67·stale 3·confirming_end 1·incomplete 3(방송 중 3채널) →
  배포 직후 06:15:47 stale 0(covered 70) → 다음 영상 확인(06:19:01 수신)이 보류 중이던 UPCOMING 세션을 06:19:03에 upstream
  종료 시각(2026-08-20 11:01:09 UTC)으로 `ENDED` 처리하고 pending 행을 지워 06:19:48 covered 71·incomplete 3이 되었다.
  incomplete 3채널은 모두 현재 방송 중이라 목록으로 표시되므로, 방송이 없으면 전체 `!라이브`가 빈 결과를 확정한다.
  이전에 stale이던 3채널의 멤버 지정 조회는 covered·0건이다.

## v7.2.8: 비공개·삭제 영상 LIVE 세션의 해소 불가 종료

v7.2.7은 `!라이브` 판정만 보호했고, 종료 전에 비공개·삭제로 바뀐 방송의 LIVE 세션 4건은 LIVE로 남아 영상 확인을
약 4분 cadence로 영구히 받았다. 비공개 전환이 생길 때마다 같은 세션이 쌓이고 `!라이브` 보호가 폴링 유지에 의존하는
구조라, 세션을 끝내는 종료 근거를 API 측에 추가했다. 2026-10-08 운영 확인 기준 roster 안 LIVE `identity_missing` 4건,
채널 `/live` identity 확인 음성(5분 내) 113채널이었다.

- **종료 근거(`UNRESOLVABLE_VIDEO`):** 시작을 관측한 LIVE 세션의 영상 확인이 `identity_missing`이면 head의
  `unresolvable_since`에 첫 관측 시각을 둔다. 첫 관측부터 `YOUTUBE_PLANE_LIVE_UNRESOLVABLE_GRACE_SECONDS`(기본 600초)
  이상 `identity_missing`이 이어지고, 같은 채널의 최신 `youtube_channel_live_checks`가 identity 확인된
  `CHANNEL_PAGE`·`UPCOMING_VIDEO`이며 마지막 LIVE positive 이후·영상 확인 예정 시각 ±5분 안이고, 관측 이후 positive가
  없으며 마지막 LIVE positive seen_at + 종료 grace가 지났을 때만 끝낸다. ended_at은 첫 `identity_missing` 시각(실제
  종료의 하한), started_at·positive clock 유지, 알림·pending·absence slot 없음. positive는 추적을 지운다.
- **대상 밖:** UPCOMING 세션(비공개 예약이 공개로 돌아오면 시작 알림이 필요하다), `identity_mismatch`·`request_failed`
  등 다른 UNKNOWN 사유, 채널 확인을 reducer로 보내는 변경. 두 신호가 동시에 거짓 양성이 되려면 로봇 확인이 player를
  막으면서 같은 채널의 `/live`는 음성을 내고 Holodex·채널 스냅샷 positive도 없어야 하는데, 방송 중인 채널의 `/live`는
  방송으로 이동해 음성을 내지 않는다.
- **구현:** migration 280(`unresolvable_since` 열, `end_reason` 어휘 CHECK 개명·확장, NOT VALID 뒤 VALIDATE. 같은 트랜잭션
  안이라 ACCESS EXCLUSIVE가 VALIDATE 스캔까지 유지되므로 잠금 완화 효과는 없으나 3,962행이라 영향이 없었다(v7.2.10 정정)), head
  적재·저장 SQL 열 추가, `live.StatusUnresolvable` 사실과 `applyUnresolvableVideo` 전이, consumer의
  `unresolvableVideoFact`(채널 음성 조회 `repository_channel_live_negative.sql`), 설정값·검증, 계약 v2·services 문서,
  CHANGELOG, VERSION 7.2.8. v7.2.7의 LiveQuery `identity_missing` 제외는 종료 전 브리지로 유지한다.

### v7.2.8 게시와 운영 배포 기록

- 작업 브랜치 `e4ce6758a`에서 `./build-all.sh --build-only --no-bump`(LOCAL CI Passed, 이미지 빌드 완료)와 pre-push
  게이트를 통과했고, PR #596의 CI 항목이 모두 통과했다. squash 병합 커밋은 `5d617748b20ab3047fa637f59d6a4e6680e47ea2`다.
- 병합 커밋의 clean worktree에서 arm64 `hololive-api` 7.2.8
  `sha256:3439e070a9fedb605bc23925de75ed156495f5f2e872c9145e16c52d1cc01c00`를 빌드해 `hololive-seoul`에 적재하고 ID·arch·
  revision을 다시 확인한 뒤 `:prod`로 승격했다. alarm-worker(6.1.2)와 collector는 바꾸지 않았다.
- 롤백 기준점(change_started_at 2026-10-08T08:18:44Z): `hololive-api:rollback-20261008T081844Z`(`bcd350e8da4d`, 7.2.7)와
  배포 트리 사본 `/opt/hololive-bot/compose/deploy-backups/pre-v7.2.8-20261008T081844Z/`. migration 280은 nullable 열
  추가와 CHECK 어휘 확장뿐이라 앱만 되돌려도 7.2.7 코드가 그대로 동작한다(되돌리면 새 비공개 전환 세션은 다시 LIVE로
  남고, 이미 `UNRESOLVABLE_VIDEO`로 끝난 세션은 그대로 유지된다). v7.2.10 검토 정정: 7.2.7은 positive에서
  `unresolvable_since`를 지우지 않으므로 롤백 기간의 positive 뒤에도 추적값이 남는다. 7.2.8·7.2.9로 재전진하면 그 잔여값으로
  지속 시간 없이 끝날 수 있으므로, 7.2.7로 되돌린 뒤에는 positive보다 이른 추적값을 무시하는 7.2.10 이상으로 재전진한다.
- `hololive-db-migrate`는 08:19:24 UTC에 `280_live_head_unresolvable_end.sql`을 적용했고(`applied=1 skipped=140
  total=141`, 제약 `chk_youtube_live_reconciliation_heads_end_reason_vocab` validated), `hololive-api`는 08:19:30에
  재생성되어 약 5초 만에 health gate를 통과했다(재시작 0, 08:40까지 ERROR·WARN 0, 알림 outbox 0).
- 효과(읽기 전용 집계): 배포 직후 첫 영상 확인이 좀비 LIVE 4건을 08:20:41–08:21:23 UTC에 `UNRESOLVABLE_TRACKED`로
  추적했고, 10분 grace 동안 `UNRESOLVABLE_RETAINED` 16건이 쌓인 뒤 08:30:43–08:31:28에 4건 모두 `UNRESOLVABLE_VIDEO`로
  `ENDED`가 되었다(추적→종료 10분 2–4초, ended_at = 첫 `identity_missing` 시각 4/4, started_at 보존 4/4, session·head
  ended_at 일치 4/4, pending 0). 영상 확인 대상은 45에서 40으로 줄었고 LIVE 세션 14건 중 `identity_missing`은 0건,
  추적 중인 LIVE 0건이다. 08:40 스냅샷 SQL은 roster 74채널 모두 projection·collected 정상이며 covered 71·incomplete 3
  (방송 중)·stale 0이다.

## v7.2.9: identity를 확인할 수 없는 UPCOMING 영상 확인의 재확인 주기 backoff

v7.2.8 뒤 UPCOMING 세션 가운데 가용성 최신값이 `identity_missing`인 것이 85건(예정 시각 1.5일 뒤~256일 전),
`identity_mismatch`인 것이 4건(194~258일 전)이었다. 그중 영상 확인 대상(지난 일정이고 검토 영수증이 없는 UPCOMING)은
35건(전체 대상 40건 중)이며, 설정 2분·실효 약 4분 주기로 같은 UNKNOWN만 반복했다. 2026-10-08 18:10 KST 기준 24시간
`IDENTITY_UNCONFIRMED`는 16,527건·57개 영상이었다(v7.2.5 기록 시점의 같은 지표는 17,349건). UPCOMING은 비공개 예약이
공개로 돌아오면 시작 알림이 필요해 끝낼 수 없고 정리는 검토 영수증이 맡으므로, 재확인 주기만 추적 기간에 비례해 늦춘다.

- **추적:** consumer는 UPCOMING의 `identity_missing`(IDENTITY_UNCONFIRMED)과 채널 불일치(IDENTITY_MISMATCH) 확인을 해소
  불가 사실로 바꾸고, reducer는 `unresolvable_since`를 두거나 유지만 한다(`UNRESOLVABLE_TRACKED`/`UNRESOLVABLE_RETAINED`).
  UPCOMING은 VerifiedTerminal이 와도 끝내지 않으며 positive는 추적을 지운다. LIVE 경로는 v7.2.8 그대로다.
- **backoff:** `live_check_videos.sql`이 UPCOMING 행에 `unresolvable_since`를 실어 주고(LIVE는 NULL), projection이 추적
  기간에 따라 UPCOMING 영상 확인 target의 `poll_interval_ms`를 기본 주기의 1·5·15·30배(2·10·30·60분, 경계 10분·1시간·24시간)
  로 정한다. cadence 변경은 membership을 새로 시작하므로 단계를 세 번으로 제한했다. 우선순위·NotBefore·LIVE 주기는 그대로다.
- **효과 추정:** 대상 35건이 실효 약 4분 주기로 하루 약 12,600회(35×360) 확인되던 것이, 모두 24시간 넘게 추적되면
  1시간 주기로 하루 840회(35×24)로 줄고, 관측·payload·application 행도 같은 비율로 줄어든다. 시작 감지는 채널 스냅샷
  positive가 맡으므로 알림 지연은 없다.

### v7.2.9 게시와 운영 배포 기록

- 작업 브랜치 `17cf709b4`에서 `./build-all.sh --build-only --no-bump`(LOCAL CI Passed, 이미지 빌드 완료)와 pre-push
  게이트를 통과했고, PR #598의 CI 항목이 모두 통과했다. squash 병합 커밋은 `9bfa66740a7145ba74c51f0f188fed0d1651fe4c`다.
- 병합 커밋의 clean worktree에서 arm64 `hololive-api` 7.2.9
  `sha256:53d752776ed75df773bc6a2e3bc7981b2a806aa3bedee35707febf53761dbf7a`를 빌드해 `hololive-seoul`에 적재하고 ID·arch·
  revision을 다시 확인한 뒤 `:prod`로 승격했다. DB migration은 없고(`applied=0 skipped=141`), alarm-worker(6.1.2)와
  collector는 바꾸지 않았다.
- 롤백 기준점(change_started_at 2026-10-08T09:23:51Z): `hololive-api:rollback-20261008T092351Z`(`3439e070a9fe`, 7.2.8)와
  배포 트리 사본 `/opt/hololive-bot/compose/deploy-backups/pre-v7.2.9-20261008T092351Z/`. 되돌리면 `unresolvable_since`
  열은 7.2.8 코드도 그대로 쓰며 UPCOMING target 주기만 2분으로 돌아온다.
- `hololive-api`는 09:24:45 UTC에 재생성되어 약 5초 만에 health gate를 통과했다(재시작 0, 09:45까지 ERROR·WARN 0, 알림
  outbox 0).
- 효과(읽기 전용 집계): 배포 뒤 첫 영상 확인이 09:27–09:29 UTC에 UPCOMING 대상 35건 전부를 `UNRESOLVABLE_TRACKED`로
  추적했고, 추적 10분 뒤인 09:35–09:38에 projection이 35건 모두의 target 주기를 2분에서 10분으로 바꿨다(LIVE 대상 8건은
  2분 유지). 09:45 기준 lease도 10분 주기로 옮겨 가는 중이었다(16건). 30분 주기는 10:26–10:28에 35건 모두 적용됐고,
  1시간 주기는 추적 24시간 뒤에 적용된다. UPCOMING 영상 확인 결정(`IDENTITY_UNCONFIRMED`·`IDENTITY_MISMATCH`·
  `UNRESOLVABLE_*`)은 배포 전 08:20–09:20 UTC 분당 9.08회에서 10:30–11:15 UTC 분당 1.16회로 줄었다(v7.2.10 검토 때 재측정.
  처음 기록한 "배포 전 약 20회"는 LIVE 확인까지 섞은 잘못된 비교라 정정한다). `!라이브` 스냅샷은 roster 74채널 모두
  projection·collected 정상, covered 69·incomplete 5(방송 중)·stale 0이다.
- 같은 날 GitHub Release [v7.2.9](https://github.com/park285/hololive-bot/releases/tag/v7.2.9)를 annotated tag
  `v7.2.9`(= `9bfa66740`)와 직전 게시 릴리즈 `v7.2.1` 기준의 GitHub 생성 노트(PR #561–#598)로 게시했다. v7.2.2~7.2.8은
  중간 태그 없이 이 릴리즈 노트에 포함된다.
- 서울 호스트의 롤백 자료는 최신 기준점만 남겼다: 이미지 태그 `prod`, `prod-arm64-9bfa6674`, `rollback-20261008T092351Z`와
  배포 트리 사본 `pre-v7.2.8-*`, `pre-v7.2.9-*`. 10-07~10-08의 rollback 태그 4개, prod-arm64 태그 4개, 배포 사본 3개는
  삭제했다(모두 커밋 SHA로 재빌드 가능).

## v7.2.10: v7.2.8·v7.2.9 검토 후속

v7.2.9 배포 뒤 7개 차원 리뷰와 지적별 3인 반박 검증으로 확정한 10건(모두 low)을 해소했다. 운영 동작을 당장 틀리게 만든 결함은
없었고, 다음 네 가지는 조건이 갖춰지면 실제로 나타나는 설계 공백이었다.

- **주기 단축 시 IDLE lease 재개 지연(collector):** projection은 positive가 추적을 지우면 target 주기를 다음 refresh에서 2분으로
  되돌리지만, collector 후보·획득 술어가 IDLE lease의 `next_due_at`(직전 획득 때의 긴 주기로 계산)만 봐서 최대 60분 동안 재확인이
  없었다. 늦춰진 예약이 공개 방송으로 바뀐 뒤 채널 스냅샷이 PARTIAL이면 종료 확인이 그만큼 늦어진다. 후보 두 SQL과 획득 SQL이
  실효 due `LEAST(next_due_at, scheduled_for + 현재 target 주기)`를 쓰고, 획득의 `date_bin` 기준점도 같은 값을 쓴다. 기준점이 직전
  slot보다 늦으므로 `scheduled_for`는 단조 증가하며, 주기가 같거나 늘면 동작은 그대로다. lease 의미는 collector가 소유하므로
  API 측 우회 대신 collector를 고치고 fleet 4대에 배포한다.
- **이른 positive의 추적 초기화:** `mergePositiveFields`가 관측 시각과 무관하게 `unresolvable_since`를 지워, 추적 시작보다 이른
  관측이 늦게 도착하면(시작 미확정 LIVE 포함) 추적이 초기화되고 종료 하한이 밀렸다. 추적 시작 이후 관측만 지운다.
- **7.2.7 롤백 잔여값:** 7.2.7은 positive에서 추적을 지우지 않으므로 롤백 뒤 재전진하면 지속 시간 없이 끝날 수 있었다. 추적은
  모든 positive보다 늦게 시작되므로, reducer·consumer(`ActiveUnresolvableSince`)와 projection SQL이 positive보다 이른 값을 추적
  없음으로 보고 reducer는 다시 추적한다.
- **head 없는 UPCOMING:** head 저장 조건(`HeadPresent || observed`) 때문에 `metadata_only`·`legacy_unknown` UPCOMING은 추적을
  저장하지 못한 채 `UNRESOLVABLE_TRACKED`만 반복 기록했다. 미확정 메타데이터는 authoritative head의 근거가 아니라는 기존 설계를
  유지해 이런 세션은 추적하지 않고 기존 결정 코드를 남긴다(`live.UnresolvableTrackable`). 2026-10-08 운영에는 지난 일정의 head 없는
  UPCOMING이 `legacy_unknown` 3건뿐이고 모두 영상 확인 대상 밖이었다.
- **시험·문서:** 관측 이후 positive 보존(`NEWER_POSITIVE_RETAINED`), 채널 `/live` 음성 술어의 부정 사례(identity 미확인 UNKNOWN·
  다른 영상 LIVE), 잔여 추적값 재시작, head 없는 UPCOMING, 주기 단축·유지 시 lease 재개 시험을 더했다. 각 수정을 되돌리면 해당
  시험이 실패함을 확인했다. 계약 v2 251행의 "해석 불가 UNKNOWN은 Reduce를 호출하지 않는다"를 예외와 맞췄고, migration 규약에
  같은 트랜잭션 안 VALIDATE는 ACCESS EXCLUSIVE를 줄이지 못한다는 점을 적었다(280은 3,962행이라 영향 없음).

## 남은 후보 (이번 범위 밖)

- head 없는 `legacy_unknown` LIVE 9건(126–136일): 운영 roster 밖 5채널이라 LiveQuery·알림·영상 확인 대상·지표 밖에
  있고 사용자 영향은 없다. 영상 확인 대상이 roster로 한정되어 종료를 증명할 기회가 없다. 대상 확장은 계약 개정이다.
- 검토 영수증 없이 수백 일 지난 UPCOMING `identity_missing`·`identity_mismatch` 세션(2026-10-08 기준 89건)의 정리:
  v7.2.9는 재확인 주기만 늦춘다. 세션 자체를 닫는 것은 검토 영수증 흐름(운영자 판단)이 맡는다.
- collector 1초 주기 후보 탐색과 항상 충돌하는 lease INSERT: v7.2.5 뒤 앱 DB CPU의 37%(절대량 약 0.02코어)다. 후보
  질의 buffer의 84%가 468행 `youtube_collection_job_leases`의 흩어진 heap(107페이지) seq scan이다. runner별 next-due 힌트는
  collector 계약·traffic 시험 개정과 AP 포함 4대 배포가 필요하고, UPDATE 우선 lease 획득은 다음 collector 릴리스에 묶는다.
- 보존 기간이 지난 `ignored_absence_scheduled_for` 원소 정리, bot durable inbox/outbox의 개별 유휴 폴링, 은퇴
  projection 세대 보존 기간, `source_observation_queue` retention용 정렬 인덱스, 알림 전달 전이 지표의 빈 sweep 집계.
- finalizer·video live check에서 비`ENDED` head의 배열까지 생략(설계 문서의 2b). 두 경로도 `ENDED` 배열은 이미
  생략하며, 나머지는 단일 영상 조회라 이득이 작아 제외했다.
- 시청자 표본(`youtube_live_viewer_samples`, 2026-10-07 리뷰 측정 약 109만 행·150MiB)의 보존·폐기 결정과 큰
  인덱스의 `REINDEX CONCURRENTLY` 효과 검증.

Fallback delta: 새 재시도·대체 경로를 추가하지 않는다. 미적재 이력과 수렴 전제 위반은 오류로 드러낸다.
