# internal/app

`hololive-kakao-bot-go` 런타임의 부트스트랩 진입점이다.

원칙:

- 루트 `internal/app` 는 façade / orchestration 역할만 유지한다.
- 구현은 `internal/app/http`, `internal/app/runtime`, `internal/app/wiring`, `internal/app/bootstrap` 아래에 둔다.
- 얇은 중복 wrapper 와 불필요한 추가 테스트 파일 누적을 피한다.

참조:

- `docs/current/architecture/app-bootstrap-boundary-guide.md`
- `docs/current/ALARM_DISPATCH_REMEDIATION_20260414.md`
