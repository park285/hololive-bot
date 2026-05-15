# 위험 등록부 요약

## R1. 고 fan-out publisher 지연

증상:

- alarm-worker publish latency 증가
- PG connection 점유 증가
- `alarm_dispatch_deliveries` insert WAL 증가

원인:

- 현재 `InsertBatch()` 내부 delivery별 insert 반복

대응:

- P2 set-based event/delivery insert
- batch size 상한
- result metric 추가

## R2. Valkey 장애 시 dispatcher가 멈춤

증상:

- PG fallback scan이 있어야 하는데 dispatcher startup/readiness가 실패

원인:

- PG mode에서도 cache service가 hard dependency로 취급될 수 있음

대응:

- P1 Valkey degraded mode
- PG mode readiness에서 Valkey를 hard fail로 보지 않음

## R3. 장애 순간 quarantine/retry update 폭증

증상:

- Iris 장애 시 DB update statement 급증

원인:

- terminal/retry update가 row-by-row

대응:

- P3 batch terminal/retry updates

## R4. reconciliation 과다 실행

증상:

- pending 처리 중 매 batch마다 stale scan/update 발생

원인:

- `DrainBatch()`마다 recovery 실행

대응:

- P3 reconciliation throttle

## R5. payload hash conflict 누락

증상:

- 같은 event_key에 다른 payload가 섞임

원인:

- 동일 batch 내부 conflict 검증 부족

대응:

- P1 in-batch hash conflict detection

## R6. group error 전파로 다른 room 처리 취소

증상:

- 한 room MarkSent 실패가 다른 room send/mark를 취소

원인:

- `errgroup.WithContext` sibling cancellation

대응:

- P3 independent group error collection

## R7. shadow duplicate와 cutover residue 혼선

증상:

- shadowed row가 pg_first에서 duplicate로 skip되거나 legacy queue residue와 충돌

대응:

- cutover 전 legacy queue drain 확인
- shadowed row 자동 pending 승격 금지 유지
- cutover runbook에 residue 처리 명시
