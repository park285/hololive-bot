# Runbook: admin-dashboard

## Role

`admin-dashboard`는 운영 대시보드 서비스입니다. Go 1.27/gin backend가 embedded frontend(React 빌드 산출물)를 서빙하고, Valkey 기반 admin 세션 인증, `hololive-api` admin plane relay, docker-proxy를 통한 컨테이너 제어를 담당합니다.

## Normal status

| Check | Expected |
|---|---|
| Health | `http://127.0.0.1:30190/health` returns `{"status":"ok"}` |
| Public ingress | Seoul Nginx proxies `admin.holoshi.com` to `100.100.1.8:30191`; `short.holoshi.com/l/*`는 `100.100.1.8:30192` short-link listener로 전달하고 고정 `/k/` Kakao navigation route는 Seoul에서 직접 처리합니다. |
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

시크릿 4종(`ADMIN_PASS_HASH`/`SESSION_SECRET`/`VALKEY_URL`/`HOLO_BOT_API_KEY`)은 2026-07-05부터 compose 보간이 아니라 scoped env_file `${ADMIN_DASHBOARD_ENV_FILE:-/etc/stack-secrets/hololive-bot/admin-dashboard.env}`(`stack-secrets` 렌더, `0600 root`)로 주입됩니다. env_file 값은 compose 보간을 거치지 않으므로 bcrypt 해시를 이스케이프 없이 원문 그대로 넣습니다.

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
| `ALLOWED_ORIGINS` | WS origin 허용 목록 (콤마 구분) | production yes |
| `ALLOW_LOCALHOST_IN_PROD` | production localhost origin 명시적 허용 | no |
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

빌드 호스트에서 이미지를 만들어 전송한 뒤, 중앙 런타임 호스트에서는 no-build로
recreate만 합니다. 전체 절차와 수용 증거는 [`release.md`](release.md#compose-service-재배포)가
소유합니다. `compose-redeploy-service.sh`는 cutover 전에 빌드하므로 빌드 호스트에서만
실행합니다.

```bash
# 빌드 호스트
./scripts/deploy/compose-redeploy-service.sh admin-dashboard
```

- 중앙 Compose env 정본은 `stack-secrets` 정적 파일 `/etc/stack-secrets/hololive-bot/compose.env`이므로 중앙 호스트 재배포는 `sudo -n env COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/compose.env ./scripts/deploy/compose-redeploy-service.sh admin-dashboard` 형태로 실행합니다.
- image build는 `GOWORK=off`로 `admin-dashboard/backend/go.mod`의 published external pin을 사용합니다. `SHARED_GO_WORKSPACE_PATH`는 local CI source 검증에만 쓰이며 image source를 바꾸지 않습니다.
- 이미지 버전 스탬프는 `HOLO_BOT_VERSION` → `-X main.Version` 으로 주입됩니다.
- compose 정의: `deploy/compose/docker-compose.prod.yml`의 `admin-dashboard` 서비스, Dockerfile: `admin-dashboard/Dockerfile`.
- `--build`가 의존성 `hololive-api` 이미지도 재빌드하므로 `hololive-api` 컨테이너가 함께 재생성됩니다(수 초 단절). 동반 재기동을 피해야 하면 사전 빌드 후 `up -d --no-deps admin-dashboard`를 사용합니다.

## Public ingress

`admin-dashboard`는 중앙 호스트에서 `127.0.0.1:30190` loopback-only로 유지합니다. 공개 도메인
`admin.holoshi.com`은 Seoul Nginx가 TLS/HTTP3 종료점을 맡고, 중앙 호스트의 host-networked
`admin-dashboard-ingress` Nginx 컨테이너가 Tailscale 전용 포트 `100.100.1.8:30191`에서 받아
`127.0.0.1:30190`으로 전달합니다.

같은 Nginx가 `100.100.1.8:30192`에서 Seoul gateway source만 허용하고 `/l/*`를
`127.0.0.1:30101` short-link listener로 전달합니다. `/k/`는 이 central ingress를 거치지
않고 Seoul의 public template가 직접 처리하며, central listener의 그 외 path는 `404`로 거부합니다.

source 제한은 `deploy/nginx/admin-dashboard-ingress.conf.template`가 적용합니다. 허용 source는 Seoul gateway
`100.100.1.5`, 중앙 Tailscale 주소 `100.100.1.8`, 로컬 loopback뿐입니다. 컨테이너는 Tailscale IP가
아직 준비되지 않아 bind에 실패해도 `restart: unless-stopped`로 재시도하므로 systemd의 early-boot
`sockets.target` ordering에 의존하지 않습니다.

설치/재적용:

```bash
sudo -n env COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/compose.env \
  ./scripts/deploy/compose.sh up -d --no-deps admin-dashboard-ingress
```

`admin-dashboard-ingress-firewall.service`는 HTTP 파싱 전에 `30191`/`30192`의 source를
loopback과 승인된 Tailscale peer로 제한합니다. `hololive-compose.service`가 이 unit을 필수 선행
조건으로 사용하므로 nft 규칙을 적용하지 못하면 public ingress도 시작하지 않습니다.

Seoul Nginx는 admin과 short-link public origin을 분리합니다. `admin.holoshi.com`은 기존 admin upstream
`100.100.1.8:30191`만 유지하고, `deploy/nginx/holoshi-public-shortlink.conf`가 전용
`short.holoshi.com` TLS/HTTP3 server와 `shortlink_backend` upstream을 소유합니다. 해당 파일은
`holoshi-nginx`의 `http` context에 한 번만 적용합니다.

```nginx
http {
    # 기존 공통 map/upstream/server 설정
    include <release-path>/deploy/nginx/holoshi-public-shortlink.conf;
}
```

template는 `short.holoshi.com/l/*`를 `100.100.1.8:30192`로 전달합니다. 또한 canonical positive
decimal ID만 받는 `/k/m/<room>/<message>`, `/k/t/<room>/<thread>`,
`/k/t/<room>/<thread>/<message>`를 고정 KakaoTalk target으로 직접 변환하고, 그 외 short-link
path는 `404`로 닫습니다. `/k/`는 유효한 경로의 `GET`을 User-Agent와 무관하게 고정 KakaoTalk
target으로 전달합니다. 알려진 Kakao scraper는 먼저 거부하고, `HEAD`와 다른 method도
`Location` 없이 거부하며 access log를 남기지 않습니다. User-Agent 판정은 preview 억제일 뿐
인증이나 완전한 crawler 차단으로 간주하지 않습니다. 기존 `/l/` method·User-Agent·logging
계약은 그대로입니다. Admin traffic과 WebSocket은 이 server를 통과하지 않습니다. 적용 전후
`nginx -t`를 통과시킨 뒤 reload합니다.

provider rollout은 `127.0.0.1:30101` listener → 중앙 `30192` ingress → Seoul template →
`scripts/deploy/shortlink-smoke.sh` public smoke → `ALARM_SHORT_LINK_BASE_URL` consumer 활성화 순서입니다.

## Logs

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs -f admin-dashboard
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs -f admin-dashboard-ingress
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
- 컨테이너 restart loop, 로그에 `required environment variable missing`, `ALLOWED_ORIGINS`, 또는 bcrypt/세션 검증 에러.

Diagnosis:
```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs --tail=50 admin-dashboard
```

Mitigation:
- `/etc/stack-secrets/hololive-bot/admin-dashboard.env`의 `ADMIN_PASS_HASH`/`SESSION_SECRET` 주입과 해시 형식(`$2b$...`, env_file은 이스케이프 없는 원문) 확인.
- production에서는 기본 compose의 `ALLOWED_ORIGINS` 또는 live-compat의 `ADMIN_DASHBOARD_ALLOWED_ORIGINS` override가 실제 접속 Origin을 포함하는지 확인합니다.

### 5. 시스템 리소스(인프라) 패널 미동작

Symptoms:
- 대시보드 로그인은 되지만 시스템 리소스 차트가 비어 있음 (`/admin/api/ws/system-stats` WS 403).

Diagnosis:
- 접속 origin이 allowlist에 있는지 확인. `WS_ORIGIN_MODE=enforce`(기본)에서 미등록 origin은 조용히 403 (로그 없음).
- production에는 코드 fallback이 없습니다. 기본 compose가 `ALLOWED_ORIGINS`를 명시하며, live-compat overlay에서는 `ADMIN_DASHBOARD_ALLOWED_ORIGINS`로 override할 수 있습니다.

Mitigation:
- 기본 compose/live-compat bind는 loopback입니다. Tailscale 직접 접속이 필요하면 먼저 tailnet ACL 또는 host firewall로 source peer를 제한한 뒤 `ADMIN_DASHBOARD_PORT_BIND_IP`와 `ADMIN_DASHBOARD_ALLOWED_ORIGINS`를 명시 override하고 `up -d --no-deps admin-dashboard`를 실행합니다.

## Smoke test

```bash
curl -s http://127.0.0.1:30190/health
curl -fsS http://100.100.1.8:30191/health   # central 또는 Seoul gateway에서 실행
curl -fsS https://admin.holoshi.com/health
./scripts/deploy/shortlink-smoke.sh          # central host에서 3-hop 302/403/404 계약 검증
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:30190/admin/api/auth/session   # 401
curl -sI http://127.0.0.1:30190/health | grep -i x-content-type-options                  # nosniff
```

## Rollback

- `docs/current/runbooks/rollback.md` 기준으로 직전 `admin-dashboard` 이미지/설정 재배포.
- short-link consumer rollback은 `ALARM_SHORT_LINK_BASE_URL`을 먼저 비우고 alarm-worker를 재기동합니다. 이미 발송된 URL을 위해 `30101` listener, 중앙 `30192` ingress, Seoul public routing은 명시적으로 승인된 미래 compatibility deprecation 전까지 무기한 유지합니다.
- 롤백 후 위 Smoke test와 대시보드 로그인 경로 재확인.

## Related

- `admin-dashboard/docs/openapi-pipeline.md` — OpenAPI 계약 파이프라인
- `docs/current/PROJECT_MAP.md` — 포트/소유 경계
