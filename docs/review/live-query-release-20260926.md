# LiveQuery 독립 리뷰와 중앙 운영 반영 기록

`DEC-20260926-hololive-live-query-read-model`, `DEC-20260926-youtube-only-stream-providers`와 `PLN-20260926-live-query-release`의 출판·운영 근거다. 사용자가 서브에이전트 적대적 리뷰, 발견 이슈 수정, 커밋·푸시·라이브 반영을 승인한 범위에서 수행했다. 시각은 별도 표기가 없으면 UTC다.

## 독립 리뷰와 수정

LiveQuery와 provider/cache 제거를 두 서브에이전트가 별도로 검토했다. DB reviewer가 보존된 pending 행을 현재 종료로 오인해 정상 LIVE와 complete-empty를 가리는 P1을 발견했다. 상태와 증거 시각으로 미해결 pending을 구분하도록 수정했고, 수정 전 실패한 반례와 실제 reducer 보존 반례를 통과시켰다. 같은 reviewer가 수정 및 최종 최적화를 재검토했다.

Provider reviewer는 제거 경로·취소·캐시 결과 소유권에 추가 기능 결함을 발견하지 않았다. AP 전송 목록의 cache-fill 소스 누락과 provider 제거에 따른 privacy scanner의 패키지 수 기준도 수정·확인했다. production DI가 실제 pool 연결/Ping을 완료한 뒤 LiveQuery를 생성함을 독립 확인했다.

재현, race, lint/NilAway와 150만 absence slot·최대 10만 pending의 비용 근거는 [구현 및 리뷰 기록](live-query-read-model-20260926.md)에 있다. 최종 장기 이력 측정 p95는 전체 123.4ms, 멤버 64.90ms였다. retained pending 스캔은 이력에 선형이며 무제한 이력에서도 같은 비용이라고 주장하지 않는다.

## 출판과 이미지

- 구현 및 수정 커밋: `727d74b5bcef8913bb2b75b59834ab745713a2ff`, `bd60590251e940603b098aa094051e69c9e35587`, `52e57f4783184b212ec8a7518356771d648e478c`.
- 로컬 필수 pre-push의 reusable/freshness/ambient 전 단계 통과. architecture, 단위/race, build, lint/NilAway, SQL/migration, 배포 계약, dependency hygiene를 실행했다. govulncheck의 호출 가능한 취약점은 0건이며, 일부 module의 비도달 advisory까지 0건이라는 뜻은 아니다.
- 별도 opt-in integration suite는 기본 설정에 따라 생략했다. 실제 PostgreSQL을 사용하는 회귀·migration/schema 검사와 integration-tag vet는 통과했다.
- [PR #527](https://github.com/park285/hololive-bot/pull/527)의 [CI 실행](https://github.com/park285/hololive-bot/actions/runs/36215118130)에서 모든 job 및 필수 `fast-gate`가 성공했다. main 직접 push는 required status 규칙으로 거부되어 PR 경로로 전환했으며, hook·원격 규칙을 우회하지 않았다.
- 저장소가 허용하는 squash 방식으로 2026-09-26 03:41:28에 `e65119bc1cdebe902625a8bab7f9851582524353`을 main에 병합했다. 검토한 PR head `52e57f4783184b212ec8a7518356771d648e478c`와 Git tree `fa81c2ae09a8d24795d1cd1926d18c577da6d9b4`가 정확히 같다.
- 이미지는 clean worktree의 PR head에서 kapu `kapu-multiarch` builder로 빌드했다. 두 이미지와 배포 bundle의 source revision은 `52e57f4783184b212ec8a7518356771d648e478c`다. squash main SHA와 image label SHA는 구분한다.

| 대상 | 버전 | architecture | 검증한 image ID |
|---|---|---|---|
| hololive-api | 4.0.1 | arm64 | `sha256:7d8c76d0b3d2b508a7c4b0a1a82520c336f8e94cd8c1d87728f0b75d5e11c7a0` |
| hololive-alarm-worker | 3.2.6 | arm64 | `sha256:78c1a02f93da0eb1ea5f7dd7e11c5acb696fb99a0de7744a566bb8e743b8db7f` |

## 중앙 전환

대상은 hololive-osaka의 중앙 API·worker와 migration 211이다. 기존 AP/collector, secret, runtime-config와 데이터 행·queue는 운영자가 변경하지 않았다. runtime host에서는 빌드·컴파일·Git pull을 수행하지 않았다.

144개 tracked 배포 파일만 묶은 bundle의 SHA256은 `ac004e65e7f007adcb6dad40f2e2071e8de43bd65d30ae1c762a4d17b1245123`이다. reviewer가 필수 preflight 파일 두 개의 포함을 확인했다. PostgreSQL HBA 및 ingress 파일은 현행 운영 파일과 검토본의 해시가 같음을 확인하고, 설치 직전에도 일치를 강제했다. env·secret·logs·data·runtime-config는 bundle에서 제외했고 기존 디렉터리 권한을 보존했다.

1. `change_started_at=2026-09-26T03:42:06Z`에 기존 이미지와 배포 파일을 보존했다.
2. kapu에서 준비한 API·worker 이미지를 전달/load하고 원격에서도 정확한 ID, arm64와 전체 revision을 검증했다. 검증한 이미지만 Compose runtime tag로 지정하고 wrapper의 `config --quiet`를 통과했다.
3. repository one-shot에 `run --rm --no-deps --pull never -T -e POSTGRES_ADMIN_PASSWORD= hololive-db-migrate`를 사용했다. 기존 pure-migrate 분기로 role/password bootstrap을 실행하지 않았다. embedded migration FS의 결과는 **applied=1, skipped=71, total=72**였다.
4. migration 211이 03:42:48.868632에 ledger에 기록됐고 부분 인덱스는 `indisvalid=true`, `indisready=true`, 크기 2360kB였다.
5. 실제 DB query의 guard·인덱스 사용·진단을 확인한 뒤 API와 worker를 각각 `up -d --no-build --no-deps --pull never`로 전환했다. 퇴역 runtime 이름이 모두 없음을 먼저 확인하여 wrapper의 cleanup이 제거한 컨테이너는 없었다. Compose가 표시한 기존 X login orphan 경고에 대한 정리 작업도 하지 않았다.

| 대상 | 새 StartedAt | 확인 결과 |
|---|---|---|
| API | 2026-09-26T03:43:38.133657231Z | candidate ID/revision 일치, healthy, restart 0, OOM false |
| worker | 2026-09-26T03:44:07.850372143Z | candidate ID/revision 일치, healthy, restart 0, OOM false |

API의 `/health`, bot `/internal/ready`, LLM `/health`, worker `/health`와 중앙 collector `/ready`를 기존 healthcheck 바이너리로 확인했다. Postgres 연결 marker는 API 4개·worker 1개, API Valkey 연결 marker는 1개였다. worker readiness의 DB/Valkey 확인도 성공했다. 현재 LLM provider는 `gemini`로 member-news/event-summary 초기화 marker를 확인했으며, 사용하지 않는 CLIProxy 연결 성공을 주장하지 않는다.

초기 확인 로그는 API 70행·worker 20행이었다. `ERR`/ERROR, panic, permission denied, x509, no-such-file, OOM 표식은 0건이었다. worker의 X Spaces `collector_failed` 경고는 다음 문단의 기존 문제로 확인했다. collector, Postgres, Valkey의 image ID·StartedAt·healthy는 전환 전과 같았다.

## 실제 DB 조회와 남은 불확실성

모든 운영 DB read session은 `default_transaction_read_only=on`을 설정하고 `SHOW transaction_read_only = on`을 먼저 확인했다. network DB 연결은 TLSv1.3이며 plaintext 연결은 없었다. 조회 SQL은 `hololive_runtime` 역할, 1초 statement timeout에서 실행했다.

03:43:07 snapshot의 활성 대상은 우이를 포함한 74개이며 확정 LIVE item은 3개였다. 채널 진단은 `inconsistent=63`, `confirming_end=7`, `incomplete=3`, `stale=1`이었다. 전체 결과는 partial이며, 미확인 상태를 방송 없음이나 전체 coverage 완료로 바꾸지 않았다.

원문 snapshot SQL의 `EXPLAIN (ANALYZE, BUFFERS)`는 execution **25.643ms**, planning **34.339ms**, shared hit/read **1641/192**였다. 새 LIVE coverage 인덱스를 한 번 읽었고 대상 index scan은 9행이었다. 이는 그 시점의 단일 운영 관측이며 p95·지속 부하 SLO 측정이 아니다.

X Spaces 경고는 보존된 worker log 최근 5000행을 message/code별로 집계했다. 같은 `collector_failed`가 배포 전에 955건 있었고 마지막 시각은 12:42:05 KST로 변경 시작 직전이었다. 배포 후 12:44:09·12:46:10 KST의 두 건도 같은 코드였다. X Spaces 관련 Go/helper/Dockerfile 및 Compose 설정에는 변경이 없고 배포된 helper/package 파일의 SHA256은 검토한 소스와 일치했다. 이 경고의 근본 원인과 X 세션 복구는 이번 변경에서 해결했다고 주장하지 않는다.

Fallback delta: **none**. 조회 실패·stale·불일치를 빈 성공으로 바꾸지 않고 upstream fallback을 추가하지 않았다. cold/warm `!라이브`의 upstream 호출 0회와 partial/unknown 출력은 로컬 회귀로 검증했다. 운영 방에 시험 메시지를 보내지는 않았다.

## 복구 지점과 작업 보존

복구 파일은 `/opt/hololive-bot/compose/rollbacks/live-query-20260926-52e57f478/previous-deploy-files.tar`이며 SHA256은 `9496a339e729f1003b09cc714229ba695317d2799c45d16b1aa5edf89a1f22e8`이다. 기존 이미지 revision은 `476a15009ebdef07d1243d389358a4e60b98728a`다.

- `hololive-api:rollback-live-query-20260926`: `sha256:8d593629a8edf590ef8951641c83728ed3f81fa572dc502beeb499c8f241a7f2`.
- `hololive-alarm-worker:rollback-live-query-20260926`: `sha256:079a40862147879dc98f064d3004e30e51dd6632500132f92b24b7afe590aab4`.

필요하면 보존한 배포 파일과 앱 이미지로 복구하고 additive index 211은 유지한다. ledger 211을 모르는 구형 migrator를 다시 실행하거나 데이터를 지우지 않는다. 복구 자산은 보존했으며 rollback 자체를 실행·검증했다고 주장하지 않는다.

기존 unrelated 수정 5개는 별도 checksum으로 원본 보존을 확인했다. squash 통합 후 기존 로컬 커밋도 `codex/live-query-original-main-20260926`에 남겼다. 메타 저장소 전체 publication gate가 금지된 Iris 경로 검사를 요구하므로 이를 우회하지 않았고, root PLN·생성 카탈로그 변경은 로컬에 남긴다. 서비스 소스 출판과 중앙 운영 반영은 위 PR·image·health 근거로 구분한다.
