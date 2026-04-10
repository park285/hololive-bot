# Settlement-Go 독립 레포 분리 설계

## Overview

settlement-go를 hololive-bot 모노레포에서 `~/gemini/settlement-go/` 독립 레포로 완전 분리한다.
코드, DB, Docker 배포, shared 의존성 모두 분리 대상.

## 결정 사항

| 항목 | 결정 |
|------|------|
| 분리 방식 | 별도 레포 + shared 의존성 내재화 |
| 로컬 경로 | `~/gemini/settlement-go/` |
| DB | 같은 PostgreSQL 인스턴스(5433), 별도 `settlement` DB |
| DB 유저 | `settlement_runtime` (최소 권한) |
| 배포 | 자체 docker-compose.prod.yml + hololive-net external 네트워크 |
| 데이터 | pg_dump → restore로 이관, 시퀀스 리셋, hololive DB에서 DROP |
| Migration | SQL 파일 + init container (프레임워크 미도입) |

## 레포 구조

```
settlement-go/
├── cmd/settlement/
│   ├── main.go
│   ├── handler.go
│   ├── formatter.go
│   ├── main_test.go
│   └── server_test.go
├── internal/
│   ├── settlement/        # 기존 pkg/settlement → internal
│   │   ├── types.go
│   │   ├── repository.go
│   │   └── scheduler.go
│   ├── iris/              # settlement 전용 축소 클라이언트
│   │   └── client.go
│   ├── logging/           # 파일 로깅 + sanitize
│   │   ├── logging.go
│   │   └── sanitize.go
│   ├── server/            # WrapH2C
│   │   └── h2c.go
│   └── cache/             # 축소 interface (Exists/Set/Close)
│       └── cache.go
├── scripts/
│   ├── migrations/
│   │   └── 001_init.sql
│   └── migrate_data.sh
├── Dockerfile
├── docker-compose.prod.yml
├── go.mod
├── .env.example
└── .gitignore
```

## Shared 의존성 내재화

### 전략: 원본 복사가 아닌 축소 interface + 최소 구현 신규 작성

| 원본 패키지 | 내재화 전략 | 예상 규모 |
|------------|-----------|----------|
| `hololive-shared/pkg/iris` | settlement 전용 축소 `Client` interface (`SendMessage`만), h2c transport 직접 구현. `httputil`/`irisx` 의존 제거, 상수/경로 인라인. `shared-go/pkg/json` → `encoding/json` 대체 | ~150줄 |
| `hololive-shared/internal/logging` | `EnableFileLogging` + `SanitizeHandler` 전체 이식. `tint`/`go-isatty`/`lumberjack` 외부 의존 유지 (보안 로깅에 필수) | ~340줄 |
| `hololive-shared/pkg/server` | `WrapH2C` 1개 함수 복사 | ~20줄 |
| `hololive-shared/pkg/service/cache` | 2-method interface 정의 (`Exists`/`Set`), `valkey-go` 직접 사용하는 경량 구현체 신규 작성. 기존 30+ 메서드 `Client` interface 사용 안 함 | ~60줄 |
| `shared-go/pkg/json` | 삭제 — `encoding/json` stdlib 대체 (sonic wrapper이므로 호환됨) | 0줄 |

### go.mod 외부 의존성 (예상)

```
github.com/jackc/pgx/v5         # PostgreSQL
github.com/valkey-io/valkey-go   # Valkey cache
github.com/lmittmann/tint        # slog 컬러 핸들러
github.com/mattn/go-isatty       # TTY 감지
golang.org/x/net                 # h2c, http2
gopkg.in/natefinch/lumberjack.v2 # 로그 로테이션
```

총 6개 direct 의존성 (기존 60+ indirect에서 대폭 축소).

### 코드 변경 상세

**iris client**: `handler.go`의 `webhookRequest` struct를 iris 패키지로 통합하지 않음 (cmd 레벨 유지). `iris.Client` interface는 `SendMessage` 1개 메서드만 정의. `H2CClient` 구현에서 `httputil.NewClient` → `&http.Client{}` 직접 생성, `httputil.CheckStatus` → 인라인 status check, `irisx.PathReply`/`irisx.HeaderBotToken` → 상수 인라인.

**cache**: `scheduler.go`의 `cache.Client` 타입 참조를 settlement 전용 축소 interface로 변경.
```go
type CacheClient interface {
    Exists(ctx context.Context, key string) (bool, error)
    Set(ctx context.Context, key string, value any, ttl time.Duration) error
    Close() error
}
```

**main.go 기본값 변경**: `POSTGRES_DB` 기본값 `"hololive"` → `"settlement"`, `POSTGRES_USER` 기본값 `"hololive_runtime"` → `"settlement_runtime"`.

## DB 분리

### 새 DB/유저 생성 (순서 주의)

```sql
-- 1. 유저 생성 먼저
CREATE USER settlement_runtime WITH PASSWORD '...';
-- 2. DB 생성 (유저를 OWNER로 지정)
CREATE DATABASE settlement OWNER settlement_runtime;
-- 3. 권한 부여
GRANT ALL PRIVILEGES ON DATABASE settlement TO settlement_runtime;
```

### DDL (`001_init.sql`)

기존 `038_create_settlement.sql`과 동일한 스키마. seed INSERT 제외.
seed는 `migrate_data.sh`의 `pg_dump --data-only`로 이관되므로 별도 불필요.
개발/스테이징 환경용 seed는 `scripts/seed.sql`로 별도 제공.

### 데이터 이관 (`migrate_data.sh`)

```bash
#!/usr/bin/env bash
set -euo pipefail

# 설정
SRC_DB="${SRC_DB:-hololive}"
DST_DB="${DST_DB:-settlement}"
PG_HOST="${PG_HOST:-localhost}"
PG_PORT="${PG_PORT:-5433}"
BACKUP_DIR="/tmp/settlement_migration_$(date +%Y%m%d_%H%M%S)"

mkdir -p "$BACKUP_DIR"

TABLES="settlement_members settlement_cycles settlement_payments"

# 1. 백업 (롤백용)
echo "=== 백업 시작 ==="
pg_dump -h "$PG_HOST" -p "$PG_PORT" -d "$SRC_DB" \
  -t settlement_members -t settlement_cycles -t settlement_payments \
  > "$BACKUP_DIR/full_backup.sql"

# 2. data-only dump
echo "=== 데이터 추출 ==="
pg_dump -h "$PG_HOST" -p "$PG_PORT" -d "$SRC_DB" --data-only \
  -t settlement_members -t settlement_cycles -t settlement_payments \
  > "$BACKUP_DIR/data.sql"

# 3. DDL 적용
echo "=== DDL 적용 ==="
psql -h "$PG_HOST" -p "$PG_PORT" -d "$DST_DB" \
  -f scripts/migrations/001_init.sql

# 4. 데이터 복원
echo "=== 데이터 이관 ==="
psql -h "$PG_HOST" -p "$PG_PORT" -d "$DST_DB" \
  -f "$BACKUP_DIR/data.sql"

# 5. 시퀀스 리셋
echo "=== 시퀀스 리셋 ==="
for tbl in $TABLES; do
  psql -h "$PG_HOST" -p "$PG_PORT" -d "$DST_DB" -c \
    "SELECT setval('${tbl}_id_seq', COALESCE((SELECT MAX(id) FROM ${tbl}), 0));"
done

# 6. 검증
echo "=== 검증 ==="
for tbl in $TABLES; do
  src=$(psql -h "$PG_HOST" -p "$PG_PORT" -d "$SRC_DB" -t -c "SELECT COUNT(*) FROM ${tbl};")
  dst=$(psql -h "$PG_HOST" -p "$PG_PORT" -d "$DST_DB" -t -c "SELECT COUNT(*) FROM ${tbl};")
  if [ "$(echo "$src" | tr -d ' ')" != "$(echo "$dst" | tr -d ' ')" ]; then
    echo "FAIL: ${tbl} row count mismatch (src=${src}, dst=${dst})"
    echo "백업 위치: $BACKUP_DIR/full_backup.sql"
    exit 1
  fi
  echo "OK: ${tbl} (${src} rows)"
done

echo "=== 이관 완료. 백업: $BACKUP_DIR ==="
echo "hololive DB에서 DROP은 수동으로 실행하세요."
```

## Docker 배포

### settlement-go/docker-compose.prod.yml

- `hololive-net` external 네트워크로 postgres/valkey 접근
- `settlement-db-migrate`: init container (psql로 001_init.sql 실행)
- `settlement-bot`: 앱 컨테이너 (port 30002)
- 기존 YAML anchor (`*app-file-log-env`, `*security-hardening`, `*default-logging`) 인라인 복사
- `depends_on` 대신 앱 레벨 connection retry (postgres/valkey가 외부 compose에 존재)
- Valkey socket: `valkey-cache-socket` external volume 참조

### Dockerfile 단순화

- `COPY shared-go`, `COPY hololive/hololive-shared` 제거
- 단일 모듈: `COPY go.mod go.sum ./` → `go mod download` → `COPY . .` → `go build ./cmd/settlement`

## hololive-bot 정리

- `docker-compose.prod.yml`에서 settlement-bot 서비스 블록 삭제
- `hololive/settlement-go/` 디렉토리 삭제 (빌드 바이너리 포함)
- `scripts/migrations/039_drop_settlement.sql` 추가 (DROP TABLE 3개)
- Iris 라우팅의 settlement-bot 경로는 외부 네트워크로 유지

## 실행 순서

1. `~/gemini/settlement-go/` 레포 초기화 (git init) + 코드 이식
2. shared 의존성 내재화 (축소 interface 신규 작성) + `encoding/json` 대체
3. go.mod 정리 (replace directive 제거, 불필요 의존성 삭제)
4. `main.go` 기본값 변경 (`POSTGRES_DB=settlement`, `POSTGRES_USER=settlement_runtime`)
5. 로컬 빌드/테스트 통과 확인
6. Dockerfile + docker-compose.prod.yml + .env.example 작성
7. settlement DB 생성 + 유저 생성
8. 001_init.sql로 DDL 적용
9. migrate_data.sh로 데이터 이관 + 시퀀스 리셋 + 자동 검증
10. settlement-go docker-compose 배포 테스트
11. hololive-bot에서 settlement-bot 서비스 제거 + settlement-go 디렉토리 삭제
12. hololive DB에서 settlement 테이블 DROP (039 migration)

## 롤백

| 실패 단계 | 복구 방법 |
|----------|----------|
| 1~6 | settlement-go 디렉토리 삭제 (hololive-bot 미변경) |
| 7~9 | `migrate_data.sh`가 생성한 백업(`$BACKUP_DIR/full_backup.sql`)에서 복원, settlement DB DROP |
| 10 | docker-compose down |
| 11 | docker-compose.prod.yml + settlement-go 디렉토리 git revert |
| 12 | 백업에서 settlement 테이블 복원 |
