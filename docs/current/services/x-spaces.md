# X 스페이스 감지와 관리자 재연결

`DEC-20260919-hololive-x-spaces`, `DEC-20260919-x-spaces-admin-reconnect`,
`DEC-20260924-x-spaces-manual-cookie-reconnect`를 따른다.

alarm-worker가 X 웹 내부 API를 조회하고, 직접 개설한 스페이스의 시작 링크를 해당 멤버의 기존 `LIVE` 구독 방에 보낸다. 공식 유료 API·유료 공급자·녹음·게스트 참여 추적은 사용하지 않는다. X 세션은 무효화될 수 있으며 영구 인증이나 자동 로그인 성공을 보장하지 않는다.

## 관리자 복구

Iris Console → 홀로봇 → 설정 → X 스페이스 연결에서 상태, 마지막 감지 성공 시각과 후보 검증 결과를 확인한다. `auth_required`이면 사용자가 직접 X 브라우저에 로그인하여 필요한 확인을 마친 뒤 `auth_token`과 `ct0`를 관리자 입력란으로 제출한다. 이 값은 채팅·명령 인자에 넣지 않는다. 관리자 변경은 기존 비밀번호 재확인·권한·CSRF·단회 mutation 경계를 사용한다.

운영 복구는 사용자의 수동 쿠키 제출과 worker의 자동 후보 검증을 결합한다.
전용 계정 비밀번호로 자동 재로그인하는 서비스는 활성화하지 않는다.

API는 후보를 AES-256-GCM으로 암호화해 DB에 저장한다. worker는 실제 읽기 요청의 성공을 확인한 뒤에만 후보를 활성 세션으로 교체한다. 후보 인증 거부는 기존 세션을 보존한다. 일시 오류는 인증 거부로 바꾸지 않고 다음 확인 시각까지 기다린다. 세대 비교로 늦게 끝난 이전 요청이 새 세션을 덮어쓰지 못한다. 확정된 인증 필요 상태의 활성 세션에는 반복 요청하지 않는다.

HTTP 401 또는 X 오류 번호 32/89는 인증 거부다. 일반 403은 접근 거부를 포함하므로 `api_error`로 보존한다. 활성 세션의 첫 인증 거부는 `state=error`, `last_error=authentication_pending`으로 저장한다. 기존 조회 간격(120~3600초) 뒤 한 번 더 읽고 성공하면 자동 복구하며, 연속 인증 거부일 때만 `auth_required`로 확정한다. 다른 오류가 끼면 연속 거부가 아니며 정상으로 간주하지도 않는다. 확인 대기와 시각은 DB에 있어 재시작으로 초기화되지 않는다. HTTP 상태와 최대 8개의 숫자 X 오류 번호만 진단 로그에 남기고 메시지·응답 원문은 버린다.

관리자 GET 응답에는 cookie와 암호문이 없다. 평문 cookie는 관리자 HTTPS 제출 처리와 API·worker 메모리 및 단발 Node helper의 stdin에서만 사용하며, 인자·환경 변수·파일·로그에 기록하지 않는다. Node stderr 원문은 노출하지 않고 고정 오류 코드만 보고한다.

## 실행 설정

`X_SPACES_CONFIG_FILE`이 없으면 worker 수집은 비활성화된다. `X_SPACES_KEY_FILE`이 없으면 관리자 연결 제출도 비활성화된다. 잘못된 설정·키는 시작 오류다.

키는 32바이트 난수를 hex 64자로 인코딩한 private regular file이다. API와 worker가 같은 키를 읽는다. static-secret master와 host manifest를 통해 `/etc/stack-secrets/hololive-bot/x-spaces/key`에 배포하며 실행 UID만 읽을 수 있는 `0600`을 사용한다. 키 교체 시 기존 암호문을 새 키로 읽을 수 없으므로 세션 재설정을 포함한 별도 변경이 필요하다. 키를 자동 생성하거나 오류 시 다른 키로 대체하지 않는다.

인증 값을 제외한 config 예시:

```json
{
  "poll_seconds": 120,
  "targets": [
    { "user_id": "X의 숫자 계정 ID", "channel_id": "기존 YouTube 채널 ID", "member_name": "표시할 멤버 이름" }
  ]
}
```

사용자 ID와 채널 매핑은 검증된 실제 값을 넣는다. 최대 100개 계정, 조회 간격 120~3600초이며 계정·채널 중복은 허용하지 않는다. 호스트 `compose.env`의 `HOLOLIVE_X_SPACES_ENABLED=1`로 활성화하면 기존 Compose 진입점이 `deploy/compose/docker-compose.x-spaces.yml`을 추가한다. 기본값은 비활성이며 재부팅·수동 배포에서도 같은 구성을 유지한다. 새 DB migration을 먼저 적용하고 API·worker·관리자 웹의 호환 버전을 함께 배포한다. cookie 자체는 배포 파일에 포함하지 않고 관리자에서 연결한다.

## 발송과 실패 경계

관측은 `avatar_content` → `AudioSpaceById` 한 경로다. 쿠키를 보내는 요청은 `https://x.com/i/api/`의 두 읽기 경로로 고정한다. 요청 ID 생성은 `x-client-transaction-id` 0.3.1을 재사용하며 공개 home·정적 JS만 추가 조회한다. redirect와 임의 대상·쓰기 메서드는 거부한다. 개별 요청은 10초, helper 전체는 90초이고 외부 응답은 2 MiB를 넘지 못한다. API 형식 변경은 오류이며 빈 목록으로 대체하지 않는다.

시작 후 15분 이내의 현재 스페이스만 발송 대상으로 삼는다. 장애 중 끝난 스페이스와 오래된 방송은 소급 발송하지 않는다. `x_space_starts`가 최초 제목·멤버 표시명을 고정하고 기존 dispatch 원장이 `x-space:start:<space-id>` 이벤트와 방별 delivery를 중복 제거한다. X 스페이스는 전용 source를 사용하므로 YouTube 알림과 합쳐지지 않으며 텍스트 링크로 보낸다. 발송 결과 불명·재시도는 기존 dispatch 계약을 유지한다. 30일이 지난 최초 관측 자료는 회당 최대 100개 정리한다.

Fallback delta: 활성 세션의 첫 인증 거부에 동일한 읽기 경로로 한 번의 확인을 추가한다. 근거는 2026-09-21 15:19 KST authentication으로 정지한 운영 세션이 같은 쿠키를 사용한 진단에서 HTTP 200으로 성공한 사례다. owner는 alarm-worker X 수집기이며, 다음 정규 조회 간격 뒤 확인하고 두 번째 연속 거부에서는 중단한다. 실패 관측은 발송하지 않으며 오래된 관측 차단도 유지한다. `authentication_pending` 및 HTTP 숫자 진단으로 구분하고, 이 경계에서도 오탐이 발생하거나 X 오류 계약이 바뀌면 재검토한다.

알림 본문은 DB의 `X_SPACE_STARTED` 기본 템플릿을 사용한다. 기존 방송 시작 알림처럼 빨간 원, 굵은 멤버명과 제목 링크를 표시하며 멤버명·제목에는 `mdsafe`를 적용한다. 제목이 없으면 링크 주소만 표시한다. 템플릿이 없거나 잘못되면 렌더링 오류를 보존하며 하드코딩 본문으로 대체하지 않는다. 실제 전송은 기존 공통 메시지 경로를 사용하므로 방 유형과 `BOT_MARKDOWN_REPLIES` 설정에 따라 Markdown 또는 일반 텍스트로 처리된다.

```markdown
## 🔴 **카자마 이로하** 스페이스 시작
[가볍게 이야기해요](https://x.com/i/spaces/1sample)
```

## Linux 전용 계정의 자동 복구

아래 내용은 운영 비활성 상태로 보존하는 선택형 구현의 기술 기록입니다.
`DEC-20260920-x-spaces-linux-recovery`는 `DEC-20260924-x-spaces-manual-cookie-reconnect`로 대체됐으며,
이 절의 설정·기동을 운영 복구 절차로 실행하지 않습니다. 현재 절차는 위 관리자 복구를 따릅니다.

선택형 `hololive-x-space-login` 서비스는 로그인만 담당합니다. Node/Playwright 1.63.0과 같은 버전의 Chromium을 고정하고 비루트·read-only·전체 capability 제거·sandbox 활성 상태로 실행합니다. browser는 매 시도 후 종료하고 쿠키는 기존 암호화 DB 후보 경로에만 저장합니다. 비밀번호·쿠키를 환경·인자·콘솔·스크린샷·HAR·trace에 기록하지 않습니다.

보호된 계정 파일은 static-secret master의 `hosts/hololive-osaka/hololive-bot/x-spaces/login.json`에 생성하고 해당 host manifest에 `0600 1000 1000 hololive-bot/x-spaces/login.json`으로 등록한 후 정본 sync 절차로 전달합니다. 계정은 아이디(선행 @ 제외)와 비밀번호 방식입니다. 최초 입력은 사용자의 TTY에서 아래 명령으로 수행하며 비밀번호를 표시하지 않고 기존 파일을 거부합니다. 비밀 값을 셸 인자나 채팅에 넣지 않습니다.

```bash
node hololive/hololive-alarm-worker/xspaces-login/configure-login.mjs \
  /home/kapu/work/stack-secrets/hosts/hololive-osaka/hololive-bot/x-spaces/login.json 1
```

파일은 `revision`(양의 정수), `username`, `password`만 받습니다. 관리자가 계정·비밀번호를 변경하거나 중단된 시도를 재개할 때는 보호된 편집 절차로 revision을 올립니다. 새 설정 세대는 기존 세션이 정상이어도 전용 계정 연결을 한 번 시도하지만, 새 쿠키 검증 전에는 기존 활성 세션을 덮어쓰지 않습니다. 이후에는 이 서비스가 연결한 세션의 `auth_required`에서만 다시 로그인합니다. 수동으로 다른 쿠키를 제출하면 자동 복구는 해당 설정 세대에서 중단됩니다.

`HOLOLIVE_X_SPACES_LOGIN_ENABLED=1`은 `HOLOLIVE_X_SPACES_ENABLED=1`과 함께 사용하며 Compose 진입점이 전용 overlay를 추가합니다. 기본은 비활성입니다. 별도 이미지와 migration 201·202 적용이 선행되어야 합니다. SQL은 db-migrate 바이너리에 내장되므로 host의 SQL 파일만 복사하면 적용되지 않습니다.

로그인 서비스의 DB pool은 최대 1개입니다. 기존 기본 연결 예산은 55/60에 reserve 5이므로 이 서비스를 활성화하려면 다른 단일 인스턴스 풀에서 1개를 배정해야 합니다. 예를 들어 중앙 alarm-worker의 최대 연결을 8→7로 조정하고 실제 워커 설정까지 반영하면 전체 예산은 그대로입니다. 전용 서비스 활성화만 하고 예산을 조정하지 않으면 정본 capacity gate가 거부합니다. 이 문서만으로 운영 pool을 변경하지 않으며 기존 ops 배포 절차를 따릅니다.

로그인은 같은 설정·세션 세대당 한 번, 최근 1시간에 최대 1회, 최근 24시간에 최대 3회입니다. 시도 소유권을 DB에 먼저 기록하므로 다중 프로세스와 재시작도 상한을 우회하지 못합니다. helper 전체는 120초이며 부모/컨테이너 종료로 하위 프로세스를 회수합니다. 결과 없이 5분 지난 실행은 `outcome_unknown`으로 남고 자동 재시도하지 않습니다. 새로운 쿠키와 완료 원장은 한 트랜잭션으로 저장하며 기존 worker의 실제 API 검증 후에만 승격합니다.

Iris Admin은 `running`, `submitted`, `connected`, `login_required`, `outcome_unknown`, `manual_override`를 구분합니다. CAPTCHA·추가 인증·잘못된 비밀번호는 중단 상태입니다. 2단계 인증이 미설정이어도 X의 추가 확인은 발생할 수 있습니다. 보호된 Linux 브라우저의 사용자 개입 경로와 실계정 로그인은 실제로 검증하기 전까지 완료로 간주하지 않습니다.

Fallback delta: 전용 계정의 인증 거부 시에만 위 상한을 가진 재로그인을 추가합니다. 네트워크/제한 오류에 로그인하지 않고, 새 공급자·유료 API·CAPTCHA 해결 서비스로 전환하지 않습니다. 원장과 복구 코드는 Hololive Bot이 소유하며 X 로그인 화면·인증 계약 변경 시 재검토합니다.

## 검증

Node 모의 응답 테스트는 비용 경로·redirect·응답 크기·호스트·상태·인증 오류를 검사한다. Go DB 테스트는 후보 실패 보존, 세대 경쟁, 암호문 변조, 재시작·제목 변경 후 중복 제거를 검사한다. 실계정 성공과 지정 방 발송 확인은 별도 실제 증거로 기록한다.
