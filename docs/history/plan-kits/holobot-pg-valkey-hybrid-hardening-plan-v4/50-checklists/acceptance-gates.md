# Acceptance Gates

## Gate A. Canary 가능

- [ ] mode pair validation 완료.
- [ ] Valkey command allowlist 테스트 완료.
- [ ] PG mode Valkey degraded startup/readiness 완료.
- [ ] legacy queue residue runbook 완료.
- [ ] 기본 metric 추가.

## Gate B. Full rollout 가능

- [ ] set-based event insert 완료.
- [ ] set-based delivery insert 완료.
- [ ] in-batch hash conflict 테스트 완료.
- [ ] reconciliation throttle 완료.
- [ ] batch retry/DLQ/quarantine update 완료.
- [ ] dispatch group error isolation 완료.
- [ ] fanout_1000 load test 통과.
- [ ] Valkey outage chaos test 통과.
- [ ] Iris timeout quarantine test 통과.

## Gate C. Production 기본값 가능

- [ ] canary 24~72시간 안정 관측.
- [ ] pending backlog 지속 증가 없음.
- [ ] quarantine 비율 정상.
- [ ] publisher p95 baseline 대비 허용 범위.
- [ ] PG pool saturation 없음.
- [ ] duplicate send report 없음.
- [ ] rollback drill 완료.
