# Dedup Remediation Plan — Valkey + PG 이중 알림 방지 개선

2026-05-23 리뷰 기반. 6개 페이즈, 정합성 → 성능 → 관측성 순서.

## 배포 전략: 빅뱅 패치

전 페이즈를 단일 브랜치에서 일괄 구현 → 단일 PR로 머지.
Phase 3 ↔ Phase 6은 동일 파일(`repository_insert.go`) 변경이므로 통합 적용.
Phase 4-4(hash conflict 로깅)는 Phase 3 코드에 직접 삽입.

**구현 순서 (파일 충돌 최소화):**
1. Phase 1 (service.go) + Phase 5 (service.go + cache layer) — 동일 파일, 한 번에
2. Phase 2 (publisher.go) — 독립 파일
3. Phase 3 + Phase 6 + Phase 4-4 (repository_insert.go) — 통합
4. Phase 4 (4-1 ~ 4-3) (service.go, fallback.go, notifier.go) — 로깅 삽입

**최종 검증 1회:**
```bash
go build ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-admin-api/... ./hololive/hololive-alarm-worker/... ./hololive/hololive-kakao-bot-go/... ./hololive/hololive-llm-sched/... ./hololive/hololive-youtube-producer/...
go test ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-admin-api/... ./hololive/hololive-alarm-worker/... ./hololive/hololive-kakao-bot-go/... ./hololive/hololive-llm-sched/... ./hololive/hololive-youtube-producer/...
```

## 목차

- [Phase 1: writeNotifiedHashFields 원자화](#phase-1)
- [Phase 2: publishValkeyBatch DoMulti 배치화](#phase-2)
- [Phase 3: Hash conflict 격리 처리](#phase-3)
- [Phase 4: Dedup slog 관측성 보강](#phase-4)
- [Phase 5: claimDedup 투기적 파이프라인](#phase-5)
- [Phase 6: Event insert 1-query 통합](#phase-6)

---

<a id="phase-1"></a>
## Phase 1: `writeNotifiedHashFields` HMSet 원자화

**분류:** 정합성(Critical) + 성능(High)
**난이도:** 낮음
**예상 변경:** ~20줄

### 문제

`dedup/service.go:206-217`에서 3개 개별 Valkey 명령(HSet, HSet, Expire)을 순차 실행.
프로세스 크래시 시 Expire 누락 → 키 영구 잔존 → 해당 스트림 알림 영구 차단.
성능: 건당 3 RTT → 배치 50건 기준 150 RTT.

### 변경 대상

```
hololive/hololive-shared/pkg/service/alarm/dedup/service.go
```

### 현재 코드 (line 206-217)

```go
func (s *Service) writeNotifiedHashFields(ctx context.Context, key string, scheduledStr string, minutesUntil int) error {
    if err := s.cache.HSet(ctx, key, "start_scheduled", scheduledStr); err != nil {
        return fmt.Errorf("mark as notified: set start_scheduled field: %w", err)
    }
    if err := s.cache.HSet(ctx, key, strconv.Itoa(minutesUntil), "1"); err != nil {
        return fmt.Errorf("mark as notified: set minute field: %w", err)
    }
    if err := s.cache.Expire(ctx, key, constants.CacheTTL.NotificationSent); err != nil {
        return fmt.Errorf("mark as notified: set expiration: %w", err)
    }
    return nil
}
```

### 목표 코드

```go
func (s *Service) writeNotifiedHashFields(ctx context.Context, key string, scheduledStr string, minutesUntil int) error {
    fields := map[string]any{
        "start_scheduled":            scheduledStr,
        strconv.Itoa(minutesUntil): "1",
    }
    if err := s.cache.HMSet(ctx, key, fields); err != nil {
        return fmt.Errorf("mark as notified: hmset fields: %w", err)
    }
    if err := s.cache.Expire(ctx, key, constants.CacheTTL.NotificationSent); err != nil {
        return fmt.Errorf("mark as notified: set expiration: %w", err)
    }
    return nil
}
```

### 참고: 동일 패턴 이미 존재

`dedup/notified_cache.go:98-113`의 `persistNotifiedHash`가 정확히 동일한 HMSet + Expire 패턴을 사용 중. 이 변경은 두 경로를 일관되게 만듦.

### 검증

```bash
go test ./hololive/hololive-shared/pkg/service/alarm/dedup/...
go build ./hololive/hololive-shared/...
```

### 검증 포인트

- 기존 테스트 `TestService_MarkAsNotified_*` 전부 통과
- `writeNotifiedHashFields` 호출 후 HGetAll로 두 필드 모두 존재 확인
- Expire가 설정되었는지 TTL 확인

---

<a id="phase-2"></a>
## Phase 2: `publishValkeyBatch` DoMulti 배치화

**분류:** 성능(High)
**난이도:** 낮음
**예상 변경:** ~30줄

### 문제

`queue/publisher.go:267-276`에서 건별 LPUSH 루프 → 50건 = 50 RTT.
cache layer의 `DoMulti`가 이미 파이프라이닝 지원.

### 변경 대상

```
hololive/hololive-shared/pkg/service/alarm/queue/publisher.go
```

### 현재 코드 (line 267-276, 314-334)

```go
func (p *Publisher) publishValkeyBatch(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) (dispatchoutbox.PublishBatchResult, error) {
    result := dispatchoutbox.PublishBatchResult{RequestedDeliveries: len(envelopes)}
    for i := range envelopes {
        if err := p.publishValkey(ctx, envelopes[i]); err != nil {
            return result, err
        }
        result.ProcessedDeliveries++
    }
    return result, nil
}

func (p *Publisher) publishValkey(ctx context.Context, envelope domain.AlarmQueueEnvelope) error {
    jsonBytes, err := json.Marshal(envelope)
    if err != nil {
        return fmt.Errorf("publish alarm queue: marshal envelope: %w", err)
    }
    cmd := p.cache.B().Lpush().Key(AlarmDispatchQueue).Element(string(jsonBytes)).Build()
    results := p.cache.DoMulti(ctx, cmd)
    // ... error handling
}
```

### 목표 코드

```go
func (p *Publisher) publishValkeyBatch(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) (dispatchoutbox.PublishBatchResult, error) {
    result := dispatchoutbox.PublishBatchResult{RequestedDeliveries: len(envelopes)}
    if len(envelopes) == 0 {
        return result, nil
    }

    cmds := make([]valkey.Completed, 0, len(envelopes))
    for i := range envelopes {
        jsonBytes, err := json.Marshal(envelopes[i])
        if err != nil {
            return result, fmt.Errorf("publish alarm queue batch: marshal envelope %d: %w", i, err)
        }
        cmds = append(cmds, p.cache.B().Lpush().Key(AlarmDispatchQueue).Element(string(jsonBytes)).Build())
    }

    responses := p.cache.DoMulti(ctx, cmds...)
    for i, resp := range responses {
        if err := resp.Error(); err != nil {
            return result, fmt.Errorf("publish alarm queue batch: lpush envelope %d: %w", i, err)
        }
        result.ProcessedDeliveries++
    }
    return result, nil
}
```

### 주의사항

- 부분 실패 시 `result.ProcessedDeliveries`가 성공 건수를 정확히 반영해야 함
- 호출자 `publishBatchAndMark` (`notifier_publish.go:31-51`)이 `ProcessedDeliveries`를 기준으로 claim 릴리스를 결정하므로 카운트 정확성이 중요
- `publishValkey` 비공개 함수는 `publishValkeyBatch`에 흡수되므로 `Publish` 단건 메서드 (`publisher.go:134`)가 `publishValkeyBatch`를 호출하는지 확인. 현재 `Publish` → `PublishBatch` → `publishEnvelopes` → `publishValkeyBatch` 경로이므로 단건 호출에도 자동 적용됨.

### Valkey import 추가 필요

```go
import "github.com/valkey-io/valkey-go"
```

파일 상단 import에 이미 있는지 확인. 현재 `publisher.go`는 `cache.Client` 인터페이스를 통해 간접 사용 중이므로 `valkey.Completed` 타입 직접 참조를 위해 import 필요할 수 있음.

### 검증

```bash
go test ./hololive/hololive-shared/pkg/service/alarm/queue/...
go build ./hololive/hololive-shared/...
```

### 검증 포인트

- 기존 publisher 테스트 전부 통과
- 빈 배치, 단건 배치, N건 배치, 중간 실패 시나리오 검증
- `result.ProcessedDeliveries` 카운트가 부분 실패 시 정확한지 확인

---

<a id="phase-3"></a>
## Phase 3: Hash conflict 격리 처리

**분류:** 정합성(Critical)
**난이도:** 중간
**예상 변경:** ~40줄

### 문제

`repository_insert.go:149-151`에서 `loadEventIDs` hash conflict 감지 시 전체 배치 실패.
원인: `BuildEventKey`는 title 미포함(event_key에 streamID/channelID/scheduleUnix 기반) → 동일 event_key.
`marshalEventPayload`는 title 포함 → payload hash 변경.
title 변경 시 hash mismatch → 해당 event_key를 포함하는 **모든 배치가 영구 실패**.

### 변경 대상

```
hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/repository_insert.go
```

### 현재 코드 (line 132-161)

```go
func loadEventIDs(ctx context.Context, tx pgx.Tx, keys []string, expectedHashes map[string]string, eventCount int) (map[string]int64, error) {
    // ...
    for existingRows.Next() {
        // ...
        if expectedHashes[key] != hash {
            return nil, fmt.Errorf("dispatch event hash conflict: event_key=%s", key)
        }
        eventIDs[key] = id
    }
    // ...
    if len(eventIDs) != eventCount {
        return nil, fmt.Errorf("load dispatch event ids: found %d of %d rows", len(eventIDs), eventCount)
    }
    // ...
}
```

### 접근법 A: hash conflict 이벤트만 건너뛰기 (권장)

conflict 이벤트를 에러 대신 skip하고, 해당 이벤트에 연결된 delivery도 제외.

```go
func loadEventIDs(ctx context.Context, tx pgx.Tx, keys []string, expectedHashes map[string]string, eventCount int) (map[string]int64, []string, error) {
    // 세 번째 반환값: conflict가 발생한 event_key 목록
    var conflictKeys []string
    // ...
    for existingRows.Next() {
        // ...
        if expectedHashes[key] != hash {
            conflictKeys = append(conflictKeys, key)
            continue
        }
        eventIDs[key] = id
    }
    // ...
    if len(eventIDs)+len(conflictKeys) != eventCount {
        return nil, nil, fmt.Errorf("load dispatch event ids: found %d+%d of %d rows", len(eventIDs), len(conflictKeys), eventCount)
    }
    return eventIDs, conflictKeys, nil
}
```

### 연쇄 변경

1. `insertEvents` (line 54-73): `loadEventIDs` 시그니처 변경 반영, conflict keys 반환
2. 호출자 `InsertBatch` / `insertPreparedBatch`: conflict event에 연결된 delivery를 배치에서 제외
3. `PublishBatchResult`에 `HashConflictEvents int` 필드가 이미 존재 → 정확한 카운트 전달

### 접근법 B: hash 갱신 (대안)

```sql
ON CONFLICT (event_key) DO UPDATE SET
    payload_hash = EXCLUDED.payload_hash,
    payload = EXCLUDED.payload,
    updated_at = NOW()
```

**주의:** 이 접근은 기존 event payload를 덮어씀. 기존 delivery가 이전 payload 기반이라면 불일치 발생. downstream consumer가 event payload를 재참조하는 경우에만 사용 가능. 이 리뷰에서는 접근법 A 권장.

### 검증

```bash
go test ./hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/...
go build ./hololive/hololive-shared/...
```

### 테스트 추가 필요

- title 변경으로 hash가 달라진 event 포함 배치 → conflict event만 skip, 나머지 정상 처리
- conflict event에 연결된 delivery가 배치에서 제외되는지 확인
- `PublishBatchResult.HashConflictEvents` 카운트 정확성

---

<a id="phase-4"></a>
## Phase 4: Dedup slog 관측성 보강

**분류:** 관측성(Important)
**난이도:** 낮음
**예상 변경:** ~30줄

### 문제

dedup critical path에 로그 전무. claim 성공/스킵 구분 불가. 폴백 복구 시점 미기록. `Send()` 배치 결과 미기록.

### 변경 대상 4개 파일

#### 4-1. `dedup/service.go` — claim 결과 로깅

`tryClaimKey` (line 363-369)에 DEBUG 레벨 로그 추가:

```go
func (s *Service) tryClaimKey(ctx context.Context, key string, ttl time.Duration) bool {
    acquired, err := s.cache.SetNX(ctx, key, "1", ttl)
    if err != nil {
        s.logger.Debug("dedup claim fallback",
            slog.String("key", key),
            slog.String("error", err.Error()),
        )
        return s.fallback.TryClaimOnOutage(key, ttl, err)
    }
    s.logger.Debug("dedup claim result",
        slog.String("key", key),
        slog.Bool("acquired", acquired),
    )
    return acquired
}
```

**주의:** DEBUG 레벨 사용. 건당 호출이므로 INFO 시 로그 폭발.

#### 4-2. `dedup/fallback.go` — 폴백 상태 변화 로깅

`TryClaimOnOutage` (line 65-80)에는 이미 WARN 로그 존재. 추가 필요 없음.

폴백에서 Valkey 복구 시점은 현재 감지 불가 — `tryClaimKey`가 다음 성공 시 자동 복구되므로 별도 로그 불필요. 대신 fallback key count가 유의미:

`tryClaim` (line 91-108) 내 cleanup 실행 시 INFO 로그 추가:

```go
func (f *LocalFallback) tryClaim(key string, ttl time.Duration) bool {
    now := f.now()
    expiresAt := now.Add(ttl).UnixNano()
    if f.keyCount.Load() >= int64(constants.LocalFallbackCleanupMaxKeys) {
        f.logger.Info("fallback cleanup triggered",
            slog.Int64("key_count", f.keyCount.Load()),
        )
        f.cleanupExpired(now)
    }
    // ...
}
```

#### 4-3. `notifier.go` — Send 배치 결과 로깅

`Send` (line 70-78) 반환 직전에 배치 결과 로그 추가:

```go
func (n *Notifier) Send(ctx context.Context, notifications []*domain.AlarmNotification) (SendResult, error) {
    result, prepared, errs := n.prepareSendBatch(ctx, notifications)
    if len(prepared) > 0 {
        errs = n.publishPreparedBatch(ctx, prepared, &result, errs)
    }
    n.logger.Info("notification batch completed",
        slog.Int("total", len(notifications)),
        slog.Int("sent", result.Sent),
        slog.Int("skipped", result.Skipped),
        slog.Int("failed", result.Failed),
    )
    return result, errors.Join(errs...)
}
```

#### 4-4. `repository_insert.go` — hash conflict 로깅 (Phase 3과 연동)

Phase 3에서 conflict를 skip 처리하면, skip 시 WARN 로그 추가:

```go
if expectedHashes[key] != hash {
    logger.Warn("dispatch event hash conflict, skipping",
        slog.String("event_key", key),
        slog.String("expected_hash", expectedHashes[key][:8]+"..."),
        slog.String("actual_hash", hash[:8]+"..."),
    )
    conflictKeys = append(conflictKeys, key)
    continue
}
```

### 검증

```bash
go test ./hololive/hololive-shared/pkg/service/alarm/dedup/...
go test ./hololive/hololive-alarm-worker/...
go build ./hololive/hololive-shared/... ./hololive/hololive-alarm-worker/...
```

### 검증 포인트

- 로그 레벨 확인: claim 결과는 DEBUG, 배치 결과는 INFO, conflict는 WARN
- 기존 테스트에서 로그 출력이 assertion에 영향 없는지 확인
- slog 필드명이 기존 코드베이스 컨벤션과 일치하는지 확인 (snake_case)

---

<a id="phase-5"></a>
## Phase 5: `claimDedup` 투기적 파이프라인

**분류:** 성능(High)
**난이도:** 중간
**예상 변경:** ~60줄 (cache layer 확장 포함)

### 문제

`notifier.go:321-366`에서 notification + logical + schedule 3단계 순차 SETNX.
50건 배치 기준 100~200 RTT.

### 전략

notification claim과 logical claim은 **둘 다 성공해야** 하므로 2 SETNX를 `DoMulti`로 한 번에 보내고, 결과에 따라 rollback.

### 변경 대상

#### 5-1. cache layer 확장

```
hololive/hololive-shared/pkg/service/cache/interface.go
hololive/hololive-shared/pkg/service/cache/service_kv.go
```

`Client` 인터페이스에 `SetNXMulti` 추가:

```go
// interface.go — Client interface에 추가
SetNXMulti(ctx context.Context, entries []SetNXEntry) ([]SetNXResult, error)
```

```go
// 새 타입
type SetNXEntry struct {
    Key   string
    Value string
    TTL   time.Duration
}

type SetNXResult struct {
    Key      string
    Acquired bool
    Err      error
}
```

`service_kv.go` 구현:

```go
func (c *Service) SetNXMulti(ctx context.Context, entries []SetNXEntry) ([]SetNXResult, error) {
    if len(entries) == 0 {
        return nil, nil
    }
    cmds := make([]valkey.Completed, 0, len(entries))
    for _, e := range entries {
        ttlSeconds, err := ttlSecondsCeil(e.TTL)
        if err != nil {
            return nil, NewCacheError("invalid ttl", "setnx_multi", e.Key, err)
        }
        cmds = append(cmds, c.client.B().Set().Key(e.Key).Value(e.Value).Nx().ExSeconds(ttlSeconds).Build())
    }
    responses := c.client.DoMulti(ctx, cmds...)
    results := make([]SetNXResult, len(entries))
    for i, resp := range responses {
        results[i].Key = entries[i].Key
        if util.IsValkeyNil(resp.Error()) {
            results[i].Acquired = false
        } else if resp.Error() != nil {
            results[i].Err = resp.Error()
        } else {
            results[i].Acquired = true
        }
    }
    return results, nil
}
```

#### 5-2. dedup service에 `TryClaimPair` 추가

```
hololive/hololive-shared/pkg/service/alarm/dedup/service.go
```

```go
func (s *Service) TryClaimPair(ctx context.Context, key1, key2 string, ttl time.Duration) (acquired1, acquired2 bool) {
    results, err := s.cache.SetNXMulti(ctx, []cache.SetNXEntry{
        {Key: key1, Value: "1", TTL: ttl},
        {Key: key2, Value: "1", TTL: ttl},
    })
    if err != nil {
        return s.fallback.TryClaimOnOutage(key1, ttl, err),
               s.fallback.TryClaimOnOutage(key2, ttl, err)
    }
    for i, r := range results {
        if r.Err != nil {
            if i == 0 {
                acquired1 = s.fallback.TryClaimOnOutage(key1, ttl, r.Err)
            } else {
                acquired2 = s.fallback.TryClaimOnOutage(key2, ttl, r.Err)
            }
        } else {
            if i == 0 { acquired1 = r.Acquired } else { acquired2 = r.Acquired }
        }
    }
    return
}
```

#### 5-3. `claimDedup` 리팩터링

```
hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/notifier.go
```

`claimDedup` (line 321-366)에서 notification + logical claim을 `TryClaimPair`로 교체:

```go
func (n *Notifier) claimDedup(ctx context.Context, payload *sendInput) ([]string, bool, error) {
    category := keys.NotificationCategory(...)
    notifyKey := keys.BuildNotifyClaimKey(...)
    logicalKey := keys.BuildLogicalEventClaimKey(...)

    notifyClaimed, logicalClaimed := n.dedupService.TryClaimPair(
        ctx, notifyKey, logicalKey, constants.CacheTTL.NotificationSent,
    )

    if !notifyClaimed {
        if logicalClaimed {
            n.releaseClaimsBestEffort(ctx, []string{logicalKey}, "...")
        }
        return nil, false, nil
    }
    if !logicalClaimed {
        n.releaseClaimsBestEffort(ctx, []string{notifyKey}, "...")
        return nil, false, nil
    }

    claimKeys := compactClaimKeys(notifyKey, logicalKey)
    // schedule change claim은 순차 유지 (발생 빈도 낮음)
    scheduleClaimKeys, scheduleClaimed, err := n.claimScheduleChangeDedup(ctx, payload)
    // ... 기존 로직 유지
}
```

### 주의사항

- `TryClaimPair`에서 key1 성공 + key2 실패 시, key1을 호출자가 명시적으로 릴리스해야 함
- schedule change claim은 발생 빈도가 낮으므로(스케줄 변경 시에만) 순차 유지가 합리적
- 기존 `TryClaimNotification`, `TryClaimLogicalEvent`는 다른 호출자가 있을 수 있으므로 제거하지 않고 유지
- `SetNXMulti`의 `DoMulti` 파이프라인은 Valkey 서버에서 원자적 실행이 아님(각 명령 독립 실행). 이는 기존 순차 SETNX와 동일한 의미론

### 검증

```bash
go test ./hololive/hololive-shared/pkg/service/cache/...
go test ./hololive/hololive-shared/pkg/service/alarm/dedup/...
go test ./hololive/hololive-alarm-worker/...
go build ./hololive/hololive-shared/... ./hololive/hololive-alarm-worker/...
```

### 테스트 추가 필요

- `SetNXMulti`: 둘 다 성공, key1만 성공, key2만 성공, 둘 다 실패, Valkey 에러 시 fallback
- `TryClaimPair`: 위 조합 + fallback 경로
- `claimDedup`: 기존 테스트가 리팩터링 후에도 동일 동작하는지 확인

---

<a id="phase-6"></a>
## Phase 6: Event insert 1-query 통합

**분류:** 성능(Medium)
**난이도:** 낮음
**예상 변경:** ~25줄

### 문제

`repository_insert.go`에서 event insert 후 별도 SELECT로 ID 조회. `ON CONFLICT DO NOTHING`이 기존 행을 RETURNING하지 않기 때문.

### 변경 대상

```
hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/repository_insert.go
```

### 현재 코드 (insertEventBatch, line 95-130)

```sql
INSERT INTO alarm_dispatch_events (...)
SELECT ... FROM input
ON CONFLICT (event_key) DO NOTHING
RETURNING event_key
```

→ 이후 별도 `loadEventIDs` (line 132-161):

```sql
SELECT id, event_key, payload_hash
FROM alarm_dispatch_events
WHERE event_key = ANY($1)
```

### 목표: no-op UPDATE trick으로 RETURNING에 모든 행 포함

```sql
INSERT INTO alarm_dispatch_events (
    event_key, payload_hash, alarm_type, channel_id, stream_id, category,
    payload_schema_version, payload
)
SELECT event_key, payload_hash, alarm_type::alarm_type, channel_id, stream_id, category, 1, payload
FROM input
ON CONFLICT (event_key) DO UPDATE SET updated_at = NOW()
RETURNING id, event_key, payload_hash
```

### 주의사항

- `DO UPDATE SET updated_at = NOW()`는 기존 행의 `updated_at`을 갱신하지만, 이는 사실상 무해. `updated_at` 컬럼이 이미 존재하고 informational 용도
- **hash conflict 검증이 RETURNING에서 직접 가능**: `loadEventIDs`의 `expectedHashes[key] != hash` 검증을 RETURNING 결과에서 수행
- `loadEventIDs` 함수 제거 또는 `insertEventBatch`에 흡수 가능
- Phase 3의 hash conflict 격리 처리와 **동시 적용 시 주의**: conflict skip 로직의 위치가 `loadEventIDs`에서 `insertEventBatch` RETURNING 루프로 이동

### 검증

```bash
go test ./hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/...
go build ./hololive/hololive-shared/...
```

### 검증 포인트

- 신규 event: id 반환됨, hash 일치
- 기존 event (동일 hash): id 반환됨, `updated_at` 갱신됨
- 기존 event (다른 hash): id 반환됨, hash mismatch 감지 (Phase 3 연동)
- 배치 내 중복 event_key: 첫 건만 insert, 나머지는 update → 모두 같은 id 반환

---

## 빅뱅 패치 구현 순서 (파일 충돌 최소화)

```
단계 1 ─ cache layer 확장 (Phase 5-1)
  ├─ interface.go: SetNXMulti 추가
  └─ service_kv.go: SetNXMulti 구현

단계 2 ─ dedup/service.go 통합 변경 (Phase 1 + Phase 5-2 + Phase 4-1)
  ├─ writeNotifiedHashFields → HMSet 원자화
  ├─ TryClaimPair 추가
  └─ tryClaimKey DEBUG 로그 추가

단계 3 ─ dedup/fallback.go 로깅 (Phase 4-2)
  └─ tryClaim cleanup INFO 로그 추가

단계 4 ─ publisher.go 배치화 (Phase 2)
  └─ publishValkeyBatch → DoMulti 단일 호출

단계 5 ─ repository_insert.go 통합 변경 (Phase 3 + Phase 6 + Phase 4-4)
  ├─ insertEventBatch: DO UPDATE + RETURNING id,event_key,payload_hash
  ├─ loadEventIDs 제거 또는 흡수
  ├─ hash conflict → skip + WARN 로그 (에러 → 건너뛰기)
  └─ insertEvents 시그니처 변경 (conflict keys 반환)

단계 6 ─ notifier.go 통합 변경 (Phase 5-3 + Phase 4-3)
  ├─ claimDedup → TryClaimPair 사용
  └─ Send() 배치 결과 INFO 로그 추가

단계 7 ─ InsertBatch 호출 체인 (Phase 3 연쇄)
  └─ conflict event에 연결된 delivery 배치에서 제외
```

## 변경 파일 요약

| 파일 | 적용 페이즈 | 변경 범위 |
|---|---|---|
| `cache/interface.go` | 5-1 | `SetNXMulti` 인터페이스 추가 |
| `cache/service_kv.go` | 5-1 | `SetNXMulti` 구현 |
| `dedup/service.go` | 1 + 4-1 + 5-2 | `writeNotifiedHashFields` + `TryClaimPair` + 로그 |
| `dedup/fallback.go` | 4-2 | cleanup 로그 |
| `queue/publisher.go` | 2 | `publishValkeyBatch` 배치화 |
| `dispatchoutbox/repository_insert.go` | 3 + 4-4 + 6 | event insert 통합 + conflict 격리 + 로그 |
| `checking/notifier.go` | 4-3 + 5-3 | `claimDedup` 리팩터 + 배치 결과 로그 |

## 전체 검증 (빅뱅 패치 완료 후 1회)

```bash
go build ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-admin-api/... ./hololive/hololive-alarm-worker/... ./hololive/hololive-kakao-bot-go/... ./hololive/hololive-llm-sched/... ./hololive/hololive-youtube-producer/...
go test ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-admin-api/... ./hololive/hololive-alarm-worker/... ./hololive/hololive-kakao-bot-go/... ./hololive/hololive-llm-sched/... ./hololive/hololive-youtube-producer/...
```

## 롤백 전략

단일 PR이므로 git revert 1회로 전체 롤백 가능. 개별 페이즈 롤백은 불가.
배포 전 staging에서 다음 시나리오 확인:
- 신규 알림 정상 발송 (claim → publish → deliver)
- 중복 알림 차단 (동일 stream 재폴링 시 skip)
- 스케줄 변경 알림 (title 또는 시간 변경 감지 → 재발송)
- Valkey 연결 끊김 시 fallback 동작 + 로그 출력
