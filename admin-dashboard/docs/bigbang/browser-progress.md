# T10 실제 artifact 브라우저 검증

`DEC-20260909-hololive-admin-bigbang-replacement`, T10/V07/V08. [실행 결과](production-browser-local-fixture.json)는 실제 구형 arm64 image `17b56c3a…`와 후보 native image `9676dfeb…`의 HTTP/WS, 보존한 실제 구형 자산과 후보 production 자산을 사용합니다. 구형 binary는 kapu의 사설 QEMU로 실행했으며 운영 상태를 바꾸지 않았습니다. 이 기록은 G10의 native 성능 측정이 아닙니다.

`test-admin-bigbang-browser-contracts.sh <native-candidate-image>`가 Chromium 152.0.7977.82, Firefox 155.0, WebKit 26.6에서 24개 case를 통과했습니다. 모든 엔진에서 실제 구형 정상 로그인/WS control, old bundle/new BFF의 409 및 구형 WS 거부, new bundle/old BFF의 metadata 차단과 프레임 적용 0회, metadata HTML/헤더 누락/본문 세대 불일치, 7개 production 메뉴·390px 모바일 이동·drawer focus 복귀/화면 밖 닫힘, 누락된 member validator 자산의 조회 전 거부를 확인했습니다. 가짜 Holo/Docker에 업무 변경이 도달한 횟수는 0입니다. 정상 메뉴의 API 응답은 모두 200이며 앱의 CSP 위반·page error는 0입니다.

세션 쿠키는 이름·domain/path·Secure/HttpOnly/SameSite metadata만 진단했습니다. 값은 기록하지 않았습니다. 실제 backend 세션을 사용하고, 원래 artifact의 자산을 양쪽 BFF에서 모두 읽어 SHA-256을 먼저 대조합니다. 브라우저 fixture의 부모는 Docker와 root 파일 준비·정리를, 자식은 브라우저를 소유합니다. 사설 WebKit ldconfig cache의 namespace가 부모의 `sudo`에 영향을 주지 않도록 Node IPC로 고정된 old/candidate 선택과 효과 수만 전달합니다.

## 판정에서 구분한 사실

- Chrome 152는 `route.fulfill`로 합성한 document의 WebSocket을 `ERR_BLOCKED_BY_LOCAL_NETWORK_ACCESS_CHECKS`로 막았습니다. 그때 HTTP 세션은 200, CSP 위반은 0, BFF에 도착한 WS는 0이었습니다. 이 거부를 BFF의 세대 차단 증거로 사용하지 않습니다. 현재 fixture는 실제 HTTPS 응답으로 검증된 자산을 제공하며 브라우저의 LNA 검사를 끄지 않습니다. `/tmp/hololive-admin-t10-production-browser-lna-repro.log`에 재현을 보존했습니다.
- Firefox는 구형 서버가 subprotocol을 선택하지 않아도 raw WebSocket을 열고 frame을 받을 수 있었습니다. 이 raw control을 기록하며 브라우저의 자동 거부를 전제로 하지 않습니다. 현재 앱은 metadata를 먼저 검사하고 `useWebSocket`의 open/message 경계에서 선택된 protocol을 확인합니다. 실제 new bundle/old BFF는 0 frame이며, protocol 없는 서버에 대한 hook의 6회 제한과 적용 0회도 기존 세 브라우저 회귀 시험에서 확인했습니다.
- Playwright 1.63.0의 `screenshotter.ts`는 WebKit의 animation 동기화를 위해 inline `body {}` style을 넣습니다(설치된 `playwright-core/lib/coreBundle.js:21547`). 앱의 CSP 판정 후 screenshot을 생성하고, 이 도구가 만든 `style-src-elem` 거부 1건은 `screenshot_tool_csp`에 별도로 남겼습니다. 앱의 CSP를 완화하거나 엔진 검사를 생략하지 않았습니다. 재현은 `/tmp/hololive-admin-t10-production-browser-screenshot-csp-repro.log`입니다.

전체 기능의 오류·offline·초안·부분 효과·응답 유실/재전송 상태는 [기능 이관 증거](feature-migration-progress.md)와 최신 frontend 175/175(엔진 3/3, skip 0)에 연결합니다. 이 production 실행은 위에 나열한 24개 case이며 모든 업무 변경을 production artifact로 다시 실행했다는 주장은 하지 않습니다. 실제 운영 접근 제한 O02, 독립 검증 기록 작성자, 승인된 release identity와 T11 운영 수용은 별도입니다.

Fallback delta: none. 테스트의 자산 혼합/오류 주입은 운영 fallback이 아닙니다.
