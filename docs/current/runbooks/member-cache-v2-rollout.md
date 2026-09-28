# Runbook: member cache V2 durable epoch rollout

## Contract

Member cache V2는 멤버 데이터를 각 process의 in-memory snapshot(전체 `members` 행)과 그 point index에만 둡니다. Valkey에는 멤버 데이터를 쓰지 않고, durable epoch를 cross-process 변경 신호로만 사용합니다. Pub/Sub은 reconcile을 앞당기는 알림일 뿐이며, 알림 payload만으로 cache freshness를 승인하지 않습니다.

| Surface | Contract |
|---|---|
| Authority key | `coord:member-cache:v2:epoch` |
| Authority value | TTL 없는 canonical base-10 integer, usable range `1..9223372036854775806` |
| Epoch semantics | 순서가 아닌 변경 신호. process가 마지막으로 본 값과 다르면(더 작은 값 포함) local snapshot과 point index를 폐기하고 다음 조회에서 PostgreSQL로 다시 적재 |
| Notification channel | `coord:member-cache:v2:epoch-notify` |
| Notification payload | `{"version":2,"epoch":<integer>}` |
| Member data in Valkey | 없음(2026-09-28 제거). 과거 `member-cache:v2:data:<epoch>:member:{channel,name,alias}:...` L2 key는 읽지도 쓰지도 않으며, 배포 시점에 남은 key는 기존 30분 TTL로 자기 소멸 |
| Legacy namespace | 제거됨(2026-08-06 contraction). unprefixed `member:*` keyspace를 읽지도 쓰지도 삭제하지도 않음 |
| Reconcile interval | `15s`; `constants.MemberCacheDefaults.EpochReconcileInterval` |

조회 경로:

- `GetByChannelID`: snapshot의 채널 대표(공유 채널이면 가장 작은 `members.id`) → 없으면 PostgreSQL `FindByChannelID`
- `GetByName`: snapshot 이름 index → 없으면 PostgreSQL `FindByName`
- `FindByAlias`: snapshot 스캔(공식 이름은 대소문자 무시, 명시 별칭은 정확히 일치, 여러 명이면 가장 작은 `members.id`) → 없으면 PostgreSQL `FindByAlias`
- snapshot이 아직 없거나 load가 실패하면 PostgreSQL을 직접 조회합니다. PostgreSQL point 조회 결과는 조회 시점 generation의 process 메모리 index에만 남습니다.

Snapshot loader는 load 시작 시점의 local generation과 publish 직전 durable epoch가 모두 유지될 때만 snapshot을 게시합니다. Epoch read가 실패하거나 value가 invalid하면 local snapshot과 point index를 폐기하고, authority를 다시 읽을 수 있을 때까지 PostgreSQL direct read로 우회합니다. Pub/Sub publish 실패는 이미 성공한 durable epoch bump를 되돌리지 않습니다.

Valkey 재시작 등으로 authority key가 사라지면 다음 reconcile이 `SET NX 1`로 다시 만들고, 이전 값보다 작더라도 변경으로 받아들여 snapshot을 버린 뒤 정상 cache로 수렴합니다(2026-09-28 이전에는 값 회귀를 fail-closed로 처리해 장수 process가 재시작 전까지 PostgreSQL direct read에 고정됐습니다). 변경 감지는 값 비교이므로, 재생성된 값이 우연히 어떤 process가 마지막으로 본 값과 같으면 그 process는 Valkey 중단 동안의 mutation을 알아채지 못할 수 있습니다. 이 경우에도 중단 동안 authority read 실패로 이미 bypass·snapshot 폐기를 거쳤다면 복구 시 다시 적재하며, 그렇지 않은 드문 경우는 snapshot TTL(5분) 또는 다음 mutation에서 수렴합니다.

## Consumer and mutation inventory

다음 runtime은 모두 `providers.ProvideMemberCache` 또는 `providers/modules.BuildInfraModule`을 통해 V2 consumer가 됩니다(2026-09-28 코드 기준 member cache 4개).

- `hololive-api`: bot, admin, llm plane의 각각 독립된 `member.Cache`
- `hololive-alarm-worker`: alarm target/member adapter

`hololive-youtube-collector`는 더 이상 member cache를 만들지 않습니다(`collectorruntime` infrastructure가 `BuildInfraModule`/member cache를 쓰지 않음). 아래 Expand rollout과 Contraction 절의 collector `a`/`b`/`c`/`d` 언급은 2026-08-06 당시 기록입니다.

조회 표면은 `AllMembers`, `GetAllChannelIDs`, `GetByChannelID`, `GetByName`, `FindByAlias`와 이를 감싼 `ServiceAdapter`입니다. Admin plane의 member mutation endpoint만 runtime mutation owner입니다.

- create member
- set graduation status
- update channel ID
- update member name
- add/remove alias

각 handler는 PostgreSQL mutation 성공 후 `Refresh` 또는 V2에서 full epoch bump로 동작하는 `InvalidateAliasCache`를 호출합니다. Epoch bump 실패 시 handler는 synchronization failure를 반환하고 해당 process는 cache bypass 상태가 됩니다.

## Expand rollout (기록용 — 2026-08-06 완료)

V1 process의 process-local snapshot은 V2 notification을 이해하지 못합니다. 따라서 첫 V2 instance가 올라가기 전부터 마지막 V1 instance가 내려갈 때까지 admin member mutation을 동결해야 합니다. 일반 조회와 비-member admin 작업은 계속 서비스할 수 있습니다.

1. Member mutation freeze를 선언하고 admin member endpoint 사용을 중지합니다.
2. 현재 authority가 없으면 첫 V2 process가 value `1`로 생성하는지 확인합니다.
3. `hololive-api`, `hololive-alarm-worker`, collector `a`/`b`/`c`/`d`를 기존 deploy runbook 순서로 V2 build에 교체합니다.
4. 모든 process에서 `hololive_member_cache_epoch`가 durable authority와 일치하고 bypass `0` 안정 상태인지 확인합니다.
5. Valkey `PUBSUB NUMSUB coord:member-cache:v2:epoch-notify`와 runtime inventory를 대조합니다. 일시적 reconnect를 고려하되, 지속적으로 누락된 subscriber가 있으면 mutation freeze를 유지합니다.
6. Canary member mutation 한 건을 수행하고 mutation 전후 epoch가 정확히 `+1`인지 확인합니다.
7. 모든 V2 process의 local epoch가 새 값으로 reconcile되고 cache bypass가 안정적으로 해제됐는지 확인합니다.
8. name/channel/alias lookup과 affected bot/admin/worker/collector health를 smoke한 뒤 mutation freeze를 해제합니다.

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
- Authority key를 지워 `1`로 재생성되게 하면(값 회귀) 모든 consumer가 snapshot을 버리고 epoch `1`에서 bypass `0`으로 수렴해야 합니다.
- Authority에 `0`, 음수, 공백 포함 값, non-decimal value 또는 saturated `9223372036854775807`을 넣은 fixture는 fail-closed해야 합니다. Production authority를 손상시키는 live drill은 금지합니다.
- 정상 운영 중 Valkey `SCAN MATCH member-cache:v2:data:*`는 배포 후 30분이 지나면 0건이어야 합니다.

## Contraction (2026-08-06 종결)

V1 rollback 경로는 2026-08-06에 종결을 선언했습니다. 근거 관측(중앙 Valkey + Prometheus):

- 6개 runtime 전부 V2 build(당시 inventory): `hololive_member_cache_epoch` = durable authority(`coord:member-cache:v2:epoch` = 1)와 일치 — hololive-api(30091), alarm-worker(30097), collector c(중앙 30096)/a(100.100.1.6)/b(100.100.1.5)/d(100.100.1.2)
- `rate(hololive_member_cache_bypass_total[30m])` = 0, reconcile 실패율 0
- Valkey `SCAN MATCH member:*` = 0건

이에 따라 legacy 경로를 코드에서 제거했습니다: mutation 후 `member:*` 전체 keyspace를 스캔·삭제하던 `deleteLegacyMemberKeys`, epoch 없는 V1 Valkey invalidation 분기, in-memory 이중 shape(`*domain.Member` 직접 저장) 수용. 이 지점부터 V1 build로의 rollback은 지원하지 않으며, 아래 Rollback 절은 기록용입니다.

## Rollback (기록용 — contraction 이후 비지원)

V2에서 V1으로 돌아가면 V1은 durable epoch를 이해하지 못하므로 혼합 상태에서 member mutation을 허용할 수 없습니다.

1. Member mutation freeze를 다시 선언합니다.
2. 문제 build의 모든 consumer를 이전 V1 ref로 교체합니다. 일부 V2와 일부 V1을 남긴 채 mutation을 재개하지 않습니다.
3. 모든 V1 process를 restart해 process-local snapshot을 PostgreSQL에서 다시 생성합니다.
4. member name/channel/alias와 worker/collector health를 확인합니다.
5. V2 subscriber가 남아 있지 않고 모든 V1 snapshot이 restart 이후 생성됐음을 확인한 뒤에만 mutation freeze를 해제합니다.

Authority key는 rollback 중 삭제하지 않습니다. 2026-09-28 이후 build는 epoch-scoped member data를 쓰지 않으므로, 그 이전 build로 되돌리면 해당 build가 L2 data를 다시 채웁니다.
