# T05 앱·세션·세대 경계 검증

2026-09-09, `DEC-20260909-hololive-admin-bigbang-replacement`. T05 구현과 집중 검증을 기록합니다. T06~T10의 업무 변경 모델·전체 화면·실제 구형 artifact 검증을 완료한 기록은 아닙니다.

## 소유권과 세대

`frontend/src/app/bootstrap.ts`가 API client·session·세대 gate·탭 사건과 UI 정리를 조립합니다. `api/client.ts`는 주입된 정책으로 Axios 한 개와 generated SDK를 만들며 `api/transport.ts`는 계약 검증·전송 1회·정책 사건만 담당합니다. API 계층의 Router/store import와 401 interceptor redirect를 제거했습니다. 임시 Docker/status owner인 `api/core.ts`의 나머지 제거는 T07/T08에 남습니다.

첫 `/admin/meta.json`은 generated SDK로 검사합니다. 응답 header·body의 clientGeneration과 no-store를 확인하며 누락·다른 세대·HTML·깨진 JSON·잘못된 객체·cache 정책 부재에서 앱을 활성화하지 않습니다. 이후 세대 증거가 없거나 다른 응답을 받으면 새 API 호출과 WS 연결을 막고 민감 query cache·member modal·session warning·인증 상태를 정리합니다. 일치하는 metadata를 나중에 받더라도 확인된 불일치 gate를 재활성화하지 않으며 새로고침을 요구합니다.

`session/state.ts`는 authGeneration과 csrfVersion을 분리합니다. `session/controller.ts`가 현재 세대인지 확인한 응답만 적용합니다. 이전 401, 지연 GET, 취소된 GET은 새 로그인/CSRF를 바꾸지 않습니다. 로그인 실패의 401은 일반 세션 폐기 사건과 구분합니다. 로그아웃은 `revocation: confirmed|unknown`과 `clientCleanup: completed`를 별도로 반환하며 UI는 서버 폐기 불확실성을 표시합니다. 이전 `useAuthBootstrap`, `lib/sessionLifecycle`, `api/adminClient`와 core의 수동 heartbeat parser를 제거했습니다.

## 쿠키 순서

일반 BFF 인증 거부에서 Set-Cookie 삭제를 제거했습니다. 오래된 요청의 401이 새 로그인 쿠키를 지우지 않습니다. 실제 HTTP cookie jar 시험에서 old GET을 보류하고 로그인한 후 old 401을 반환해도 새 쿠키와 인증 GET 200이 유지되었습니다. 명시적 logout·heartbeat 만료의 쿠키 정리는 유지합니다.

로그인·세션 GET·heartbeat·logout은 `session/cookie-lock.ts`에서 같은 origin의 Web Lock을 공유합니다. 인증 동작은 ifAvailable로 즉시 BUSY를 반환하고 대기·재전송하지 않습니다. 조회 대기만 기존 API timeout인 30초 안에서 허용합니다. 화면이 취소되어도 전송한 인증 응답의 쿠키 처리가 끝나기 전에 잠금을 해제하지 않습니다. JS의 stale 응답 검사로 이미 적용된 Set-Cookie를 되돌린다는 가정을 사용하지 않습니다. [Web Locks 명세](https://w3c.github.io/web-locks/)의 callback 완료/잠금 해제와 ifAvailable 계약을 확인했습니다.

`session/channel.ts`는 login/logout/rotation/local_cleanup 사건 종류만 전송합니다. token·cookie·session ID·사용자 payload를 포함하지 않습니다. 다른 탭은 현재 쿠키로 세션을 조회합니다. 활동 시각 동기화는 인증 사건과 분리하며 기존 idle/heartbeat 상한을 유지합니다. Web Locks 부재에서는 인증을 우회하지 않습니다.

## 실제 브라우저 검증

`src/hooks/session-tabs.browser.test.mjs`는 bounded systemd user cgroup에서 Chromium, Firefox, WebKit을 실행합니다. 실제 HTTP 응답의 HttpOnly cookie를 공유하는 두 탭에서 아래를 확인했습니다.

- 보류된 세션 GET(화면 취소 포함)·heartbeat·logout 동안 새 로그인은 `SESSION_BUSY`이고 로그인 HTTP 호출은 0회입니다. 보류 응답이 끝난 뒤 명시적으로 로그인하면 최신 쿠키가 유지됩니다.
- 일반 조회의 지연 401과 새 로그인이 겹쳐도 현재 인증을 유지합니다.
- CSRF 403은 POST 재실행 없이 GET으로 복구하고 다음 정기 heartbeat에서 사용합니다. 취소 3회는 실패로 세지 않으며 서버 실패 3회와 idle 전환의 기존 종료 동작을 보존합니다.
- 실제 logout UI가 서버 폐기 불확실성을 표시하고 다른 탭도 서버 세션을 확인합니다.
- 잘못된 metadata 6종에서 인증/업무 요청은 0회입니다.
- 실제 WS handshake는 `admin-stats.<clientGeneration>` 하나를 요청합니다. 협상 누락 상태에서 프레임은 0개이고 최초+재연결 5회 모두 metadata를 먼저 검사합니다. 일시적인 metadata 실패 후 재연결은 허용하지만, 확인된 세대 차이 뒤에는 추가 handshake/API 요청이 0회입니다.
- C01 정정에 따라 방 단위 알람 그룹의 키보드 펼침·독립적인 방 이름 수정·방/채널 삭제 callback과 큰 ID를 실제 렌더링에서 확인합니다. 미지원 사용자 이름 버튼은 없습니다. 이는 전체 AlarmsPage 업무 시나리오 완료를 뜻하지 않습니다.

이 실행형 시험이 heartbeat/WS 소스 문자열 검사와 미지원 userName을 기대하던 알람 버튼 검사를 대체했습니다. 명시적 any 금지는 TypeScript AST의 AnyKeyword로 검사하여 `AbortSignal.any` 같은 실제 API 이름을 타입 우회로 오인하지 않습니다.

WebKit의 부족한 native test library는 [환경 manifest](webkit-test-environment.json)의 61개 공개 Ubuntu package를 `/tmp/hololive-admin-webkit-deps.CS2ksX`에 추출했습니다. 호스트 패키지를 설치/업그레이드하지 않았습니다. Playwright의 dlopen 점검이 ldconfig cache를 읽으므로 bwrap namespace 안에서만 사설 cache와 `/dev`를 제공합니다. 라이브러리·host 검사 skip flag는 사용하지 않았습니다. ESM의 401을 받은 FFmpeg package는 접근 가능한 공개 archive의 정확한 버전으로 준비했습니다.

## 검증 기록

- session/controller + transport 집중 시험: 10/10 통과.
- auth/Docker/MSW/settings API integration: 99/99 통과. empty body/204 사례는 세대 header를 유효하게 둬 응답 의미만 검사하며, 세대 누락 자체는 별도 gate/browser 시험에서 검증합니다.
- `corepack npm run lint`, `corepack npm run build` 통과. build는 500 kB 초과 chunk 경고를 냈으며 T10의 동결 JS 전송량 예산 PASS로 치환하지 않습니다.
- `bash scripts/ci/admin-dashboard-go-ci.sh` exit 0: gofmt, workspace/tidy drift, vet, staticcheck, golangci-lint 0 issues, NilAway, build, 전체 test·race, govulncheck 통과. client/store close도 lifecycle callback 안에서 수행하고 deadline 초과를 오류에 포함하는 최종 변경을 검증했습니다. 호출 코드·import package 취약점은 0개이며 module-only 미사용 openpgp advisory는 T03과 같습니다. 로그: `/tmp/hololive-admin-t05-go-ci.log`.
- 전체 프런트 테스트: 159/159 통과, skip 0. 이 실행 안의 Chromium/Firefox/WebKit 시험도 3/3 통과했습니다. 로그: `/tmp/hololive-admin-t05-front-test.log`.
- metadata 재시도 중 보호 화면이 세션 확인을 기다리도록 정리한 마지막 변경은 Chromium의 실제 보류 응답·재시도 시험에서 통과했습니다. 로그: `/tmp/hololive-admin-t05-bootstrap-final.log`. 이 변경 이후 같은 후보 전체 검증은 T10에서 다시 묶습니다.
- 마지막 `corepack npm run lint`와 `corepack npm run test:contract`도 통과했습니다. generated validator/Go DTO/clean fixture 재생성 6개, transport 4개입니다.

전체 프런트 시험 명령은 다음 환경을 사용합니다. 테스트 경로만 가리키며 운영 설정에는 적용하지 않습니다.

```sh
env XDG_RUNTIME_DIR=/run/user/1000 \
  DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus \
  ADMIN_TEST_LDCACHE=/tmp/hololive-admin-webkit-deps.CS2ksX/ld.so.cache \
  LD_LIBRARY_PATH=/tmp/hololive-admin-webkit-deps.CS2ksX/root/usr/lib/x86_64-linux-gnu:/tmp/hololive-admin-webkit-deps.CS2ksX/root/usr/lib/x86_64-linux-gnu/gstreamer-1.0 \
  GST_PLUGIN_PATH=/tmp/hololive-admin-webkit-deps.CS2ksX/root/usr/lib/x86_64-linux-gnu/gstreamer-1.0 \
  corepack npm test
```

Fallback delta: 일반 인증 거부의 쿠키 삭제와 이전 401의 강제 redirect 경로를 제거했습니다. 업무 변경 재시도·호환 경로·인증 우회는 추가하지 않았습니다. 기존 조회/WS 복구의 상한은 유지합니다.
