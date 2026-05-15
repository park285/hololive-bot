# 03. PATH-Level Decision Matrix

이 문서는 파일 경로 단위로 “건드릴 것, 참조만 할 것, 건드리면 안 되는 것”을 정의합니다.

## Contract paths

| Path | Decision | Touch level | 이유 |
|---|---|---:|---|
| `docs/current/contracts/alarm.md` | 계약 유지, 문서 참조 | Read-only reference | envelope, queue key, API path contract 유지 |
| `docs/current/contracts/valkey_ephemeral_contract.md` | 계약 유지, invariant 근거 | Read-only reference | PG source of truth, Valkey wakeup-only 원칙 유지 |
| `hololive/hololive-shared/pkg/contracts/alarm/*` | 변경 금지 | No touch | `QueueEnvelopeVersionV1`, queue constants, fixtures 유지 |
| `hololive/hololive-shared/pkg/domain/alarm*.go` | 변경 금지 | No touch | domain shape 변경은 계약 변경으로 간주 |
| `hololive/hololive-shared/pkg/domain/alarm_dispatch_source.go` | 변경 금지 | No touch | source kind contract 유지 |

## Publisher paths

| Path | Decision | Touch level | 이유 |
|---|---|---:|---|
| `hololive/hololive-shared/pkg/service/alarm/queue/publisher.go` | 기본적으로 변경하지 않음 | No touch or metrics only | 이미 `pg_first` insert + wakeup 구조 존재 |
| `hololive/hololive-shared/pkg/service/alarm/queue/metrics.go` | 필요 시 metric만 추가 | Safe extension | contract 아님. publish latency/duplicate/hash conflict 유지 |
| `hololive/hololive-alarm-worker/internal/app/build_runtime.go` | mode validation 유지 | No touch unless validation bug | 이미 `pg_first` requires `pg` 검증 존재 |

## Consumer and runner paths

| Path | Decision | Touch level | Phase | 이유 |
|---|---|---:|---|---|
| `hololive/hololive-alarm-worker/internal/app/alarm_dispatch_runner.go` | 핵심 변경 | Must touch | P1, P2 | failure classification, idle wait state, maxBatchesPerWake |
| `hololive/hololive-alarm-worker/internal/app/build_egress.go` | 핵심 변경 | Must touch | P2, P3, P4 | PG consumer option wiring, idle waiter injection, maintenance runner wiring |
| `hololive/hololive-alarm-worker/internal/app/env.go` | optional env parser 추가 | Safe extension | P2, P3, P4 | duration env parsing 필요 |
| `hololive/hololive-alarm-worker/internal/app/alarm_dispatch_idle.go` | 신규 파일 | New path | P2 | PG wakeup wait + fallback polling |
| `hololive/hololive-alarm-worker/internal/app/alarm_dispatch_maintenance.go` | 신규 파일 | New path | P4 | retention/backlog probe maintenance |
| `hololive/hololive-alarm-worker/internal/app/alarm_dispatch_runner_test.go` | 테스트 보강 | Must touch | P1, P2 | post-send quarantine, idle waiter 검증 |
| `hololive/hololive-alarm-worker/internal/app/runtime_split_test.go` | 필요 시 config test 보강 | Optional touch | P3 | mode/env wiring 검증 |

## Dispatch outbox paths

| Path | Decision | Touch level | 이유 |
|---|---|---:|---|
| `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/consumer.go` | 되도록 변경하지 않음 | No touch | 현재 recovery/claim/marking 책임 보유 |
| `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/repository.go` | 변경 금지에 가깝게 유지 | No touch | SQL state transition 안정성 유지 |
| `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/repository_terminal.go` | 변경 금지 | No touch | DLQ=leased, quarantine=sending 조건이 핵심 invariant |
| `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/repository_insert.go` | 변경 금지 | No touch | set-based insert와 dedupe 유지 |
| `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/metrics.go` | metric extension 가능 | Safe extension | backlog/retention metric 추가 가능 |

## Standalone dispatcher paths

| Path | Decision | Touch level | 이유 |
|---|---|---:|---|
| `hololive/hololive-dispatcher-go/internal/app/runtime.go` | 참조 모델 | Read-only reference | PG wakeup loop, maxBatchesPerWake 참고 |
| `hololive/hololive-dispatcher-go/internal/app/runtime_wakeup.go` | 참조 모델 | Read-only reference | `BRPOP alarm:dispatch:wakeup` fallback logic 참고 |
| `hololive/hololive-dispatcher-go/internal/app/config.go` | 참조 모델 | Read-only reference | consumer mode config와 validation 참고 |
| `hololive/hololive-dispatcher-go/internal/dispatch/dispatcher.go` | 참조 모델 | Read-only reference | PG send failure policy 참고 |

## Migration and script paths

| Path | Decision | Touch level | 이유 |
|---|---|---:|---|
| `hololive/hololive-kakao-bot-go/scripts/migrations/058_create_alarm_dispatch_outbox.sql` | 변경 금지 | No touch | 기존 schema 유지 |
| `hololive/hololive-kakao-bot-go/scripts/migrations/059_harden_alarm_dispatch_outbox.sql` | 변경 금지 | No touch | retention index 이미 존재 |
| `scripts/runtime/alarm-dispatch-outbox-retention.sh` | 유지 | No touch | 수동 fallback runbook으로 유지 |
| `scripts/runtime/alarm-dispatch-outbox-status.sh` | 필요 시 문서에 사용 | Read-only reference | cutover status 확인 |
| `scripts/runtime/alarm-dispatch-outbox-requeue.sh` | 유지 | No touch | operator ack 기반 수동 replay |

## Deploy and docs paths

| Path | Decision | Touch level | Phase | 이유 |
|---|---|---:|---|---|
| `docker-compose.prod.yml` | optional env default 추가 | Safe touch | P3, P4 | alarm-worker 내장 PG path tuning |
| `docs/current/runbooks/alarm-dispatch-pg-outbox-cutover.md` | runbook 확장 | Must touch | P6 | phase gate, rollback, observability 추가 |
| `docs/current/contracts/*` | 계약 변경 금지 | No touch | all | contract freeze |

## No-touch 요약

아래는 이번 작업에서 절대 변경하지 않습니다.

```text
hololive/hololive-shared/pkg/contracts/alarm/*
hololive/hololive-shared/pkg/domain/alarm*.go
hololive/hololive-kakao-bot-go/scripts/migrations/058_create_alarm_dispatch_outbox.sql
hololive/hololive-kakao-bot-go/scripts/migrations/059_harden_alarm_dispatch_outbox.sql
```

단, 별도의 사유가 생겨도 이번 phase 묶음에서는 변경하지 않고 별도 RFC로 분리합니다.
