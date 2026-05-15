# Phase 05. Observability와 load test

## 목표

전환 전에 병목이 publisher, dispatcher, PG, Valkey, Iris 중 어디인지 바로 볼 수 있게 합니다.

## 작업

1. publish batch metric을 추가합니다.
2. claim/send/quarantine metric을 추가합니다.
3. wakeup sent/suppressed/failed metric을 추가합니다.
4. pending/leased/sending gauge는 app-side 또는 낮은 빈도 collector로 수집합니다.
5. fan-out load test fixture를 추가합니다.
6. Valkey outage, PG slow insert, Iris timeout chaos test를 추가합니다.

## 완료 기준

- 1 event + 100/1,000/10,000 room publish benchmark가 있습니다.
- Valkey wakeup 장애 시 PG fallback latency를 측정합니다.
- Iris timeout 시 sending/quarantine 동작을 확인합니다.
- dashboard에 canary 판단 지표가 있습니다.

## 금지

- metric 수집을 위해 hot table에 고빈도 full count query를 날리지 않습니다.
- 테스트에서 Valkey payload queue 재도입 금지.

## 관련 task cards

- `T20-metrics-catalog-implementation.md`
- `T21-load-test-fixtures.md`
- `T22-chaos-test-scenarios.md`
