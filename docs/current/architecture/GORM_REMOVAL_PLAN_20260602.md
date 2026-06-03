# GORM 제거 플랜 (2026-06-02)

> 관련: `REFACTORING_PLAN_20260602.md`(P1/P2 domain persistence 누수), cross-cutting 마스터 `iris-stack/docs/REFACTORING_PLAN_20260602.md`
> 범위: **hololive-bot 전용**(cbgk·iris-client-go·shared-go는 GORM 미사용 — 검증). 데이터 접근을 `pgx`로 단일화.

## 0. 권고 (TL;DR)

**제거는 기술적으로 깨끗하고 타당하나, P0가 아니며 단계적으로 진행해야 합니다.** 근거:

- **고급 ORM 기능 미사용** — `Preload` 0, `Association` 0, `gorm.DeletedAt`(soft delete) 0, hook(BeforeCreate 등) 0. 이식이 어려운 부분이 아예 없습니다.
- **이미 절반이 raw SQL** — `Exec` 60 + `Scan` 57 + `Raw` 7. GORM은 사실상 query-builder + row scanner + tx + `ON CONFLICT` 편의 계층일 뿐입니다.
- **pgx가 이미 기반** — `dbx.Client`가 `pgxpool.Pool` + `sql.DB` + `gorm.DB`를 함께 보유(`gorm.DB`는 `stdlib.OpenDBFromPool`로 파생). GORM은 위에 얹힌 층이라 걷어내면 됩니다.
- **숨은 이득**: prod 스키마는 SQL migration(`scripts/migrations/*.sql`)이 SSOT인데 **test 스키마만 GORM `AutoMigrate`(struct tag)** 로 만들어집니다 — 두 정의가 drift할 수 있는 잠재 버그. 제거하면 test도 실제 migration SQL로 통일됩니다.
- (주의) **실제 비용은 test 스키마 전환** — `AutoMigrate` 호출 ~44개 test 파일. 이 test-schema 경로를 먼저 해결하지 않으면 repository 이식을 시작할 수 없습니다. **이것이 진짜 블로커이자 최대 작업량**입니다.
- (주의) **가치는 중간** — GORM이 버그를 일으키고 있지 않습니다. 이득은 의존성 1개 제거, dbx 단순화, 명시적 SQL, 단일 데이터 접근 패턴, test/prod 스키마 통일입니다. 긴급하지 않습니다.

**결론**: 두 목표를 분리해 권고합니다.
1. **(권장, 저위험) domain 순수화** — entity struct + `database/sql` import를 `pkg/domain`에서 분리. GORM 제거와 무관하게 가치 있음.
2. **(선택, 중비용) 전체 GORM→pgx 이식** — feasible하나 ~50 repository 파일 + ~44 test 파일. "단일 데이터 접근 패턴/의존성 축소"를 원할 때 Wave 5로 단계 진행.

## 1. 측정된 footprint (2026-06-02)

| 항목 | 값 |
|---|---|
| `gorm.io/gorm` import (non-test) | hololive-shared **50**, youtube-producer 6, alarm-worker 2, bot/admin-api/llm-sched 0 |
| 집중도 | hololive-shared `pkg/service/*` **46** / `internal/dbx` 3 / `pkg/repository` 1 |
| 쿼리 스타일 (non-test) | `Exec` 60, `Scan` 57, `Where` 37, `Find` 22, `First` 21, `Model` 19, `Create` 19, `Raw` 7, `Updates` 6, `Delete` 3, `Save` 2 |
| `ON CONFLICT`/`Clauses` | `clause.OnConflict` 8, `.Clauses(` 6 |
| `gorm.Expr` | 19 (UPDATE 식, 예: `attempt_count = attempt_count + 1`) |
| `Migrator().HasTable` | 9 (youtube tiering 등) |
| `AutoMigrate` | **test 전용** (non-test는 `dbx/migrate.go` 정의 + `doc.go`뿐, 호출 0) |
| 고급 기능 | Preload 0, Association 0, DeletedAt 0, hook 0, Joins 2 |
| domain 오염 | `gorm:` tag 182, `TableName()` 21, `database/sql`·`driver` import 3파일 |
| tx | `.Transaction(` 12 (panic→rollback은 GORM이 보장) |

## 2. 목표 아키텍처

```
runtime → repository(pgx) → dbx.Client{ pool *pgxpool.Pool }   // gorm.DB, sql.DB 제거
                              └ persistence/entity (db struct + 컬럼 매핑)
pkg/domain → 순수 value object/interface (gorm tag·database/sql 없음)
test → dbtest.ApplyMigrations(pool, scripts/migrations/*.sql)   // AutoMigrate 대체
```

- **row scanning**: `github.com/georgysavva/scany/v2/pgxscan` 권장 — 새 ORM 도입 없이 기존 struct로 `Get`/`Select` scan. (대안: `sqlc`로 컴파일타임 타입 SQL — 더 강하나 도입 비용↑. 신규 ORM은 배제.)
- **dbx.Client**: `gorm.DB`/`sql.DB`/`gorm.io/driver/postgres`/`gormlogger` 제거, `pgxpool.Pool`만 유지. `QueryExecMode`·DSN 마스킹·DNS fallback·double-close 안전성은 그대로.

## 3. 진짜 블로커: test 스키마 (Phase 0에서 선결)

현재 ~44개 test가 `db.AutoMigrate(&domain.X{}, ...)`로 struct tag에서 스키마를 만듭니다. prod는 `scripts/migrations/*.sql`(60+개)이 SSOT입니다. **GORM 제거 = AutoMigrate 소멸**이므로, 그 전에 test가 실제 migration SQL을 적용하도록 바꿔야 합니다.

- `internal/dbx/dbtest`(또는 기존 testutil)에 `ApplyMigrations(ctx, pool, dir)` 추가 — `scripts/migrations/*.sql`를 번호순 적용. (testcontainers-postgres 또는 기존 test DB 모두 호환.)
- 부수효과(이득): test 스키마가 prod migration과 **동일**해져 tag-vs-SQL drift가 사라짐.
- 선결 확인: migration 디렉터리가 `idempotent`하게 재적용 가능한지(IF NOT EXISTS 등) — 대부분 그러함. seed migration(`012-seed-all-templates.sql` 등)은 test fixture와 충돌 가능 → test용 schema-only subset 분리 검토.

## 4. GORM → pgx 변환 표

| GORM | pgx(+pgxscan) 대체 |
|---|---|
| `db.Transaction(fn)` | `pgx.BeginTxFunc(ctx, pool, opts, fn)` (rollback-on-error 자동; panic은 명시 `defer tx.Rollback`) |
| `db.Create(&x)` | `pool.Exec(ctx, "INSERT ... VALUES ($1,...)", ...)` |
| `db.Clauses(clause.OnConflict{...}).Create(&x)` | `INSERT ... ON CONFLICT (...) DO UPDATE/NOTHING` raw |
| `db.Where(...).Find(&xs)` | `pgxscan.Select(ctx, pool, &xs, sql, args...)` |
| `db.First(&x)` / `.Take` | `pgxscan.Get(ctx, pool, &x, sql, args...)` (`pgx.ErrNoRows` 처리) |
| `db.Model(&x).Updates(...)` | `UPDATE ... SET ... WHERE ...` raw |
| `db.Exec(sql, args)` | `pool.Exec(ctx, sql, args...)` (거의 동일) |
| `db.Raw(sql).Scan(&x)` | `pgxscan.Get/Select` |
| `gorm.Expr("c + ?", n)` | raw SQL 식으로 인라인 |
| `db.Migrator().HasTable(t)` | `information_schema` 쿼리, 또는 제거(migration 후 존재 가정) |
| `AutoMigrate(...)` (test) | `dbtest.ApplyMigrations` (Phase 0) |
| `gorm:"column:..."` tag | `db:"..."` tag(pgxscan) 또는 명시 컬럼 목록 |

## 5. 단계별 플랜

**Phase 0 — test 스키마 + 파일럿 (선결)**
- `dbtest.ApplyMigrations` 도입(§3). 한 개 작은 repository(예: `pkg/service/settings` 또는 `acl`)를 파일럿으로 pgxscan 이식, 기존 test 통과 확인. pgxscan vs sqlc 최종 결정.

**Phase 1 — domain 순수화 (GORM 제거와 독립적, 권장 선행)**
- `pkg/domain`의 entity struct(YouTube*, Notification*, MajorEvent 등 §footprint)를 `pkg/persistence/entity`(또는 서비스별 internal)로 이동. domain엔 순수 타입만. `AlarmTypes.Value()/Scan()`·`database/sql` import 분리.
- 이 단계만으로도 마스터 P1/P2(domain persistence 누수)를 해소.

**Phase 2 — repository 이식 (모듈 단위, 각 PR + test)**
- 의존도 낮은 순: `settings`/`acl`/`member`/`template` → `auth` → `alarm`+`dispatchoutbox` → `notification`+`delivery` → `youtube/{stats,tracking,poller/batchrepo,outbox/delivery}`.
- 각 repository를 pgxscan으로 교체, `clause.OnConflict`→raw `ON CONFLICT`, `gorm.Expr`→raw. `SKIP LOCKED`/CTE 쿼리는 이미 raw라 변경 최소.

**Phase 3 — dbx 정리 + 의존성 제거**
- `dbx.Client`에서 `gorm.DB`/`sql.DB` 제거(`pgxpool.Pool`만). `Migrator().HasTable`(9곳) 제거/대체. `go.mod`에서 `gorm.io/gorm`, `gorm.io/driver/postgres` 제거. `go mod tidy`.

**Phase 4 — 검증**
- 전 모듈 `go test ./...` + 통합 test(dispatcher exactly-once 등). prod 스모크는 별도 배포 승인 하에.

## 6. 위험 & 완화

| 위험 | 완화 |
|---|---|
| NULL/scan 의미 차이 (GORM zero-value vs pgx `pgtype`) | nullable 컬럼은 `*T`/`pgtype.*`로 명시. 파일럿에서 패턴 확정 |
| tx rollback-on-panic (GORM 무상 제공) | `pgx.BeginTxFunc` 사용 + repository에 `defer recover→rollback` 규약 |
| `ON CONFLICT` 번역 누락 | 8곳 `clause.OnConflict`를 raw로 1:1 이전, 각 test로 고정 |
| `gorm.Expr` UPDATE 식 (19곳) | raw SQL로 직역, 동시성(`attempt_count+1`) 의미 보존 확인 |
| test 스키마 drift | Phase 0가 오히려 교정(prod migration 적용) |
| 대규모 동시 변경 | 모듈 단위 PR, 각 독립 배포 가능 |

## 7. 노력 · 시퀀싱

- 마스터 로드맵 기준 **Wave 5(장기·단계적)**. P0/P1(jamo DoS, QueuedPool recover, alarm 전달, 계약 SSOT)을 선행.
- 개략 규모: Phase 0~1 = Small-Medium(파일럿+entity 이동), Phase 2 = 모듈별 Medium ×7, Phase 3~4 = Small.
- 각 submodule 자체 remote 커밋 후 meta-repo SHA bump.

## 8. 결정 포인트 (사용자 확인 필요)

1. **목표 범위**: domain 순수화만(권장 최소) vs 전체 GORM 제거(Wave 5)?
2. **scanner**: `pgxscan`(점진·저비용, 권장) vs `sqlc`(컴파일타임 타입, 비용↑)?
3. **test 스키마**: 기존 test DB + migration 적용 vs testcontainers 전환?
4. **착수 시점**: 지금(P0 이전) vs P0/P1 Wave 1~2 완료 후?
