# Phase 00. Preflight, 계약 고정, 위험 감사

## 목표

이 phase는 runtime 동작을 바꾸지 않습니다. 목표는 최종 설계 계약을 코드/문서/테스트에 고정하고, 이후 phase에서 LLM 또는 개발자가 잘못된 방향으로 구현하지 않도록 안전장치를 만드는 것입니다.

가장 중요한 결과물은 다음입니다.

1. Valkey는 durable queue가 아니라는 계약 문서
2. PostgreSQL 2테이블 ledger가 final target이라는 계약 문서
3. Valkey hot path O(1) command policy
4. 기존 legacy queue, publisher, dispatcher 흐름의 현재 상태 audit
5. 변경 전 golden test 또는 smoke test

## 작업 범위

### 1. 기존 dispatch 흐름 확인

다음 항목을 코드에서 찾아 정리합니다.

```text
- alarm-worker가 알림을 만드는 위치
- 기존 queue.Publisher 인터페이스
- 기존 Publish() 호출 지점
- Valkey LPUSH/RPUSH/BRPOP/RPOP/LPOP 사용 지점
- dispatcher-go가 queue consumer를 조립하는 위치
- Iris client 호출 위치
- retry/DLQ 처리 위치
- docker-compose/prod env에서 dispatcher-go에 PostgreSQL env가 있는지 여부
```

찾을 때는 `rg`를 사용합니다.

```bash
rg "Publish\(" .
rg "LPUSH|RPUSH|BRPOP|BLPOP|RPOP|LPOP|PUBLISH|KEYS|SCAN|LRANGE|SMEMBERS|HGETALL" .
rg "Iris|SendMessage|dispatch" hololive/hololive-dispatcher-go hololive/hololive-shared hololive/hololive-alarm-worker
```

이 명령은 repository search이므로 Valkey runtime command policy와는 별개입니다.

### 2. 문서 추가

권장 문서 경로는 repository 관례에 맞춰 조정합니다.

```text
docs/alarm-dispatch/final-contract.md
docs/alarm-dispatch/valkey-command-policy.md
docs/alarm-dispatch/v3-rollout-overview.md
```

핵심 문장:

```text
Valkey는 알람 dispatch의 durable queue가 아니다.
PostgreSQL alarm_dispatch_events + alarm_dispatch_deliveries가 production dispatch ledger다.
Valkey wakeup은 유실 가능해야 하며 payload를 담지 않는다.
```

### 3. 금지 명령 audit 추가

운영 hot path에서 다음 command를 직접 쓰는 경우를 찾습니다.

```text
KEYS
SCAN without bounded count and non-hot path comment
LRANGE 0 -1
SMEMBERS on unbounded key
HGETALL on unbounded key
ZRANGE/ZREVRANGE without small LIMIT
PUBLISH for alarm dispatch wakeup
Lua loop over variable-size data
```

이 phase에서는 아직 모두 고치지 않아도 됩니다. 단, dispatch hot path에 있으면 다음 phase로 넘어가기 전에 설계상 제거 계획을 문서화해야 합니다.

### 4. 기존 behavior golden test

다음은 변경 전 동작이 깨지지 않았음을 보장하기 위한 테스트입니다.

- 기존 Valkey publish가 성공하면 dispatcher가 발송한다.
- 기존 retry/DLQ 흐름이 유지된다.
- `Publish()` 호출자가 현재 에러를 받는 방식이 유지된다.
- dispatcher-go가 PostgreSQL 없이도 기존 Valkey mode로 실행된다.

이 phase에서는 새 ledger를 사용하지 않습니다.

## 완료 기준

- runtime behavior 변경 없음
- 금지 Valkey command audit 결과가 문서화됨
- final target이 단일 outbox가 아니라 2테이블 ledger임이 문서화됨
- 기존 테스트가 통과함
- 다음 phase에서 작업할 파일 후보가 명확함

## no-go 조건

다음 상황이면 Phase 01로 넘어가지 않습니다.

- dispatcher-go가 현재 DB 없이 동작하는지 확인하지 않음
- 기존 publisher 호출 지점을 파악하지 않음
- dispatch hot path에서 `KEYS`, unbounded `SCAN`, Pub/Sub `PUBLISH` default wakeup을 계속 쓰기로 결정했는데 예외 사유가 없음
- Valkey 내구성 강화로 해결하려는 문서가 남아 있음

## LLM 작업 프롬프트

```text
저장소의 alarm dispatch 현재 흐름을 감사하세요. runtime 동작을 바꾸지 마세요.
목표는 문서와 audit 결과를 만드는 것입니다.
특히 Valkey를 durable queue로 간주하는 코드/문서 표현을 찾고, PostgreSQL events+deliveries ledger가 최종 target임을 명시하세요.
Valkey hot path에서 KEYS, unbounded SCAN, LRANGE 0 -1, SMEMBERS, HGETALL, Pub/Sub PUBLISH wakeup 사용 지점을 목록화하세요.
수정은 docs/test 중심으로 제한하고, production behavior를 바꾸지 마세요.
```
