# T00. Mode pair와 preflight 검증

## 목적

잘못된 mode 조합으로 중복 발송 또는 유실이 생기지 않게 합니다.

## 작업 대상

- `hololive/hololive-alarm-worker/internal/app/build_runtime.go`
- `hololive/hololive-dispatcher-go/internal/app/config.go`
- `docs/current/runbooks/alarm-dispatch-pg-outbox-cutover.md`

## 작업

1. alarm-worker에서 `publisher=pg_first`이면 `consumer=pg`가 명시되어야 합니다.
2. dispatcher에서 `consumer=pg`이면 `publish_mode=pg_first`가 명시되어야 합니다.
3. unknown mode는 startup error여야 합니다.
4. runbook에 금지 조합을 표로 넣습니다.

## 완료 기준

- `pg_first/valkey` 조합 테스트 실패.
- `shadow/pg` 조합 테스트 실패.
- empty publish mode + consumer pg 테스트 실패.
- `pg_first/pg` 테스트 성공.

## LLM 프롬프트

위 파일만 수정하십시오. mode validation 로직과 테스트를 추가/보강하십시오. 기존 default `valkey_only/valkey`는 깨지면 안 됩니다.
