# Phase 0: 인프라 준비

> [← 메인 계획서](IMPLEMENTATION_PLAN.md)

## 1. DB User 생성

### 1.1 init-db 스크립트 수정

**파일**: `hololive-kakao-bot-go/scripts/init-db/01-create-hololive-db.sh`

기존 스크립트 상단 변수 블록에 scraper 유저 추가:

```bash
# 추가할 변수 (HOLOLIVE_MIGRATOR_USER 선언부 아래)
HOLOLIVE_SCRAPER_USER="${HOLOLIVE_SCRAPER_USER:-hololive_scraper}"
HOLOLIVE_SCRAPER_PASSWORD="${HOLOLIVE_SCRAPER_PASSWORD:-$DB_PASSWORD_FALLBACK}"
```

기존 `psql` HEREDOC 내부에 아래 블록 추가 (hololive_migrator 생성 직후):

```sql
-- hololive_scraper: scraper 전용 (최소 권한)
SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'hololive_scraper', :'hololive_scraper_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'hololive_scraper') \gexec

SELECT format(
  'ALTER ROLE %I WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION INHERIT PASSWORD %L',
  :'hololive_scraper', :'hololive_scraper_password'
) \gexec
```

`GRANT CONNECT` 행에 scraper 유저 추가:

```sql
SELECT format('GRANT CONNECT ON DATABASE %I TO %I, %I, %I',
  :'hololive_db', :'hololive_user', :'hololive_migrator', :'hololive_scraper') \gexec
```

hololive DB 연결 후 schema 권한:

```sql
SELECT format('GRANT USAGE ON SCHEMA public TO %I', :'hololive_scraper') \gexec
```

`psql` 호출 시 `--set` 파라미터 추가:

```bash
--set=hololive_scraper="${HOLOLIVE_SCRAPER_USER}" \
--set=hololive_scraper_password="${HOLOLIVE_SCRAPER_PASSWORD}" \
```

`docker-compose.prod.yml` `holo-postgres` 서비스의 `environment`에 추가:

```yaml
HOLOLIVE_SCRAPER_USER: ${HOLOLIVE_SCRAPER_USER:-hololive_scraper}
```

### 1.2 마이그레이션 035

**파일**: `hololive-kakao-bot-go/scripts/migrations/035_add_scraper_user.sql`

```sql
-- 035: hololive_scraper DB user 권한 설정
-- 목적: Rust scraper 서비스 전용 최소 권한 DB 유저
-- 대상 테이블: major_events (R/W), major_event_subscriptions (R)

DO $$
BEGIN
    -- major_events: INSERT, UPDATE, SELECT (scraper가 upsert + expired update + link check)
    EXECUTE format(
        'GRANT SELECT, INSERT, UPDATE ON TABLE major_events TO %I',
        current_setting('app.scraper_user', true)
    );

    -- major_events_id_seq: USAGE (INSERT 시 SERIAL 채번)
    EXECUTE format(
        'GRANT USAGE, SELECT ON SEQUENCE major_events_id_seq TO %I',
        current_setting('app.scraper_user', true)
    );

    -- major_event_subscriptions: SELECT only (scraper는 구독 정보 읽기만)
    EXECUTE format(
        'GRANT SELECT ON TABLE major_event_subscriptions TO %I',
        current_setting('app.scraper_user', true)
    );

EXCEPTION WHEN undefined_object THEN
    RAISE NOTICE 'scraper user not found, skipping GRANT (run init-db first)';
END $$;
```

> 주의: 신규 배포 시 init-db가 먼저 실행되므로 role이 이미 존재한다.
> 기존 환경에서 마이그레이션만 실행하는 경우 `app.scraper_user` GUC 또는 직접 `hololive_scraper`를 하드코딩할 수 있다.
> 실제 적용 시 `apply-all.sh`에서 `PGUSER` 설정과 GUC 전달 방식을 결정해야 한다.

### 1.3 기존 DB 환경 bootstrap + migration (운영 반영)

기존 볼륨에서는 `/docker-entrypoint-initdb.d`가 재실행되지 않으므로,
`hololive_scraper` role 누락 시 scraper가 `password authentication failed`로 degraded 상태가 될 수 있다.

이를 방지하기 위해 `hololive-db-migrate`는 아래 순서로 동작하도록 반영한다:

1. `/migrations/bootstrap-and-apply.sh`
   - admin 계정으로 `hololive_scraper` role 생성/갱신
   - `GRANT CONNECT`, `GRANT USAGE ON SCHEMA public`
2. `/migrations/apply-all.sh`
   - 기존 SQL migration(035 포함) 실행

관련 파일:
- `hololive-kakao-bot-go/scripts/migrations/bootstrap-and-apply.sh`
- `docker-compose.prod.yml` (`hololive-db-migrate` command)

**간소화 대안** (하드코딩 버전 -- 권장):

```sql
-- 035: hololive_scraper DB user 권한 설정
-- Role이 없으면 GRANT 실패하므로 init-db 실행이 선행 조건

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT SELECT, INSERT, UPDATE ON TABLE major_events TO hololive_scraper;
        GRANT USAGE, SELECT ON SEQUENCE major_events_id_seq TO hololive_scraper;
        GRANT SELECT ON TABLE major_event_subscriptions TO hololive_scraper;
    ELSE
        RAISE NOTICE 'Role hololive_scraper does not exist, skipping GRANT';
    END IF;
END $$;

-- Rollback:
-- REVOKE ALL PRIVILEGES ON TABLE major_events FROM hololive_scraper;
-- REVOKE ALL PRIVILEGES ON SEQUENCE major_events_id_seq FROM hololive_scraper;
-- REVOKE ALL PRIVILEGES ON TABLE major_event_subscriptions FROM hololive_scraper;
```

## 2. Cargo Workspace Scaffold

### 2.1 디렉토리 구조

```
hololive-scraper-rs/
  IMPLEMENTATION_PLAN.md
  Cargo.toml                    # workspace root
  rust-toolchain.toml
  config.toml                   # 런타임 설정 (환경변수 override)
  Dockerfile
  crates/
    scraper-core/
      Cargo.toml
      src/
        lib.rs
        model.rs                # MajorEvent, enums
        error.rs                # thiserror 에러 타입
    scraper-service/
      Cargo.toml
      src/
        lib.rs
        rss_parser.rs           # RSS XML 파싱
        date_extractor.rs       # 날짜 추출 (CRITICAL)
        scraper.rs              # ScrapeAndStore 파이프라인
        link_checker.rs         # HEAD/GET 링크 검증
        scheduler.rs            # tokio 기반 스케줄러
      testdata/
        events_feed.xml         # Go testdata 복사
        supernova_reboot_real.html
    scraper-infra/
      Cargo.toml
      src/
        lib.rs
        repository.rs           # sqlx 쿼리
        config.rs               # TOML + env 설정 로드
    scraper-app/
      Cargo.toml
      src/
        main.rs                 # 진입점 (axum health + scheduler)
```

### 2.2 Workspace Cargo.toml

**파일**: `hololive-scraper-rs/Cargo.toml`

```toml
[workspace]
resolver = "3"
members = [
    "crates/scraper/core",
    "crates/scraper/service",
    "crates/scraper/infra",
    "crates/scraper/app",
]

[workspace.package]
edition = "2024"
license = "MIT"

[workspace.dependencies]
# async runtime
tokio = { version = "1.44", features = ["full"] }
tokio-util = { version = "0.7", features = ["rt"] }

# http
reqwest = { version = "0.12", default-features = false, features = [
    "rustls-tls", "gzip", "brotli", "socks", "http2"
] }
axum = { version = "0.8", features = ["macros"] }
tower-http = { version = "0.6", features = ["trace", "cors"] }

# db
sqlx = { version = "0.8", features = [
    "runtime-tokio", "tls-rustls", "postgres", "chrono", "macros"
] }

# serialization
serde = { version = "1.0", features = ["derive"] }
serde_json = "1.0"
quick-xml = { version = "0.37", features = ["serialize"] }

# parsing
regex = "1.11"
# NOTE: `scraper` HTML crate (0.22) 는 Phase 0~2에서 미사용.
# 추가 시 workspace crate명(scraper-core 등)과 충돌하지 않도록 rename 필요:
# html-scraper = { package = "scraper", version = "0.22" }
unicode-normalization = "0.1"

# time
chrono = { version = "0.4", features = ["serde"] }
chrono-tz = "0.10"

# config
config = "0.15"
clap = { version = "4.5", features = ["derive", "env"] }

# observability
tracing = "0.1"
tracing-subscriber = { version = "0.3", features = ["env-filter", "json"] }
opentelemetry = "0.28"

# error handling
thiserror = "2.0"
anyhow = "1.0"

# utils
url = "2.5"
rand = "0.9"

# workspace internal
scraper-core = { path = "crates/scraper/core" }
scraper-service = { path = "crates/scraper/service" }
scraper-infra = { path = "crates/scraper/infra" }
```

### 2.3 Crate별 Cargo.toml

**scraper-core/Cargo.toml**:
```toml
[package]
name = "scraper-core"
version = "0.1.0"
edition.workspace = true

[dependencies]
chrono = { workspace = true }
serde = { workspace = true }
sqlx = { workspace = true }
thiserror = { workspace = true }
```

**scraper-service/Cargo.toml**:
```toml
[package]
name = "scraper-service"
version = "0.1.0"
edition.workspace = true

[dependencies]
scraper-core = { workspace = true }
scraper-infra = { workspace = true }
tokio = { workspace = true }
tokio-util = { workspace = true }
reqwest = { workspace = true }
quick-xml = { workspace = true }
serde = { workspace = true }
regex = { workspace = true }
unicode-normalization = { workspace = true }
chrono = { workspace = true }
chrono-tz = { workspace = true }
tracing = { workspace = true }
thiserror = { workspace = true }

[dev-dependencies]
tokio = { workspace = true, features = ["test-util"] }
```

**scraper-infra/Cargo.toml**:
```toml
[package]
name = "scraper-infra"
version = "0.1.0"
edition.workspace = true

[dependencies]
scraper-core = { workspace = true }
sqlx = { workspace = true }
chrono = { workspace = true }
config = { workspace = true }
serde = { workspace = true }
tracing = { workspace = true }
thiserror = { workspace = true }
```

**scraper-app/Cargo.toml**:
```toml
[package]
name = "scraper-app"
version = "0.1.0"
edition.workspace = true

[dependencies]
scraper-core = { workspace = true }
scraper-service = { workspace = true }
scraper-infra = { workspace = true }
tokio = { workspace = true }
tokio-util = { workspace = true }
axum = { workspace = true }
tower-http = { workspace = true }
reqwest = { workspace = true }
sqlx = { workspace = true }
chrono = { workspace = true }
clap = { workspace = true }
tracing = { workspace = true }
tracing-subscriber = { workspace = true }
serde = { workspace = true }
```

### 2.4 rust-toolchain.toml

**파일**: `hololive-scraper-rs/rust-toolchain.toml`

```toml
[toolchain]
channel = "nightly"
components = ["rustfmt", "clippy"]
targets = ["x86_64-unknown-linux-musl"]
```

## 3. Docker Integration

### 3.1 docker-compose.prod.yml 추가 서비스

`hololive-bot` 서비스 아래에 추가:

```yaml
  hololive-scraper:
    image: hololive-scraper-rs:prod
    build:
      context: .
      dockerfile: hololive-scraper-rs/Dockerfile
    container_name: hololive-scraper-rs
    profiles: ["hololive"]
    restart: always
    labels:
      deunhealth.restart.on.unhealthy: "true"
    environment:
      SCRAPER__DATABASE__HOST: holo-postgres
      SCRAPER__DATABASE__PORT: ${POSTGRES_PORT:-5432}
      SCRAPER__DATABASE__NAME: hololive
      SCRAPER__DATABASE__USER: ${HOLOLIVE_SCRAPER_USER:-hololive_scraper}
      SCRAPER__DATABASE__PASSWORD: ${DB_PASSWORD}
      SCRAPER__DATABASE__SSLMODE: disable
      SCRAPER__DATABASE__MAX_CONNECTIONS: 5
      SCRAPER__PROXY__SOCKS5_URL: socks5://vpn-scraper-proxy:1080
      SCRAPER__SCHEDULER__SCRAPE_HOUR_KST: 6
      SCRAPER__HEALTH__PORT: 30010
      RUST_LOG: info,scraper_service=debug,sqlx=warn
      TZ: Asia/Seoul
      # OpenTelemetry
      OTEL_ENABLED: "true"
      OTEL_SERVICE_NAME: hololive-scraper-rs
      OTEL_EXPORTER_OTLP_ENDPOINT: jaeger:4317
      OTEL_EXPORTER_OTLP_INSECURE: "true"
      OTEL_SAMPLE_RATE: "0.1"
    ports:
      - "127.0.0.1:30010:30010"
    depends_on:
      holo-postgres:
        condition: service_healthy
      hololive-db-migrate:
        condition: service_completed_successfully
      vpn-scraper-proxy:
        condition: service_healthy
    deploy:
      resources:
        limits:
          memory: 128m
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "-T", "5", "http://localhost:30010/health"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 10s
    logging: *default-logging
    <<: *security-hardening
    networks:
      - llm-bot-net
```

### 3.2 Dockerfile (multi-stage, cargo-chef)

**파일**: `hololive-scraper-rs/Dockerfile`

```dockerfile
# syntax=docker/dockerfile:1.7

# Stage 1: Chef (dependency cache layer)
FROM rustlang/rust:nightly-alpine AS chef

RUN apk add --no-cache musl-dev pkgconfig openssl-dev && \
    cargo install cargo-chef --locked

WORKDIR /app

# Stage 2: Planner (dependency recipe)
FROM chef AS planner
COPY hololive-scraper-rs/ .
RUN cargo chef prepare --recipe-path recipe.json

# Stage 3: Builder (cached dependency build + app build)
FROM chef AS builder

COPY --from=planner /app/recipe.json recipe.json
RUN cargo chef cook --release --recipe-path recipe.json --target x86_64-unknown-linux-musl

COPY hololive-scraper-rs/ .
RUN cargo build --release --target x86_64-unknown-linux-musl --bin scraper-app && \
    strip /app/target/x86_64-unknown-linux-musl/release/scraper-app

# Stage 4: Runtime (minimal)
FROM alpine:3.23

RUN apk add --no-cache ca-certificates tini tzdata wget

ENV TZ=Asia/Seoul

RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

WORKDIR /app

COPY --from=builder --link --chown=appuser:appuser \
    /app/target/x86_64-unknown-linux-musl/release/scraper-app ./bin/scraper

EXPOSE 30010

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q --spider http://127.0.0.1:30010/health || exit 1

USER appuser

ENTRYPOINT ["/sbin/tini", "--"]
CMD ["./bin/scraper"]
```

### 3.3 Health Endpoint Spec

- **Path**: `GET /health`
- **Port**: 30010
- **Response 200**:
  ```json
  {
    "status": "ok",
    "version": "0.1.0",
    "db_connected": true,
    "last_scrape_at": "2026-02-23T06:00:00+09:00",
    "next_scrape_at": "2026-02-24T06:00:00+09:00"
  }
  ```
- **Response 503**: DB 미연결 또는 scheduler 미시작

## 4. CI Pipeline

GitHub Actions workflow (`.github/workflows/scraper-rs.yml`):

```yaml
name: hololive-scraper-rs

on:
  push:
    paths: ['hololive-scraper-rs/**']
  pull_request:
    paths: ['hololive-scraper-rs/**']

jobs:
  check:
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: hololive-scraper-rs
    steps:
      - uses: actions/checkout@v4
      - uses: dtolnay/rust-toolchain@nightly
        with:
          components: rustfmt, clippy
      - uses: Swatinem/rust-cache@v2
        with:
          workspaces: hololive-scraper-rs
      - name: fmt
        run: cargo fmt --all -- --check
      - name: clippy
        run: cargo clippy --all-targets --all-features -- -D warnings
      - name: test
        run: cargo test --all
      - name: build
        run: cargo build --release
```

## 5. Phase 0 체크리스트

- [ ] `01-create-hololive-db.sh` 에 `hololive_scraper` role 생성 로직 추가
- [ ] `docker-compose.prod.yml` `holo-postgres` environment에 `HOLOLIVE_SCRAPER_USER` 추가
- [ ] `scripts/migrations/035_add_scraper_user.sql` 작성 및 `apply-all.sh` 적용 확인
- [ ] `hololive-scraper-rs/` 디렉토리 및 Cargo workspace 생성
- [ ] `rust-toolchain.toml` 설정 (nightly)
- [ ] 4개 crate skeleton (`scraper-core`, `scraper-service`, `scraper-infra`, `scraper-app`)
- [ ] `cargo build` 성공 확인
- [ ] `Dockerfile` 작성 (multi-stage, cargo-chef, musl static)
- [ ] `docker-compose.prod.yml` `hololive-scraper` 서비스 추가
- [ ] `docker compose build hololive-scraper` 성공 확인
- [ ] Health endpoint (`GET /health` on :30010) 동작 확인
- [ ] CI workflow `.github/workflows/scraper-rs.yml` 작성
- [ ] `cargo fmt`, `cargo clippy`, `cargo test` 모두 통과
