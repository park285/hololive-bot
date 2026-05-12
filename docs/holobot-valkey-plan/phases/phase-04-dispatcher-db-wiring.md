# Phase 04. dispatcher-go PostgreSQL wiring

## 목표

`dispatcher-go`가 PostgreSQL repository를 사용할 수 있도록 config, runtime wiring, readiness, lifecycle cleanup을 추가합니다. 단, 이 phase에서 기본 consumer mode는 여전히 Valkey입니다.

이 작업은 PG consumer 구현과 분리합니다. 먼저 dispatcher process가 DB 연결을 안전하게 가질 수 있게 만드는 것이 목표입니다.

## 왜 별도 phase인가

기존 dispatcher-go가 Valkey cache/queue와 Iris client만 조립하고 PostgreSQL 연결을 갖지 않는 구조라면, PG consumer를 바로 붙이는 순간 다음이 한꺼번에 바뀝니다.

- config/env parsing
- DB pool lifecycle
- readiness/liveness
- docker-compose/prod env
- migration dependency
- repository injection
- shutdown cleanup

이것을 PG consumer 로직과 같은 PR에 넣으면 장애 원인을 분리하기 어렵습니다.

## config 추가

권장 env:

```text
ALARM_DISPATCH_CONSUMER_MODE=valkey|pg
POSTGRES_HOST
POSTGRES_PORT
POSTGRES_DB
POSTGRES_USER
POSTGRES_PASSWORD
POSTGRES_SSL_MODE
POSTGRES_MAX_CONNS
POSTGRES_MIN_CONNS
POSTGRES_CONNECT_TIMEOUT_SECONDS
```

이미 shared config가 있다면 재사용합니다. 새 이름을 만들 때는 기존 서비스의 PostgreSQL config naming과 맞춥니다.

## mode별 DB 연결 정책

권장 기본:

```text
CONSUMER_MODE=valkey:
  PostgreSQL env가 없어도 기존 동작 유지
  DB pool 생성하지 않음 또는 optional 생성

CONSUMER_MODE=pg:
  PostgreSQL env 필수
  startup에서 DB pool 생성
  readiness에 DB ping 반영
```

운영 전환 전에는 `consumer_mode=valkey`가 기본값이어야 합니다.

## readiness 정책

```text
liveness:
  process가 살아 있으면 true

readiness, consumer_mode=valkey:
  기존 Valkey/Iris readiness 기준 유지

readiness, consumer_mode=pg:
  Valkey는 wakeup/cache helper이므로 degraded 가능
  PostgreSQL은 필수
  Iris는 필수
```

PG mode에서 PostgreSQL 연결이 실패하면 ready=false여야 합니다.

Valkey wakeup 실패는 ready=false로 만들지 않는 편이 좋습니다. fallback scan이 있으므로 Valkey wakeup은 latency helper입니다. 단, Valkey가 cache/index에도 쓰이고 해당 cache가 dispatch render에 필수라면 별도 판단이 필요합니다.

## repository wiring

runtime composition 예시:

```text
config load
  -> if consumer_mode=pg: create pg pool
  -> create dispatch ledger repository
  -> create pg consumer
  -> create dispatcher service
```

Valkey mode에서는 기존 consumer를 사용합니다.

```text
consumer_mode=valkey -> LegacyValkeyConsumer
consumer_mode=pg     -> PGDispatchConsumer
```

## lifecycle cleanup

process shutdown 시:

```text
1. stop accepting new work
2. stop wakeup wait loop
3. finish or cancel in-flight send according to timeout
4. close DB pool
5. close Valkey client
```

in-flight `sending` row를 shutdown에서 직접 retry로 되돌리지 않습니다. 이미 external send가 진행되었을 수 있습니다. stale sending reconciliation이 처리합니다.

`leased` 상태에서 아직 MarkSending 전이라면 lease 만료 후 retry됩니다. shutdown 시 즉시 unlock을 시도할 수는 있지만, 복잡도를 줄이려면 lease 만료 기반 복구가 안전합니다.

## docker-compose / deployment

`dispatcher-go`에 PostgreSQL env를 추가합니다. 단, 기본 consumer mode가 Valkey라면 DB env 없이도 이전 deployment가 깨지지 않도록 합니다.

권장 rollout:

```text
1. dispatcher-go image 배포, consumer_mode=valkey 유지
2. PostgreSQL env 주입, 여전히 consumer_mode=valkey
3. readiness 확인
4. 테스트 환경에서 consumer_mode=pg dry run
```

## 테스트

필수 test:

1. consumer_mode=valkey + PostgreSQL env 없음 -> startup 성공
2. consumer_mode=pg + PostgreSQL env 없음 -> startup 실패 또는 config error
3. consumer_mode=pg + DB ping 실패 -> readiness false
4. consumer_mode=pg + DB ping 성공 -> readiness true
5. shutdown에서 DB pool close 호출
6. Valkey wakeup error가 PG mode readiness를 즉시 false로 만들지 않음

## 완료 기준

- dispatcher-go가 PG mode config를 읽을 수 있음
- 기본값은 기존 Valkey mode
- PG mode에서 DB readiness가 반영됨
- runtime에 repository injection point가 생김
- PG consumer 로직은 아직 본격 구현하지 않아도 됨

## no-go 조건

- 기본 consumer mode가 갑자기 pg로 바뀜
- Valkey mode에서도 PostgreSQL env가 필수가 됨
- DB 연결 실패를 무시하고 PG mode ready=true가 됨
- shutdown에서 sending row를 자동 retry로 되돌림

## LLM 작업 프롬프트

```text
dispatcher-go에 PostgreSQL config와 runtime wiring을 추가하세요.
이 phase에서는 기본 consumer mode를 valkey로 유지하고, 실제 PG consumer 동작 전환은 하지 마세요.
consumer_mode=valkey에서는 PostgreSQL env가 없어도 기존 실행이 깨지지 않아야 합니다.
consumer_mode=pg에서는 PostgreSQL env와 DB ping이 readiness에 반영되어야 합니다.
DB pool lifecycle cleanup을 추가하세요.
shutdown 중 sending row를 자동 retry로 되돌리지 마세요.
```
