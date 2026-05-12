# Load Test Plan

## 목적

`pg_first/pg + wakeup`이 고 fan-out 알림에서 안전한지 확인합니다.

## 테스트 데이터

```text
fanout_100
fanout_1000
fanout_10000
mixed_100_events_1000_deliveries
duplicate_heavy
hash_conflict
```

## 측정값

- publish p50/p95/p99 latency
- InsertBatch SQL statement count
- PG pool wait time
- PG transaction duration
- WAL growth
- delivery inserted/sec
- dispatcher claim latency
- dispatcher send latency
- pending backlog slope
- quarantine count

## 성공 기준 예시

절대 수치는 환경마다 다르므로 baseline 대비로 봅니다.

- set-based insert가 row-by-row 대비 statement count를 크게 줄입니다.
- fanout_1000에서 publisher p95가 canary 허용 범위 안입니다.
- pending backlog가 지속 증가하지 않습니다.
- Valkey wakeup disabled 상태에서도 fallback scan이 dispatch를 진행합니다.
