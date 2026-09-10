# Hololive 관리자 빅뱅 교체 계획 검토

2026-09-09. 사용자 요청은 “리뷰후 작업문서화 해보자”입니다. 이번 산출물은 소스 대조 검토와 후속 작업 설계이며 구현·의존성 설치·운영 검증 결과가 아닙니다.

## 검토 결론

단일 릴리스, 기존 upstream 계약 보존, 신규 실행 경로의 단일화라는 방향을 유지하고 아래 F01~F07을 [작업 문서](../../../docs/agent-workflows/plans/2026-09-09-hololive-admin-bigbang-replacement.md)에 반영했습니다. 사용자의 MFA 제외와 PLN 등록 차단 해소 지시에 따라 보완 설계를 확정했습니다. 출시 준비 완료나 배포 승인을 뜻하지 않습니다.

## 기준과 확인 범위

| 항목 | 확인한 내용 |
|---|---|
| 입력 | workspace 루트 `hololive_admin_bigbang_replacement_plan.md`, v2.0, 검토 전 SHA-256 `b723ada3334b6f626244ae584f6e9efbdcd8ea286bac96393ed711d596332aae` |
| 원안 기준 | `hololive-bot@6ba2ede142501eb5bc436f9d1af2587a3f1d81b0` |
| 검토 기준 | `hololive-bot@2323f98c8446f42de581c6f8d2e67e408e3d92df`; 시작 시 작업 트리 변경 없음 |
| 기준 사이 차이 | `admin-dashboard/AGENTS.md`, `frontend/package-lock.json`만 변경; 대상 backend 소스·보안 overlay·admin runbook에는 차이 없음 |
| 계약 출발점 | OpenAPI 3.1.0, HTTP operation 33개: GET 14개, 나머지 19개. 나머지는 로그인·로그아웃·heartbeat 3개와 업무 변경 16개이며, 실제 실행형 route parity 검증은 미실행 |
| 별도 등록 경로 | `/health`, `/admin/api/ws/system-stats`, `/admin/api/openapi.json`, `/admin/docs`, 자산 경로, SPA/404/405 처리. 33개만으로 전체 라우트를 대표하지 않음 |
| UI | 메뉴 manifest 7개 및 설정 내 Docker 기능 확인. 모든 화면의 동작·외부 소비자 전수 확인은 미실행 |
| 도구 | lock 기준 Query 5.102.8, Axios 1.19.0, swagger-typescript-api 13.12.6. 루트 transitive Ajv는 6.15.0이며 Ajv 2020-12 생성기·Playwright는 직접 의존성에 없음 |
| 실행 범위 | 로컬 소스·설정 template·테스트·runbook과 공식 문서 조회. 실제 credential, 운영망, DB, Docker Engine에 접근하지 않음 |

## F01 · 높음 · 세대 검사와 health·문서 경로가 충돌합니다

원안 §11은 모든 JSON API에 세대를 요구하면서 `/admin/meta.json`만 예외로 둡니다. 하지만 현재 `/health`도 JSON을 반환하고 Dockerfile과 Compose의 healthcheck는 세대 헤더 없이 호출합니다. 그대로 적용하면 정상 후보를 unhealthy로 판정할 수 있습니다. 원안 §15의 health 보존 조건과 충돌합니다. [라우트](../../admin-dashboard/backend/internal/app/routes.go), [health handler](../../admin-dashboard/backend/internal/app/handlers.go), [Dockerfile](../../admin-dashboard/Dockerfile), [Compose](../../deploy/compose/docker-compose.prod.yml)

작업 문서는 `/health`를 세대·세션 없는 기존 liveness 경로로 유지하고, `/admin/meta.json`은 별도 공개 bootstrap 경로로 명시합니다. `/admin/api/**`의 JSON 요청은 로그인과 OpenAPI 조회를 포함해 세대를 요구합니다. `/admin/docs`의 HTML 탐색은 쿠키 인증과 활성화 설정을 적용하고, 내부 API 조회는 동일 SDK로 연결합니다. 자산·SPA·WS·404·405도 endpoint inventory에 각각 분류합니다. metadata 실패·HTML·304·헤더 누락을 bootstrap 성공으로 해석하지 않는 시험이 필요합니다.

## F02 · 높음 · WebSocket의 역방향 세대 확인이 미정입니다

원안 §11의 query 또는 subprotocol 전송만으로는 신형 브라우저가 구형 서버의 세대를 확인한다는 보장이 없습니다. 구형 업그레이더에는 세대 해석·응답 계약이 없으며, 신형 앱이 잘못 연결되면 구형 프레임을 소비하거나 재연결을 반복할 수 있습니다. 일반 WebSocket 브라우저 API는 임의 응답 헤더를 읽는 인터페이스를 제공하지 않습니다. [현재 업그레이더](../../admin-dashboard/backend/internal/app/handlers.go), [WHATWG WebSocket 인터페이스](https://websockets.spec.whatwg.org/#the-websocket-interface)

보완안은 비밀 없는 `admin-stats.<clientGeneration>` subprotocol 하나를 요청하고 서버가 정확히 같은 값을 선택하게 하는 것입니다. 브라우저는 프로토콜 확인 전 프레임을 적용하지 않습니다. 프로토콜을 요청했으나 서버가 응답하지 않으면 브라우저 연결 자체가 실패하도록 정의돼 있습니다. 다만 일반 handshake 실패나 close 1006만으로 세대 불일치를 단정하지 않습니다. 최초 연결·재연결 전 SDK metadata 확인이 실패하면 연결을 시작하지 않고, 미확정 상태와 불일치를 구분합니다. 추가 자동 재연결 예외는 만들지 않습니다. 구형→신형과 신형→구형을 모두 실제 bundle로 시험해야 합니다. [WHATWG handshake](https://websockets.spec.whatwg.org/#opening-handshake)

## F03 · 높음 · 출시 전 게이트와 배포 후 인수가 순환합니다

원안 §18 G12에는 실제 artifact에 대한 운영 수용 기록이 필요하지만 §23은 12개 gate를 봉인한 후 운영 전환을 시작합니다. 전환 후 §23의 10단계에서야 수용 기록이 생깁니다. 미래의 운영 인수를 미리 PASS로 쓰거나, 모든 gate를 기다리다가 배포를 시작할 수 없는 해석이 가능합니다.

G01~G12는 모두 그대로 필수로 두되 **출시 전 G12는 리허설·runbook·담당자 인계 수용**으로 정의합니다. 실제 운영 배포의 identity, 재로그인, smoke, 관찰 종료 수용은 별도 cutover 기록으로 남깁니다. 출시 준비 완료, 운영 전환 완료, 전체 작업 완료를 구분하며, 출시 전에 운영 전환 완료를 선언하지 않습니다. 현재 G01~G12는 모두 NOT RUN입니다.

## F04 · 높음 · 첫 전환에서 구형 서버에 없는 drain 기능을 전제합니다

원안 §22~23은 구형의 admission 차단, in-flight 수·상태·효과 분류를 요구합니다. 확인한 구형 Runtime은 HTTP shutdown과 background/client 종료를 소유하지만, 외부 운영자가 사용할 변경 admission·요청 receipt inventory는 제공하지 않습니다. 신규 BFF에 기능을 구현해도 아직 운영 중인 구형 BFF의 첫 drain 문제는 해결되지 않습니다. [Runtime 수명](../../admin-dashboard/backend/internal/app/app.go), [현재 라우트](../../admin-dashboard/backend/internal/app/routes.go)

작업 문서는 기존 ingress와 독립 운영 제어로 첫 전환을 리허설하도록 보완합니다. 기존 keep-alive·열린 탭·WS와 직접 loopback 경로까지 새 변경 유입을 막고, 기존 timeout·접수 요청과 소유 API의 효과 확인을 연결합니다. 연결 수 0, 프로세스 종료, context 취소만으로 업무 효과 완료를 주장하지 않습니다. 근거를 확보하지 못하면 해당 전환은 NO-GO입니다. 구형 BFF의 선행 패치 배포가 필요하다는 결론이 나오면 “운영 전환 1회”의 의미를 바꾸므로 설계 결정을 다시 검토해야 합니다.

## F05 · 높음 · 보존할 로그인·자원 방어가 목록에서 빠졌습니다

원안 §3·10은 세션·CSRF·WS·Docker를 잘 다루지만 현재의 분산 로그인 제한, bcrypt 동시성 제한, 잘못된 사용자명에도 bcrypt를 수행하는 타이밍 방어를 작업과 시험 항목에 연결하지 않았습니다. Runtime 해체 중 소유자를 누락할 위험이 있습니다. 로컬 IP 제한 외에 Valkey의 IP·계정·전체 bucket과 저장소 오류 시 거부가 이미 존재합니다. WS도 전체 16개·세션 계열별 4개와 1초 폐기 감시가 있습니다. [로그인 흐름](../../admin-dashboard/backend/internal/app/session_handlers.go), [분산 제한](../../admin-dashboard/backend/internal/app/login_limiter.go), [Runtime](../../admin-dashboard/backend/internal/app/app.go), [WS 감시](../../admin-dashboard/backend/internal/app/handlers.go)

또한 `LoadSecure`의 32바이트 secret 하한, 파일 교체 검사, 현재 환경변수 alias는 별개입니다. 내부 구형 loader 제거가 문서화된 설정 입력을 삭제할 권한은 아닙니다. 기존 alias는 소비자와 설정 이행 범위를 조사하기 전 제거 목록에 넣지 않습니다. 새 alias는 추가하지 않습니다. [secret loader](../../admin-dashboard/backend/internal/config/secret_files.go), [설정 계약](../current/runbooks/admin-dashboard.md#key-environment-variables)

이 항목들을 baseline 보안 manifest와 소유자별 회귀 시험에 포함해야 “기존 보장 유지”를 검증할 수 있습니다. 관찰한 안전 실패·조회/스트림 재시도·이미지 placeholder도 이름만으로 전부 제거하지 말고 기존 계약과 무근거 fallback을 구분합니다.

## F06 · 중간 · 생성·검증 도구는 버전과 호출 지점까지 고정해야 합니다

원안 §9는 `components.schemas` 번들만 설명합니다. 실행 계약은 operation별 query/path/header, inline request/response schema, status·content-type, 오류 envelope까지 포함해야 합니다. schema 함수가 생성됐다는 사실만으로 모든 소비 경로가 검증된 것은 아닙니다. [현재 명세](../../admin-dashboard/backend/internal/openapi/spec.json), [OpenAPI 3.1](https://spec.openapis.org/oas/v3.1.0.html)

현재 transitive Ajv 6.15.0을 새 2020-12 생성기로 암묵 사용하지 않습니다. 직접 build dependency, format 지원, ESM 출력, SDK transport 주입 template와 fixtures를 좁은 spike에서 검증하고 버전을 고정해야 합니다. standalone validator도 Ajv runtime helper를 참조할 수 있으므로 compiler 배제와 runtime dependency 부재를 동일시하지 않습니다. 신규 runtime dependency가 필요하면 실제 패키지·버전·효과를 확인해 승인 범위를 따릅니다. [lockfile](../../admin-dashboard/frontend/package-lock.json), [Ajv standalone](https://ajv.js.org/standalone.html)

기존 `public-pr-frontend-gate.sh`도 swagger mirror와 generated 경로를 직접 검사합니다. 정본만 옮기고 CI consumer를 빠뜨리면 실패합니다. [CI consumer](../../scripts/ci/public-pr-frontend-gate.sh)

원안의 `retry=false`와 `networkMode=always` 조합은 서로 다른 책임입니다. `always`는 오프라인 상태를 무시하므로 제출 전 거부는 controller가 담당하고, 전체 interceptor·mutation 경로의 실제 호출 횟수로 자동 재실행 부재를 검증해야 합니다. 기존 Chromium 다중 탭 시험은 재사용할 행동 근거이며 Firefox·WebKit을 시험했다는 증거는 아닙니다. [Query 문서](https://tanstack.com/query/latest/docs/framework/react/guides/network-mode), [기존 브라우저 시험](../../admin-dashboard/frontend/src/hooks/session-tabs.browser.test.mjs)

## F07 · 높음 · 참조 runbook에 상충하는 배포 지시가 남아 있습니다

현재 admin runbook은 로컬 빌드·원격 no-build를 안내한 다음 중앙 호스트에서 build를 포함하는 `compose-redeploy-service.sh`를 실행하는 명령도 안내합니다. 또 `--build`로 Hololive API가 함께 재생성된다는 설명이 남아 있습니다. 실제 helper의 현재 `up` 경로는 `--no-deps`를 사용합니다. 원안을 실행 문서로 바꾸면서 이 구간을 그대로 인용하면 금지된 원격 빌드나 다른 서비스 영향으로 이어질 수 있습니다. [runbook](../current/runbooks/admin-dashboard.md#deploy-container-recreation), [helper](../../scripts/deploy/compose-redeploy-service.sh)

작업 문서에서는 `kapu`에서만 테스트·linux/arm64 image build를 수행하고, 중앙 `hololive-osaka`에는 검증한 artifact를 전달한 뒤 `up -d --no-build --no-deps`로 승인한 관리자 서비스만 전환하도록 고정합니다. 보안 overlay 적용 순서, 전용 proxy 기동 순서와 이미지 revision 확인도 포함합니다. 공유 ingress 프로세스에는 short-link listener가 함께 있으므로 컨테이너 전체 정지로 정비하지 않습니다. 실제 적용 명령은 owning release runbook과 일치하도록 T09에서 수정·리허설합니다. 이번 검토에서는 운영 절차를 실행하지 않았습니다.

## 검토 제안

`DEC-20260909-hololive-admin-bigbang-replacement`에 빅뱅 구조와 F01~F07 보완을 기록합니다. 최초 proposed였으나, 사용자가 해당 상태로 인한 PLN 등록 거부를 인용해 해소를 지시했으므로 보완 설계를 accepted / planned로 확정했습니다. 이 지시는 문서와 등록 차단 해소에 적용하며 코드·계약 적용·배포 실행 권한으로 확대하지 않습니다. 기존 `DEC-20260825-hololive-admin-auth-boundaries-preserved`와 `DEC-20260731-legacy-fade-out-no-dual-path`는 제약으로 유지합니다. upstream의 API-key 경계와 사용자 session 경계를 합치지 않습니다.

사용자의 “mfa 관련은 x”에 따라 MFA 도입·연동·등록·복구·검증은 제외합니다. 공개 진입점의 보안 게이트는 기존 로그인·세션·CSRF·네트워크 접근 제한을 검증하며 MFA를 출시 차단 조건이나 후속 필수 작업으로 넣지 않습니다.

별도 인적 검토나 subagent 검토는 수행하지 않았습니다. 이번 검토는 실행 결과가 아닌 연결된 소스·문서에 근거합니다. 특히 외부 BFF 소비자, 배포 revision, 공개 진입점의 네트워크 접근 제한, 세션 폐기 범위, 실제 복구 artifact, 정비 예산은 미확인입니다. 이를 확인하는 작업을 제거하거나 PASS로 채우지 않습니다.

## 후속 보안 검증 범위

작업 문서에 ASVS 2.1.1 입력 규칙, 7.1.1 세션 수명, 7.4.1 서버 폐기, 8.1.1 접근 규칙을 반영합니다. WS의 TLS·Origin은 ASVS 4.4.1/4.4.2로, 연결·종료·retry 상한은 13.1.3으로 연결합니다. 브라우저 지원·CSP는 3.1.1, 관리자 다중 방어는 8.4.2의 추가 검토 항목입니다. L2/L3 항목이나 ASVS 전체 준수를 이미 검증했다는 뜻은 아닙니다. 입력 검증·암호·로그 구현 시 해당 세부 요구를 소유자별로 다시 확인합니다.

## 문서 검증 기록

`bash tools/checks/check-decision-catalog.sh check --submodules`는 exit 0이며 전체 258개 DEC, 생성 색인 11개, meta/submodule pending heading 0개를 확인했습니다. 이 검사는 문서·결정 카탈로그의 정합성 검사이며 G01~G12의 증거가 아닙니다.

등록 차단은 해소했습니다. 사용자의 해소 지시와 MFA 제외를 반영해 governing DEC를 accepted / planned로 확정하고, semantic `plans create`·`plans ready`를 모두 exit 0으로 완료했습니다. `PLN-20260909-hololive-admin-bigbang-replacement`는 ready이며 `plans gate --json`은 exit 0, `strict_validation=passed`, `gate_passed=true`, `reasons=[]`를 반환했습니다. 실행 작업·인수·검증 항목 각 11개는 모두 pending으로 보존했습니다.

문서의 로컬 링크 27개, T/AC/V marker 33개와 capsule 크기·섹션 크기·공백을 점검했습니다. 코드·브라우저·성능 시험과 운영 검증은 실행하지 않았으며 출시 게이트는 모두 NOT RUN입니다.
