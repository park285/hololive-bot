# Prompt: metrics와 load tests 구현

## 목표

pg_first 전환 시 병목과 correctness 이상을 metric/load test로 확인합니다.

## 요구사항

1. publish batch result metric.
2. wakeup sent/suppressed/failed metric.
3. claim/send/retry/dlq/quarantine metric.
4. fanout_100, fanout_1000, fanout_10000 fixture.
5. Valkey outage, Iris timeout chaos test.

## 금지

- room_id, stream_id를 metric label로 쓰지 마십시오.
- metric 수집 때문에 hot table에 고빈도 full count query를 쓰지 마십시오.
