# 전체 변경 main 합류 마무리 인계 - 2026-10-16

**상태: 통합·전체 gate·독립 migration 리뷰를 마치고 PR #641(squash)로 원격 `main`(`848fb2792`)에 발행했습니다(6절). 운영 중앙·AP a/b/c/d에 적용하고 수용 검사를 마쳤습니다(7절). 배포 뒤 발견한 alarm 결함의 근본 수정, Seoul 점검 창 배포 절차 수정, 중앙 issuer c 정렬과 이전 버전 정리는 PR #643·#644와 운영 적용으로 마쳤습니다(8절).** 첫 세션은 OAI 서버 장애로 리뷰 에이전트를 실행할 수 없어 세션을 넘겼고, 이어받은 세션이 리뷰와 커밋을 마쳤습니다. 세부 근거는 [10-15 인계](2026-10-15-sql-db-review-handoff.md)와 [317-321 압축 인계](2026-10-16-sql-compaction-handoff.md)에 있습니다.

## 1. 승인 범위

- 사용자는 10-15 작업(필수 1-5, N1-N7, 315·316), 10-16 압축 작업(317-321)과 통합 보완 전체를 인수하여 **로컬 `main`에 커밋·합류**하도록 지시했습니다.
- 이어받은 세션에서 사용자는 다른 세션의 잔여 작업까지 인수·리뷰하여 원격 반영으로 마무리하도록 지시했습니다(6절).
- 커밋 단계에서는 운영 DDL·데이터 변경·배포·재시작이 승인되지 않았습니다. 발행 뒤 사용자는 운영 배포와 로컬 branch·worktree 정리를 지시했습니다. 이때 사전 전체 백업은 작업 완료 뒤 삭제, `VACUUM FULL`은 같은 점검 창, 점검은 merge 직후, plan gate는 배포 전후로 정했습니다(7절).
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

## 5. 운영 적용 경계

커밋 단계에서 정리한 적용 조건입니다. 실제 적용 결과는 7절에 있습니다.

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
- 발행 결과: PR #641이 `fast-gate` 통과 뒤 `848fb2792`로 squash merge되었습니다. 로컬 main은 `reset --keep`으로 origin/main에 맞췄고 발행 branch는 원격·로컬 모두 삭제되었습니다. iris-stack 메타 pointer(hololive-bot·chat-bot-go-kakao)는 이 배포 기록이 발행된 뒤 원격 SHA로 갱신합니다.

## 7. 운영 배포 결과 (2026-10-16, revision `848fb2792`, 10.1.0)

- **준비:**
  - kapu에서 ARM64 image(api·alarm-worker·collector)를 `DOCKER_DEFAULT_PLATFORM=linux/arm64`로 빌드했습니다. 전송 뒤 중앙에서 architecture와 전체 revision이 맞는지 확인했습니다.
  - 중앙 복구점은 `/opt/hololive-bot/compose/sql-compaction-20261016T022736Z/previous`(deploy tree·compose checksum·서비스·image 목록)입니다. image tag `rollback-sql-compaction-20261016t022736z`(3개 repo)도 남겼습니다.
  - 배포 전 plan gate(`scripts/runtime/pg-hotpath-explain-snapshot.sh`)를 통과했습니다.
- **중앙 점검 창 (02:29:23Z 시작):**
  1. AP a/b/d와 중앙 collector c, api, worker를 정지했습니다.
  2. `pg_dump -Fc` 전체 백업(405MB)을 만들고 sha256과 `pg_restore --list`로 확인했습니다.
  3. migration 9개를 운영 tree에 넣고 checksum을 대조했습니다. 운영 tree의 다른 파일은 호스트에서 조정한 compose와 예전 사본이라 git과 달라서 교체하지 않았습니다.
  4. `:prod` 승격 뒤 `db-migrate --statement-timeout=10m`을 실행했습니다(02:30:38Z-02:35:45Z). 결과는 `applied=7 skipped=175 total=182`이고 ledger 마지막은 `321_source_application_vocab_cutover.sql`입니다.
  5. 같은 창에서 `VACUUM (FULL, ANALYZE) source_observation_applications`를 26초 동안 실행했습니다. 크기는 1,259MB에서 691MB로 줄었고 관련 표 4개도 `ANALYZE`했습니다.
  6. worker·api·collector c를 02:36:36Z-02:36:56Z에 기동했습니다. 모두 healthy이고 재시작은 0회입니다.
- **AP 배포:**
  - osaka a: `ap-host-native-deploy.sh osaka --apply`, 02:37:19Z-02:41:21Z.
  - osaka2 d: 같은 스크립트, 02:41:42Z-02:46:19Z.
  - 두 호스트 모두 `READY`, `first_success`, handoff `PROCESSED`, `NRestarts=0`이고 스크립트 안의 완료 검사를 통과했습니다.
- **Seoul b 경위:**
  1. 공식 `ap-deploy.sh seoul --apply`는 사전 검사에서 "기존 b가 실행 중이고 healthy"를 요구합니다. b가 점검 창에서 정지되어 있었으므로 배포를 거부했습니다(원격 변경 없음).
  2. 사용자 승인으로 수동 no-build cutover를 했습니다(03:16:35Z). 이전 collector image는 `hololive-youtube-collector:rollback-20261016T031540Z`로 보존했습니다.
  3. 수동 cutover는 PO issuer b를 `c98d54f2c`로 두었습니다. 그런데 완료 검사와 공식 배포의 rollback 기준점은 collector b와 PO issuer b의 revision이 같기를 요구합니다. 그래서 다시 실행한 공식 배포는 cutover 전에 거부되었고 스크립트가 변경 전 상태로 자동 복원했습니다.
  4. 사용자 승인으로 kapu에서 PO issuer 산출물(`build-po-sandbox-artifact.sh arm64`)을 빌드했습니다. 공식 스크립트와 같은 검증(sha256, image ID·revision·architecture·version, broker healthcheck, socket 권한)을 거쳐 PO b를 교체했습니다. active receipt를 갱신하고 collector b를 다시 만들었습니다(03:23:44Z). 이 상태에서 완료 검사를 통과했습니다.
  5. 이어서 공식 `ap-deploy.sh seoul --apply`를 다시 실행해 원격 source tree와 rollback 기준점을 정식으로 갱신했습니다. cutover는 03:24:32Z이고, `collector AP completion check passed` 상태로 rollback 없이 끝났습니다.
  - b는 02:29Z부터 03:16Z까지 정지했습니다. 그동안 a·c·d가 수집했습니다.
- **수용 검사:**
  - `ap-completion-check.sh`: osaka·osaka2·seoul 모두 exit 0입니다.
  - 중앙: api health exit 0, collector c `/ready`는 `READY`/`first_success`입니다.
  - 새 schema 흐름: 02:36 이후 신규 관측 6,750건이 들어왔고 queue는 `PENDING` 1건만 남았습니다. `observation_key`는 모두 32바이트이며 신규 관측의 감사 행도 생성되었습니다.
  - 배포 후 plan gate는 PASS 8건, FAIL/WARN 0건입니다. 두 claim 경로는 배포 전과 같은 index를 쓰고, 실행 시간은 alarm 0.742→0.662ms, outbox 0.342→0.389ms입니다.
  - 배포 후 크기: `source_observations` 692MB, `source_observation_applications` 662MB, DB 1,761MB, 중앙 디스크 여유 33G.
  - 로그에는 secret key 이름만 나타났고 `KEY=값` 형태는 0건이었습니다.
- **정리:**
  - 사용자 지시에 따라 일회성 백업 `/var/backups/hololive-predeploy-sql-compaction-20261016T022736Z/`를 삭제했습니다.
  - 이로써 317-321 적용 전 DB로 되돌릴 복구점은 없습니다. 문제가 생기면 forward fix로 대응합니다.
  - 중앙 previous tree·rollback tag, Seoul `backups/seoul-collector-20261016T03*`와 rollback tag(collector `031540Z`·`031902Z`·`032423Z`, issuer `031902Z`·`032423Z`), native AP의 이전 release는 보존했습니다. 이것들을 정리하려면 별도 승인이 필요합니다.
- **배포와 무관한 기존 결함 (8절에서 수정):**
  - 증상: alarm-worker는 10-15 10시대(UTC)부터 매분 같은 event_key 하나에 대해 `dispatch event hash conflict`를 경고합니다. 그 결과 `publish queue batch: one or more deliveries were not accepted` scheduler 오류와 `alarm.youtube.live_guardrail.missing_dispatch`가 이어집니다(구독 방 1개).
  - 시작 시점: Loki 집계로 시작이 이번 점검 창(10-16 02:29Z)보다 앞섭니다. 따라서 이번 배포가 원인이 아닙니다.
  - 관측: 저장된 payload hash는 고정되어 있고 새로 계산한 hash는 매분 달라집니다.
  - 원인(8절에서 확인): 매번 바뀐 값은 `stream.viewer_count`였습니다. 재발행 자체는 24시간 근거 창 결함에서 생겼습니다.
  - 영향 정정: 그 방의 live 알림은 10-14 09:57Z에 이미 발송되었습니다. 이전에 적은 "알림 1건 미발송"은 잘못된 서술입니다. 실제 영향은 매분 반복된 재발행 시도, 경고·오류 소음, collision 행 증가였고, 같은 event에 나중에 합류하는 방은 거절될 위험이 있었습니다.
  - 근거 경로: `hololive-alarm-worker/internal/service/alarm/dispatchoutbox/repository_event_preflight.go`.

## 8. 배포 뒤 결함 근본 수정과 이전 버전 정리 (2026-10-16)

- **alarm 결함 원인 (운영 읽기 전용 확인):**
  - 10-14 09:52Z에 시작해 42시간 넘게 이어진 live가 있었고, 이 방송의 catchup은 10-14 09:57Z에 SENT되었습니다.
  - catchup·guardrail의 "이미 알린 방" 근거 하한이 평가 시각 − 24시간이었습니다. 그래서 10-15 09:58Z부터 최초 SENT가 창 밖으로 밀렸고, 같은 방에 catchup을 매분 다시 만들었습니다.
  - canonical event payload에 `stream.viewer_count`가 들어 있어 재발행이 `duplicate_sent` 대신 `rejected_collision`이 되었습니다.
  - 결과: hash conflict WARN, scheduler ERROR, `missing_dispatch` WARN이 매분 났고, collision 행이 297건까지 늘었습니다.
- **수정 (PR #643, `3951da83d`):**
  - 근거 하한을 방송별 실제 시작 시각 − 24시간으로 바꿨습니다. 실제 시작 시각이 없거나 미래이면 평가 시각을 기준으로 하고, 예정 시각은 쓰지 않습니다.
  - event payload identity에서 `viewer_count`를 뺐습니다.
  - 적대적 리뷰 2회에서 차단·중요 결함은 없었고, 경미 지적은 반영했습니다.
- **worker 배포:**
  - kapu에서 `build-all.sh --build-only --no-bump hololive-alarm-worker`(local CI 포함)로 arm64 image `aeaa6579e8ce`(revision `3951da83d`, 10.1.0)를 빌드했습니다.
  - 중앙에서 worker만 `--no-build --no-deps`로 교체했습니다(05:16:17Z). 미발행 upcoming 후보와 진행 중 delivery가 모두 0건일 때 진행했습니다.
  - rollback point는 `hololive-alarm-worker:rollback-alarm-dispatch-20261016T051616Z`(848)입니다.
- **worker 수용:**
  - 교체 뒤 8분 동안 hash conflict, not accepted, missing_dispatch, collision, ERROR가 모두 0건이었습니다. 교체 전 30분 동안은 hash conflict 30건, not accepted 90줄, missing_dispatch 30건, ERROR 30건이었습니다.
  - 새 근거 SQL 두 개의 `pg_stat_statements` 호출 수가 1분마다 늘어 checker가 새 코드로 도는 것을 확인했습니다.
  - health는 healthy이고 재시작은 0회입니다. 교체 뒤 WARN 1건은 확인용 `/metrics` 요청의 404였습니다.
- **Seoul 점검 창 절차 (PR #644, `9c12e5875`):**
  - `AP_COLLECTOR_MAINTENANCE_STOPPED=true scripts/deploy/ap-deploy.sh seoul`이 정지한 b를 받습니다.
  - cutover가 실패하면 이전 image로 되돌리되 collector는 `up --no-start`로 만들기만 합니다.
  - 기준점 짝 검사와 읽기 전용 검증은 rollback을 걸기 전으로 옮겼습니다.
  - 적대적 리뷰 2회의 중요 지적(핵심 경로 테스트 공백)을 반영했습니다. 사용 조건은 [youtube-collector runbook](../runbooks/youtube-collector.md)에 있습니다.
- **중앙 issuer c 정렬:**
  - 정렬 전에는 issuer c가 `c98d54f2c`, collector c가 `848fb2792`였습니다. 이 상태에서는 다음 공식 paired 배포(`po-central-cutover.sh`)가 짝 검사에서 거부됩니다.
  - Seoul에서 운영 중인 것과 같은 848 issuer 산출물을 사용했습니다(image `4ddbe64b47a3`, `1914722e054a`).
  - 공식 `po-central-remote.sh`의 검증·복원 함수를 그대로 써서 issuer c를 교체하고 collector c를 다시 만들었습니다(04:56:20Z).
  - 공식 `po-central-cutover.sh check`가 `central collector/issuer completion verified revision=848fb2792...`를 냈고, collector c는 `READY`·`first_success`입니다.
- **이전 버전 정리 (사용자 승인):** 317-321을 적용했고 적용 전 DB 백업을 삭제했으므로, 848 이전 image와 release는 유효한 rollback 대상이 아닙니다. 그래서 정리했습니다.
  - Seoul: tag 12개와 백업 5개를 지우고 dangling image를 prune했습니다(1.05GB). 남은 기준점은 `seoul-collector-20261016T032423Z`(848)입니다.
  - 중앙: tag 32개, 이전 배포 디렉터리 9개, issuer 정렬 backup·staging을 지웠습니다. 남은 것은 `:prod`와 worker rollback point이고, 디스크 사용량은 15G에서 12G로 줄었습니다.
  - osaka·osaka2: 이전 release 3개씩과 `previous` link를 지웠습니다. native rollback은 기준점이 없어 거절하므로, 문제가 생기면 fix-forward합니다.
  - kapu: 앱 repo 4개에서 848 이전 tag 146개를 지우고 dangling image를 prune했습니다. image 사용량은 72GB에서 49GB로 줄었습니다. DB image, 호스트 이전 snapshot(`hololive-migration/*`), 보조 image는 남겼습니다. 이 중에는 현재 중앙 postgres·deunhealth image의 원본도 있습니다.
- **남은 한계:**
  - 실제 시작 시각 없이 24시간을 넘긴 방송은 이전처럼 catchup을 다시 시도할 수 있습니다. 확인 시점의 LIVE 세션 중 `started_at`이 빈 경우는 0건이었습니다.
  - 그 방의 catchup delivery가 DLQ·QUARANTINED·CANCELLED이면, catchup이 허용되는 동안 재발행이 `rejected_terminal`로 반복될 수 있습니다. 이번 변경 범위 밖의 기존 동작입니다.
  - 기존 collision 297행은 남아 있습니다. 사라지려면 SENT 보존 기간(90일)을 넘겨야 하며, 90일을 넘는 live는 근거를 잃는 경계 사례입니다.
  - host-native a/d 배포에는 점검 정지 mode가 없어, 실패하면 `previous` release를 다시 시작합니다(runbook 명시).
  - Seoul 배포에서 읽기 전용 블록이 `cutover_armed=true` 앞에 있다는 순서 자체는 테스트로 검증하지 않습니다.

**Fallback delta: none. Retry delta: none.**
