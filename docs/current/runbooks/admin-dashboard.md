# Runbook: admin-dashboard

## Role

`admin-dashboard`는 운영 대시보드 서비스입니다. Go 1.26/gin backend가 embedded frontend(React 빌드 산출물)를 서빙하고, Valkey 기반 admin 세션 인증, `hololive-api` admin plane relay, docker-proxy를 통한 컨테이너 제어를 담당합니다.

## Normal status

| Check | Expected |
|---|---|
| Health | `http://127.0.0.1:30190/health` returns `{"status":"ok"}` |
| Public ingress | Seoul Caddy proxies `admin.holo-oshi.com` to `100.100.1.3:30191`; central `admin-dashboard-ingress.socket` bridges that to loopback `127.0.0.1:30190` |
| Container | `admin-dashboard` healthy (`./bin/healthcheck` 기반 compose healthcheck) |
| Auth | 미인증 `/admin/api/*` 호출이 401 JSON 반환 |
| Logs | no repeated valkey/session/relay errors |

## Dependencies

| Dependency | Required | Failure impact |
|---|---|---|
| Valkey (`valkey-cache`) | yes | 로그인/세션 전체 실패 (503 store unavailable) |
| `hololive-api` (admin plane) | partial | holo 데이터 조회/뮤테이션 relay 실패 |
| `docker-proxy` | partial | 컨테이너 상태 조회/start/stop/restart 실패 |
| Embedded frontend assets | yes | 대시보드 UI 미서빙 (API는 동작) |

## Key environment variables

시크릿 4종(`ADMIN_PASS_HASH`/`SESSION_SECRET`/`VALKEY_URL`/`HOLO_BOT_API_KEY`)은 2026-07-05부터 compose 보간이 아니라 scoped env_file `${ADMIN_DASHBOARD_ENV_FILE:-/run/hololive-bot/admin-dashboard.env}`(OpenBao Agent 렌더, `0600 root`)로 주입됩니다. env_file 값은 compose 보간을 거치지 않으므로 bcrypt 해시를 이스케이프 없이 원문 그대로 넣습니다.

| Env | Purpose | Required |
|---|---|---|
| `PORT` | HTTP port (기본 30190) | no |
| `ENV` | `production` 여부 (localhost origin 차단 등) | no |
| `ADMIN_USER` | 로그인 사용자명 (기본 `admin`) | no |
| `ADMIN_PASS_HASH` (alias `ADMIN_PASS_BCRYPT`) | bcrypt 해시 | yes |
| `SESSION_SECRET` (alias `ADMIN_SECRET_KEY`) | 세션/CSRF 서명 키 (16바이트 이상) | yes |
| `VALKEY_URL` | `host:port` 또는 `:urlencoded_password@host:port` (스킴 금지) | yes |
| `DOCKER_HOST` | docker-proxy 주소 | no |
| `HOLO_ADMIN_API_URL` (alias `HOLO_BOT_URL`) | holo relay 대상 | no |
| `HOLO_BOT_API_KEY` (alias `API_SECRET_KEY`) | relay 인증 키 | partial |
| `FORCE_HTTPS` | HSTS + Secure cookie | no |
| `CSRF_MODE` / `WS_ORIGIN_MODE` | `enforce`/`monitor`/`off` | no |
| `ALLOWED_ORIGINS` | WS origin 허용 목록 (콤마 구분) | no |
| `SESSION_TOKEN_ROTATION` | 세션 토큰 회전 활성화 | no |
| `LOG_LEVEL` / `LOG_DIR` | 로그 레벨, 파일 로깅 디렉터리 (`/app/logs`) | no |
| `ENABLE_OPENAPI` / `ENABLE_SWAGGER_UI` | 스펙/문서 노출 (production 기본 off) | no |
| `TRUST_FORWARDED_HEADERS` | X-Forwarded-For 신뢰 (rate limiter IP) | no |

## Build · Test · CI

```bash
# backend 빌드/테스트 (repo root 기준, go.work 워크스페이스)
go build ./admin-dashboard/backend/...
go test -race ./admin-dashboard/backend/...

# 전용 CI 게이트 (gofmt/vet/staticcheck/govulncheck/build/test)
./scripts/ci/admin-dashboard-go-ci.sh

# 전체 게이트 (architecture gate 포함)
./scripts/ci/local-ci.sh

# Rust 잔재 차단 게이트
./scripts/architecture/check-admin-dashboard-go-only.sh

# frontend (변경 시)
cd admin-dashboard/frontend && npm ci && npm run lint && npm run build
```

## Deploy (container recreation)

```bash
# 이미지 재빌드 + 컨테이너 재생성 (canonical 경로, alias: admin)
./scripts/deploy/compose-redeploy-service.sh admin-dashboard
```

- 중앙 Compose env 정본은 OpenBao 렌더 파일 `/run/hololive-bot/compose.env` (`0600 root`)이므로 중앙 호스트 재배포는 `sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/compose.env ./scripts/deploy/compose-redeploy-service.sh admin-dashboard` 형태로 실행합니다.
- `shared_go_workspace` build context는 스크립트가 `SHARED_GO_WORKSPACE_PATH`로 자동 해석합니다 (기본 `../shared-go`).
- 이미지 버전 스탬프는 `HOLO_BOT_VERSION` → `-X main.Version` 으로 주입됩니다.
- compose 정의: `deploy/compose/docker-compose.prod.yml`의 `admin-dashboard` 서비스, Dockerfile: `admin-dashboard/Dockerfile`.
- `--build`가 의존성 `hololive-api` 이미지도 재빌드하므로 `hololive-api` 컨테이너가 함께 재생성됩니다(수 초 단절). 동반 재기동을 피해야 하면 사전 빌드 후 `up -d --no-deps admin-dashboard`를 사용합니다.

## Public ingress

`admin-dashboard`는 중앙 호스트에서 `127.0.0.1:30190` loopback-only로 유지합니다. 공개 도메인
`admin.holo-oshi.com`은 Seoul Caddy가 종료점을 맡고, 중앙 호스트의
`admin-dashboard-ingress.socket`이 Tailscale 전용 포트 `100.100.1.3:30191`에서 받아
`127.0.0.1:30190`으로 전달합니다.

중앙 호스트 source 제한은 `admin-dashboard-ingress-firewall.service`가
`/etc/nftables.d/admin-dashboard-ingress.nft`를 로드해서 적용합니다. 허용 source는 Seoul gateway
`100.100.1.5`와 로컬 loopback뿐입니다.

설치/재적용:

```bash
sudo scripts/deploy/sync-opt-current.sh
sudo systemctl enable --now admin-dashboard-ingress-firewall.service admin-dashboard-ingress.socket
```

Seoul Caddy upstream:

```text
reverse_proxy 100.100.1.3:30191
```

## Logs

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs -f admin-dashboard
tail -f logs/admin-dashboard.log
```

## Common failure modes

### 1. 로그인/세션 전면 실패

Symptoms:
- `/admin/api/auth/login` 또는 인증 API가 503 반환.

Diagnosis:
```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml ps valkey-cache admin-dashboard
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs --tail=200 admin-dashboard
```

Mitigation:
- `valkey-cache` health와 `VALKEY_URL`/`CACHE_PASSWORD` 일치 확인.

### 2. holo 데이터 조회 실패

Symptoms:
- 대시보드 멤버/스트림/설정 화면이 에러 표시.

Diagnosis:
```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api ./bin/healthcheck https://127.0.0.1:30006/health
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs --tail=200 hololive-api
```

Mitigation:
- `hololive-api` (admin plane) health 복구, `HOLO_ADMIN_API_URL`/`HOLO_BOT_API_KEY` 확인.

### 3. 컨테이너 제어(restart 등) 실패

Symptoms:
- Docker 탭 액션이 실패하거나 컨테이너 목록이 비어 있음.

Diagnosis:
```bash
docker ps --filter name=docker-proxy
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs --tail=100 docker-proxy
```

Mitigation:
- `docker-proxy` 기동 확인, `DOCKER_HOST` 값 확인.

### 4. 시작 직후 즉시 종료 (config 검증 실패)

Symptoms:
- 컨테이너 restart loop, 로그에 `required environment variable missing` 또는 bcrypt/세션 검증 에러.

Diagnosis:
```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs --tail=50 admin-dashboard
```

Mitigation:
- `/run/hololive-bot/admin-dashboard.env`의 `ADMIN_PASS_HASH`/`SESSION_SECRET` 주입과 해시 형식(`$2b$...`, env_file은 이스케이프 없는 원문) 확인.

### 5. 시스템 리소스(인프라) 패널 미동작

Symptoms:
- 대시보드 로그인은 되지만 시스템 리소스 차트가 비어 있음 (`/admin/api/ws/system-stats` WS 403).

Diagnosis:
- 접속 origin이 allowlist에 있는지 확인. `WS_ORIGIN_MODE=enforce`(기본)에서 미등록 origin은 조용히 403 (로그 없음).
- fallback allowlist는 `https://admin.capu.blog` 하나뿐이며 production에서는 localhost 계열이 제거됨.

Mitigation:
- 기본 compose/live-compat bind는 loopback입니다. Tailscale 직접 접속이 필요하면 먼저 tailnet ACL 또는 host firewall로 source peer를 제한한 뒤 `ADMIN_DASHBOARD_PORT_BIND_IP`와 `ADMIN_DASHBOARD_ALLOWED_ORIGINS`를 명시 override하고 `up -d --no-deps admin-dashboard`를 실행합니다.

## Smoke test

```bash
curl -s http://127.0.0.1:30190/health
curl -fsS http://100.100.1.3:30191/health   # central 또는 Seoul gateway에서 실행
curl -fsS https://admin.holo-oshi.com/health
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:30190/admin/api/auth/session   # 401
curl -sI http://127.0.0.1:30190/health | grep -i x-content-type-options                  # nosniff
```

## Rollback

- `docs/current/runbooks/rollback.md` 기준으로 직전 `admin-dashboard` 이미지/설정 재배포.
- 롤백 후 위 Smoke test와 대시보드 로그인 경로 재확인.

## Related

- `admin-dashboard/AGENTS.md` — 모듈 규칙
- `admin-dashboard/docs/openapi-pipeline.md` — OpenAPI 계약 파이프라인
- `docs/current/PROJECT_MAP.md` — 포트/소유 경계
