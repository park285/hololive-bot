# Admin Dashboard Docs

이 디렉터리는 `admin-dashboard` 전용 문서를 둡니다.

## 문서 인덱스

- `README.md`: 문서 인덱스 및 현재 구조 개요
- `openapi-pipeline.md`: OpenAPI SSOT(`spec.json`) → generated client 흐름
- `SESSION_PREWARNING_BACKEND_CONTRACT.md`: 세션 pre-warning UX 백엔드 계약 / 프론트 handoff
- `FRONTEND_IMPROVEMENTS.md`: 프런트엔드 개선 기록
- `BOT_ONLY_MIGRATION_STATUS_20260309.md`: bot-only 전환 시점 기록 (Rust 백엔드 시절, 파일 경로 무효 — 문서 상단 주석 참고)

## 현재 구조

- 백엔드는 Go입니다 (`go 1.26` toolchain, gin router). 모듈은 `admin-dashboard/backend`이고 진입점은 `backend/cmd/admin-dashboard/main.go`입니다.
- 프런트엔드는 React + Vite (`admin-dashboard/frontend`)이며, generated API client는 백엔드 OpenAPI(`swagger.json`)에서 생성합니다.
- 프런트 개발 프록시 기본 대상은 `http://localhost:30190`입니다. (실제 값은 `frontend/vite.config.ts` 확인)

## 백엔드 레이아웃

- `backend/internal/app/`: gin runtime 조립.
  - `routes.go`: 라우트 테이블 — `/admin/api/*` 아래 public(login) / 인증(`r.auth()`) / CSRF(`r.csrf()`) 그룹, `/admin/docs`(Swagger UI)
  - `handlers.go`: docker / status / health / WebSocket 핸들러
  - `session_handlers.go`: auth 핸들러 (login, logout, heartbeat, session)
  - `middleware.go`: auth / CSRF / ETag / security-header 미들웨어
  - `responses.go`, `app.go`: 응답 헬퍼와 runtime 조립
- `backend/internal/holo/`: hololive-api로 향하는 typed reverse proxy (`/admin/api/holo/*`)
- `backend/internal/docker/client.go`: docker control. 컨테이너 목록/`restart`/`stop`/`start`. Container JSON은 `managed`·`stopBlocked` 필드를 포함하고, 인프라 컨테이너(`valkey`·`postgres`·`deunhealth`·`admin` prefix) `stop`은 403으로 거부하며 `restart`만 허용합니다.
- `backend/internal/session/`: Valkey 기반 세션 store. `session.go`가 store/연결, `lifecycle.go`가 Lua CAS 스크립트로 refresh/rotate를 처리합니다.
- `backend/internal/config/config.go`: 환경변수 로딩과 검증 (`SessionConfig`, `SecurityConfig` 등). `rotation_interval < expiry_duration`을 강제하고, `FORCE_HTTPS`가 켜졌는데 `TRUST_FORWARDED_HEADERS`/`TRUSTED_PROXY_CIDRS` 신뢰 설정이 없으면 시작 시 경고합니다.
- `backend/internal/openapi/`: embed된 OpenAPI SSOT(`spec.json`)와 export 헬퍼
- `backend/internal/auth/`, `backend/internal/status/`, `backend/internal/httpx/`, `backend/internal/static/`: 인증 토큰/쿠키, 상태 집계, HTTP 유틸, 정적 자산 서빙
- `backend/cmd/`: `admin-dashboard`(서버), `export-openapi`(spec export), `healthcheck`

## OpenAPI / generated client

- SSOT는 손으로 관리하는 `backend/internal/openapi/spec.json`이며 빌드 타임에 embed됩니다.
- 런타임에는 `EnableOpenAPI`일 때 `GET /admin/api/openapi.json`, `EnableSwaggerUI`일 때 `GET /admin/docs`로만 노출됩니다.
- 파이프라인 전체는 `openapi-pipeline.md` 참고.

## 검증 명령

```bash
cd admin-dashboard/backend
make lint    # gofmt + go vet + staticcheck
make test
make build
```

리포지토리 게이트 (meta-repo root 기준):

```bash
bash scripts/ci/admin-dashboard-go-ci.sh              # staticcheck, golangci-lint, NilAway, race tests, govulncheck
bash scripts/architecture/check-admin-dashboard-go-only.sh
```

프런트:

```bash
cd admin-dashboard/frontend
npm ci
npm run generate:api   # backend spec에서 swagger.json + generated client 재생성
npm run lint
npm run build
```
