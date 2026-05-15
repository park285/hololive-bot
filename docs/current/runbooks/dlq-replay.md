# Runbook: dlq-replay

## Role

Alarm dispatch DLQ를 확인하고 재처리 가능성을 판단하기 위한 운영 기준입니다.

## Normal status

| Check | Expected |
|---|---|
| DLQ length | normally `0` or explained by a known incident |
| Retry queue | delayed entries eventually drain |
| Logs | alarm-worker dispatch errors match DLQ growth cause |

## Keys

| Key | Type | Purpose |
|---|---|---|
| `alarm:dispatch:queue` | Valkey list | active dispatch queue |
| `alarm:dispatch:retry` | Valkey sorted set | delayed retry queue |
| `alarm:dispatch:dlq` | Valkey list | dead-letter/raw preserved payloads |

## Diagnosis

```bash
./scripts/deploy/compose.sh -f docker-compose.prod.yml exec valkey-cache valkey-cli -s /var/run/valkey/valkey-cache.sock LLEN alarm:dispatch:dlq
./scripts/deploy/compose.sh -f docker-compose.prod.yml exec valkey-cache valkey-cli -s /var/run/valkey/valkey-cache.sock LRANGE alarm:dispatch:dlq 0 20
./scripts/deploy/compose.sh -f docker-compose.prod.yml logs --tail=300 hololive-alarm-worker
```

## Replay Safety Checklist

- Confirm whether each DLQ item is raw invalid JSON, unsupported version, delayed retry wrapper, or a valid envelope moved after send failure.
- Confirm the producer/consumer contract mismatch is fixed.
- Confirm Iris and Valkey dependencies are healthy.
- Preserve a copy of DLQ payload samples before mutation.
- Do not replay unsupported versions without a migration plan.

## Replay

Automatic replay script is not part of this task. Use a reviewed one-off command or script only after the checklist is satisfied.

Pseudo-flow:

```bash
# 1. Export DLQ payloads.
# 2. Validate/transform only payloads that are safe to replay.
# 3. LPUSH safe envelopes back to alarm:dispatch:queue.
# 4. Remove only replayed payloads from alarm:dispatch:dlq.
```

## Unsupported Version vs Invalid JSON

- Unsupported version: JSON parsed but `version` is not accepted by the consumer. Fix compatibility or transform before replay.
- Invalid JSON: raw payload cannot parse as an envelope. Do not replay without identifying the producer bug.
- Invalid delayed retry wrapper: raw sorted-set member is preserved to DLQ; inspect wrapper before reconstructing payload.

## Related contracts

- `../contracts/alarm.md`
- `../QUEUE_AND_PUBSUB_CONTRACTS.md`
