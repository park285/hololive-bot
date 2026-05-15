# T04. PG Consumer Config Parity

## 목표

standalone dispatcher와 alarm-worker 내장 PG consumer가 같은 운영 env 의미를 갖도록 합니다.

## PATH

```text
hololive/hololive-alarm-worker/internal/app/build_egress.go
hololive/hololive-alarm-worker/internal/app/env.go
docker-compose.prod.yml
```

## 변경 내용

- `ALARM_DISPATCH_LEASE_SECONDS`
- `ALARM_DISPATCH_RECOVERY_INTERVAL_MS`
- `ALARM_DISPATCH_RECOVERY_BATCH_SIZE`
- `ALARM_DISPATCH_MAX_BATCH`

위 env를 alarm-worker 내장 PG consumer에 반영합니다.

## 테스트

- unset defaults.
- invalid value fallback.
- pg mode wiring.
- valkey mode unchanged.
