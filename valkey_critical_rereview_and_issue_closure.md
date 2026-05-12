# Valkey 고부하/복구 이슈 비판적 재리뷰 및 해결안

작성 기준: `park285/hololive-bot` `main` after PR #117/#118.

## 재리뷰 관점

이번 재리뷰는 기존 패치가 맞다는 전제가 아니라, 다음 질문을 기준으로 다시 보았다.

1. sentinel이 stale 상태로 남아 recovery를 방해하지 않는가?
2. version key가 없거나 0인 상태에서 local snapshot을 잘못 재사용하지 않는가?
3. version bump 시점이 실제 cache mutation 완료보다 먼저 발생하지 않는가?
4. 1분 periodic recovery가 timeout 없이 오래 묶일 수 있는가?
5. intentionally empty platform mapping 때문에 매분 SyncPlatformMappings가 반복되지 않는가?
6. rebuild/invalidatation에서 대량 DEL이 순간 spike를 만들지 않는가?
7. admin 전체 조회가 Valkey round trip을 불필요하게 많이 만들지 않는가?

## 발견한 추가 버그/리스크

### 1. `alarm:subscriber_cache_empty` stale marker 리스크

`WarmSubscriberCacheFromAlarms`는 DB 알람이 0개면 `alarm:subscriber_cache_empty`를 세팅한다. 하지만 일반 `AddAlarm` cache write 경로에서 이 marker를 지우지 않으면 다음 상황이 가능하다.

```text
1. DB 알람 0개 상태에서 warm-up
2. alarm:subscriber_cache_empty = 1
3. 사용자가 첫 알람 추가
4. channel registry는 생기지만 empty marker가 남음
5. 이후 alarm:channel_registry만 유실
6. recovery loop가 empty marker를 보고 DB warm-up skip
```

해결: AddAlarm cache mutation에서 empty marker를 원자적으로 삭제하고 channel registry version을 bump한다.

### 2. version key missing/0일 때 local snapshot 재사용 리스크

이전 제안처럼 `Get(version)` 결과가 0일 때도 `lastVersion == 0`으로 판단하면, version key가 없는 상태에서 local channel registry snapshot을 계속 재사용할 수 있다.

해결: `alarm:channel_registry:version`이 존재하고 양수일 때만 snapshot skip을 허용한다. version이 없거나 0이면 항상 `SMEMBERS alarm:channel_registry`를 수행한다.

### 3. atomic AddAlarm script에서 version bump 시점

version bump는 subscriber set write까지 완료된 뒤 마지막에 발생해야 한다. version을 중간에 먼저 쓰면, 다른 runtime이 version 변경을 보고 channel registry를 읽었지만 typed subscriber set은 아직 덜 반영된 시점을 관측할 수 있다.

해결: Lua script 내 subscriber SADD loop 이후 `DEL empty marker`, `SET version` 순서로 실행한다.

### 4. periodic recovery timeout 누락

즉시 recovery는 `alarmCacheRecoveryTimeout`을 사용하지만 periodic recovery는 parent context를 그대로 사용한다. DB/Valkey가 오래 멈추면 recovery goroutine이 오래 묶일 수 있다.

해결: ticker마다 `context.WithTimeout(ctx, alarmCacheRecoveryTimeout)`을 만들고 그 context로 recovery를 수행한다.

### 5. platform mapping intentionally empty 상태에서 매분 sync 반복 가능

`syncPlatformMappingsIfMissing`는 `alarm:chzzk_channels`, `alarm:twitch_logins`, `alarm:twitch_channel_logins` 중 하나가 없으면 sync를 수행한다. 그런데 실제로 mapping이 0개인 환경에서는 full sync 후에도 hash key가 삭제된 상태일 수 있어 매분 sync가 반복될 수 있다.

해결: 각 mapping hash마다 empty marker를 둔다.

```text
alarm:chzzk_channels_empty
alarm:twitch_logins_empty
alarm:twitch_channel_logins_empty
```

mapping key가 없고 empty marker가 있으면 intentionally empty 상태로 보고 sync를 skip한다. mapping이 생기면 incremental sync에서 empty marker를 삭제한다.

### 6. rebuild/invalidatation 대량 DEL spike

`DelMany`는 현재 하나의 `DEL key...` 명령에 모든 key를 넣는다. key 수가 수천~수만으로 늘면 순간 부하가 커질 수 있다.

해결: `DelMany` 내부에서 500개 chunk 단위로 나누어 삭제한다. interface는 변경하지 않는다.


### 8. platform empty marker가 room alarm key로 오인될 수 있는 리스크

새로 추가되는 `alarm:chzzk_channels_empty`, `alarm:twitch_logins_empty`, `alarm:twitch_channel_logins_empty`는 모두 `alarm:` prefix를 가진다. 기존 `isRoomAlarmCacheKey`는 `alarm:` 뒤 suffix에 `:`가 없는 키를 room alarm key로 판단하므로, 이 empty marker들이 room alarm key로 오인될 수 있다.

해결: `isRoomAlarmCacheKey` switch exclude 목록에 platform mapping key와 empty marker key를 모두 추가한다.

### 7. Admin 전체 알람 조회의 N회 HGET

`GetAllAlarmKeys`는 전체 알람 entry마다 `GetMemberName`을 호출할 수 있다. 이는 결국 `HGET alarm:member_names channelID` N회가 된다.

해결: channel ID를 먼저 모으고 `getMemberNamesBatch`로 batch HGET을 수행한다. 기존 동작처럼 member name 조회 실패는 best-effort로 무시한다.

## 제공 파일

- `valkey_critical_rereview_fixes.patch`: 실제 적용용 unified diff 수준 패치
- `valkey_critical_rereview_and_issue_closure.md`: 이슈 close comment와 검증 계획 포함 문서

## 권장 PR 분리

### PR A. correctness: empty marker + registry version

포함 파일:

- `hololive-shared/pkg/service/alarm/keys/keys.go`
- `hololive-shared/pkg/service/notification/alarm_types.go`
- `hololive-shared/pkg/service/alarm/cache_warm.go`
- `hololive-shared/pkg/service/notification/alarm_service_mutation.go`

닫는 이슈:

- stale `alarm:subscriber_cache_empty`
- stream-ingester version source 기반 최적화 준비

검증:

```bash
go test ./hololive/hololive-shared/pkg/service/alarm -run 'TestWarmSubscriberCacheFromAlarms_.*Version|TestWarmSubscriberCacheFromAlarms_.*Empty' -count=1
go test ./hololive/hololive-shared/pkg/service/notification -run 'TestAddAlarmClearsSubscriberCacheEmptyMarker|Test.*Alarm.*Cache' -count=1
```

### PR B. performance: stream-ingester registry version check

포함 파일:

- `hololive-stream-ingester/internal/runtime/youtube_poll_target_refresh.go`

닫는 이슈:

- 5초마다 `SMEMBERS alarm:channel_registry` 수행

검증:

```bash
go test ./hololive/hololive-stream-ingester/internal/runtime -run 'TestYouTubePollTargetRefresher.*Version|TestYouTubePollTarget.*' -count=1
```

### PR C. recovery timeout + platform mapping empty marker

포함 파일:

- `hololive-alarm-worker/internal/service/alarm/scheduler/runtime_scheduler_cache_recovery.go`
- `hololive-shared/pkg/service/notification/alarm_platform_mapping.go`
- `hololive-shared/pkg/service/alarm/keys/keys.go`

닫는 이슈:

- periodic recovery timeout 누락
- intentionally empty platform mapping에서 매분 sync 반복

검증:

```bash
go test ./hololive/hololive-alarm-worker/internal/service/alarm/scheduler -run 'TestRuntimeScheduler.*Recovery' -count=1
go test ./hololive/hololive-shared/pkg/service/notification -run 'Test.*PlatformMapping.*' -count=1
```

### PR D. bulk delete/admin 조회 최적화

포함 파일:

- `hololive-shared/pkg/service/cache/service.go`
- `hololive-shared/pkg/service/notification/alarm_admin.go`

닫는 이슈:

- rebuild/member invalidation 대량 DEL spike
- admin 전체 조회 member name HGET N회

검증:

```bash
go test ./hololive/hololive-shared/pkg/service/cache -run 'Test.*DelMany' -count=1
go test ./hololive/hololive-shared/pkg/service/notification -run 'Test.*GetAllAlarmKeys.*' -count=1
```

## 전체 검증 명령

```bash
go test ./hololive/hololive-shared/... ./hololive/hololive-alarm-worker/... ./hololive/hololive-stream-ingester/... -count=1
go build ./hololive/hololive-shared/... ./hololive/hololive-alarm-worker/... ./hololive/hololive-stream-ingester/...
git diff --check
```

## 운영 검증 명령

```bash
valkey-cli SCARD alarm:channel_registry
valkey-cli GET alarm:channel_registry:version
valkey-cli EXISTS alarm:subscriber_cache_empty
valkey-cli HLEN alarm:chzzk_channels
valkey-cli EXISTS alarm:chzzk_channels_empty
valkey-cli HLEN alarm:twitch_logins
valkey-cli EXISTS alarm:twitch_logins_empty
valkey-cli HLEN alarm:twitch_channel_logins
valkey-cli EXISTS alarm:twitch_channel_logins_empty
valkey-cli SLOWLOG GET 128
valkey-cli INFO commandstats | egrep 'cmdstat_(smembers|hgetall|scan|del|eval|zrangebyscore|rpop|brpop|get|set)'
```

## 이슈 close comment 초안

### stale subscriber empty marker

Resolved.

`alarm:subscriber_cache_empty` is now cleared on successful alarm cache mutation and `alarm:channel_registry:version` is bumped on Add/Remove/Clear/Warm paths. This prevents a stale empty sentinel from suppressing DB warm-up when `alarm:channel_registry` is silently lost.

### stream-ingester repeated SMEMBERS

Resolved.

The YouTube poll target refresher now checks `alarm:channel_registry:version` first and only performs `SMEMBERS alarm:channel_registry` when a positive version changes or no trusted snapshot exists. Missing/zero version remains conservative and still reads the set.

### periodic recovery timeout

Resolved.

Periodic alarm cache recovery now runs with `alarmCacheRecoveryTimeout`, matching immediate recovery behavior and preventing indefinite recovery stalls.

### platform mapping empty hash repeated sync

Resolved.

Platform mapping sync now records explicit empty markers for Chzzk/Twitch mapping hashes. Recovery skips sync when a hash is intentionally empty, but still syncs when the hash is missing without an empty marker.

### large DelMany spike

Resolved.

`DelMany` now chunks large delete requests into bounded batches, reducing single-command memory/free spikes during rebuild and invalidation flows.

### admin full alarm listing HGET fanout

Resolved.

`GetAllAlarmKeys` now batches member-name lookup through the existing batch helper, reducing per-entry Valkey round trips while preserving best-effort behavior.
