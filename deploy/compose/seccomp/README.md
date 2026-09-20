# X 로그인 브라우저 syscall 프로필

`x-space-login.json`은 Microsoft Playwright v1.63.0의
[seccomp_profile.json](https://github.com/microsoft/playwright/blob/v1.63.0/utils/docker/seccomp_profile.json)을 기반으로 합니다.
상위 프로젝트는 Apache-2.0 라이선스이며 Microsoft와 Playwright 기여자가 저작권을 보유합니다.

상위 프로필은 Docker 기본 제한에 Chromium sandbox의 사용자 네임스페이스 생성 syscall을 추가합니다.
이 서비스는 모든 컨테이너 capability를 제거하므로 `chroot`의 capability 기반 프로필 생성 조건만 제거했습니다.
커널의 권한 검사는 그대로이며 Chromium이 자체 생성한 사용자 네임스페이스 내부에서만 해당 권한을 얻습니다.
이 변경 없이 로컬 실제 브라우저가 `sys_chroot("/proc/self/fdinfo/")`에서 종료되는 것을 재현했습니다.
`--no-sandbox`, privileged, host IPC, SYS_ADMIN을 사용하지 않습니다.
