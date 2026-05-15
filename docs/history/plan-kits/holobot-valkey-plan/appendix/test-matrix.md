# Appendix. Test Matrix

## 1. Schema / repository

| 영역 | 테스트 | 기대 |
|---|---|---|
| events | 같은 event_key + 같은 payload_hash | event duplicate로 처리 |
| events | 같은 event_key + 다른 payload_hash | error 또는 hash conflict |
| events | payload에 room_id 포함 | validation 실패 |
| deliveries | 같은 event 1개 + room 1000개 | events 1 row, deliveries 1000 row |
| deliveries | 같은 dedupe_key 재삽입 | duplicate delivery skip |
| claim | pending/retry | claim 대상 |
| claim | shadowed/sent/dlq/quarantined/cancelled | claim 제외 |
| claim | 동시 worker 2개 | 같은 delivery 중복 claim 없음 |
| loading | ClaimDue 후 event load | distinct event_id로 LoadEventsByID 호출 |
| status | MarkSending | leased + locked_by 일치에서만 성공 |
| status | MarkSent | sending + locked_by 일치에서만 성공 |
| recovery | expired leased | retry로 복구 |
| recovery | stale sending | quarantine |
| retention | terminal delete | limit만큼만 삭제 |

## 2. Publisher

| 모드 | 테스트 | 기대 |
|---|---|---|
| Publish | wrapper | PublishBatch size 1 호출 |
| PublishBatch | batch behavior | Publish 반복 호출 없음 |
| grouping | 여러 room 같은 event | event 1개, delivery N개 |
| valkey_only | 기존 path | PG insert 없음 |
| shadow | Valkey success 후 PG shadow insert | 순서 보장 |
| shadow | Valkey failure | PG insert 없음 |
| shadow | PG failure | SHADOW_FATAL에 따라 fatal/non-fatal |
| pg_first | PG pending insert | legacy active queue LPUSH 없음 |
| pg_first | commit 후 wakeup | wakeup best effort |
| pg_first | wakeup failure | publish success 유지 |
| validation | room_id in event payload | reject |

## 3. Valkey wakeup / command policy

| 테스트 | 기대 |
|---|---|
| NotifyDispatchAvailable | SET guard NX PX 호출 |
| guard success | LPUSH token 1개 + PEXPIRE |
| guard fail | LPUSH 없음, suppressed metric |
| token content | payload 없음 |
| WaitDispatchWakeup | BRPOP fixed key 1개 |
| wakeup missing | fallback scan 동작 |
| fake client command audit | PUBLISH/KEYS/SCAN/LRANGE 0 -1 없음 |
| list TTL | guard TTL 이하 |
| exception command | 주석과 bounded 근거 존재 |

## 4. Dispatcher / PG consumer

| 테스트 | 기대 |
|---|---|
| empty claim | sleep/wakeup wait로 전환 |
| due claim | batch 처리 |
| maxBatchesPerWake | 무한 루프 방지 |
| render failure | MarkSending 호출 없음, retry/DLQ |
| MarkSending conflict | Iris send 금지 |
| send success | MarkSent 호출 |
| MarkSent failure | 즉시 retry 없음 |
| ambiguous send error | quarantine |
| shadowed row | claim 안 됨 |
| stale leased | retry reconciliation |
| stale sending | quarantine reconciliation |

## 5. Cutover

| 시나리오 | 기대 |
|---|---|
| shadow mode | 실제 발송은 legacy, PG는 shadowed |
| shadow -> pg consumer accidentally | shadowed claim 안 됨 |
| pg_first + consumer=valkey | no-go로 차단/경고 |
| valkey_only + consumer=pg | no-go로 차단/경고 |
| legacy queue drain | exact key LLEN/ZCARD만 사용 |
| rollback shadow | valkey_only로 복귀 가능 |
| rollback pg_first | sending quarantine 권장 |

## 6. Operational / metrics

| 테스트 | 기대 |
|---|---|
| metric label | event_key/room_id/dedupe_key 없음 |
| retention job | bounded duration/limit |
| dashboard | app metric 중심 |
| admin requeue | force acknowledgement 필요 |
| admin action | audit log/metric |

## 7. 권장 go test 묶음

실제 경로는 저장소 구조에 맞게 조정합니다.

```bash
go test ./hololive/hololive-shared/pkg/domain -count=1
go test ./hololive/hololive-shared/pkg/service/alarm/... -count=1
go test ./hololive/hololive-alarm-worker/internal/... -count=1
go test ./hololive/hololive-dispatcher-go/internal/... -count=1
git diff --check
```

PostgreSQL repository test는 가능하면 실제 PostgreSQL test container 또는 integration DB로 검증합니다. SQLite로 `FOR UPDATE SKIP LOCKED` 동작을 대체 검증하지 않습니다.
