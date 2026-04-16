# Admin Dashboard

통합 관리자 대시보드입니다. Rust 백엔드가 인증, Docker 제어, 상태 집계, 정적 파일 서빙을 담당하고, React 프런트가 UI를 제공합니다.

## 현재 구조

```text
admin-dashboard/
├── backend/   # Rust 2024 + axum
├── frontend/  # React 19 + TypeScript + Vite 8
└── docs/      # 대시보드 전용 문서
```

## 주요 엔트리포인트

- 백엔드: `backend/src/main.rs`
- 프런트: `frontend/src/main.tsx`
- OpenAPI export: `backend/src/bin/export-openapi.rs`

## 백엔드

```bash
cd admin-dashboard/backend
cargo fmt --check
cargo clippy -- -D warnings
cargo test
cargo run --bin export-openapi > docs/swagger.json
```

### 주요 환경 변수

| 변수 | 설명 | 기본값 |
|---|---|---|
| `PORT` | HTTP 포트 | `30190` |
| `VALKEY_URL` | 세션 저장소 주소 | `valkey-cache:6379` |
| `DOCKER_HOST` | Docker API 주소 | `tcp://docker-proxy:2375` |
| `HOLO_ADMIN_API_URL` | 업스트림 hololive admin API | `http://hololive-admin-api:30006` |
| `HOLO_BOT_URL` | legacy fallback alias for upstream admin API | `http://hololive-admin-api:30006` |
| `HOLO_BOT_API_KEY` | 업스트림 내부 인증 헤더 값 (`API_SECRET_KEY` fallback 지원) | 빈 값 |
| `ENABLE_OPENAPI` | 인증 뒤 OpenAPI JSON 노출 여부 | production에서는 기본 `false` |
| `ENABLE_SWAGGER_UI` | 인증 뒤 Swagger UI 노출 여부 | production에서는 기본 `false` |
| `ADMIN_USER` | 관리자 계정명 | `admin` |
| `ADMIN_PASS_HASH` | bcrypt 비밀번호 해시 | 필수 |
| `SESSION_SECRET` | 세션/HMAC/CSRF 시크릿 | 필수 |
| `ALLOWED_ORIGINS` | 허용 Origin 목록 | fallback 사용 |
| `CSRF_MODE` | `enforce`/`monitor`/`off` | `enforce` |
| `WS_ORIGIN_MODE` | WebSocket Origin 검증 모드 | `enforce` |

## 프런트

```bash
cd admin-dashboard/frontend
npm ci
npm test
npm run generate:api
npm run lint
npm run build
```

### 개발 서버

기본 프록시 대상은 `http://localhost:30190`입니다. 필요하면 `ADMIN_DASHBOARD_PROXY_TARGET`으로 덮어쓸 수 있습니다.

```bash
cd admin-dashboard/frontend
ADMIN_DASHBOARD_PROXY_TARGET=http://localhost:30190 npm run dev
```

## API 경로

- `POST /admin/api/auth/login`
- `GET /admin/api/auth/session`
- `POST /admin/api/auth/logout`
- `POST /admin/api/auth/heartbeat`
- `GET /admin/api/docker/health`
- `GET /admin/api/docker/containers`
- `GET /admin/api/status`
- `GET /admin/api/ws/system-stats`
- typed holo contract: `GET/POST/PATCH/DELETE /admin/api/holo/*`

## OpenAPI / Generated Client

프런트 generated client는 Rust backend의 OpenAPI에서 생성합니다.

```bash
cd admin-dashboard/frontend
npm run generate:api
```

이 스크립트는 내부적으로 backend OpenAPI를 `backend/docs/swagger.json`으로 export한 뒤 `src/api/generated/`를 갱신합니다.

`src/api/adminClient.ts`가 generated `Admin` singleton의 단일 소유자입니다. transport source-of-truth는 이 인스턴스 하나만 사용합니다. `src/api/core.ts`는 compatibility wrapper이고, 별도의 `holoClient.ts` 레이어는 더 이상 존재하지 않습니다.

CI도 같은 경로를 강제합니다. `admin-dashboard-frontend` workflow는 `npm run generate:api` 뒤에 `git diff --exit-code -- ../backend/docs/swagger.json src/api/generated`를 실행해서 generated client drift를 즉시 실패시킵니다.

주의:
- `src/api/generated/*`는 수동 수정 금지
- holo dashboard endpoint는 `/admin/api/holo/*` typed contract에 먼저 추가
- `/admin/docs`와 `/admin/api/openapi.json`은 runtime에서 인증 뒤에만 노출됩니다.
