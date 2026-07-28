# Runbook: member cache V2 durable epoch rollout

## Contract

Member cache V2는 Valkey의 durable epoch를 cross-process freshness authority로 사용합니다. Pub/Sub은 reconcile을 앞당기는 알림일 뿐이며, 알림 payload만으로 cache freshness를 승인하지 않습니다.

| Surface | Contract |
|---|---|
| Authority key | `coord:member-cache:v2:epoch` |
| Authority value | TTL 없는 canonical base-10 integer, usable range `1..9223372036854775806` |
| Notification channel | `coord:member-cache:v2:epoch-notify` |
| Notification payload | `{"version":2,"epoch":<integer>}` |
| V2 data namespace | `member-cache:v2:data:<epoch>:member:{channel,name,alias}:...` |
| Legacy namespace | `member:*`; expand 기간에 mutation 후 best-effort로 삭제하지만 authority로 사용하지 않음 |
| Reconcile interval | `15s`; `constants.MemberCacheDefaults.EpochReconcileInterval` |

`coord:member-cache:v2:*`는 legacy `member:*` pattern 밖에 있으므로 V1 `InvalidateAll`이 authority를 삭제할 수 없습니다. Snapshot loader는 load 시작 시점의 local generation과 publish 직전 durable epoch가 모두 유지될 때만 snapshot을 게시합니다. Epoch read가 실패하거나 value가 invalid하면 local snapshot과 point index를 폐기하고 PostgreSQL direct read로 우회합니다. Pub/Sub publish 실패는 이미 성공한 durable epoch bump를 되돌리지 않습니다.

## Consumer and mutation inventory

다음 runtime은 모두 `providers.ProvideMemberCache` 또는 `providers/modules.BuildInfraModule`을 통해 V2 consumer가 됩니다.

- `hololive-api`: bot, admin, llm plane의 각각 독립된 `member.Cache`
- `hololive-alarm-worker`: alarm target/member adapter
- `hololive-youtube-producer`: Osaka `a`, Seoul `b`, central `c`, Osaka2 `d`

조회 표면은 `AllMembers`, `GetAllChannelIDs`, `GetByChannelID`, `GetByName`, `FindByAlias`와 이를 감싼 `ServiceAdapter`입니다. Admin plane의 member mutation endpoint만 runtime mutation owner입니다.

- create member
- set graduation status
- update channel ID
- update member name
- add/remove alias

각 handler는 PostgreSQL mutation 성공 후 `Refresh` 또는 V2에서 full epoch bump로 동작하는 `InvalidateAliasCache`를 호출합니다. Epoch bump 실패 시 handler는 synchronization failure를 반환하고 해당 process는 cache bypass 상태가 됩니다.

## Expand rollout

V1 process의 process-local snapshot은 V2 notification을 이해하지 못합니다. 따라서 첫 V2 instance가 올라가기 전부터 마지막 V1 instance가 내려갈 때까지 admin member mutation을 동결해야 합니다. 일반 조회와 비-member admin 작업은 계속 서비스할 수 있습니다.

1. Member mutation freeze를 선언하고 admin member endpoint 사용을 중지합니다.
2. 현재 authority가 없으면 첫 V2 process가 value `1`로 생성하는지 확인합니다.
3. `hololive-api`, `hololive-alarm-worker`, producer `a`/`b`/`c`/`d`를 기존 deploy runbook 순서로 V2 build에 교체합니다.
4. 모든 process에서 `hololive_member_cache_epoch`가 durable authority와 일치하고 bypass `0` 안정 상태인지 확인합니다.
5. Valkey `PUBSUB NUMSUB coord:member-cache:v2:epoch-notify`와 runtime inventory를 대조합니다. 일시적 reconnect를 고려하되, 지속적으로 누락된 subscriber가 있으면 mutation freeze를 유지합니다.
6. Canary member mutation 한 건을 수행하고 mutation 전후 epoch가 정확히 `+1`인지 확인합니다.
7. 모든 V2 process의 local epoch가 새 값으로 reconcile되고 cache bypass가 안정적으로 해제됐는지 확인합니다.
8. name/channel/alias lookup과 affected bot/admin/worker/producer health를 smoke한 뒤 mutation freeze를 해제합니다.

관찰 metric:

- `hololive_member_cache_epoch` must match `coord:member-cache:v2:epoch`
- `rate(hololive_member_cache_epoch_reconcile_total{result="failed"}[5m])` must return to `0`
- `rate(hololive_member_cache_bypass_total[5m])` must return to `0`
- `rate(hololive_member_cache_epoch_notifications_total{result="failed"}[5m])` is alerting evidence, not freshness authority

Reconcile failure 또는 bypass가 계속 증가하면 Valkey authority를 복구하기 전까지 stale cache를 다시 활성화하지 않습니다. PostgreSQL direct read 증가에 따른 latency와 pool 사용량을 함께 관찰합니다.

## Failure drills

- Pub/Sub notification을 한 consumer에서 유실시킨 뒤 `15s` periodic reconcile 안에 새 epoch로 수렴해야 합니다.
- Subscriber 연결을 끊었다가 복구하면 subscription confirmation 직후 durable epoch를 다시 읽어 missed epoch를 복구해야 합니다.
- Notification publish만 실패시켜도 mutation의 epoch bump와 다른 consumer의 periodic reconcile은 유지돼야 합니다.
- Valkey authority GET을 실패시키면 stale snapshot이 아니라 PostgreSQL direct read가 제공돼야 합니다.
- Authority에 `0`, 음수, 공백 포함 값, non-decimal value 또는 saturated `9223372036854775807`을 넣은 fixture는 fail-closed해야 합니다. Production authority를 손상시키는 live drill은 금지합니다.

## Rollback

V2에서 V1으로 돌아가면 V1은 durable epoch를 이해하지 못하므로 혼합 상태에서 member mutation을 허용할 수 없습니다.

1. Member mutation freeze를 다시 선언합니다.
2. 문제 build의 모든 consumer를 이전 V1 ref로 교체합니다. 일부 V2와 일부 V1을 남긴 채 mutation을 재개하지 않습니다.
3. 모든 V1 process를 restart해 process-local snapshot을 PostgreSQL에서 다시 생성합니다.
4. member name/channel/alias와 worker/producer health를 확인합니다.
5. V2 subscriber가 남아 있지 않고 모든 V1 snapshot이 restart 이후 생성됐음을 확인한 뒤에만 mutation freeze를 해제합니다.

Authority key와 epoch-scoped V2 data는 rollback 중 삭제하지 않습니다. V2 data는 기존 TTL로 만료되고, authority는 다음 V2 rollout에서 단조 증가를 이어갑니다. 모든 consumer의 V2 정착이 확인되기 전에는 legacy read/write path 제거를 별도 변경으로 진행하지 않습니다.
