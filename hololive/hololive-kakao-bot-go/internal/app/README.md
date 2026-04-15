# internal/app

이 디렉터리는 `hololive-kakao-bot-go` 런타임의 부트스트랩 진입점이다.
구현 helper 는 `internal/app/http`, `internal/app/runtime`, `internal/app/wiring`, `internal/app/bootstrap` 아래로 분리하고,
루트 `internal/app` 는 façade / orchestration / local shape adapter 역할을 유지한다.

현재 기준 구조 분리 가이드는 아래 문서를 따른다.

- `docs/current/APP_BOOTSTRAP_BOUNDARY_GUIDE.md`
- `docs/current/ALARM_DISPATCH_REMEDIATION_20260414.md`

핵심 원칙은 다음과 같다.

- `internal/app` 밖으로 노출되는 façade 는 최소화한다.
- HTTP router 구현은 `internal/app/http` 로 이동시키고, `internal/app` 에는 forwarding façade 를 유지한다.
- bootstrap helper 구현은 `internal/app/bootstrap` 로 이동시키고, 루트에는 thin wrapper 를 유지한다.
- bootstrap / runtime / http / wiring 책임을 분리한다.
- 얇은 helper 중복과 `*_additional_test.go` 누적을 멈춘다.
