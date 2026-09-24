# 통합 관리자 앱 연결

관리자 앱의 대표 주소는 **https://admin.holoshi.com**입니다. Iris·ChatBotGo·홀로라이브 화면을
iris-seoul의 단일 `iris-console.service`가 제공합니다. 웹·SSR·인증·세션·PWA·native bundle은
Iris Console이 소유하며 이 저장소에는 독립 `admin-dashboard` 서비스가 없습니다.

## 계정과 권한

로그인은 기존 Iris 계정 이름과 사용자가 TTY에서 설정한 새 비밀번호 하나로 통합합니다.
모든 관리 화면의 세션과 재인증을 이 공통 계정이 소유하며 별도 홀로 계정은 없습니다.
임시 조회 계정은 발급한 범위만 사용하며 변경·운영자 세션 제어를 할 수 없습니다.
홀로 웹의 Docker·호스트 파일·원시 자원 관리 화면과 API·소켓·전용 프록시 연결은 폐기했습니다.

홀로의 화면 경로는 `/dashboard/stats`, `/dashboard/members`, `/dashboard/alarms`, `/dashboard/rooms`,
`/dashboard/settings`, `/dashboard/streams`, `/dashboard/calendar`입니다. 기존 디자인과 업무 API 계약을 유지합니다.
브라우저 쿠키·비밀 없는 탭 사건·cookie 변경 잠금·root manifest/service worker를 공통으로 사용합니다.

## 중앙 호스트의 역할

- `hololive-api`와 업무 봇은 Osaka에서 기존 이미지와 데이터·인증서를 사용합니다.
- `docker-compose.admin-web.yml`은 중앙 H3 관리 API의 승인된 Tailscale publish입니다. 웹 컨테이너가 없더라도 유지합니다.
- `admin-dashboard-ingress`는 기존 shortlink 소비자를 위해 이름을 유지합니다. 30192의 단축 링크와 30193의 health만 제공합니다.
  이전 웹 포트 30190/30191, 웹 전용 Docker proxy와 secret materializer는 사용하지 않습니다.
- 공유 `docker-proxy`·`deunhealth`, PostgreSQL·Valkey와 AP collector는 이 전환의 삭제 대상이 아닙니다.

공개 HTTPS와 Certbot 갱신은 스택 `deploy/holoshi-nginx`가, 내부 신뢰는 기존 공통 CA와 서비스별 leaf가 소유합니다.
공개 인증서를 내부 H3 private key와 공유하지 않으며 TLS 검증을 끄지 않습니다. 관리 API key와 공통 로그인 hash는
Iris의 플랫폼 secret master/manifest를 거쳐 전달하며 여기에서 중간 파일을 만들지 않습니다.

## 적용·검증과 복구

검증한 공통 앱 generation·계정 범위와 대표 origin이 먼저 준비돼야 독립 Osaka 웹을 종료할 수 있습니다.
운영 변경은 승인된 효과에 한정하며, 기존 native generation·공개 ingress 설정·Osaka deploy snapshot과
secret 복구 자료를 보존합니다. DB migration이나 봇 재시작은 이 웹 전환만으로 실행하지 않습니다.

`public-pr-frontend-gate.sh`는 독립 웹/전용 credential·Docker proxy 부재와 shortlink ingress 보존을 검사합니다.
루트 local-ci/pre-push 및 Compose/실제 Nginx 검사를 마친 뒤 로컬 빌드 산출물만 호스트에 전송합니다.
공통 인증 조회·SSR·권한 거절·logout/PWA와 기존 봇·단축 링크 상태를 확인하고 임시 조회 계정은 정확한 ID로 폐기합니다.
실제 AI 호출·메시지/푸시 발송·iOS 실기기 검증을 자동 smoke에 포함하지 않습니다.

현재 전환 상태와 검증 근거는 스택의 `DEC-20260914-admin-unified-login`을, 자세한 native 적용은
Iris Console의 운영 문서를 따릅니다.
Fallback delta: none.
