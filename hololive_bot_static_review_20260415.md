# hololive-bot-review-bundle 정적 리뷰 보고서 (2026-04-15)

이 문서는 `/mnt/data/hololive-bot-review-bundle-full-20260415T080058Z.tar.gz`를 풀어 정적으로 스캔한 결과를 정리한 것이다.

## 검증 범위와 한계

- 전체 번들을 풀어 Go monorepo와 `admin-dashboard`를 함께 점검했다.
- 아키텍처 가드 스크립트(`check-file-loc.sh`, `check-go-module-loc.sh`)를 실행해 구조적 부채를 확인했다.
- 다만 이 환경의 Go 버전은 `go1.23.2`이고, 저장소 `go.work`는 `go >= 1.26.2`를 요구해 실제 `go test ./...`와 컴파일 검증은 수행하지 못했다.
- 따라서 아래 내용은 정적 분석 + 코드 경로 추적 기반이며, 특히 동시성·에러 처리·상태 정합성 관련 항목에 무게를 두었다.

## 가장 우선순위가 높은 결론

1. `youtube/outbox` 계층에 **claim token 소유권이 행 단위로 정렬되지 않는 버그**가 있다. 실패한 한 건이 다른 건의 claim까지 풀어 중복 발송 레이스를 만들 수 있다.
2. `ACL service`는 **DB / 메모리 / 캐시의 실패 의미가 깨져 있다**. 지금은 캐시 반영 실패가 발생해도 DB와 메모리는 이미 바뀐 상태로 남는다.
3. `cache.Service.MSet`은 **marshal 실패를 부분적으로 무시하고 성공을 반환**한다. 일부 키만 기록되고 성공처럼 보이는 최악의 유형이다.
4. `Holodex SearchChannels`는 **첫 페이지 limit 응답만 로컬 필터링**한다. 전체 채널 목록 캐시가 있는데도 정확도와 네트워크 효율을 동시에 잃고 있다.
5. `bootstrap`, `alarm_service`, `acl/service`, `outbox` 주변에는 **wrapper/alias indirection, dead lifecycle, duplicated orchestration**가 많다. 유지보수성 저하가 이미 파일 크기 가드 초과로 드러난다.

## 아키텍처 가드 위반

### check-file-loc.sh
- `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher_send.go:919 > 850`
- `hololive/hololive-shared/pkg/service/youtube/poller/published_at_resolver_repository.go:531 > 450`
- `hololive/hololive-shared/pkg/service/youtube/tracking/observation_compare.go:735 > 720`
- `hololive/hololive-kakao-bot-go/internal/service/acl/service.go:561 > 520`
- `hololive/hololive-shared/pkg/service/youtube/poller/published_at_resolver.go:535 > 500`
- `hololive/hololive-shared/pkg/service/youtube/poller/repository_batch_delivery_state.go:475 > 450`
- `hololive/hololive-shared/pkg/service/youtube/tracking/alarm_state_repository.go:612 > 600`
- `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher_claim_gate.go:596 > 500`
- `hololive/hololive-kakao-bot-go/internal/service/notification/alarm_service.go:827 > 650`
- threshold 없음:
  - `hololive/hololive-shared/pkg/service/youtube/outbox/delivery_telemetry_repository.go:427 > 400`
  - `hololive/hololive-stream-ingester/internal/ops/community_shorts_alarm_sent_history_dataset_render.go:414 > 400`

### check-go-module-loc.sh
- `hololive/hololive-shared/pkg/service/youtube/poller/repository_batch_delivery_state.go:475 > 450`
- `hololive/hololive-kakao-bot-go/internal/service/acl/service.go:561 > 520`

---

## P0-1. Outbox claim token 소유권 버그

### 위치
- `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher_claim_gate.go`
- `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher_send.go`

### 왜 실제 버그인가

`deliveryClaimSelection`은 현재 `claimTokens []deliveryClaimToken`만 가진다. 그런데 `selectClaimedDeliveries`는 중복 claim identity를 reuse할 때 재사용된 행에 대한 token을 별도로 보관하지 않는다. 그 결과 `sendRows` 길이와 `claimTokens` 길이가 달라질 수 있다.

이 상태에서 `dispatcher_claim_gate.go`의 `claimTokensForIndex()`는 다음과 같이 동작한다.

- `len(claimTokens) == total`이면 해당 index의 token 1개 반환
- 아니면 **전체 claimTokens slice를 그대로 반환**

즉, 한 행 실패 시 그 행이 소유하지 않은 다른 행의 claim까지 `releaseDeliveryClaims`에 넘길 수 있다. 특히 dedupe/reuse 경로에서 한 건의 실패가 다른 건의 claim을 조기 해제해 중복 발송 레이스를 만들 수 있다.

### 패치 원칙
- aggregate token 목록과 별개로, **행별 token ownership**을 명시적으로 저장해야 한다.
- `claimTokensForIndex()` 같은 길이 추론 기반 복구 로직은 제거해야 한다.

### 제안 diff

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher_claim_gate.go b/hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher_claim_gate.go
@@
 type deliveryClaimSelection struct {
     sendRows               []domain.YouTubeNotificationDelivery
     sendOutboxes           []domain.YouTubeNotificationOutbox
     claimTokens            []deliveryClaimToken
+    rowClaimTokens         [][]deliveryClaimToken
     deferredRows           []domain.YouTubeNotificationDelivery
     deferredOutboxes       []domain.YouTubeNotificationOutbox
     skipRows               []domain.YouTubeNotificationDelivery
     skipOutboxes           []domain.YouTubeNotificationOutbox
     skipReasons            []string
@@
 func (d *Dispatcher) selectClaimedDeliveries(
     ctx context.Context,
     rows []domain.YouTubeNotificationDelivery,
     outboxes []domain.YouTubeNotificationOutbox,
 ) deliveryClaimSelection {
@@
     for i := range rows {
         claimIdentity := buildDeliveryClaimIdentity(rows[i], outboxes[i])
         claimToken, reused, skip, reason := reuseCache.resolve(claimIdentity)
         if skip {
             selection.skipRows = append(selection.skipRows, rows[i])
             selection.skipOutboxes = append(selection.skipOutboxes, outboxes[i])
             selection.skipReasons = append(selection.skipReasons, reason)
             continue
         }
+        rowTokens := []deliveryClaimToken(nil)
         if claimToken != nil && !reused {
-            selection.claimTokens = append(selection.claimTokens, *claimToken)
+            token := *claimToken
+            selection.claimTokens = append(selection.claimTokens, token)
+            rowTokens = []deliveryClaimToken{token}
         }
         selection.sendRows = append(selection.sendRows, rows[i])
         selection.sendOutboxes = append(selection.sendOutboxes, outboxes[i])
+        selection.rowClaimTokens = append(selection.rowClaimTokens, rowTokens)
     }
     return selection
 }
@@
-func claimTokensForIndex(claimTokens []deliveryClaimToken, idx, total int) []deliveryClaimToken {
-    if len(claimTokens) == 0 {
-        return nil
-    }
-    if len(claimTokens) == total && idx >= 0 && idx < len(claimTokens) {
-        return []deliveryClaimToken{claimTokens[idx]}
-    }
-    return claimTokens
-}
```

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher_send.go b/hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher_send.go
@@
-        d.dispatchClaimedRowsIndividually(ctx, claimSelection.sendRows, claimSelection.sendOutboxes, formattedMessages, formatFailures, claimSelection.claimTokens, result, mu)
+        d.dispatchClaimedRowsIndividually(
+            ctx,
+            claimSelection.sendRows,
+            claimSelection.sendOutboxes,
+            formattedMessages,
+            formatFailures,
+            claimSelection.rowClaimTokens,
+            result,
+            mu,
+        )
         return
     }
@@
 func (d *Dispatcher) dispatchClaimedRowsIndividually(
     ctx context.Context,
     rows []domain.YouTubeNotificationDelivery,
     outboxes []domain.YouTubeNotificationOutbox,
     formattedMessages map[string]string,
     formatFailures map[string]error,
-    claimTokens []deliveryClaimToken,
+    rowClaimTokens [][]deliveryClaimToken,
     result *dispatchClaimedRowsResult,
     mu *sync.Mutex,
 ) {
     for i := range rows {
-        claims := claimTokensForIndex(claimTokens, i, len(rows))
+        var claims []deliveryClaimToken
+        if i >= 0 && i < len(rowClaimTokens) {
+            claims = rowClaimTokens[i]
+        }
         d.dispatchClaimedDeliveryRow(ctx, rows[i], outboxes[i], formattedMessages, formatFailures, claims, result, mu)
     }
 }
```

### 반드시 추가할 테스트
- 중복 claim identity 2건 중 1건만 실제 token을 갖고 reuse되는 상황을 만들 것.
- 첫 번째 행 실패 시 `releaseDeliveryClaims` 호출 인자가 자기 token만 포함하는지 검증.
- 두 번째 행 성공/실패가 첫 번째 실패와 독립적으로 동작하는지 검증.

테스트 이름 예시:
- `TestSelectClaimedDeliveries_AssignsRowClaimTokens`
- `TestDispatchClaimedRowsIndividually_ReleasesOnlyOwnedClaimsOnFailure`

---

## P0-2. ACL service 상태 정합성 붕괴

### 위치
- `hololive/hololive-kakao-bot-go/internal/service/acl/service.go`
- 관련 테스트:
  - `service_db_test.go`
  - `service_test.go`

### 왜 위험한가

이 서비스는 DB, 프로세스 메모리, Valkey 캐시를 동시에 갱신한다. 그런데 현재 실패 의미가 깨져 있다.

대표 사례:
- `SetEnabled`, `SetMode`: DB 갱신 → 메모리 갱신 → 캐시 동기화. 캐시 실패 시 에러를 반환하지만 DB/메모리는 이미 변경된 상태.
- `AddRoom`, `RemoveRoom`: 메모리와 DB/캐시 갱신 순서가 제각각이며, 캐시 실패 시 rollback이 없어 상태가 갈라진다.
- `GetACLStatus`는 map iteration 결과를 그대로 반환해 room 순서가 비결정적이다.

더 큰 문제는 테스트가 이 비정상 의미를 **정답으로 고정**하고 있다는 점이다.
예:
- `TestACLService_SetEnabledReturnsCacheSyncErrorAfterDBCommit`
- `TestACLService_SetModeReturnsCacheSyncErrorAfterDBCommit`
- `TestACLService_AddRoomReturnsCacheSyncErrorAfterDBCommit`
- `TestACLService_RemoveRoomReturnsCacheSyncErrorAfterDBCommit`

### 패치 원칙
- API가 `error`를 반환할 때는, 호출자 관점에서 “변경이 적용되지 않았다”는 의미가 되도록 맞추는 편이 안전하다.
- 즉, 캐시 실패 시 **DB와 메모리까지 rollback**하든지, 아니면 반대로 **에러를 반환하지 않고 reconcile 작업**으로 설계를 바꿔야 한다.
- 현 코드/호출자 기대를 고려하면 즉시 패치는 rollback 방식이 현실적이다.

### 제안 diff (핵심)

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/service/acl/service.go b/hololive/hololive-kakao-bot-go/internal/service/acl/service.go
@@
 import (
     "context"
+    "errors"
     "fmt"
     "log/slog"
+    "sort"
@@
 func (s *Service) GetACLStatus() ACLStatus {
     s.mu.RLock()
     defer s.mu.RUnlock()
     rooms := make([]string, 0, len(s.activeRooms()))
     for room := range s.activeRooms() {
         rooms = append(rooms, room)
     }
+    sort.Strings(rooms)
     return ACLStatus{
         Enabled: s.enabled,
         Mode:    s.mode,
         Rooms:   rooms,
     }
 }
@@
 func (s *Service) SetEnabled(ctx context.Context, enabled bool) error {
     s.mu.RLock()
     current := s.enabled
     s.mu.RUnlock()
     if current == enabled {
         return nil
     }
     if err := s.upsertSetting(dbKeyEnabled, fmt.Sprintf("%t", enabled)); err != nil {
         return fmt.Errorf("persist acl enabled: %w", err)
     }
     s.mu.Lock()
     s.enabled = enabled
     s.mu.Unlock()
     if err := s.syncSettingsToValkey(ctx); err != nil {
+        rollbackErr := s.upsertSetting(dbKeyEnabled, fmt.Sprintf("%t", current))
+        s.mu.Lock()
+        s.enabled = current
+        s.mu.Unlock()
-        return fmt.Errorf("sync acl settings to cache: %w", err)
+        return errors.Join(
+            fmt.Errorf("sync acl settings to cache: %w", err),
+            wrapRollbackError("rollback acl enabled", rollbackErr),
+        )
     }
     s.logger.Info("ACL enabled state updated", slog.Bool("enabled", enabled))
     return nil
 }
@@
 func (s *Service) SetMode(ctx context.Context, mode ACLMode) error {
     s.mu.RLock()
     current := s.mode
     s.mu.RUnlock()
     if current == mode {
         return nil
     }
     if err := s.upsertSetting(dbKeyMode, string(mode)); err != nil {
         return fmt.Errorf("persist acl mode: %w", err)
     }
     s.mu.Lock()
     s.mode = mode
     s.mu.Unlock()
     if err := s.syncSettingsToValkey(ctx); err != nil {
+        rollbackErr := s.upsertSetting(dbKeyMode, string(current))
+        s.mu.Lock()
+        s.mode = current
+        s.mu.Unlock()
-        return fmt.Errorf("sync acl settings to cache: %w", err)
+        return errors.Join(
+            fmt.Errorf("sync acl settings to cache: %w", err),
+            wrapRollbackError("rollback acl mode", rollbackErr),
+        )
     }
     s.logger.Info("ACL mode updated", slog.String("mode", string(mode)))
     return nil
 }
```

`AddRoom`/`RemoveRoom`는 더 강하게 순서를 정리하는 것이 좋다.

```diff
@@
 func (s *Service) AddRoom(ctx context.Context, room string) (bool, error) {
     room = stringutil.TrimSpace(room)
     if room == "" {
         return false, nil
     }
-    s.mu.Lock()
-    if _, exists := s.activeRooms()[room]; exists {
-        s.mu.Unlock()
-        return false, nil
-    }
-    s.activeRooms()[room] = struct{}{}
-    mode := s.mode
-    s.mu.Unlock()
+    s.mu.RLock()
+    if _, exists := s.activeRooms()[room]; exists {
+        s.mu.RUnlock()
+        return false, nil
+    }
+    mode := s.mode
+    s.mu.RUnlock()

     listType := string(mode)
     if err := s.db.Create(&Room{RoomID: room, ListType: listType}).Error; err != nil {
-        s.mu.Lock()
-        delete(s.activeRooms(), room)
-        s.mu.Unlock()
         return false, fmt.Errorf("persist acl room: %w", err)
     }

     if _, err := s.cache.SAdd(ctx, s.valkeyKeyForMode(mode), []string{room}); err != nil {
-        return false, fmt.Errorf("sync acl room add to cache: %w", err)
+        rollbackErr := s.db.Where("room_id = ? AND list_type = ?", room, listType).Delete(&Room{}).Error
+        return false, errors.Join(
+            fmt.Errorf("sync acl room add to cache: %w", err),
+            wrapRollbackError("rollback acl room add", rollbackErr),
+        )
     }
+    s.mu.Lock()
+    s.activeRooms()[room] = struct{}{}
+    s.mu.Unlock()
     return true, nil
 }
```

```diff
@@
 func (s *Service) RemoveRoom(ctx context.Context, room string) (bool, error) {
     room = stringutil.TrimSpace(room)
     if room == "" {
         return false, nil
     }
-    s.mu.Lock()
-    if _, exists := s.activeRooms()[room]; !exists {
-        s.mu.Unlock()
-        return false, nil
-    }
-    delete(s.activeRooms(), room)
-    mode := s.mode
-    s.mu.Unlock()
+    s.mu.RLock()
+    if _, exists := s.activeRooms()[room]; !exists {
+        s.mu.RUnlock()
+        return false, nil
+    }
+    mode := s.mode
+    s.mu.RUnlock()

     listType := string(mode)
     if err := s.db.Where("room_id = ? AND list_type = ?", room, listType).Delete(&Room{}).Error; err != nil {
-        s.mu.Lock()
-        s.activeRooms()[room] = struct{}{}
-        s.mu.Unlock()
         return false, fmt.Errorf("delete acl room: %w", err)
     }

     if _, err := s.cache.SRem(ctx, s.valkeyKeyForMode(mode), []string{room}); err != nil {
-        return false, fmt.Errorf("sync acl room removal to cache: %w", err)
+        rollbackErr := s.db.Create(&Room{RoomID: room, ListType: listType}).Error
+        return false, errors.Join(
+            fmt.Errorf("sync acl room removal to cache: %w", err),
+            wrapRollbackError("rollback acl room removal", rollbackErr),
+        )
     }
+    s.mu.Lock()
+    delete(s.activeRooms(), room)
+    s.mu.Unlock()
     return true, nil
 }
```

보조 헬퍼:

```diff
@@
+func wrapRollbackError(action string, err error) error {
+    if err == nil {
+        return nil
+    }
+    return fmt.Errorf("%s: %w", action, err)
+}
```

### 테스트도 함께 뒤집어야 한다

기존 테스트는 위험한 의미를 고정하고 있으므로 아래 방향으로 바꿔야 한다.

- `SetEnabled/SetMode`: 캐시 sync 실패 후 DB, 메모리 모두 이전 상태여야 한다.
- `AddRoom`: 캐시 sync 실패 후 room이 DB/메모리 어디에도 남아 있지 않아야 한다.
- `RemoveRoom`: 캐시 sync 실패 후 room이 DB/메모리에 계속 남아 있어야 한다.
- `GetACLStatus`: 반환 room 목록이 정렬되어야 한다.

---

## P0-3. cache.MSet의 부분 성공 은폐

### 위치
- `hololive/hololive-shared/pkg/service/cache/service.go`

### 왜 위험한가

현재 `MSet`은 value marshal 실패 시 로그만 남기고 `continue` 한다. 그 후 남은 명령만 실행하고 `nil`을 반환할 수 있다. 최악의 경우 모든 키가 marshal 실패해도 명령이 0개라서 성공처럼 보인다.

이건 “성능 최적화”가 아니라 **데이터 손실 은폐**다.

### 제안 diff

```diff
diff --git a/hololive/hololive-shared/pkg/service/cache/service.go b/hololive/hololive-shared/pkg/service/cache/service.go
@@
 func (c *Service) MSet(ctx context.Context, pairs map[string]any, ttl time.Duration) error {
     if len(pairs) == 0 {
         return nil
     }

     cmds := make([]valkey.Completed, 0, len(pairs))
     for key, value := range pairs {
         jsonData, err := json.Marshal(value)
         if err != nil {
             c.logger.Error("Failed to marshal value for MSet",
                 slog.String("key", key),
                 slog.Any("error", err),
             )
-            continue
+            return NewCacheError("failed to marshal value for mset", "mset", key, err)
         }

         cmd := c.client.B().Set().Key(key).Value(string(jsonData)).ExSeconds(int64(ttl.Seconds())).Build()
         cmds = append(cmds, cmd)
     }
+    if len(cmds) != len(pairs) {
+        return NewCacheError(
+            "mset command count mismatch",
+            "mset",
+            "",
+            fmt.Errorf("commands=%d pairs=%d", len(cmds), len(pairs)),
+        )
+    }

     for _, resp := range c.client.DoMulti(ctx, cmds...) {
         if err := resp.Error(); err != nil {
             return NewCacheError("mset failed", "mset", "", err)
         }
     }
     return nil
 }
```

### 추가 테스트
- marshal 불가능한 값이 하나라도 있으면 `MSet`이 에러를 반환해야 한다.
- 성공과 실패가 섞인 경우에도 “좋은 키만 반쯤 저장”되지 않도록 fail-fast를 검증해야 한다.

---

## P1-1. Holodex SearchChannels 정확도와 네트워크 효율 동시 손실

### 위치
- `hololive/hololive-shared/pkg/service/holodex/service_channels.go`

### 문제
`SearchChannels`는 `/channels`를 `limit=DefaultChannelLimit`로 조회한 뒤 그 결과만 로컬 필터링한다. 즉, 첫 페이지에 없는 채널은 검색되지 않는다.

그런데 같은 파일 아래에는 이미 **전체 Hololive 채널 목록을 페이지네이션으로 가져와 캐시하는** `fetchHololiveChannelList()`가 존재한다.

정리하면:
- 정확도 손실: 첫 페이지 밖 결과 miss
- 네트워크 낭비: 전체 목록 캐시를 재사용하지 않음
- 중복 로직: 같은 데이터 소스를 두 방식으로 접근

### 제안 diff

```diff
diff --git a/hololive/hololive-shared/pkg/service/holodex/service_channels.go b/hololive/hololive-shared/pkg/service/holodex/service_channels.go
@@
 func (h *Service) SearchChannels(ctx context.Context, query string) ([]*domain.Channel, error) {
-    if cached, found := h.cacheManager.GetSearchChannels(ctx, query); found {
+    query = stringutil.TrimSpace(query)
+    if cached, found := h.cacheManager.GetSearchChannels(ctx, query); found {
         return cached, nil
     }
-
-    query = stringutil.TrimSpace(query)
-    params := url.Values{}
-    params.Set("limit", strconv.Itoa(DefaultChannelLimit))
-    channels, err := h.fetchChannels(ctx, params)
+    channels, err := h.fetchHololiveChannelList(ctx)
     if err != nil {
         h.logger.Error("Failed to search channels",
             slog.String("query", query),
             slog.Any("error", err),
         )
         return nil, fmt.Errorf("search channels: %w", err)
     }

-    filtered := make([]*domain.Channel, 0, len(channels))
-    lowerQuery := strings.ToLower(query)
-    for _, channel := range channels {
-        if channel == nil {
-            continue
-        }
-        if !h.filter.IsHololiveChannel(channel.Org, channel.Group) {
-            continue
-        }
-        if lowerQuery == "" ||
-            strings.Contains(strings.ToLower(channel.Name), lowerQuery) ||
-            strings.Contains(strings.ToLower(channel.EnglishName), lowerQuery) ||
-            strings.Contains(strings.ToLower(channel.ID), lowerQuery) {
-            filtered = append(filtered, channel)
-        }
-    }
+    filtered := filterChannelsByQuery(channels, query, h.filter)

     h.cacheManager.SetSearchChannels(ctx, query, filtered)
     return filtered, nil
 }
+
+func filterChannelsByQuery(channels []*domain.Channel, query string, filter *StreamFilter) []*domain.Channel {
+    filtered := make([]*domain.Channel, 0, len(channels))
+    lowerQuery := strings.ToLower(stringutil.TrimSpace(query))
+    for _, channel := range channels {
+        if channel == nil {
+            continue
+        }
+        if !filter.IsHololiveChannel(channel.Org, channel.Group) {
+            continue
+        }
+        if lowerQuery == "" ||
+            strings.Contains(strings.ToLower(channel.Name), lowerQuery) ||
+            strings.Contains(strings.ToLower(channel.EnglishName), lowerQuery) ||
+            strings.Contains(strings.ToLower(channel.ID), lowerQuery) {
+            filtered = append(filtered, channel)
+        }
+    }
+    return filtered
+}
```

---

## P1-2. Chzzk client의 nil logger panic + 결과 순서 비결정성

### 위치
- `hololive/hololive-kakao-bot-go/internal/service/chzzk/client.go`

### 문제 1: nil logger panic
`NewClientWithConfig`가 `cfg.Logger`를 그대로 저장한다. 이후 에러 경로에서 `c.logger.Warn/Error`를 바로 호출한다. 즉, nil logger가 들어오면 정상 경로에서는 조용하지만 장애 시점에 panic으로 번질 수 있다.

### 문제 2: 대상 채널 순서 비결정성
`normalizeChannelTargets`가 map으로 dedupe한 뒤 iteration 결과를 그대로 slice로 만든다. 이후 상태 체크 경로와 page scan 경로가 결과 순서를 다르게 만들 수 있다.

### 제안 diff

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/service/chzzk/client.go b/hololive/hololive-kakao-bot-go/internal/service/chzzk/client.go
@@
 func NewClientWithConfig(cfg ClientConfig) *Client {
+    logger := cfg.Logger
+    if logger == nil {
+        logger = slog.Default()
+    }
     return &Client{
         httpClient: cfg.HTTPClient,
         baseURL:    cfg.BaseURL,
-        logger:     cfg.Logger,
+        logger:     logger,
         now:        cfg.Now,
         sleep:      cfg.Sleep,
     }
 }
@@
 func normalizeChannelTargets(channelIDs []string) []string {
     targetSet := make(map[string]struct{}, len(channelIDs))
     for _, channelID := range channelIDs {
         channelID = strings.TrimSpace(channelID)
         if channelID == "" {
             continue
         }
         targetSet[channelID] = struct{}{}
     }
     targets := make([]string, 0, len(targetSet))
     for channelID := range targetSet {
         targets = append(targets, channelID)
     }
+    slices.Sort(targets)
     return targets
 }
```

선택적으로 `getLivesByPageScan` 반환도 입력 channelIDs 순서로 재정렬하는 것이 좋다.

---

## P1-3. Holodex API client transport profile 불일치

### 위치
- `shared-go/pkg/httputil/client.go`
- `hololive/hololive-shared/pkg/service/holodex/api_client.go`

### 문제
공용 HTTP 유틸에는 transport pool/timeouts를 설정한 `NewExternalAPIClient`가 있는데, Holodex API client 기본값은 plain `NewClient(timeout)`를 사용한다. 주입 없는 생성 경로에서는 shared transport profile을 우회한다.

### 제안 diff

```diff
diff --git a/hololive/hololive-shared/pkg/service/holodex/api_client.go b/hololive/hololive-shared/pkg/service/holodex/api_client.go
@@
     if httpClient == nil {
-        httpClient = httputil.NewClient(constants.APIConfig.HolodexTimeout)
+        httpClient = httputil.NewExternalAPIClient(constants.APIConfig.HolodexTimeout)
     }
```

이 변경은 코드량이 거의 없고, 네트워크 동작을 공용 정책으로 맞춘다.

---

## P1-4. Member repository의 silent partial read

### 위치
- `hololive/hololive-shared/pkg/service/member/repository.go`

### 문제
row scan/parse 실패를 로그만 남기고 `continue` 하는 패턴이 반복된다. 이러면 데이터 손상이나 스키마 드리프트가 “그냥 일부 결과가 없는 것처럼” 보인다. 운영에서 가장 찾기 어려운 유형이다.

### 권장 수정 방향
- 기본 함수는 fail-fast 또는 `errors.Join`으로 partial error를 함께 반환.
- 최소한 startup / sync / warmup 경로에서는 strict mode를 사용.
- 행 드랍 건수 metric 추가.

예시 스켈레톤:

```go
var rowErrs []error
for rows.Next() {
    ...
    if err := rows.Scan(...); err != nil {
        rowErrs = append(rowErrs, fmt.Errorf("scan member row: %w", err))
        continue
    }
}
if len(rowErrs) > 0 {
    return members, errors.Join(rowErrs...)
}
return members, nil
```

---

## P1-5. Twitch client의 무제한 fan-in 요청

### 위치
- `hololive/hololive-kakao-bot-go/internal/service/twitch/client.go`

### 문제
`GetStreamsByUserLogins` 류 경로가 `user_login` 쿼리 파라미터를 한 요청에 계속 추가하는 구조다. 입력 크기에 대한 chunking/상한이 없다.

### 위험
- 요청 URL 길이 증가
- 상위 호출자가 큰 입력을 넣으면 단일 실패 반경 확대
- retry 시 비용이 커짐

### 권장 패치
- `const maxUserLoginsPerRequest = ...` 도입
- chunk 단위 호출 후 결과 merge
- chunk 실패 시 어느 묶음이 실패했는지 로깅

---

## P2-1. AlarmService는 이미 God object 단계

### 위치
- `hololive/hololive-kakao-bot-go/internal/service/notification/alarm_service.go`

### 관찰
- 827 LOC로 내부 가드 초과
- `Close()`가 no-op인데 `CloseAllAlarmServices()` 글로벌 registry가 따로 있다
- 알람 CRUD, 캐시 반영, 구독자 인덱스, room/user metadata cache, platform mapping sync가 한 파일에 섞여 있다
- `removeAlarmFromCache`, `clearRoomAlarmsFromCache` 등에서 캐시 choregraphy가 반복된다

### 판단
이 파일은 지금 당장은 “버그 하나”보다는 **변경 위험이 너무 큰 구조**가 문제다. 이미 lifecycle abstraction이 형식만 있고 실체가 없다. 전형적인 리팩토링 중단 흔적, 혹은 AI/자동완성 주도 분할 실패 흔적이다.

### 권장 분리
- `alarm_service_mutation.go`
- `alarm_service_cache.go`
- `alarm_service_platform_mapping.go`
- `alarm_service_query.go`
- `alarm_service_lifecycle.go`

그리고 아래 둘 중 하나를 선택해야 한다.
1. 진짜 닫아야 할 리소스가 있으면 `Close()`를 구현
2. 아니면 글로벌 registry와 `CloseAllAlarmServices()`를 삭제

---

## P2-2. Bootstrap wrapper/alias 레이어는 제거하는 편이 맞다

### 위치
- `hololive/hololive-kakao-bot-go/internal/app/*`
- 특히 `bootstrap_*` wrapper 파일들, `bootstrap_services_types.go`, `bootstrap_type_aliases_test.go`

### 관찰
여러 파일이 실제 구현 없이 `internal/app/bootstrap` 패키지 함수만 다시 감싼다. 타입 alias 파일과 alias equality test까지 있다. 예를 들면:
- `bootstrap_services.go`
- `bootstrap_services_llm_clients.go`
- `bootstrap_bot_server.go`
- `bootstrap_bot_config_subscriber.go`
- `bootstrap_services_types.go`
- `bootstrap_type_aliases_test.go`

### 판단
이건 설계 계층이 아니라 **이중 내비게이션 표면**이다. 호출자는 wrapper를 따라가고 다시 bootstrap 패키지로 점프해야 한다. 테스트도 동작이 아니라 별칭을 검증한다. 유지보수 이득이 거의 없다.

### 패치 방향
- 호출부에서 `appbootstrap`을 직접 사용하도록 바꾼다.
- wrapper 함수 및 alias 파일을 제거한다.
- alias 전용 테스트를 삭제하고 실제 bootstrap 동작 테스트만 남긴다.

예시:

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/app/bootstrap_bot.go b/hololive/hololive-kakao-bot-go/internal/app/bootstrap_bot.go
@@
-    infra, err := initCoreInfrastructure(ctx, cfg, logger)
+    infra, err := appbootstrap.InitCoreInfrastructure(ctx, cfg, logger)
```

이 변경을 모든 호출부에 일괄 적용한 뒤 wrapper 파일을 삭제하면 된다.

---

## P2-3. Holodex stream service 중복도 매우 높음

### 위치
- `hololive/hololive-shared/pkg/service/holodex/service_streams.go`

### 관찰
`GetLiveStreamsByOrg`와 `GetUpcomingStreamsByOrg`는 본문 구조가 거의 동일하다. 상태값, 캐시 키, fallback 메시지 정도만 다르다. 정적 비교 시 body similarity가 약 86.5% 수준이다.

### 권장 패치
공통 orchestration을 `getStreamsByOrgWithFallback(...)` 같은 내부 헬퍼로 뽑아야 한다. 이 변경은 기능 수정이 아니라 drift 방지 목적이다. 지금 구조에서는 한쪽만 수정되고 다른 쪽이 뒤처질 확률이 높다.

---

## P2-4. Scraper proxy manager는 transport tuning과 dial fallback이 과복잡

### 위치
- `hololive/hololive-shared/pkg/service/youtube/scraper/proxy_manager.go`

### 관찰
- transport 생성 로직이 길고 shared profile과 중복된다
- context 미지원 SOCKS5 dialer를 위해 goroutine-per-dial fallback을 사용한다

### 판단
즉시 기능 버그라고 단정하긴 어렵지만, 취소가 빈번한 상황에서 goroutine 축적이 일시적으로 커질 수 있다. 또한 네트워크 정책이 공용 유틸과 분산돼 drift 가능성이 높다.

### 권장 패치
- transport builder를 공통 helper로 추출
- SOCKS5 dial fallback을 작은 어댑터로 격리
- dial 시도/취소 metric 추가

---

## Admin dashboard 짧은 결론

Rust/TS 대시보드도 훑었지만, 현재 기준 운영 리스크는 Go 런타임 계층이 훨씬 크다.

다만 한 가지는 주석으로 남길 가치가 있다.

### `auth/session.rs`
세션 refresh 시 Redis TTL은 연장하지만 직렬화된 payload 안의 `expires_at`는 갱신되지 않을 여지가 있다. 현재 검증 로직이 TTL/절대 시간 기준이라 당장 치명상은 아니지만, 향후 UI나 다른 코드가 serialized `expires_at`를 신뢰하면 오해를 낳을 수 있다.

---

## 추천 실행 순서

### Day 1
1. Outbox claim ownership 버그 패치 + 회귀 테스트
2. ACL rollback semantics 패치 + 기존 “위험한 의미” 테스트 교체
3. cache.MSet fail-fast 패치 + 단위 테스트

### Day 2
4. Holodex SearchChannels 전체 목록 캐시 재사용
5. Chzzk nil logger / deterministic ordering 패치
6. Holodex API client default transport 통일

### Day 3+
7. AlarmService 분할
8. Bootstrap wrapper 제거
9. Member repository strict/partial-error 설계 도입
10. Holodex stream duplication 제거

---

## 최종 판단

이 저장소는 “당장 망가질 코드”와 “다음 변경 때 망가지기 쉬운 코드”가 섞여 있다.

- **당장 고쳐야 하는 실제 버그**는 outbox claim token, ACL 정합성, cache.MSet 세 가지다.
- **정확도/성능 개선**은 Holodex SearchChannels와 transport profile 통일이 가장 비용 대비 효과가 좋다.
- **구조적 부채**는 AlarmService, ACL service, bootstrap wrapper 계층에서 가장 뚜렷하다.
- **AI 냄새**는 wrapper/alias/test-for-alias, no-op lifecycle, 과도한 orchestration duplication에서 강하게 보인다.

실행 우선순위를 잘 잡으면, 큰 구조 개편 없이도 먼저 장애 확률을 꽤 낮출 수 있다.
