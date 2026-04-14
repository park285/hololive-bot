# internal/app

이 디렉터리는 현재 `hololive-kakao-bot-go` 런타임의 부트스트랩 진입점이지만, 책임이 많이 섞여 있다.

현재 기준 구조 분리 가이드는 아래 문서를 따른다.

- `docs/current/APP_BOOTSTRAP_BOUNDARY_GUIDE.md`
- `docs/current/ALARM_DISPATCH_REMEDIATION_20260414.md`

핵심 원칙은 다음과 같다.

- `internal/app` 밖으로 노출되는 façade 는 최소화한다.
- bootstrap / runtime / http / wiring 책임을 분리한다.
- 얇은 helper 중복과 `*_additional_test.go` 누적을 멈춘다.
