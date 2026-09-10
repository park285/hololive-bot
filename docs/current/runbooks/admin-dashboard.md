# Runbook: admin-dashboard

## Role

`admin-dashboard`는 운영 대시보드 서비스입니다. Go 1.27/gin backend가 embedded frontend(React 빌드 산출물)를 서빙하고, Valkey 기반 admin 세션 인증, `hololive-api` admin plane relay, 전용 `admin-docker-proxy`를 통한 컨테이너 제어를 담당합니다.

## Normal status

| Check | Expected |
|---|---|
| Health | `http://127.0.0.1:30190/health` returns `{"status":"ok"}` |
| Public ingress | Seoul Nginx proxies `admin.holoshi.com` to `100.100.1.8:30191`; `short.holoshi.com/l/*`는 `100.100.1.8:30192` short-link listener로 전달하고 고정 `/k/` Kakao navigation route는 Seoul에서 직접 처리합니다. |
| Container | `admin-dashboard` healthy (`./bin/healthcheck` 기반 compose healthcheck) |
| Auth | 현재 generation header가 있는 미인증 API는 401 JSON, generation 누락/불일치는 인증 전에 409 JSON 반환 |
| Generation | `/admin/meta.json`은 `Cache-Control: no-store`; BFF·SDK·validator·WS protocol이 같은 contract SHA-256 사용 |
| Logs | no repeated valkey/session/relay errors |

## Dependencies

| Dependency | Required | Failure impact |
|---|---|---|
| Valkey (`valkey-cache`) | yes | 로그인/세션 전체 실패 (503 store unavailable) |
| `hololive-api` (admin plane) | partial | holo 데이터 조회/뮤테이션 relay 실패 |
| `admin-docker-proxy` | partial | 컨테이너 상태 조회/start/stop/restart 실패 |
| Embedded frontend assets | yes | 대시보드 UI 미서빙 (API는 동작) |

## Key environment variables

시크릿 4종(`ADMIN_PASS_HASH`/`SESSION_SECRET`/`VALKEY_URL`/`HOLO_BOT_API_KEY`)의 정본은 `${ADMIN_DASHBOARD_ENV_FILE:-/etc/stack-secrets/hololive-bot/admin-dashboard.env}`입니다. 정상 systemd 시작 경로가 이를 `/run/hololive-bot/admin-secrets`에 파일로 준비하고, 필수 `docker-compose.admin-security.yml` 오버레이가 `env_file`을 비운 뒤 `*_FILE`로 주입합니다. bcrypt 해시는 compose 보간을 거치지 않는 원문을 사용합니다.

| Env | Purpose | Required |
|---|---|---|
| `PORT` | HTTP port (기본 30190) | no |
| `ENV` | `production` 여부 (localhost origin 차단 등) | no |
| `ADMIN_USER` | 로그인 사용자명 (기본 `admin`) | no |
| `ADMIN_PASS_HASH` (alias `ADMIN_PASS_BCRYPT`) | cost 10 이상인 bcrypt 해시 | yes |
| `SESSION_SECRET` (alias `ADMIN_SECRET_KEY`) | 세션/CSRF 서명 키 (운영 진입점은 32바이트 이상) | yes |
| `VALKEY_URL` | `host:port` 또는 `:urlencoded_password@host:port` (스킴 금지) | yes |
| `DOCKER_HOST` | 운영에서는 전용 `admin-docker-proxy` 주소 | no |
| `HOLO_ADMIN_API_URL` (alias `HOLO_BOT_URL`) | holo relay 대상 | no |
| `HOLO_BOT_API_KEY` (alias `API_SECRET_KEY`) | relay 인증 키 | partial |
| `FORCE_HTTPS` | HSTS + Secure cookie | no |
| `CSRF_MODE` / `WS_ORIGIN_MODE` | `enforce`/`monitor`/`off` (production은 `enforce`만 허용) | no |
| `ALLOWED_ORIGINS` | WS origin 허용 목록 (콤마 구분) | production yes |
| `ALLOW_LOCALHOST_IN_PROD` | production localhost origin 명시적 허용 | no |
| `SESSION_TOKEN_ROTATION` | 세션 토큰 회전 활성화 | no |
| `LOG_LEVEL` / `LOG_DIR` | 로그 레벨, 파일 로깅 디렉터리 (`/app/logs`) | no |
| `ENABLE_OPENAPI` / `ENABLE_SWAGGER_UI` | 스펙/문서 노출 (production 기본 off) | no |
| `TRUST_FORWARDED_HEADERS` | X-Forwarded-For 신뢰 (rate limiter IP) | no |

`admin.login`, `admin.authentication.denied`, `admin.csrf.denied`,
`admin.websocket_origin.denied`, `admin.mutation` 보안 이벤트에는 서버가 만든 `request_id`와
행위자, 클라이언트 IP, 작업, 결과가 기록됩니다. 세션·CSRF 토큰이나 내부 API 키는 감사
필드에 포함하지 않습니다.

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
cd admin-dashboard/frontend && corepack npm ci && corepack npm run lint && corepack npm run build
```

## Deploy (container recreation)

빌드 호스트에서 이미지를 만들어 전송한 뒤, 중앙 런타임 호스트에서는 no-build로
recreate만 합니다. 전체 절차와 수용 증거는 [`release.md`](release.md#compose-service-재배포)가
소유합니다. `compose-redeploy-service.sh`는 cutover 전에 빌드하므로 빌드 호스트에서만
실행합니다.

첫 bigbang 교체는 일반 recreate만으로 수행하지 않습니다. 관리자 origin 차단·구형 요청과 WS 종료·관리자 session prefix 폐기·새 서명 secret·동일 세대 BFF/assets/proxy의 전환 및 전체 rollback 리허설이 먼저 필요합니다. 실행 권한과 G01~G12 증거를 확인한 뒤 승인된 후보를 사용합니다. 준비 상태는 `PLN-20260909-hololive-admin-bigbang-replacement`의 T09/T10에서 관리합니다.

- 중앙 Compose env 정본은 `/etc/stack-secrets/hololive-bot/compose.env`입니다. `systemd-compose-up.sh`의 `prod → admin-security → live-compat` overlay 순서를 보존합니다.
- kapu에서 `linux/arm64` image의 검토된 40자리 `REVISION`, `VERSION`, digest를 확인하고 전송합니다. 미커밋 로컬 검증 이미지를 production revision으로 표시하지 않습니다.
- image build는 `GOWORK=off`로 `admin-dashboard/backend/go.mod`의 published external pin을 사용합니다. `SHARED_GO_WORKSPACE_PATH`는 local CI source 검증에만 쓰이며 image source를 바꾸지 않습니다.
- 이미지 버전 스탬프는 `HOLO_BOT_VERSION` → `-X main.Version` 으로 주입됩니다.
- compose 정의: `deploy/compose/docker-compose.prod.yml`의 `admin-dashboard` 서비스, Dockerfile: `admin-dashboard/Dockerfile`.
- 중앙에서는 `compose-redeploy-service.sh`, `--build`를 실행하지 않습니다. 이미지를 미리 load한 후 승인된 서비스만 `up -d --no-build --no-deps`로 전환합니다. 관리자 교체로 업무 서비스나 DB migration을 실행하지 않습니다.
- `admin-dashboard-ingress`는 short-link listener를 공유하므로 관리자 정비를 위해 컨테이너 전체를 정지하지 않습니다.

## 첫 bigbang 전환과 전체 rollback

아래는 T11에서 승인된 관리자 전환에만 실행하는 순서입니다. `admin-dashboard:bigbang-*-fixture`와 revision `unknown`인 로컬 시험 image는 운영에 사용하지 않습니다. T10의 같은 후보 G01~G12, 검토된 40자리 source revision·arm64 image digest, SDK/validator generation·proxy policy hash, 보존한 구형 image/deploy tree, 실제 O02 source 차단을 먼저 확인합니다.

1. kapu에서 owning release 절차로 image와 비밀이 없는 deploy tree를 준비·검증합니다. 승인된 목적지에 전송하고 중앙에서 load한 뒤 architecture·revision·digest를 다시 확인합니다. 구형 image와 deploy tree는 새 image 밖에 보존합니다.
2. 중앙의 실제 `admin-dashboard`, `admin-dashboard-ingress`, `valkey-cache` container ID와 ingress의 `/etc/nginx/admin-dashboard-ingress.conf` mount source를 확인해 기록합니다. 이름만으로 다른 image를 같은 후보라고 판단하지 않습니다.
3. ingress를 정비로 바꾸고 즉시 기존 BFF의 `docker stop --time 25`를 시작합니다. Nginx 503은 새 연결의 정비 확인입니다. 기존 keep-alive·직접 경로의 신규 업무 차단은 구형 BFF listener 종료를 확인한 뒤 성립합니다. 진행 중이던 변경은 별도로 효과를 확인하고 `unknown`을 보존합니다. 구형 WS와 process 종료를 확인해야 다음 단계로 갑니다. 25초 초과·강제 종료·미분류 효과는 NO-GO입니다.

```bash
# hololive-osaka의 검토된 deploy tree에서, 위에서 확인한 실제 ID/path를 사용합니다.
sudo -n bash scripts/deploy/admin-dashboard-maintenance.sh fence "$ingress_id" "$ingress_config"
sudo -n docker stop --time 25 "$old_bff_id"
sudo -n bash scripts/deploy/purge-admin-dashboard-sessions.sh "$valkey_id" "$old_bff_id" "$ingress_id"
```

4. `stack-platform-ops`의 정본·manifest 절차로 새로운 `SESSION_SECRET`을 준비하고 승인된 중앙 consumer에 반영합니다. 기존 password hash·Holo API key·Valkey credential은 해당 변경이 승인되지 않았다면 유지합니다. root로 `materialize-admin-dashboard-secrets.sh`를 실행해 root/runtime-group·0640 파일을 준비합니다. 이전 signing secret을 되살리지 않습니다.
5. 정비 marker를 유지한 상태로 승인된 image와 proxy 정책을 준비합니다. 필요한 서비스만 아래의 no-build/no-deps 경로를 사용하고 업무 서비스·DB migration을 포함하지 않습니다. marker는 후속 Compose preflight에도 반영됩니다.

```bash
sudo -n env COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/compose.env \
  bash scripts/deploy/compose.sh \
  -f deploy/compose/docker-compose.prod.yml \
  -f deploy/compose/docker-compose.admin-security.yml \
  -f deploy/compose/docker-compose.live-compat.yml \
  up -d --no-build --no-deps admin-docker-proxy admin-dashboard
```

6. 정비 중 loopback으로 `/health`, no-store metadata, image/generation/policy 일치, 이전 cookie 거부, 새 로그인·CSRF·WS protocol·Docker 양쪽 정책을 확인합니다. 30192 short-link와 다른 서비스가 유지되는지 확인합니다. 실제 변경 smoke는 별도로 승인된 작업만 수행합니다.
7. 검증 후 `admin-dashboard-maintenance.sh open`을 실행합니다. HUP 이전 worker가 종료되고 admin 200·short-link 200이 확인되어야 개방 완료입니다. 개방 실패 시 도구는 origin을 한 번 다시 닫고 `maintenance_compensation=confirmed|unknown`을 남깁니다. `unknown`이면 개방 성공으로 해석하지 말고 전환을 중지합니다. 이후 300초 관찰 상한 안에서 로그인·조회·WS·오류와 short-link를 확인하고 수용자를 기록합니다.

실패 시 정비를 유지하고 현재 BFF를 종료한 뒤 같은 prefix purge를 수행합니다. **새로운 rollback signing secret**과 보존한 구형 image·배포 설정·proxy 정책·embedded assets 전체를 복구하여 no-build/no-deps로 시작합니다. 새로운 후보와 구형 후보의 이전 cookie가 모두 거부되는지, 새 로그인이 되는지 확인한 뒤에만 origin을 엽니다. 업무 데이터·공유 cache snapshot·이전 secret을 복원하거나 불명 작업을 replay하지 않습니다.

로컬 증거와 실행 명령은 [첫 전환 기록](../../../admin-dashboard/docs/bigbang/cutover-progress.md), [실제 proxy 경계](../../../admin-dashboard/docs/bigbang/docker-boundary-evidence.md), [보존한 구형 artifact](../../../admin-dashboard/docs/bigbang/old-artifact.json)에 있습니다. 로컬 리허설이 실제 firewall 적용·운영 승인·수용을 대신하지 않습니다.

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
  ./scripts/deploy/compose.sh up -d --no-build --no-deps admin-dashboard-ingress
```

`admin-dashboard-ingress-firewall.service`는 HTTP 파싱 전에 `30191`/`30192`의 source를
loopback과 승인된 Tailscale peer로 제한하도록 정의되어 있습니다. unit 정의만으로 현재 적용을 주장하지 않습니다. 2026-09-09 읽기 전용 조사에서는 unit이 disabled/inactive이고 기대 nft table이 없어 O02로 기록했습니다. 2026-09-10 KST의 [재확인](../../../admin-dashboard/docs/bigbang/operational-boundary-refresh.json)에서도 같은 상태였습니다. 첫 전환 전에 실제 source 차단을 확인하고, 필요한 변경은 운영 권한 안에서 적용해야 합니다. [최초 조사 근거](../../../admin-dashboard/docs/bigbang/consumer-audit.json).

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

Provider rollout은 `127.0.0.1:30101` listener → 중앙 `30192` ingress → Seoul template →
`scripts/deploy/shortlink-smoke.sh` public smoke → grouped message path용 `ALARM_SHORT_LINK_BASE_URL` consumer 활성화 순서입니다.

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
docker ps --filter name=admin-docker-proxy
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml -f deploy/compose/docker-compose.admin-security.yml logs --tail=100 admin-docker-proxy
```

Mitigation:
- `admin-docker-proxy` 기동 확인, `DOCKER_HOST` 값 확인.

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
- 접속 origin이 allowlist에 있는지 확인. `WS_ORIGIN_MODE=enforce`(기본)에서 미등록 origin은 403이며 `admin.websocket_origin.denied` 감사 이벤트로 기록됩니다.
- production에는 코드 fallback이 없습니다. 기본 compose가 `ALLOWED_ORIGINS`를 명시하며, live-compat overlay에서는 `ADMIN_DASHBOARD_ALLOWED_ORIGINS`로 override할 수 있습니다.

Mitigation:
- 기본 compose/live-compat bind는 loopback입니다. Tailscale 직접 접속이 필요하면 먼저 tailnet ACL 또는 host firewall로 source peer를 제한한 뒤 승인 범위에서 `ADMIN_DASHBOARD_PORT_BIND_IP`와 `ADMIN_DASHBOARD_ALLOWED_ORIGINS`를 명시 override하고 `up -d --no-build --no-deps admin-dashboard`를 실행합니다.

## Smoke test

```bash
curl -s http://127.0.0.1:30190/health
curl -fsS http://100.100.1.8:30191/health   # central 또는 Seoul gateway에서 실행
curl -fsS https://admin.holoshi.com/health
./scripts/deploy/shortlink-smoke.sh          # central host에서 3-hop 302/403/404 계약 검증
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:30190/admin/api/auth/session   # 409: generation 없음
curl -sI http://127.0.0.1:30190/health | grep -i x-content-type-options                  # nosniff
```

## Rollback

- 관리자 bigbang rollback은 관리자 정비를 유지한 채 직전 image·설정·proxy 정책·assets 전체를 복구합니다. 관리자 전용 session prefix만 폐기하고 새로운 서명 secret을 발급합니다. 이전 secret 복원, FLUSHDB/FLUSHALL, 공유 DB snapshot 복원, 결과 불명 업무 재실행은 금지합니다. T09의 격리 리허설과 승인된 artifact가 선행 조건입니다.
- Short-link consumer rollback은 `ALARM_SHORT_LINK_BASE_URL`을 먼저 비우고 alarm-worker를 재기동합니다. 이미 발송된 URL을 위해 `30101` listener, 중앙 `30192` ingress, Seoul public routing은 명시적으로 승인된 미래 compatibility deprecation 전까지 무기한 유지합니다.
- 롤백 후 위 Smoke test와 대시보드 로그인 경로 재확인.

## Related

- `admin-dashboard/docs/openapi-pipeline.md` — OpenAPI 계약 파이프라인
- `docs/current/PROJECT_MAP.md` — 포트/소유 경계
