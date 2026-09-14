# Admin Dashboard Runbook

## Role

`admin-dashboard`의 웹 소스·OpenAPI·인증·세션·보호된 변경 요청은 Iris Admin이 소유합니다.
기존 홀로 디자인을 유지하는 React SSR·PWA와 Rust gateway를 사용합니다.
이 저장소는 Compose 서비스·업무 API·배포 연결을 소유합니다. 서비스·기본 이미지·실행 파일 이름과 포트 30190은 유지합니다.

## Dependencies

- Iris Admin의 검증된 웹 image. `ADMIN_DASHBOARD_IMAGE`와 해당 저장소의 `IRIS_ADMIN_REVISION`으로 고정합니다.
- Holo 업무 관리 API `https://hololive-api:30006`과 `DNS:hololive-api` SAN을 포함하는 내부 H3 인증서.
- 웹의 신뢰 파일은 발급 CA bundle `certs/iris-ca.pem`입니다. 서버 leaf `hololive-h3.crt`를 Rust 신뢰 anchor로 사용하면 실제 CA 발급 인증서 연결이 실패합니다.
- root 전용 admin secret source의 `ADMIN_PASS_HASH`(Iris CLI가 생성한 Argon2id)와 기존 `HOLO_BOT_API_KEY`.

`materialize-admin-dashboard-secrets.sh`가 `/run/hololive-bot/iris-admin-credentials`에
`login-password-hash`·`bot-hololive` 두 0400 파일을 만듭니다. 비밀 값을 환경 변수에 넣지 않고 read-only mount합니다.
Iris READ/OPS·Docker·Valkey 세션·다른 봇 자격증명은 새 웹에 제공하지 않습니다.
웹 로그는 stdout/stderr이며 기존 host log bind mount를 받지 않습니다.
임시 조회 계정용 `/run/hololive-bot/test-account`는 UID 1000의 0700 tmpfs입니다.
기존 `admin-dashboard test-account` 명령은 Iris 공통 CLI의 디렉터리·계정 ID 인자를 사용합니다.
계정은 동시에 한 개, 15분으로 고정되며 발급·폐기에 gateway 재시작은 필요하지 않습니다.
운영자 계정은 유지하고 임시 계정에는 재인증·변경 ID 선점·업무 변경 권한을 주지 않습니다.

Compose 순서는 `docker-compose.prod.yml → docker-compose.admin-security.yml → docker-compose.live-compat.yml`입니다.
호스트 127.0.0.1:30190 publish와 기존 HTTPS ingress를 유지합니다.
신뢰 proxy는 고정 network gateway 172.23.0.1 한 개이며 정확한 origin 한 개를 사용합니다.
공유 `docker-proxy`는 `deunhealth` 전용입니다.

## Build and verification

웹 빌드·계약·보안·브라우저 검사는 Iris Admin의 `scripts/verify-all.sh`가 소유합니다.
웹 image는 해당 저장소의 `scripts/build-hololive-image.sh`에서 생성합니다.
이 저장소의 `build-all.sh`는 Go 업무 image만 빌드합니다.
`scripts/ci/public-pr-frontend-gate.sh`는 웹 소유권·자격증명·네트워크·Docker 권한 제거를 검사합니다.
Go module·race·NilAway·배포 계약 검사는 기존 local-ci/pre-push 경로를 유지합니다.

## Common failure modes

- 시작 실패: Argon2id hash·0400 파일·asset/SSR image를 확인합니다. 기존 bcrypt hash는 새 웹에서 받지 않습니다.
- Holo 502/503: H3 readiness·토큰·CA 및 `hololive-api` SAN을 확인합니다. TLS hostname override로 우회하지 않습니다.
- 세션 종료: 재시작·절대 만료·로그아웃 뒤 다시 로그인합니다. GET은 유효 시간을 연장하거나 쿠키를 바꾸지 않습니다.
- 변경 403/409: CSRF·현재 쿠키·재인증·generation·중복 mutation 거부입니다. 자동 재전송하지 않습니다.
- 일부 반영/결과 불명: 저장값과 runtime 상태를 다시 조회합니다. 실패 응답을 성공이나 무변경으로 추정하지 않습니다.

## Smoke test

먼저 로컬 fixture에서 Iris 전체 검사를 실행합니다. 운영 smoke는 승인된 image·origin에서 수행합니다.
비밀이나 응답 body 전체를 기록하지 않고 health·metadata no-store·업무 SSR·로그아웃을 확인합니다.
Docker/status/system-stats·다른 봇·Iris 관리 경로가 거부되는지 확인합니다.
로그인 후 쿠키는 Secure·HttpOnly·SameSite=Strict이며 GET/SSR에는 Set-Cookie가 없어야 합니다.

세션은 15분 rotation·이전 쿠키 30초 유예·서버 유휴 30분·절대 8시간입니다.
논리 세션과 CSRF가 유지되어 초안·변경 ID가 회전으로 초기화되지 않습니다.
새 mutation은 현재 쿠키로만 선점하고 family 전체 logout이 이전/현재 쿠키를 함께 폐기합니다.

## Rollback

전환 전 image ID·각 저장소 commit·Compose·이전 비밀 저장소 revision을 확보합니다.
관리 origin의 정비 상태에서 이전 image와 일치하는 Compose·비밀 계약을 함께 복원하고 다시 로그인합니다.
구형 웹 복원은 Docker 권한을 되살릴 수 있으므로 필요한 접근 제한을 함께 확인합니다.
기존 H3 클라이언트에 영향을 주는 인증서 rollback은 별도 판단합니다.

인증서 SAN 추가·Argon2id 전환·admin 전용 proxy 컨테이너 제거·PWA와 실기기 검증의 상세 순서는
[Iris Admin 홀로 운영 문서](https://github.com/park285/iris-admin/blob/main/docs/operations/hololive-web.md)를 따릅니다.
2026-09-14 코드 이관 작업에서는 운영 배포·재시작·실제 계정 변경을 실행하지 않았습니다.
