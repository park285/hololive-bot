# 관리자 빅뱅 교체 기준 목록

2026-09-09 KST. `PLN-20260909-hololive-admin-bigbang-replacement` T01의 로컬 조사·검증 근거입니다. 결정은 `DEC-20260909-hololive-admin-bigbang-replacement`, 제약은 `DEC-20260825-hololive-admin-auth-boundaries-preserved`와 `DEC-20260731-legacy-fade-out-no-dual-path`입니다.

## 실행 권한과 현재 범위

사용자는 시작 인계 문서 경로를 지정하여 “작업을 필요한 스킬들을 사용하여 진행”하고, 이어 “`$modern-go-guidelines:use-modern-go` `$security-guidance`를 준수하며 진행”하도록 지시했습니다. 이 근거는 관리자 BFF·프런트의 로컬 구현·격리 검증·전환 준비에 적용합니다. 시작 문서가 제외한 실제 배포·원격 쓰기·재시작·secret 교체·운영 세션 폐기·운영 변경 smoke·커밋·push는 포함하지 않습니다. MFA 관련 작업·검증·출시 조건은 제외합니다. 다른 세션 통지와 subagent 위임은 하지 않았습니다.

`executing-plans`의 strict gate는 exit 0, `strict_validation=passed`, `gate_passed=true`, `reasons=[]`였습니다. semantic `plans start`와 `plans task start`로 PLN과 T01을 시작했습니다. 최초 task-start 호출은 앞선 start 프로세스가 lock을 소유하여 변경 없이 거부됐고, 그 프로세스의 정상 완료를 확인한 뒤 task-start를 수행했습니다.

이 목록은 후보 구현이나 출시 게이트의 통과 증거가 아닙니다. 저장소 밖 소비자 확인 C01 때문에 최초에는 `B01` blocker를 열었습니다. 사용자의 “니가 체크해봐” 지시에 따라 운영 ingress·Loki·설치된 automation을 직접 확인한 [소비자 조사](consumer-audit.json)로 로컬 구현의 consumer 범위를 동결합니다. 최종 T/AC/V 상태는 PLN에서 확인하며, 이 조사로 출시 게이트를 완료 처리하지 않습니다.

## 기준 identity

| 대상 | 관찰한 값 | 해석 |
|---|---|---|
| 로컬 Hololive source | `2323f98c8446f42de581c6f8d2e67e408e3d92df` | 인계 기준과 동일; 원안 SHA로 되돌리지 않음 |
| 운영 `admin-dashboard` image ID | `sha256:17b56c3a48b055fdab3d60691ec40e90de1f32e65691e1be0718b316b84e4fb7` | SSH를 통한 Docker의 지정 필드만 조회; image archive 확보는 아님 |
| 운영 image revision | `6ba2ede142501eb5bc436f9d1af2587a3f1d81b0` | `arm64`, 관찰 시 `healthy`; 교체 후보 아님 |
| 두 SHA 사이 관련 차이 | `admin-dashboard/AGENTS.md`, `frontend/package-lock.json` | backend·ingress·admin-security overlay 차이 없음; 실제 old bundle은 재빌드로 대체할 수 없음 |
| 도구 계약 | Go `go.mod` 1.27.1, Gin 1.12.0, Node `^24.20.0`, npm 12.0.2 | 버전 변경·의존성 추가 없음 |

운영 identity 조회 명령은 `ssh -o BatchMode=yes -o ConnectTimeout=8 100.100.1.8`에서 `docker inspect`의 Name/Image/Health, `docker image inspect`의 Id/Architecture/revision label만 출력했습니다. env·secret·업무 payload는 읽지 않았습니다. `/opt/hololive-bot/compose/current`를 확인했지만 복구 archive나 deploy tree revision을 검증한 것은 아닙니다.

## API·화면 동결 범위

[endpoint-feature-parity.json](endpoint-feature-parity.json)은 endpoint별 method/path/operationId, 접근, 입력·응답의 명세 pointer, 실제 owner, 확인한 consumer, 신규 owner, 검증 후보와 미확인 범위를 담습니다. 명세 값은 `backend/internal/openapi/spec.json`이 정본이며 manifest의 hash로 기준을 식별합니다. 신규 owner는 계획 경로이고 아직 구현된 경로가 아닙니다.

실제 `Runtime.Handler()`의 Gin 등록 목록을 읽는 [TestBaselineRouteInventory](../../backend/internal/app/route_inventory_test.go)는 33 operation과 별도 등록 경로 7개를 양방향 비교합니다. spec 누락·manifest 누락·중복을 거부합니다. 33 operation 중 GET 14개, 나머지 19개이고, 업무 변경은 로그인·로그아웃·heartbeat를 제외한 16개입니다. 모든 보호 API에 쿠키가 없으면 401, 변경 API에 유효 세션만 있고 CSRF가 없으면 403임을 실제 handler로 확인했습니다. 로그인에는 빈 body 거부 400을 확인했습니다. 정상 업무 실행·SDK·validator·후보 기능 동등성은 이 시험의 범위가 아닙니다.

| 별도 경로 | 현재 owner·consumer | 신규 계약·검증 |
|---|---|---|
| `GET /health` | `app/handlers.go`; Dockerfile·Compose·healthcheck | 세션/세대 없는 기존 liveness 유지 |
| `GET /admin/api/ws/system-stats` | `app/handlers.go`; `hooks/useWebSocket.ts` | metadata 선확인, 정확한 `admin-stats.<generation>` 협상 후 frame 소비 |
| `GET /admin/api/openapi.json` | `app/handlers.go`; Swagger UI | 인증·feature flag 유지, API 세대 검사 적용 |
| `GET /admin/docs` | `app/handlers.go`; 문서 사용자 | HTML 인증·flag 유지, 내부 fetch는 단일 SDK |
| `GET /assets/*filepath` | `static/static.go`; production HTML/JS | 1년 immutable, 누락 자산은 404 |
| `GET /favicon.svg` | `static/static.go`; HTML | 1일 cache, SVG MIME |
| `GET /theme-init.js` | `static/static.go`; HTML | no-cache, 외부 script, theme 초기화 |
| NoRoute/NoMethod | `app/routes.go` | 현재 `/admin/api/` 404 JSON·등록 경로의 405 JSON; 다른 NoRoute는 SPA 200. 신규는 API/asset 오인 HTML 금지 |
| 신규 `/admin/meta.json` | 현재 없음 | public no-store; HTML·304·헤더/세대 누락을 성공으로 처리하지 않음 |

메뉴 정본은 `frontend/src/routes/route-definitions.ts`와 `manifest.ts`의 stats/streams/members/calendar/alarms/rooms/settings 7개입니다. Docker는 settings 메뉴 안의 별도 기능입니다. 인증·테마·다중 탭·경고 modal·mobile drawer·focus·긴 ID와 한글·가상화도 공통 기능으로 기록했습니다. 후보 화면별 필수 상태와 tested/total은 T07/T10 실행 시 확정하며 현재 미실행입니다.

숨은 API는 `GET /admin/api/holo/stats/youtube/community-shorts`입니다. 등록·명세·생성 SDK는 있으나 `frontend/src` 검색에서 화면 호출을 찾지 못했습니다. 이를 폐기 승인이나 미사용 증거로 간주하지 않습니다. `dockerApi.checkHealth`는 설정의 Docker 컴포넌트에서 사용합니다. members alias 추가/제거도 hook과 화면에서 사용합니다.

인증·Docker·status의 `api/core.ts`와 stats의 `features/stats/api.ts`는 직접 Axios 경로입니다. 나머지 주요 feature API는 생성 Admin을 쓰지만 `adminClient.ts`가 생성 후 instance를 재할당합니다. T02/T05/T08에서 하나의 주입 transport로 함께 이전할 consumer입니다.

## 소비자 검색 범위와 C01

확인한 검색어는 `/admin/api`, `30190`, `admin-dashboard`, `ADMIN_DASHBOARD`입니다. 다음 범위를 구분해서 조사했습니다.

| 범위 | 결과·owner | 한계 |
|---|---|---|
| `admin-dashboard/frontend/src` 전체 소스·생성 SDK | 위 feature/core/WS/문서 consumer | 화면 동등성 전수 실행은 아님 |
| `hololive-bot/scripts`, `deploy`, `.github`, `docs/current` | health gate·Compose·ingress·build/CI·문서 | 호스트 밖 사용자 스크립트는 포함하지 않음 |
| `hololive-bot/hololive` Go 소스 | 설정/배포 회귀 fixture와 internalhttp 시험 | 별도 BFF 업무 호출 소스는 발견하지 못함 |
| `chat-bot-go-kakao/internal`, `cmd`; `twentyq-bot/internal`, `cmd`; `iris-client-go` Go 소스 | 비테스트 코드에서 BFF 호출 발견하지 못함 | 없는 ChatBotGo `config/` 경로의 검색 오류는 실제 소유 소스 범위로 다시 검색하여 해소 |
| `shared-go/pkg/httputil/login_ratelimit.go`, adoption 문서 | BFF가 사용하는 shared library | BFF 외부 소비자가 아님 |
| `iris-client-go/README.md` | Iris client/webhook 방향과 인증 경계 | Iris root/native/tools를 검색하지 않았음; 이 문서만으로 임의의 Iris 확장이나 외부 자동화 부재를 주장하지 않음 |

**C01 조사 결과:** 운영자 답변만을 선행조건으로 두지 않고 직접 확인했습니다. Loki의 336시간 보관 설정을 확인하고 UTC `2026-08-26T00:00:00Z`~`2026-09-09T05:10:00Z`를 조회했습니다. 중앙과 서울 ingress에서 관리자 API·문서 관련 기록이 각각 142건이며, method/정규화 path/status 집계가 일치합니다. 같은 요청의 두 hop이므로 284건으로 중복 집계하지 않습니다. 중앙 142건은 전부 브라우저 식별자·서울 gateway source이고, 서울 142건은 전부 admin origin입니다. `/admin/docs`·`/admin/api/openapi.json` 요청은 0건입니다. 두 host에서 14개 일별 구간 모두 로그 존재를 확인했습니다.

kapu·hololive-osaka·iris-seoul의 설치된 systemd/cron·`/usr/local/bin`·`/usr/local/sbin`, kapu의 user unit·local bin·workspace tools를 같은 endpoint/port/domain 검색어로 조사했습니다. 별도 BFF 업무 소비자를 발견하지 못했습니다. 원격 두 host의 사용자 crontab은 없고, kapu crontab에도 일치 항목이 없었습니다. BFF의 persistent audit 파일도 raw 내용을 출력하지 않고 집계했습니다.

**범위 판정:** 현재 source·설치 automation·보관 트래픽에서 확인된 runtime consumer는 동시 교체 대상인 관리자 웹 UI와 유지 대상인 `/health` 진단입니다. 이를 T01→T02 로컬 구현의 동결 근거로 사용합니다. User-Agent를 신원 증명으로 취급하거나 보관 이전·장기 비활성·관리되지 않는 개인 장치의 소비자 부재까지 주장하지 않습니다. 별도 소비자가 새로 발견되면 owner·operation·동시 전환 범위를 기록하고 관련 종속 작업을 재평가합니다. AC01/G06의 후보 검증을 통과했다는 뜻은 아닙니다.

## 보존할 보안·자원 계약

ASVS 2.1.1의 입력 규칙, 7.1.1의 세션 수명, 8.1.1의 접근 규칙을 아래와 manifest에 연결합니다. ASVS 전체 준수나 L2/L3 검증 완료를 의미하지 않습니다. MFA는 추가하지 않습니다.

| 경계 | 현재 실제 값·동작 | owner·기존 검증 | 신규 검증 담당 |
|---|---|---|---|
| local login limiter | 5분 동안 5회 실패, 15분 lockout, 1분 청소, identity 10,000 상한 | `shared-go/pkg/httputil/login_ratelimit.go`; `TestLoginRateLimited` | T03 |
| distributed limiter | 15분, IP 10·설정된 account 30·global 200; 성공 시 IP/account만 삭제 | `app/login_limiter.go`; `login_limiter_test.go` | T03; Valkey 오류에서 거부하는 실행 회귀 추가 |
| bcrypt slot·타이밍 | `max(GOMAXPROCS(0),1)` slot, 대기열 없이 429; 잘못된 사용자명에도 bcrypt 비교; cost 최소 10 | `app/session_handlers.go`, `app/app.go`; `TestLoginRejectsWhenPasswordHashCapacityIsExhausted`, `TestLoadRejectsWeakAdminPasswordBcryptCost` | T03; 잘못된 사용자명 비교 실행 여부 검증 보강 |
| 로그인 실패 지연 | 실패 횟수×500ms, 최대 3초, context 취소 감지 | `waitForLoginBackoff`, `TestWaitForLoginBackoffStopsWhenRequestIsCanceled` | T03 |
| body/header | login 16 KiB, heartbeat 1 KiB, holo 변경 2 MiB, upstream/Docker 응답 8 MiB, drain 64 KiB; HTTP header 16 KiB | `session_handlers.go`, `holo/handlers.go`, `holo/client.go`, `docker/client.go`, `app/app.go` | T02/T03/T04; endpoint별 입력 allowlist와 오류 envelope |
| HTTP 시간 | header 5초·read 15초·write 60초·idle 120초; shutdown 20초 | `app/app.go#Run` | T04/T09; 효과 완료와 timeout 구분 |
| session 수명 | TTL 30분·절대 8시간·heartbeat 5분·회전 15분·유예 30초·idle 10분·idle 경고 9분·idle TTL 10초·절대 경고 5분 | `config.DefaultSessionConfig`, session integration tests | T03 |
| 세션 설정 입력 | `SESSION_TOKEN_ROTATION`, `SESSION_HEARTBEAT_INTERVAL_MS`, `SESSION_ABSOLUTE_WARNING_WINDOW_MS`, `SESSION_IDLE_TIMEOUT_MS`, `SESSION_IDLE_WARNING_TIMEOUT_MS` | `config.LoadSessionConfig` | T03; 기존 이름 유지 |
| family 원자 폐기 | family lease·CAS 회전·동시 logout; 저장소 실패는 503, 폐기 확인 실패를 성공으로 응답하지 않음 | `session/lifecycle.go`, `family_integration_test.go`, `logout_family_test.go`, `logout_failure_test.go` | T03/T05 |
| admin session namespace | `session:admin:` 및 그 아래 `family:`; 로그인 limiter는 별도 `login:admin:limit:` | `session/session.go`, `app/login_limiter.go` | T09; 운영 실제 소유/폐기 계획 확인 전 데이터 변경 금지 |
| CSRF/cookie | session 결합 HMAC, cookie/header 비교, HttpOnly session, SameSite Strict, Secure는 FORCE_HTTPS | `auth/`, `app/middleware.go`, `session_status_csrf_test.go` | T03/T05; 지연 Set-Cookie 브라우저 시험 |
| WS | 전체 16·family별 4, 폐기/절대 만료/store 실패 1초 감시, handshake 5초·write 5초·pong 60초·ping 54초, inbound 512 bytes | `app/handlers.go`, `websocket_session_security_test.go` | T04/T05; origin 검증을 우회해 upgrader를 만들 수 없게 소유 강화 |
| config file | regular file, symlink 거부, open 전후 SameFile, 64 KiB, embedded NUL/개행 거부, `_FILE`+직접값 충돌 거부 | `config/secret_files.go`, `secret_files_test.go` | T03; 현재 owner/mode 허용 검사는 없음. 신규 loader에서 추가 |
| 서명 입력 | `LoadSecure` 최소 32 bytes; 내부 `Load`만의 16-byte 검사는 진입점 보장이 아님 | `cmd/admin-dashboard/main.go`, `TestLoadSecureRequires32ByteSessionSecret` | T03; process 환경 materialization 제거 |
| proxy·production | `TRUST_FORWARDED_HEADERS` 사용 시 `TRUSTED_PROXY_CIDRS` 필수; CSRF/WS enforce, 명시 origin; FORCE_HTTPS 기본 true | `config/config.go`, `middleware_client_ip_test.go`, `origins_test.go` | T03/T09; 실제 배포값은 미검증 |
| 감사·응답 | 서버 생성 request ID, route≤256/target≤128, token/body를 감사 항목에 넣지 않음; upstream 오류의 제한된 필드만 전달 | `app/audit.go`, `audit_test.go`, `holo/client_io_test.go` | T03/T04; 새 envelope에서 upstream 401과 세션 401 구분 |
| CSP·static | `app/csp.go`의 최종 CSP가 기존 middleware를 덮음; index/theme no-cache, assets immutable | `TestHealthAndSecurityHeaders`, `static/static_test.go`, 프런트 `index-html.test.ts` | T03/T08; 최종 header owner 하나 |

Docker 앱은 현재 관리 prefix와 migration 제외 규칙을 사용하며, 전용 proxy는 정확한 이름을 허용합니다. proxy의 start/stop/restart 대상은 `hololive-api`, `hololive-alarm-worker`, `hololive-youtube-collector-c`입니다. `holo-postgres`, `valkey-cache`, `deunhealth`, `admin-dashboard`, `admin-dashboard-ingress`, `admin-docker-proxy`는 start/restart만 허용합니다. inspect/exec/create·ID 요청은 proxy에서 허용하지 않습니다. T04는 두 경계를 공통 비밀 없는 정책에서 생성하고 독립 거부 fixture로 검증합니다. BFF/ingress/proxy 자기 restart의 응답 유실은 unknown이며 자동 재실행하지 않습니다.

기존 조회 retry 1회와 WS reconnect 5회(3초 시작, 최대 30초 backoff), session CAS 충돌 한정 반복은 각각의 기존 소유 계약을 조사하여 이전합니다. 이를 업무 변경 retry로 확대하지 않습니다. malformed streams를 빈 배열로 치환하는 부분과 구형 stats payload 허용은 retirement 대상이며 정상 빈 배열·명시된 이미지 placeholder는 보존합니다. 마지막 연결 경로 대조에서 구형 세션의 FamilyID/LastRotatedAt 보정과 WS session ID 대체도 `R19`로 추가했습니다. 총 폐기 추적 항목은 19개이며, session 알고리즘 재사용과 구형 필드 보정 제거를 구분합니다.

설정 alias `ADMIN_PASS_BCRYPT`, `ADMIN_SECRET_KEY`, `HOLO_BOT_URL`, `API_SECRET_KEY`와 4개 `_FILE` 입력은 기존 runbook 계약입니다. 구형 내부 loader를 없애도 입력 이름은 제거하지 않습니다. 새 runtime 의존성·새 alias는 추가하지 않았습니다.

## 성능·전환·artifact 동결

측정 조건은 [performance.md](performance.md)에 후보 결과를 보기 전에 고정합니다. p95 +15%, 정상 RSS·초기 JS 전송량 +10%를 보존합니다. 실제 baseline/후보 성능 결과는 NOT RUN입니다.

구형 artifact는 위 **실행 중 image ID**와 당시 deploy tree, 실제 embedded old bundle의 hash/file list를 T09에서 함께 확보합니다. 운영 image 밖의 로컬 전용 artifact 경로에 export하고 SHA-256·revision·architecture를 기록하며, 테스트 환경에 운영 secret·업무 DB를 복사하지 않습니다. 원격 image tag 변경이나 transfer/install은 이 조사에서 수행하지 않았습니다. 소스 재빌드나 현재 lock으로 만든 bundle은 실제 old bundle 검증을 대신하지 않습니다. 복구 archive 소유·위치·용량·접근 가능 여부는 T09의 미충족 입력입니다.

첫 drain의 owner는 `deploy/nginx/admin-dashboard-ingress.conf.template`, host 접근 제어, 구형 BFF와 upstream입니다. ingress는 30191 관리자와 30192 short-link를 한 프로세스에서 처리합니다. 현재 admin proxy read/send timeout은 3600초이며, 별도 admission/receipt API는 구형 BFF에 없습니다. 설정 reload만으로 이미 연결된 worker·keep-alive·WS가 모두 닫힌다고 가정하지 않습니다.

T09 리허설은 실제 old artifact와 fake upstream의 call/effect ledger에서 다음을 입증해야 합니다.

1. 관리자 origin 신규 변경, 열린 탭, 기존 keep-alive, WS, loopback 직접 접속을 모두 fence합니다. 동시에 short-link 30192와 다른 업무 서비스가 유지되는지 검사합니다.
2. fence 전에 접수된 16종 업무 변경을 확인된 성공/부분 효과/거부/unknown으로 분류합니다. 시간 경과·연결 0·context 취소·프로세스 종료는 업무 완료 근거가 아닙니다.
3. 기존 WS·프로세스 종료와 effect ledger를 대조합니다. 상한 내 판정 불가하면 unknown을 남기고 NO-GO로 종료하며 요청을 재생하지 않습니다.
4. 정비 중 새 BFF/assets/proxy를 동일 세대로 검증하고, rollback도 새 서명 secret과 관리자 전용 session만 폐기하는 절차로 리허설합니다. 공유 DB/Valkey 전체 flush나 snapshot 복원은 금지합니다.

기존 artifact에 admission 패치의 선행 배포가 필요하다는 증거가 생기면 운영 전환 1회 제약의 실제 충돌로 기록합니다. 이를 몰래 별도 배포하거나 현재 accepted 결정을 근거 없이 proposed로 되돌리지 않습니다.

F07 runbook의 중앙 build-wrapper 실행 및 API 동반 재생성 설명 충돌은 여전히 T09 수정 대상입니다. 실행 절차는 kapu local build 후 hololive-osaka의 검증된 image에 `up -d --no-build --no-deps`를 적용하는 범위입니다. 소비자 조사 중 실제 `admin-dashboard-ingress-firewall.service`가 loaded/disabled/inactive임을 확인했습니다. Nginx의 source allow/deny와 BFF loopback publish는 존재합니다. 문서의 활성 nft unit 전제와 실제 상태 차이는 O02로 기록하며, T09/G07 전에 정합화하고 검증합니다. 이번 조사에서는 방화벽이나 unit을 변경하지 않았습니다.

## 실제 검증

명령의 기본 위치는 `hololive-bot/`입니다. 테스트는 변경 전 기초 회귀와 T01에 추가한 검사로 구분합니다.

| 명령 | 관찰 결과 | 범위 |
|---|---|---|
| `go test -count=1 ./admin-dashboard/backend/internal/...` | PASS, 10 packages | T01 새 검사 추가 전 기존 backend 회귀 |
| `go test -count=1 ./hololive/hololive-api/internal/planes/admin/app/http/... ./hololive/hololive-api/internal/planes/admin/internal/server/api/... ./hololive/hololive-api/internal/planes/admin/internal/service/auth/...` | PASS, 3 packages | upstream 경계의 기존 회귀; auth는 dbtest의 격리 DB 사용 |
| 프런트 `corepack npm test` | 최초 FAIL | 사용자 bus 환경 누락으로 systemd-run browser 실패, BaseModal도 완료 DOM 미확인 |
| 프런트 `env XDG_RUNTIME_DIR=/run/user/1000 DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus corepack npm test` | PASS, 156 tests, skip 0 | 소유 사용자/기존 bus socket 확인 후 실행 환경만 보완; Chromium 기존 시험 포함 |
| `go test -count=1 -v ./admin-dashboard/backend/internal/app -run '^TestBaselineRouteInventory$'` | PASS, 33 subtests·7 exceptions | 실제 등록·명세·목록 대조 및 인증/CSRF 거부 |
| `bash scripts/architecture/check-admin-contract.sh --baseline` | PASS | manifest 필수 항목·spec hash·실제 route/access 및 health/404/405 JSON·nosniff 검사; 신규 SDK/validator 검사는 NOT RUN으로 명시 |
| `go test -race -count=1 ./admin-dashboard/backend/internal/app` 및 `go vet ./admin-dashboard/backend/internal/app` | PASS | 기존 app 회귀와 T01 검사; 검사 책임을 helper로 나눈 뒤 변경한 두 T01 시험에 race 재실행도 PASS |
| repository `ensure_golangci_lint`의 `run -c .golangci.yml ./admin-dashboard/backend/internal/app/...` | PASS, 0 issues | 최초 maintidx/공백 지적은 테스트의 목록 읽기·등록·접근 검사 책임을 분리해 해소; suppression 없음 |
| `shellcheck scripts/architecture/check-admin-contract.sh`, `bash -n scripts/architecture/check-admin-contract.sh` | PASS | 새 검사 스크립트 |
| Node built-in JSON/path 대조 | PASS | 명세 pointer 151개·현재 파일 참조 257개·메뉴 7개·초기 폐기 항목 18개 존재 확인; R19의 함수·caller는 별도 소스 검색으로 확인 |

새 검사에서 manifest가 없는 초기 상태가 실패하는 것도 확인했습니다. 문서 카탈로그의 최초 stale 결과는 canonical `render`로 갱신했습니다. 브라우저 시험 후 해당 task 단위의 systemd user unit이 남아 있지 않음을 확인했습니다. 이 결과는 후보 SDK/validator/G01~G12의 통과가 아닙니다. 새로운 운영 실패 경로를 바꾸지 않았습니다. **Fallback delta: none.**

## 미충족 입력과 다음 작업

| 항목 | 담당·종속 단계 | 해소 근거 |
|---|---|---|
| C01 외부 consumer 조사 | T01 로컬 구현 범위 동결 | consumer-audit.json의 실제 ingress·Loki·설치 automation 근거; 장기 비활성 소비자의 부재를 단정하지 않음 |
| D01 실제 첫 drain | T09→T10/G11 | old artifact·차단·기존 연결·effect ledger 리허설 |
| A01 old archive | T09→G08/G11 | 실제 bundle/image/deploy tree의 hash·보관·복구 접근 |
| O01 secret/접근 제한 | T09→G07/G11 | owning manifest의 값 없는 metadata와 운영 접근 거부 증거; stack-platform-ops 경유 |
| O02 방화벽 unit 전제 불일치 | T09→G07 | 실제 disabled/inactive; 적용된 source 제한과 문서의 pre-HTTP fence 전제를 대조하여 승인 범위에서 정합화 |
| P01 측정 harness | T01 조건 동결, T10 실행 | performance.md의 조건을 구현한 실행 명령과 기준/후보 측정 |
| R01 검토·인수 담당자 | T10/G12 | 작성자와 구별한 검토자·운영 책임자의 인계 수용 |

T01 목록 작성만으로 AC02/V03, AC08/V06/V10을 완료 처리하지 않습니다. 소비자 조사 근거로 B01을 해소한 뒤 T02의 2020-12 생성기/validator spike를 먼저 검증하고, 종속 계약을 확정합니다.
