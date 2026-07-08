# Admin Dashboard

통합 관리자 대시보드입니다. Go 백엔드가 인증, Docker 제어, 상태 집계, Holo Admin API proxy, 정적 파일 서빙을 담당하고, React 프런트가 UI를 제공합니다.

## 현재 구조

```text
admin-dashboard/
├── backend/   # Go 1.26.5 + net/http
├── frontend/  # React 19 + TypeScript + Vite
└── docs/      # 대시보드 전용 문서
```

## 주요 엔트리포인트

- 백엔드: `backend/cmd/admin-dashboard/main.go`
- 헬스체크: `backend/cmd/healthcheck/main.go`
- 라우팅/미들웨어: `backend/internal/app/app.go`
- 프런트: `frontend/src/main.tsx`

## 백엔드

```bash
cd admin-dashboard/backend
make fmt
make vet
make staticcheck
make test
make build
```

저장소 루트에서는 다음 strict gate를 사용합니다.

```bash
./scripts/architecture/check-admin-dashboard-go-only.sh
./scripts/ci/admin-dashboard-go-ci.sh
```

### 주요 환경 변수

| 변수 | 설명 | 기본값 |
|---|---|---|
| `PORT` | HTTP 포트 | `30190` |
| `VALKEY_URL` | 세션 저장소 주소. scheme 없이 `host:port` 또는 `:urlencoded_password@host:port` 형식을 사용합니다. | `valkey-cache:6379` |
| `DOCKER_HOST` | Docker API 주소 | `tcp://docker-proxy:2375` |
| `HOLO_ADMIN_API_URL` | 업스트림 hololive admin API | `https://hololive-api:30006` |
| `HOLO_BOT_URL` | legacy fallback alias for upstream admin API | `https://hololive-api:30006` |
| `HOLO_BOT_API_KEY` | 업스트림 내부 인증 헤더 값 (`API_SECRET_KEY` fallback 지원) | 빈 값 |
| `ENABLE_OPENAPI` | 인증 뒤 OpenAPI JSON 노출 여부 | production에서는 기본 `false` |
| `ENABLE_SWAGGER_UI` | 인증 뒤 docs page 노출 여부 | production에서는 기본 `false` |
| `ADMIN_USER` | 관리자 계정명 | `admin` |
| `ADMIN_PASS_HASH` | bcrypt 비밀번호 해시 | 필수 |
| `SESSION_SECRET` | 세션/HMAC/CSRF 시크릿 | 필수 |
| `ALLOWED_ORIGINS` | 허용 Origin 목록 | fallback 사용 |
| `CSRF_MODE` | `enforce`/`monitor`/`off` | `enforce` |
| `WS_ORIGIN_MODE` | WebSocket Origin 검증 모드 | `enforce` |
| `FORCE_HTTPS` | Secure cookie/HSTS 강제 | `true` |
| `TRUST_FORWARDED_HEADERS` | rate limit IP 산정 시 forwarded header 신뢰 | `false` |

## 프런트

```bash
cd admin-dashboard/frontend
npm ci
npm test
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
- `POST /admin/api/docker/containers/{name}/restart|stop|start`
- `GET /admin/api/status`
- `GET /admin/api/ws/system-stats`
- typed holo contract proxy: `GET/POST/PATCH/DELETE /admin/api/holo/*`

## OpenAPI / Generated Client

Go backend는 인증 뒤 `/admin/api/openapi.json`을 제공합니다. 이 문서는 dashboard runtime route inventory의 가벼운 OpenAPI envelope이며, 프런트 generated client drift 검증이 필요하면 `admin-dashboard/frontend`의 generation script가 이 endpoint를 기준으로 사용하도록 맞춥니다.

주의:

- `admin-dashboard/backend` 아래 Rust/Cargo artifact는 금지입니다.
- `src/api/generated/*`는 수동 수정 금지입니다.
- dashboard endpoint는 `/admin/api/holo/*` proxy contract에 먼저 추가합니다.
- `/admin/docs`와 `/admin/api/openapi.json`은 runtime에서 인증 뒤에만 노출됩니다.
