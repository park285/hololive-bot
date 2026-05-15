# T21. Load test fixtures

## 목적

고 fan-out에서 pg_first가 버티는지 측정합니다.

## 시나리오

```text
fanout_100:    1 event -> 100 rooms
fanout_1000:   1 event -> 1,000 rooms
fanout_10000:  1 event -> 10,000 rooms
mixed_1000:    100 events -> 1,000 deliveries
duplicates:    same batch duplicate deliveries
hash_conflict: same event_key different payload
```

## 측정

- publish p50/p95/p99 latency
- SQL statement count
- PG pool wait time
- WAL growth
- inserted/duplicate ratio
- dispatcher claim/send latency

## 완료 기준

- row-by-row 대비 set-based 개선이 수치로 보입니다.
- fanout_1000에서 publish latency가 운영 허용 범위 안입니다.
- duplicate/hash conflict test가 correctness를 확인합니다.

## LLM 프롬프트

고 fan-out 알림 publish/load test fixture를 작성하십시오. SQL statement count와 latency를 비교할 수 있게 하십시오.
