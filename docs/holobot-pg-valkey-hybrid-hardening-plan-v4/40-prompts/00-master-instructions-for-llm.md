# Master LLM Instructions

이 저장소의 alarm dispatch hardening 작업을 수행할 때 다음 원칙을 지키십시오.

## 목표 모드

```text
ALARM_DISPATCH_PUBLISH_MODE=pg_first
ALARM_DISPATCH_CONSUMER_MODE=pg
ALARM_DISPATCH_WAKEUP_ENABLED=true
```

## 절대 원칙

1. PostgreSQL이 durable source of truth입니다.
2. Valkey는 payload 없는 wakeup/cache/index helper입니다.
3. Valkey wakeup 실패는 dispatch 유실을 만들면 안 됩니다.
4. hot path에서 고복잡도 Valkey 명령을 쓰지 마십시오.
5. event payload는 room-agnostic이어야 합니다.
6. `sending` ambiguous failure는 Iris idempotency 전까지 quarantine입니다.
7. batch 작업은 bounded여야 합니다.
8. unbounded SQL delete/update/scan을 쓰지 마십시오.

## 작업 방식

한 번에 하나의 task card만 수행하십시오. 각 task card는 다음 순서로 처리하십시오.

```text
1. 관련 파일 읽기
2. 현재 behavior 요약
3. 변경 설계
4. 코드 수정
5. 테스트 추가
6. go test 또는 관련 테스트 실행
7. 변경 결과와 남은 위험 보고
```

## 금지

- `PublishBatch()` 내부에서 `Publish()` 반복 호출 금지.
- Valkey `PUBLISH`를 dispatch wakeup 기본값으로 재도입 금지.
- `KEYS`, unbounded `SCAN`, unbounded `LRANGE` 금지.
- terminal row 자동 pending 복구 금지.
- shadowed row 자동 pending 승격 금지.
- Iris idempotency 전 stale sending retry 금지.
