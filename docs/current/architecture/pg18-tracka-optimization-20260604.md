# PG18 Track A 최적화 적용 결과 (2026-06-04)

`docs/PG18_DB_OPTIMIZATION_PLAN_20260603.md`(iris-stack 메타리포) Track A(A1~A5)의 적용·측정 기록.
대상: `holo-postgres` (postgres:18.4, 512MB cap, 호스트 단일 backing DB).

## A1 — 관측 기반 구축 (적용 완료)

`deploy/compose/docker-compose.prod.yml`의 holo-postgres `command:` 블록으로 명시화, 컨테이너 recreate로 적용:

- `shared_preload_libraries=pg_stat_statements`, `pg_stat_statements.track=all`, `pg_stat_statements.max=5000`, `compute_query_id=on`, `track_io_timing=on`, `track_wal_io_timing=on`
- 정정(리뷰 반영): 플랜 v2의 "track_wal_io_timing은 PG18에서 제거된 GUC" 주장은 **오류** — PG18.4에 존재(`pg_settings` context=superuser 실측). 원 의도대로 켜서 `pg_stat_io(object='wal')`에 WAL I/O 시간까지 집계.
- extension: `pg_stat_statements 1.12` 생성 (기존 볼륨 1회 수동 + fresh 볼륨용 `init-db/05-create-pg-stat-statements.sql`)

재시작 영향: 5개 의존 서비스(kakao-bot/alarm-worker/llm-sched/youtube-producer/admin-api) 전부 healthy 유지, 재시작 후 3분 로그 sweep 에러 0건 — 재연결 백오프 정상, retry storm 없음.

## A2 — 512MB envelope 명시화 (적용 완료)

예산 근거: 기존 pool max = 4서비스×기본 25 + alarm-worker 8 = 이론상 108 > `max_connections=100`이었으나, 현재 compose는 서비스별 pool cap과 `max_connections=60`을 명시한다. 일반 운영 목표는 40 connections 이하이며 실측 동시 커넥션은 33 수준이었다.
work_mem 역산: (512 − 128(shared_buffers) − 64(maintenance) − ~80(io worker·OS·커넥션 오버헤드)) / 40conns ≈ 6MB → 다중 정렬/해시 노드 마진 적용 4MB.

적용값(전부 `command:` 명시): `shared_buffers=128MB`, `effective_cache_size=256MB`(stock 4GB는 512MB cap에 과대 힌트였음), `maintenance_work_mem=64MB`, `work_mem=4MB`, `io_method=worker`, `io_workers=3`, `effective_io_concurrency=16`, `maintenance_io_concurrency=16`.

OOM 검증: `VACUUM (ANALYZE)` 부하 유도 중 RSS 78MiB(15% of cap, 기준 ≤80%), `OOMKilled=false`.

## A3 — pgx exec_mode 재평가 (D2: `exec` 유지 → 2026-08-02 `cache_statement`로 전환, 아래 재평가 참조)

- 현행: compose `POSTGRES_QUERY_EXEC_MODE:-exec` (코드 기본값은 `cache_statement`이나 운영은 exec 고정).
- 근거: pg_stat_statements 기준 top query mean이 전부 sub-ms(0.027~0.43ms), 커넥션 여유(33/100) — `cache_statement`의 개선 여지가 측정 한계 이하. per-conn statement cache는 512MB envelope에 추가 메모리 압박. 플랜 Stop-rule D2("우열 모호하면 exec 유지, 근거 기록") 적용.
- 재평가 트리거: top query mean > 5ms 또는 DB CPU 포화 시 A/B 측정(`pg_stat_statements_reset()` 후 모드별 부하 비교).

### A3 재평가 결과 (2026-08-02, `cache_statement` 전환)

위 D2 판정은 2026-06-04 시점 기록이며, 아래 근거로 뒤집혔다. compose 기본값과 `scripts/deploy/ap-host-native-deploy.sh`의 host env를 `cache_statement`로 전환한다(코드 기본값과 일치).

전환 근거:

1. **A2의 512MB envelope 전제 소멸** — A3이 기록한 "per-conn statement cache는 512MB envelope에 추가 메모리 압박"은 당시 `shared_buffers=128MB` / 512MB cap 기준이었다. 현재 compose는 `shared_buffers=512MB`, 컨테이너 `memory: 2g`로 확대되어 메모리 압박 근거 자체가 사라졌다.
2. **exec 계열 인코딩 위험군 제거** — 2026-08-01 jsonb 장애로 드러났듯 `exec`는 서버 describe 없이 클라이언트가 Go 타입만 보고 인코딩을 정하므로, `[]byte`→`jsonb` 같은 바인딩이 운영에서만 실패한다. `cache_statement`는 서버 describe 결과로 인코딩을 정해 이 위험군을 구조적으로 제거한다.
3. **스택 단일 모드 합류** — ChatBotGo·TwentyQ는 이미 `cache_statement`로 운영 중이다. hololive만 `exec`로 남아 있어 스택 전체에 모드별 인코딩 차이라는 이중 기준이 있었다.

성능 근거는 **여전히 중립**이다. A3 원 측정의 "top query mean sub-ms(0.027~0.43ms)"는 그대로여서 `cache_statement`의 개선 여지는 측정 한계 이하다. 즉 이 전환은 성능 최적화가 아니라 인코딩 정합성·스택 일관성 결정이다.

DDL invalidation 리스크는 이중으로 bounded다:

- pgx는 쿼리 오류 시 해당 SQL의 statement/description 캐시를 즉시 무효화한다(pgx v5.10.0 `rows.go:172-180`). 마이그레이션으로 stale해진 캐시는 첫 실패 1회로 자정된다.
- hololive pool은 `MaxConnLifetime=5m`(`hololive/hololive-shared/pkg/constants/network.go`) + jitter `lifetime/5=1m`(`shared-go/pkg/db/pgxdb/pool.go`의 `overlayLifetime`)이라 커넥션이 5~6분 내 전량 재생성된다. stale 캐시 노출 창의 상한이 여기서 한 번 더 닫힌다.

롤백: `POSTGRES_QUERY_EXEC_MODE=exec`를 env로 주입하면 즉시 되돌아간다(코드 변경 불필요). 단 `hololive/hololive-dbtest`의 테스트 pool 모드는 compose 기본값과 `TestProductionComposePinsTheTestPoolQueryExecMode`로 묶여 있어, 기본값을 되돌릴 때는 같은 커밋에서 함께 되돌려야 한다.

## A4 — skip scan / AIO 실효성 (측정 완료, 분류 확정)

캡처: `pg18-tracka-explain-capture-20260604.txt` (EXPLAIN ANALYZE/GENERIC_PLAN 5종).

| 쿼리 | 플랜 | 분류 |
|---|---|---|
| ClaimDue (`alarm_dispatch_deliveries`) | `idx_alarm_dispatch_deliveries_due` partial index, `Index Searches: 1` | 효과 있음(기존 인덱스 설계가 최적) |
| outbox claim CTE | 현재 release gate 기대값은 due-first partial index `idx_yno_pending_due_created_id` 사용이다. 과거 캡처의 `idx_yno_status_created`는 067 이전 기준이다. | 효과 있음 |
| delivery claim CTE | (재캡처) Seq Scan 61행 filter, 0.051ms — 테이블 극소라 seq가 최적, partial index는 규모 증가 시 발동 | 조건부 |
| members 조회 | Seq Scan 126행 + sort — 전체 조회라 정상 | 효과 없음(정상) |
| live-stream events | `idx_alarm_dispatch_events_live_stream_created` 사용, `Index Searches: 1` | 효과 있음 |

- **skip scan: 효과 없음(정상 결론)** — BIGSERIAL 단일 PK + 조건 내장 partial index 설계라 multicolumn skip 기회 자체가 없음. 인덱스 조정 마이그레이션 불필요.
- **AIO: 조건부** — 데이터셋이 shared_buffers에 상주해 read I/O 희소(EXPLAIN에 I/O Timings 미출력 = 전부 buffer hit). 데이터 증가로 disk read가 생기면 `pg_stat_io`로 재평가.
- PG18 `Index Searches: N` 라인 출력 확인 — skip scan 판정 경로 동작.

## A5 — PG18 신기능 적용성

| 기능 | 판정 | 사유 |
|---|---|---|
| `uuidv7()` | 불채택 | BIGSERIAL PK 위주, 교체 ROI 없음. 신규 시계열 테이블에만 후보 |
| virtual generated columns | 불채택 | `GENERATED ALWAYS AS` 사용처 0건 |
| temporal `WITHOUT OVERLAPS` | 후속 | `lock_expires_at` lease 겹침 방지를 앱단 처리 중 — 스키마 전환은 별도 플랜 필요 |
| `RETURNING old.*/new.*` | 후속 | ClaimDue CTE 감사/diff 단순화 후보, 명확 ROI 확인 시만 |

## 후속 항목 (비차단)

1. pool cap과 `max_connections=60`은 명시되었지만, 포화 근접 시 `POSTGRES_POOL_MAX_CONNS` 서비스별 추가 하향 또는 cap/max_connections 재검토가 필요하다.
2. AIO 파라미터(`io_workers`/`effective_io_concurrency`)는 disk read가 실제 발생할 때 `pg_stat_io` 기반 재조정.

## 롤백

`deploy/compose/docker-compose.prod.yml`의 holo-postgres `command:` 블록 git revert + `compose-redeploy-service.sh holo-postgres` recreate. 데이터 볼륨(`holo-pg-data`) 무관. extension은 잔존해도 무해(`shared_preload_libraries` 제거 시 자동 비활성).
