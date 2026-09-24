# X 전용 계정 로그인 복구

## Role

이 문서는 운영 비활성 상태인 선택형 구현을 설명합니다.
`DEC-20260924-x-spaces-manual-cookie-reconnect`에 따라 현재 복구는 사용자가 Iris Console에
쿠키를 직접 제출하고 worker가 검증하는 [관리자 복구](../services/x-spaces.md#관리자-복구)입니다.
아래 자동 로그인 서비스를 활성화하거나 계정 설정 세대를 변경해 운영 복구를 시도하지 않습니다.

선택형 `hololive-x-space-login` 컨테이너가 전용 계정 로그인과 쿠키 후보 제출만 소유합니다.
스페이스 조회·후보 검증·방 구독·발송은 기존 alarm-worker가 계속 담당합니다.
실패한 인증을 성공으로 바꾸지 않고 사용자 개입이 필요한 경우 중단합니다.

## Dependencies

- 정본 migration 201·202가 내장된 db-migrate 적용, PostgreSQL verify-full, 기존 X 세션 암호화 키.
- Playwright 1.63.0과 고정 digest의 동일 버전 Linux 브라우저 이미지. 모든 빌드는 kapu에서 수행합니다.
- 보호된 `login.json` 파일(0600, UID/GID 1000), 명시적 X Spaces 및 로그인 활성화 설정.
- 추가 DB 연결 1개를 포함하고 reserve 5를 유지하는 capacity gate.
- read-only 루트·private tmpfs·비루트·cap_drop ALL·전용 seccomp·Chromium sandbox·init·core dump 금지.

계정 입력·시도 상한·원장 상태의 상세 계약은 [X 스페이스 서비스 문서](../services/x-spaces.md#linux-전용-계정의-자동-복구)를 따릅니다.

## Common failure modes

- `login_required`: 추가 인증 또는 로그인 거절입니다. 같은 설정 세대로 반복 로그인하지 않습니다.
- `outcome_unknown`: 외부 로그인 결과가 불명확합니다. X 계정 세션과 시도 원장을 확인하기 전 재실행하지 않습니다.
- `manual_override`: 관리자가 다른 쿠키를 제출했으므로 오래된 자동 로그인 결과를 폐기했습니다.
- 후보가 오래 `submitted`이면 alarm-worker의 실제 검증 상태와 연결 오류를 확인합니다. 성공으로 덮어쓰지 않습니다.
- helper 시간 초과 시 부모 서비스가 종료하여 컨테이너 하위 프로세스를 회수하고, 다음 기동은 원장 상태를 확인합니다.
- 추가 인증의 실제 화면은 계정/시점에 따라 달라집니다. 보호된 Linux 브라우저 접속 경로를 검증하기 전 접근 주소를 임의로 노출하거나 인증 우회를 시도하지 않습니다.

## Smoke test

로컬 Node 모의 검사와 네트워크 없는 비루트 컨테이너의 `test/browser-smoke.mjs`로 일반 로그인·추가 인증을 확인합니다.
이는 X 실계정 로그인 성공의 증거가 아닙니다. 실제 계정 연결 뒤에는 원장 `submitted`→`connected`,
활성 revision, 후속 X 조회 성공과 helper 종료를 확인합니다. 로그에는 시도 ID·고정 오류 코드만 있어야 합니다.
정본 Compose의 healthcheck는 비밀 없는 heartbeat 파일의 갱신 시각을 검사합니다.

## Rollback

별도 서비스와 활성화 설정만 기존 ops 절차로 중지합니다. 로그인 서비스의 중지는 이미 검증된
활성 쿠키를 삭제하지 않으며 기존 alarm-worker는 해당 인증으로 계속 수집합니다.
시도 원장·암호화 키·기존 인증을 삭제하거나 결과 불명을 성공으로 바꾸지 않습니다.
기존 사용자 계정은 전용 계정 후보 검증 성공 전까지 유지합니다.
