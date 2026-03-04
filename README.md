# Hololive Bot

홀로라이브 VTuber 알림/관리 플랫폼. KakaoTalk 챗봇을 통해 배신 알림, 스트림 상태, 멤버 뉴스 등을 제공합니다.

## 아키텍처

하이브리드 구조 (Rust + Go):

| 영역 | 언어 | 역할 |
|------|------|------|
| Compute 집약 | Rust | RSS scraper, VTuber alarm checker, dispatcher |
| Network 집약 | Go | Bot, stream-ingester, admin, llm-sched |

데이터 흐름: `webhook → handler → service → repository → PostgreSQL/Valkey`

알림 흐름: `scraper-rs LPUSH alarm:dispatch:queue → dispatcher BRPOP → Iris(Redroid) → KakaoTalk`

## 모듈 구조

### Go 모듈 (6개, go.work)
| 모듈 | 역할 | 포트 |
|------|------|------|
| `hololive-kakao-bot-go` | Main bot (webhook + command routing) | 30001 |
| `hololive-admin` | Admin service | 30002 |
| `hololive-llm-sched` | LLM scheduler (major event + member news + delivery) | 30003 |
| `hololive-stream-ingester` | YouTube/Holodex/Chzzk/Twitch polling + stats | 30004 |
| `hololive-shared` | Shared Go library (hololive domain) | - |
| `shared-go` | Shared Go utilities (errors, stringutil, valkeyx, etc.) | - |

### Rust crates (14개, Cargo workspace)
```
crates/
├── alarm/       (core, service, infra, app)
├── scraper/     (core, service, infra, app)
├── dispatcher/  (app, formatter, notification, template)
└── shared/      (core, infra)
```

### 인프라
| 항목 | 설명 |
|------|------|
| PostgreSQL | 메인 데이터베이스 (Docker) |
| Valkey | 캐시/큐 (k3s) |
| k3s | Go/Rust 서비스 배포 |
| Iris (Redroid) | KakaoTalk 자동화 |

상세: `docs/PROJECT_MAP.md`

## 개발

### 사전 조건
- Go 1.26+
- Rust nightly
- PostgreSQL, Valkey

### 빌드
```bash
# Go
go work sync
go build ./hololive/hololive-kakao-bot-go/...

# Rust
cd hololive/hololive-rs
cargo build --workspace
```

### 테스트
```bash
# Go (전체)
go test ./hololive/...

# Rust
cd hololive/hololive-rs
cargo test --workspace
```

### Pre-commit hook
- Rust: `cargo fmt --check` + `cargo clippy --workspace -D warnings`
- Go: `go vet` + `golangci-lint run`
- 위치: `hololive/hololive-rs/.githooks/pre-commit`

## 배포

k3s 기반. 상세 배포 가이드: `docs/runbook_execution/K3S_DEPLOYMENT_GUIDE.md`

```bash
# 기본 배포
kubectl kustomize k8s/overlays/prod --enable-helm | kubectl apply --server-side -f -
```

### 로그 정책
- SSOT: **stdout → Fluent Bit → Loki** 단일 경로
- Grafana: `http://localhost:30090`
- CLI: `./scripts/logs/tail.sh <service>`, `./scripts/logs/query.sh <service>`

## 문서

- `docs/README.md` — 문서 인덱스
- `docs/PROJECT_MAP.md` — 모듈 구조
- `docs/NEXT_TODO.md` — 향후 작업
- `docs/architecture/` — 아키텍처 결정 기록
- `docs/modularization/` — 모듈화 계획
