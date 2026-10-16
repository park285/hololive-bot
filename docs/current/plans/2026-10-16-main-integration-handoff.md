# 전체 변경 main 합류 마무리 인계 - 2026-10-16

**상태: 통합·전체 gate·독립 migration 리뷰를 마치고 PR(squash)로 원격 `main`에 발행합니다(6절). 운영 미적용.** 첫 세션은 OAI 서버 장애로 리뷰 에이전트를 실행할 수 없어 세션을 넘겼고, 이어받은 세션이 리뷰와 커밋을 마쳤습니다. 세부 근거는 [10-15 인계](2026-10-15-sql-db-review-handoff.md)와 [317-321 압축 인계](2026-10-16-sql-compaction-handoff.md)에 있습니다.

## 1. 승인 범위

- 사용자는 10-15 작업(필수 1-5, N1-N7, 315·316), 10-16 압축 작업(317-321)과 통합 보완 전체를 인수하여 **로컬 `main`에 커밋·합류**하도록 지시했습니다.
- 이어받은 세션에서 사용자는 다른 세션의 잔여 작업까지 인수·리뷰하여 원격 반영으로 마무리하도록 지시했습니다(6절).
- 운영 DDL·데이터 변경·배포·재시작은 승인되지 않았습니다.
- 새 retry/fallback/cache, 의존성 변경, 이미 적용된 migration(314 이하) 수정은 범위 밖입니다.

## 2. 커밋 직전 작업 트리 상태 (`/home/kapu/work/iris-stack/hololive-bot`)

| 항목 | 값 |
|---|---|
| branch / HEAD | `main` / `21e60d245` (원격 `f814da318`를 history 보존 merge한 로컬 commit) |
| origin/main 대비 | ahead 4, 미push |
| 스테이징 | 202파일, +11,811 / -2,821. 전체 pre-push gate 통과 대상 |
| 미스테이징 | 문서 4개: `CHANGELOG.md`, `docs/current/plans/2026-10-16-sql-compaction-handoff.md`, `docs/current/services/hololive-api.md`, `docs/current/services/youtube-collector.md` |
| untracked | 이 문서 |
| writer guard | push_lock·ref_transaction_lock 없음 |
| 임시 자원 | 임시 probe 프로그램·디렉터리와 소유 PostgreSQL 컨테이너는 모두 제거했습니다 |

이어받은 세션 시작 시 같은 상태(다른 worktree 없음)를 다시 확인했습니다. 미스테이징 문서 4개는 gate 시작 **이후**의 문서 수정입니다. 코드·SQL·스키마 입력은 gate 이후 바뀌지 않았습니다. 내용은 317-321 적용 순서(315만 적용한 DB·구 binary 혼합 금지, 점검 창·백업·용량 확인), collector의 bytea 키·사전 resolver 계약, CHANGELOG 압축·통합 보완 항목입니다.

## 3. 완료된 검증

- **전체 pre-push gate 통과(711.7초):** 스테이징 202파일 대상 `env -u TEST_DATABASE_URL -u TEST_DATABASE_OWNER_TOKEN -u ALLOW_EXTERNAL_TEST_DB DOCKER_HOST=unix:///var/run/docker.sock FULL_PRE_PUSH=true RACE_TEST_PARALLEL=3 bash scripts/ci/pre-push-gate.sh`. migration manifest·SQL ownership, golangci-lint 0 issues, NilAway, Go build, race test, ap-rsync, PostgreSQL capacity, YouTube.js helper gate를 포함합니다.
- **govulncheck:** 도달 가능 취약점 0. 호출되지 않는 module advisory `GO-2026-5932`(`golang.org/x/crypto/openpgp`, 수정판 없음) 1건이 있습니다. 이번 변경과 무관하며 의존성은 바꾸지 않았습니다.
- **실제 migration CLI smoke(소유 PG 18.6):** 316까지의 baseline(177 파일)에서 원본 10,003행·감사 10,002행으로 시작했습니다. 317 마지막 원본 행 실패 시 10,000행 commit + 3행 pending, 318 마지막 감사 행 실패 시 10,000행 + 2행 pending이 남았습니다. 실패 원인 제거 뒤 같은 CLI 재진입으로 321까지 완료했고 전체 필드가 일치했습니다. 런타임 application INSERT의 최초 1행·중복 0행, scraper resolver의 ID 공유, 두 역할의 사전 직접 INSERT 거절(`42501`)도 확인했습니다.
- **회귀 테스트:** `TestCompactionBackfillResumesCommittedBatches`(신규)가 수정 전 실패하고 수정 후 통과했습니다. provenance·vocab·schema golden 테스트도 통과했습니다.
- **독립 리뷰:**
  - 10-15 통합: `AcceptAdminChanges`·`AcceptExecutionChanges`는 차단 결함 없이 수락했습니다.
  - live refresh(316): 안전성·측정 리뷰 모두 로컬 채택을 수락했습니다.
  - 317-321 caller 계약(`ReviewCompactionCallers`): 결함 없음. canonical hex identity, publish/collision 경로, claim/application, replay, retention 보호, 권한을 확인했습니다. 319 동시 index 중단은 runner의 invalid index 정리로 처리되므로 결함으로 보지 않았습니다.
  - 317/318 batch commit(이어받은 세션의 읽기 전용 reviewer): 차단 결함 0으로 수락했습니다. 결론은 4절에 있습니다.
- **이어받은 세션의 직접 확인:**
  - 러너 경로: `migrationrunner`(`runner.go` `execSegment`)와 dbtest(`migrations.go` `applyMigrationSegment`)는 `sqlsplit.Segments`의 비트랜잭션 문장을 인자 없는 `Exec`로 하나씩 보냅니다. pgx v5.11.0은 인자 없는 `Exec`를 simple protocol 단일 Query로 전송합니다. advisory lock은 session 단위라 내부 `COMMIT`으로 풀리지 않습니다.
  - 고정 이미지 PG 18.6 probe(소유 임시 컨테이너, 확인 뒤 제거): `statement_timeout=2s`에서 batch 2개가 commit된 뒤 2.000초에 취소되었고 commit분은 남았습니다. `set_config('lock_timeout','3s',true)`는 내부 `COMMIT` 뒤 세션 값으로 돌아갔습니다. 명시적 `BEGIN` 블록 안과 다중 문장 simple query의 `DO ... COMMIT`은 `invalid transaction termination`이었습니다.
  - `bash tools/checks/check-stack-db-access-policy.sh`, `bash tools/checks/check-stack-retry-contract.sh`(iris-stack 루트): 둘 다 exit 0.

## 4. 남은 작업의 진행 결과

1. **317/318 batch commit 독립 안전성 리뷰: 완료, 차단 결함 0.**
   - top-level `DO`는 명시적 transaction 밖의 단독 autocommit 문장으로 실행되어 내부 `COMMIT`이 합법입니다. `EXCEPTION` 절이 없고, 분할기는 태그 달린 dollar quote를 보존합니다.
   - keyset 재진입: `previous_id`는 batch 최대 id라 범위가 겹치거나 비지 않습니다. 빈 표와 `upper_id` NULL은 즉시 종료합니다. 재진입은 처음부터 다시 읽지만, `IS DISTINCT FROM` 가드(317 `$backfill$`, 318 `$backfill$`)가 완료 행을 다시 쓰지 않습니다. 매 batch 첫 줄이 `lock_timeout` 3s를 다시 겁니다.
   - timeout 서술: `statement_timeout`은 `DO` 전체에 걸리며 내부 `COMMIT`으로 다시 시작하지 않습니다. `lock_timeout`은 획득 대기만 제한합니다. 두 서술 모두 맞고 위 probe와 일치합니다.
   - 순서·멱등성: 준비(사전·nullable 열) → backfill → `NOT VALID` 제약 → 미검증 제약만 `VALIDATE` → 파괴적 cutover 순서가 맞습니다. 각 단계에서 중간 실패한 뒤 같은 파일을 재실행해도 열·타입·제약 가드로 성공합니다.
   - 비차단 지적: `source_compaction_resume_test.go`는 완료/미완료 행 수만 확인합니다. 가드를 지워도 통과하므로 "완료 batch를 다시 쓰지 않음"은 테스트가 아니라 SQL 검토로만 확인됩니다. 가드가 없어져도 정확성은 그대로이고 재실행의 쓰기 증폭만 늘어나므로, 이번 합류에서는 테스트를 바꾸지 않았습니다. 운영 관련 지적은 5절에 반영했습니다.
2. **리뷰 결과 반영:** 코드·SQL 수정이 없으므로 gate를 다시 실행하지 않고 3절의 통과 결과를 재사용했습니다. gate 이후 변경은 문서뿐입니다.
3. **스테이징·커밋:** 6절.
4. **인계 문서 갱신:** 이 문서와 [압축 인계](2026-10-16-sql-compaction-handoff.md) 7절. 원격 발행과 iris-stack 메타 pointer 갱신은 6절에 있습니다.

## 5. 운영 적용 경계 (이번 범위 아님)

- 315·316 다음 317-321 순서로, API·worker·collector a/b/c/d를 중지한 점검 창에서 `db-migrate`로 적용합니다. 이후 신규 API·collector image를 배포합니다.
- 315만 적용한 DB나 구 binary와 혼합 운용하면 안 됩니다. image-only rollback도 할 수 없습니다.
- 317은 원본 표(약 1GB)를 재작성하고 index를 재빌드합니다. 이를 위해 heap/index·WAL 여유 공간이 필요하며 ACCESS EXCLUSIVE 잠금이 걸립니다. backfill이 대부분의 행에 새 tuple을 쓰고 cutover가 표 전체를 다시 쓰므로, 여유 공간은 이 두 번의 쓰기를 기준으로 산정합니다(추론, 운영 규모 미측정).
- 317·318이 중간에 실패하면 서비스를 다시 띄우지 말고 정지 상태에서 같은 `db-migrate`를 재실행합니다. 준비 단계 뒤에 들어온 새 출처·어휘 조합은 사전에 추가되지 않아 NULL로 남습니다. 그러면 `VALIDATE`가 `23514`로 계속 실패합니다. 데이터는 잃지 않지만 수동 조치가 필요합니다.
- backfill `DO`는 timeout으로 끊겨도 commit된 batch가 남으므로 재실행할 때마다 진척됩니다. 반면 317 cutover 재작성, `VALIDATE`, 318 끝 블록, 321은 실패하면 통째로 되돌려지는 단일 문장입니다. 따라서 `--statement-timeout`(최대 10m) 안에 끝나지 않으면 재실행해도 진척이 없습니다. 운영 규모 사본 리허설로 소요 시간을 먼저 확인하고, 한도 초과 시에는 [API runbook](../runbooks/hololive-api.md)의 `db-migrate --statement-timeout` 절차를 따릅니다.
- 318-321 이후의 감사 표 공간 회수는 별도 VACUUM FULL 단계입니다.
- 사전 백업과 이전 image/compose 복구점을 준비하고, 적용 뒤 실제 크기와 `pg_stat_statements`를 재측정해야 합니다.
- 소형 tmpfs fixture 결과를 운영 소요 시간·용량 보장으로 쓰지 않습니다. 운영 절차는 `hololive-bot-ops`·`stack-platform-ops` 소유이며 별도 승인 대상입니다.

## 6. 커밋·발행 결과

- 로컬 합류 커밋: `948ab90e5` `feat(db): SQL 리뷰 후속 수정과 원본 저장 구조 압축 합류`와 SHA 기록 `4951df777`. 부모는 `21e60d245`이고 203파일, +11,894 / -2,821입니다.
- hook: pre-commit의 `staging-scope-guard`가 추적 dirty 파일 전체(203개) 스테이징을 차단했습니다. 의도된 전체 합류이므로 이 커밋만 `IRIS_STACK_ALLOW_BULK_STAGE=1`로 재시도했습니다. writer guard·gofmt·identity·제목 정책 hook은 그대로 실행되어 통과했습니다. `--no-verify`·amend·force는 쓰지 않았습니다.
- 다른 세션 잔여 작업 인수:
  - 로컬 main의 `d36a9d058`·`0a5124c13`·`ccdafb4cb`·`21e60d245`는 원격 #640 squash 전 이력입니다. `21e60d245`와 `f814da318`의 tree가 같으므로(`6454ea09d`) 새로 발행할 내용이 없어 발행 branch에서 제외했습니다.
  - chat-bot-go-kakao `e6f049633`(`fix(prompt)`)은 원격에 이미 있고 iris-stack 메타 pointer만 남아 있었습니다.
  - 그 밖의 submodule·메타 저장소·worktree에는 미커밋·미발행 작업이 없었습니다.
- 발행 경로: 원격 ruleset은 `fast-gate` 필수(strict)와 non-fast-forward 금지이며, 저장소는 squash merge만 허용합니다. `origin/main`(`f814da318`) 위에 위 두 커밋을 cherry-pick한 `feat/sql-compaction-20261016`으로 PR을 열고, `fast-gate` 통과 뒤 squash merge합니다. tree는 로컬 main과 같습니다(`8db1a5e63`). 발행 commit은 origin/main에서 제목 `feat(db): SQL 리뷰 후속 수정과 원본 저장 구조 압축 합류 (#번호)`로 찾습니다. 로컬 SHA는 원격에 남지 않습니다.
- 발행 뒤: 로컬 main을 발행된 origin/main으로 맞추고, iris-stack 메타 pointer(hololive-bot·chat-bot-go-kakao)를 원격에 있는 SHA로 갱신합니다.

**Fallback delta: none. Retry delta: none.**
