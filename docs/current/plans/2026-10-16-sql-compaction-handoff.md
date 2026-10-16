# SQL 구조 압축(317–321) 세션 인계 — 2026-10-16

**상태: 전체 변경의 로컬 main 합류 대상으로 인수, 원격 미발행·운영 미적용.** 초기 작성 세션의 기록과 통합 인수 보완을 구분합니다. 측정 근거·설계 판단은 [10월 15일 인계 문서](2026-10-15-sql-db-review-handoff.md), 인수 과정에서 직접 실행한 검증은 아래 7절에 기록합니다.

## 1. 최초 인계 시점의 작업 트리 (`main`, HEAD `21e60d245`)

아래는 압축 작업을 넘겨받기 전의 기록입니다. 사용자는 이후 317–321을 포함한 전체 변경을 함께 인수·검증하여 로컬 main에 합류하도록 선택했습니다. 원격 발행·운영 적용 승인은 아닙니다.

| 묶음 | 상태 | 내용 |
|---|---|---|
| 10-15 작업 | **스테이징됨** (168파일) | 필수 1–5·N1–N7 수정, migration 315(retention bounded scan)·316(live refresh 필터), 문서. `git diff --cached`로 확인 |
| 10-16 작업(이 세션) | **미스테이징·untracked** (33파일 수정 + 6파일 신규) | 아래 2절. `git diff`와 `git ls-files --others --exclude-standard`로 확인 |
| 인수자의 임시 probe | 소유 확인·제거 완료 | `hololive/hololive-api/cmd/compaction-integration-probe/main.go`는 통합 인수자가 만든 316 baseline bootstrap입니다. 실제 CLI smoke 뒤 제거했으며 커밋하지 않습니다 |
| 통합 보완 | 합류 대상 포함 | notification cleanup의 `count(id)`와 `scripts/deploy/ap-rsync-files.txt` 누락 2개 항목을 포함합니다 |

스테이징 묶음은 이 세션 시작 시 변경 범위 `local-ci.sh`를 통과했습니다. 이후 이 세션의 변경을 포함한 변경 범위 `local-ci.sh`도 통과했습니다(5절).

## 2. 이 세션이 바꾼 것

### migration (모두 `hololive/hololive-api/scripts/migrations/`, manifest 178–182번)

| 파일 | 역할 | 적용 조건 |
|---|---|---|
| `317_source_observation_provenance_compaction.sql` | `source_observation_provenances(id integer, job_key, collector_instance, collection_job_kind)` 사전 표 + `source_observation_provenance_id()` resolver. 원본 표의 text 3열을 `provenance_id` FK로 전환 뒤 `observation_key`를 `bytea(32)`로 TYPE 변경(전체 재작성). lock API 2개·보호 함수 교체 | 점검 창. 원본 표 약 1GB 재작성·index 재빌드용 heap/WAL 공간 필요. ACCESS EXCLUSIVE를 COMMIT까지 보유 |
| `318_source_application_vocab_codes.sql` | `source_observation_entity_kinds`·`source_observation_decisions` 사전, 코드 열 준비·5,000행 단위 commit backfill·FK·NOT NULL 검증 | 점검 창. 후속 batch 실패 시 완료분을 보존하고 파일 재실행으로 재개 |
| `319_source_application_entity_index.sql` | `CREATE UNIQUE INDEX CONCURRENTLY (observation_id, entity_kind_id, entity_key)` | 단일 문장 |
| `320_source_application_active_index_drop.sql` | 구 `uq_source_observation_application_active` `DROP INDEX CONCURRENTLY` | 단일 문장 |
| `321_source_application_vocab_cutover.sql` | 감사 표 text 열 제거, bounds CHECK 교체, 보호 함수 최종본(live session 판정을 코드 조인으로) | 점검 창. 뒤이어 `VACUUM (FULL, ANALYZE) source_observation_applications` 필요 |

317·318은 준비 단계 뒤 keyset batch마다 commit하며 기존 코드 값은 다시 쓰지 않습니다. 원본 text 열은 backfill·FK 검증 뒤 원자적 bytea cutover에서 제거됩니다. 비-hex 실패는 그 cutover를 롤백하므로 기존 text 열과 완료 backfill을 보존합니다. top-level DO를 외부 transaction으로 감싸지 않으며 문장·CLI 시간 제한은 유지합니다.

### 런타임 SQL (Go 소스 변경 없음)

- `hololive-youtube-collector/.../sourceobservation/queries/repository_publish_set_0032_32.sql`: distinct 출처 3-tuple마다 resolver 1회, `decode(observation_key,'hex')` 저장, RETURNING은 `encode`.
- `hololive-api/internal/youtube/sourceobservation/queries/repository_claim_lock_0013_13.sql`: 사전 표 조인으로 기존 열 이름·순서 유지, observation_key는 `encode`.
- 같은 디렉터리 `repository_application_insert_0014_14.sql`(resolver + 코드 index ON CONFLICT), `repository_community_window_ready_0086_86.sql`(이름→코드 subselect).
- `hololive-youtube-collector/.../collectorruntime/testqueries/seed_observation_queue.sql`: sha256 키·resolver.

### 테스트·golden·문서

- 최종 스키마 pool을 쓰는 fixture 20개 파일을 sha256 키·resolver·사전 조인으로 갱신(API sourceobservation 12, API youtube runtime 1, collector 2, worker 1, dbtest 4). migration 시점 pool을 쓰는 테스트는 구 스키마 그대로 둡니다.
- 새 `hololive/hololive-dbtest/source_provenance_vocab_migration_test.go` 3건: 317 왕복·보호·lock API hex 계약·비-hex 롤백/재실행, 318–321 어휘 왕복·index 교체·중복 거부·CASCADE.
- `hololive-dbtest/migrations_p1_test.go`: 권한 매트릭스에 사전 표 3개 추가, post-baseline 목록에 317–321 추가.
- `hololive-dbtest/testdata/schema_snapshot.golden.sql` 재생성.
- `CHANGELOG.md` 미출시 2항목, `migrations/CONVENTIONS.md` 스키마 표준 ④(사전 코드화 선례), 10-15 인계 문서 마지막 두 절.

## 3. 결정 근거 요약 (운영 읽기 2026-10-15 15:26–15:34 UTC, guard `on`)

- DB 2,362MB, 하루 약 300MB 증가. 원본 DELETE 0건으로 retention 정체(P1)가 지속됨. 운영 ledger는 314까지.
- 보호 prefix 탐색과 동기 refresh 비용이 관측됐지만 중첩 함수 시간은 상위 문장에 포함됩니다. 315는 후보 순회, 316은 기술적 UPDATE만 개선하므로 전체 executor 비용의 일정 비율이 사라진다고 확정하지 않습니다. 적용 후 삭제·FK/WAL·의미 변경의 잔여 비용을 따로 측정합니다.
- 원본 표 heap 621MB(행 456B)·index 431MB, identity index 밀도 62%·fragmentation 45%는 최초 인계의 관측값입니다. 출처 문자열·64자 키가 큰 비중을 차지하며, sha256 hex 형식은 표본에서 확인됐습니다. migration은 전체 변환과 32바이트 제약 검증으로 부적합 데이터를 거절합니다.
- 사용자는 구조 압축 3·4·5 전부를 선택했습니다. 후보별 기존 추정(180+86+120+94+70MB)의 단순 합계는 550MB이지만, 키 폭·재작성·retention 효과의 중첩을 제거한 실현 절감치가 아닙니다. 보존 정책 7일→3일은 선택하지 않았습니다.

## 4. 다음 단계와 승인 경계

1. **로컬 main 합류(승인됨).** 기존 스테이징 묶음과 317–321, 통합 보완을 함께 리뷰·검증하여 커밋합니다. 설정된 사용자 identity를 사용하고 AI 귀속 trailer를 넣지 않습니다.
2. **원격 발행(별도 승인).** `scripts/ci/pre-push-gate.sh`와 당시 branch protection을 확인하고 정해진 PR/main 경로를 따릅니다. 이 인수에는 push·PR 생성이 포함되지 않습니다.
3. **운영 적용(별도 승인, `hololive-bot-ops`·`stack-platform-ops`).** 순서: 점검 전 DB 백업·복구점 확인 → API·worker·collector a/b/c/d 중지 → 신규 이미지의 `db-migrate`로 315–321 적용(317의 재작성 시간·WAL, 318의 backfill 시간 감안) → `VACUUM (FULL, ANALYZE) source_observation_applications` → 신규 API/worker/collector 기동 → health·PROCESSED handoff·lock wait 확인. image-only rollback은 신규 schema를 되돌리지 못합니다.
4. **적용 후 재측정.** 테이블·index 크기, 보호 조회·기술적 UPDATE refresh·잔여 의미 변경 비용, 실제 retention 진행, 사전 표 행 수를 확인합니다. 초기 인계 수치를 새 조회 결과처럼 재사용하지 않습니다.

운영 DDL·데이터 변경·배포·재시작·Git 발행·원격 쓰기는 이 문서로 승인되지 않습니다.

## 5. 초기 작성 세션의 검증 기록

| 검사 | 결과 |
|---|---|
| `bash scripts/architecture/check-migration-manifest.sh` | OK |
| `bash scripts/architecture/check-sql-ownership.sh`, 스택 `tools/checks/check-stack-db-access-policy.sh` | 통과 |
| `go build` API·collector·worker | 통과 |
| `go test ./hololive/hololive-dbtest`(전체, 82초) | 통과 |
| `go test` API `internal/youtube/sourceobservation`, `internal/planes/youtube/runtime` | 통과 |
| `go test` collector `runtime/sourceobservation`, `runtime/collectorruntime`; worker `dispatchoutbox` | 통과 |
| `SCHEMA_SNAPSHOT_UPDATE=1 go test -run TestSchemaSnapshotGolden ./hololive/hololive-dbtest` | golden 재생성·통과 |
| `env -u TEST_DATABASE_URL GOEXPERIMENT=jsonv2 LOCAL_CI_GO_SCOPE=changed BASE_REF=HEAD HEAD_REF=HEAD bash scripts/ci/local-ci.sh` | 통과(Go 8패키지, lint 0 issues, NilAway, race/non-race). integration-only 미실행 |

실행하지 않은 것: `RUN_INTEGRATION_TESTS=true scripts/ci/local-ci.sh --integration-tests-only`, `./build-all.sh --build-only --no-bump`, pre-push gate, 운영 적용 후 재측정.

## 6. 알려진 한계

- resolver 함수는 REPEATABLE READ 이상에서 사전 행 동시 추가 패자에게 SQLSTATE 40001을 내며 호출자의 기존 재시도에 맡깁니다. 새 retry/fallback은 추가하지 않았습니다.
- 사전 표는 자동 정리하지 않습니다. collector 재기동마다 `collector_instance` 조합이 늘 수 있으나 수 MB 규모로 추정합니다.
- `source_observation_collisions`·`source_reconciliation_conflicts`의 text 열은 소형이라 그대로입니다.
- 이 세션이 만든 Claude 문서 초안(SQL·DB 리뷰 후속 작업 인계, 1절만 채움)은 사용자가 Markdown 인계로 방향을 바꿔 중단했습니다. 참고용이며 정본은 저장소의 이 문서와 10-15 문서입니다.

**Fallback delta: none. Retry delta: none.**

## 7. 전체 합류 인수의 추가 검증

- 초기 backfill은 원본·감사 표 전체를 한 transaction으로 갱신해 대형 backfill 규약을 위반했습니다. 후반 행 실패를 주입한 두 회귀에서 준비 열까지 롤백되는 것을 먼저 확인한 뒤 5,000행 단위 commit·keyset 재개로 바꿨습니다. `source_compaction_resume_test.go`와 기존 압축 회귀·schema golden이 통과했습니다.
- 소유 PostgreSQL 18.6에서 실제 production `migrationrunner`로 316 baseline을 만들고, 실제 `cmd/db-migrate`로 upgrade·실패 복구를 실행했습니다. 원본 10,003행의 마지막 batch 실패 시 10,000행 확정/3행 미완료, 감사 10,002행에서는 10,000행 확정/2행 미완료였습니다. 결함을 제거하고 CLI를 재실행하자 321까지 완료됐으며 표현 변환을 역정규화한 **원래 모든 열의 JSONB 값이 정확히 일치**했습니다. 재진입은 applied=0/skipped=182입니다.
- 실제 application INSERT SQL을 `hololive_runtime`으로 비어 있지 않은 입력에 실행해 최초 1행·중복 0행과 기존 decision 보존을 확인했습니다. scraper resolver의 ID 공유와 두 역할의 사전 직접 INSERT 거절(`42501`)도 확인했습니다.
- 운영 관측·운영 규모의 재작성 시간·WAL 용량은 인수 과정에서 재측정하지 않았습니다. 작은 tmpfs fixture의 결과를 운영 용량·소요 시간 보장으로 사용하지 않습니다.
- 317·318 batch commit의 독립 안전성 리뷰는 차단 결함 없이 수락했습니다. 리뷰 결론, PG 18.6 timeout probe, 실패 뒤 재실행·timeout 관련 운영 주의 사항은 [main 합류 인계](2026-10-16-main-integration-handoff.md) 3-5절에 있습니다. 로컬 합류 커밋 `948ab90e5`는 PR #641 squash로 원격 main `848fb2792`에 발행했고, 2026-10-16 운영에 적용했습니다(그 문서 6·7절).
