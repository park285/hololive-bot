# hololive-bot 번들 전방위 코드 리뷰 (2026-04-15)

분석 대상: `/mnt/data/hololive_review`  
스캔 기준: 성능, I/O, 네트워크 계층, 상태 일관성, 코드 중복, 거대 객체, LLM/프롬프트 관리, 테스트 공백  
분석 방식: **정적 스캔 중심**. 이 번들 안의 코드/설정/테스트를 전부 훑고, 실제로 위험한 경로를 우선순위화했습니다.

## 먼저 분명히 해둘 점

이 저장소는 `go.work` 와 각 모듈 `go.mod` 가 모두 `go 1.26.2` / `toolchain go1.26.2` 를 요구합니다. 현재 컨테이너의 Go 툴체인과 맞지 않아서, 이번 리뷰는 **컴파일/테스트 실행 없이 정적 분석으로 진행**했습니다.

- `go.work`: `go 1.26.2`, `toolchain go1.26.2`
- `hololive-shared/go.mod`: `go 1.26.2`
- `hololive-kakao-bot-go/go.mod`: `go 1.26.2`
- `hololive-llm-sched/go.mod`: `go 1.26.2`
- `hololive-stream-ingester/go.mod`: `go 1.26.2`

이 한계는 솔직히 적습니다. 다만 아래 지적들은 “추측성 인상평”이 아니라, **실제 코드 흐름상 상태가 틀어질 수 있는 지점** 위주로 정리했습니다.

---

## 전체 스캔에서 가장 먼저 보인 그림

파일 수와 덩치만 봐도 병목과 유지보수 위험이 몰린 축이 꽤 선명합니다.

- 전체 파일 수: **1604**
- Go 파일 수: **994**
- TS 파일 수: **62**
- TSX 파일 수: **57**
- SQL 파일 수: **59**

Go 코드가 특히 많이 몰린 모듈은 아래입니다.

- `hololive/hololive-shared`: **444개**
- `hololive/hololive-kakao-bot-go`: **282개**
- `hololive/hololive-stream-ingester`: **123개**
- `hololive/hololive-llm-sched`: **97개**
- `shared-go`: **32개**
- `hololive/hololive-dispatcher-go`: **13개**

비테스트 기준으로 덩치가 가장 큰 파일들은 아래입니다.

- `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher_send.go` — **724 LOC**
- `hololive/hololive-shared/pkg/service/youtube/poller/repository_batch.go` — **703 LOC**
- `hololive/hololive-shared/pkg/service/member/repository.go` — **677 LOC**
- `hololive/hololive-shared/pkg/service/youtube/tracking/observation_compare.go` — **645 LOC**
- `hololive/hololive-shared/pkg/service/holodex/scraper.go` — **566 LOC**
- `hololive/hololive-llm-sched/internal/service/majorevent/repository.go` — **513 LOC**
- `hololive/hololive-shared/pkg/service/youtube/tracking/alarm_state_repository.go` — **497 LOC**
- `hololive/hololive-shared/pkg/service/youtube/poller/scheduler.go` — **491 LOC**
- `hololive/hololive-shared/pkg/service/alarm/queue/consumer.go` — **482 LOC**
- `hololive/hololive-kakao-bot-go/internal/service/notification/alarm_service.go` — **470 LOC**

패키지 단위로 보면 이쪽이 특히 위험합니다.

- `hololive/hololive-shared/pkg/service/youtube/outbox` — **14388 LOC / 53 files**
- `hololive/hololive-stream-ingester/internal/ops` — **9530 LOC / 46 files**
- `hololive/hololive-shared/pkg/service/youtube/poller` — **8093 LOC / 26 files**
- `hololive/hololive-stream-ingester/internal/runtime` — **6284 LOC / 40 files**
- `hololive/hololive-shared/pkg/service/youtube/scraper` — **5188 LOC / 33 files**
- `hololive/hololive-shared/pkg/service/youtube/tracking` — **5169 LOC / 17 files**
- `hololive/hololive-shared/pkg/service/holodex` — **4585 LOC / 23 files**
- `hololive/hololive-kakao-bot-go/internal/server` — **4007 LOC / 28 files**
- `hololive/hololive-kakao-bot-go/internal/app` — **4006 LOC / 55 files**

리뷰 결론을 한 줄로 줄이면 이렇습니다.

**이 저장소의 제일 큰 문제는 단순 성능이 아니라, 상태 일관성과 책임 분리 붕괴입니다.**  
I/O 자체는 생각보다 많이 제한돼 있고, 네트워크 요청도 대체로 context-aware 합니다. 대신 **“메모리/DB/Valkey/외부 전송”의 성공 조건이 서로 다른데 한 군데 성공한 것을 전체 성공으로 간주하는 코드**가 여럿 보입니다. 이게 진짜 위험합니다.

---

## 우선순위

### P0. 바로 패치해야 하는 것

1. `scheduler_alerts.go`: **방 여러 개 중 한 방만 성공해도 전체 알림을 sent 처리**
2. `acl/service.go`: **모드/메모리/DB/Valkey 동기화 순서가 잘못돼 있고, Add/Remove 는 현재 모드가 중간에 바뀌면 잘못된 Valkey 키를 건드릴 수 있음**
3. `notification/alarm_service.go` + `alarm_persistence.go`: **사용자 CRUD 성공 응답 이후 DB 영속화가 비동기라 재시작 시 유실 가능**
4. `twitch/client.go`: **401 반복 시 재귀 재호출로 실패 경로가 과도하게 증폭**
5. `summarizer.go`: **LLM 요약 캐시 키가 입력 이벤트를 반영하지 않아 잘못된 요약을 재사용**

### P1. 다음 스프린트에서 구조적으로 쪼개야 하는 것

6. `holodex/scraper.go`: 네트워크/매핑/캐시/폴백이 한 서비스에 섞임
7. `stream-ingester/internal/ops`: 보고서 수집/가공/Markdown 렌더링이 복붙형으로 증식
8. `cmd/*/main.go`: 거의 같은 부팅 코드가 5곳에 반복
9. `summarizer_prompt_assets.go`: 거대한 프롬프트 상수를 Go 코드에 직접 넣어 diff noise 유발

---

# 1) `hololive-shared/pkg/service/youtube/scheduler_alerts.go`

## 문제 요약

이 파일은 실제 버그입니다.

현재 구현은 **동일한 마일스톤/approaching 알림을 여러 방으로 보내는 동안, 단 한 방이라도 성공하면 그 알림 전체를 sent 처리**합니다.

문제 구간:

- `SendMilestoneAlerts()` 69~88
- `dispatchMilestoneAlertWorks()` 141~182
- `sendApproachingAlerts()` 91~105
- `dispatchApproachingAlertWorks()` 210~251

핵심은 이 부분입니다.

```go
successByWork := make([]atomic.Bool, len(works))
...
if err := sendMessage(room, work.message); err != nil {
    ...
    return nil
}
successByWork[i].Store(true)
...
if successByWork[i].Load() {
    sentNotifications = append(sentNotifications, work.notification)
}
```

이건 **“한 방이라도 성공”** 이라는 뜻입니다.  
그 뒤 `markMilestoneNotificationsSent()` / `markApproachingNotificationsSent()` 가 호출되므로, 실패한 방이 있어도 재시도 대상에서 빠질 수 있습니다.

즉:

- room A 성공
- room B 실패
- DB 상 noti sent = true
- room B 는 영원히 재시도 안 됨

이건 알림 시스템 기준으로 꽤 심각합니다.

## 왜 이게 특히 위험한가

이 경로는 “부분 성공”을 표현할 저장 모델이 없습니다.  
그러니 지금 설계에서 안전한 기본값은 둘 중 하나뿐입니다.

- **모든 방이 성공했을 때만 sent 처리**
- 또는 **room 단위 전송 상태를 따로 저장**

지금 저장 모델을 크게 바꾸지 않고 최소 수정으로 막으려면 첫 번째가 맞습니다.

## 최소 안전 패치

아래 diff는 “모든 대상 방 전송이 성공했을 때만 notified 처리”로 바꿉니다.  
부분 성공은 경고 로그를 남기고, DB 상태는 unsent 로 유지해 재시도되게 만듭니다.

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/scheduler_alerts.go b/hololive/hololive-shared/pkg/service/youtube/scheduler_alerts.go
index 1111111..2222222 100644
--- a/hololive/hololive-shared/pkg/service/youtube/scheduler_alerts.go
+++ b/hololive/hololive-shared/pkg/service/youtube/scheduler_alerts.go
@@
 type milestoneAlertWork struct {
 	notification ytstats.MilestoneNotification
 	message      string
 }

 type approachingAlertWork struct {
 	notification ytstats.ApproachingNotification
 	message      string
 }
+
+type milestoneDispatchResult struct {
+	notification ytstats.MilestoneNotification
+	targetRooms  int
+	successCount atomic.Int32
+	failureCount atomic.Int32
+}
+
+type approachingDispatchResult struct {
+	notification ytstats.ApproachingNotification
+	targetRooms  int
+	successCount atomic.Int32
+	failureCount atomic.Int32
+}
@@
 func (ys *schedulerImpl) dispatchMilestoneAlertWorks(
 	ctx context.Context,
 	sendMessage func(room, message string) error,
 	rooms []string,
 	works []milestoneAlertWork,
 ) []ytstats.MilestoneNotification {
 	if len(works) == 0 || len(rooms) == 0 {
 		return nil
 	}

-	successByWork := make([]atomic.Bool, len(works))
+	results := make([]milestoneDispatchResult, len(works))
+	for i := range works {
+		results[i] = milestoneDispatchResult{
+			notification: works[i].notification,
+			targetRooms:  len(rooms),
+		}
+	}
+
 	eg, _ := errgroup.WithContext(ctx)
 	eg.SetLimit(4)

 	for i, work := range works {
 		i := i
@@
 			room := room
 			eg.Go(func() error {
 				if err := sendMessage(room, work.message); err != nil {
 					ys.logger.Error("Failed to send milestone notification",
 						slog.String("room", room),
 						slog.String("member", work.notification.MemberName),
 						slog.Any("error", err))
+					results[i].failureCount.Add(1)
 					return nil
 				}
-				successByWork[i].Store(true)
+				results[i].successCount.Add(1)
 				return nil
 			})
 		}
 	}

 	_ = eg.Wait()
 	sentNotifications := make([]ytstats.MilestoneNotification, 0, len(works))
-	for i, work := range works {
-		if successByWork[i].Load() {
-			sentNotifications = append(sentNotifications, work.notification)
-		}
+	for i := range results {
+		successCount := int(results[i].successCount.Load())
+		failureCount := int(results[i].failureCount.Load())
+		if results[i].targetRooms > 0 &&
+			successCount == results[i].targetRooms &&
+			failureCount == 0 {
+			sentNotifications = append(sentNotifications, results[i].notification)
+			continue
+		}
+		if successCount > 0 {
+			ys.logger.Warn("Milestone notification partially sent; keeping unsent state for retry",
+				slog.String("member", results[i].notification.MemberName),
+				slog.Int("target_rooms", results[i].targetRooms),
+				slog.Int("success_count", successCount),
+				slog.Int("failure_count", failureCount))
+		}
 	}
 	return sentNotifications
 }
@@
 func (ys *schedulerImpl) dispatchApproachingAlertWorks(
 	ctx context.Context,
 	sendMessage func(room, message string) error,
 	rooms []string,
 	works []approachingAlertWork,
 ) []ytstats.ApproachingNotification {
 	if len(works) == 0 || len(rooms) == 0 {
 		return nil
 	}

-	successByWork := make([]atomic.Bool, len(works))
+	results := make([]approachingDispatchResult, len(works))
+	for i := range works {
+		results[i] = approachingDispatchResult{
+			notification: works[i].notification,
+			targetRooms:  len(rooms),
+		}
+	}
+
 	eg, _ := errgroup.WithContext(ctx)
 	eg.SetLimit(4)
@@
 			room := room
 			eg.Go(func() error {
 				if err := sendMessage(room, work.message); err != nil {
 					ys.logger.Error("Failed to send approaching notification",
 						slog.String("room", room),
 						slog.String("member", work.notification.MemberName),
 						slog.Any("error", err))
+					results[i].failureCount.Add(1)
 					return nil
 				}
-				successByWork[i].Store(true)
+				results[i].successCount.Add(1)
 				return nil
 			})
 		}
 	}

 	_ = eg.Wait()
 	sentNotifications := make([]ytstats.ApproachingNotification, 0, len(works))
-	for i, work := range works {
-		if successByWork[i].Load() {
-			sentNotifications = append(sentNotifications, work.notification)
-		}
+	for i := range results {
+		successCount := int(results[i].successCount.Load())
+		failureCount := int(results[i].failureCount.Load())
+		if results[i].targetRooms > 0 &&
+			successCount == results[i].targetRooms &&
+			failureCount == 0 {
+			sentNotifications = append(sentNotifications, results[i].notification)
+			continue
+		}
+		if successCount > 0 {
+			ys.logger.Warn("Approaching notification partially sent; keeping unsent state for retry",
+				slog.String("member", results[i].notification.MemberName),
+				slog.Int("target_rooms", results[i].targetRooms),
+				slog.Int("success_count", successCount),
+				slog.Int("failure_count", failureCount))
+		}
 	}
 	return sentNotifications
 }
```

## 이 패치 다음에 바로 추가해야 할 테스트

새 파일: `hololive/hololive-shared/pkg/service/youtube/scheduler_alerts_test.go`

반드시 넣을 테스트:

- `TestSendMilestoneAlerts_DoesNotMarkSentWhenAnyRoomFails`
- `TestSendMilestoneAlerts_MarksSentOnlyWhenAllRoomsSucceed`
- `TestSendApproachingAlerts_DoesNotMarkSentWhenAnyRoomFails`

테스트 검증 포인트는 단순합니다.

- rooms = `["room-a", "room-b"]`
- `sendMessage` 가 `room-b` 에서만 에러
- `MarkMilestonesNotifiedBatch` 또는 `MarkApproachingChatNotifiedBatch` 가 호출되면 **실패**

---

# 2) `hololive-kakao-bot-go/internal/service/acl/service.go`

이 서비스는 단일 버그가 아니라 **상태 일관성 문제가 여러 개 겹쳐 있는 곳**입니다.

## 확인된 문제

### 2-1. `AddRoom` / `RemoveRoom` 가 현재 모드를 캡처하지 않음

문제 구간:

- `AddRoom()` 409~449
- `RemoveRoom()` 451~492

현재 흐름은 이렇습니다.

1. lock 안에서 `targetRooms := s.activeRoomsMap()` 와 `lt := string(s.mode)` 를 가져옴
2. lock 풀고 DB 반영
3. **다시 현재 모드**를 읽어서 `valkeyKey := s.valkeyKeyForMode(s.currentMode())`

이 사이에 `SetMode()` 가 들어오면,  
DB 와 메모리는 whitelist 기준으로 변경됐는데 Valkey 는 blacklist 키에 반영될 수 있습니다.

이건 실제 경쟁 조건입니다.

### 2-2. `SetEnabled` / `SetMode` 가 메모리를 DB보다 먼저 바꿈

문제 구간:

- `SetEnabled()` 365~385
- `SetMode()` 387~407

둘 다 현재는:

1. 메모리 먼저 변경
2. DB 저장
3. Valkey 동기화

이 순서입니다. DB 저장이 실패하면 메모리 상태만 바뀐 채 남습니다.

즉:

- 메모리 = 새 값
- DB = 옛 값
- Valkey = 옛 값 혹은 미동기화

이건 서비스 내부 관점에서도 틀린 상태입니다.

### 2-3. 초기화 경로에서 DB `Create` 에러를 무시

문제 구간:

- `loadFromDatabase()` 179~181
- `loadFromDatabase()` 194~196
- `loadFromDatabase()` 218~221

초기 settings/rooms insert 에서 `s.db.Create(...)` 결과를 확인하지 않습니다.  
초기화가 성공한 척 지나가고, 다음 재시작에서 다시 초기화가 반복되거나 설정이 사라질 수 있습니다.

## 최소 안전 패치

### 2-A. 현재 모드를 캡처하는 helper 추가

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/service/acl/service.go b/hololive/hololive-kakao-bot-go/internal/service/acl/service.go
index 3333333..4444444 100644
--- a/hololive/hololive-kakao-bot-go/internal/service/acl/service.go
+++ b/hololive/hololive-kakao-bot-go/internal/service/acl/service.go
@@
 func (s *Service) activeRoomsMap() map[string]struct{} {
 	if s.mode == ACLModeBlacklist {
 		return s.blacklistRooms
 	}

 	return s.whitelistRooms
 }
+
+func (s *Service) roomsMapForMode(mode ACLMode) map[string]struct{} {
+	if mode == ACLModeBlacklist {
+		return s.blacklistRooms
+	}
+
+	return s.whitelistRooms
+}
```

### 2-B. 초기화 시 ignored DB error 제거

```diff
@@
 	if isFirstInit {
 		s.enabled = defaultEnabled
-		s.db.Create(&Settings{Key: dbKeyEnabled, Value: fmt.Sprintf("%t", defaultEnabled)})
+		if err := s.db.Create(&Settings{Key: dbKeyEnabled, Value: fmt.Sprintf("%t", defaultEnabled)}).Error; err != nil {
+			return fmt.Errorf("failed to initialize ACL enabled setting: %w", err)
+		}
 	} else if result.Error != nil {
 		return fmt.Errorf("failed to load ACL enabled setting: %w", result.Error)
 	} else {
 		s.enabled = settings.Value == "true"
 	}
@@
 	if modeFirstInit {
 		s.mode = defaultMode
-		s.db.Create(&Settings{Key: dbKeyMode, Value: string(defaultMode)})
+		if err := s.db.Create(&Settings{Key: dbKeyMode, Value: string(defaultMode)}).Error; err != nil {
+			return fmt.Errorf("failed to initialize ACL mode setting: %w", err)
+		}
 	} else if modeResult.Error != nil {
 		return fmt.Errorf("failed to load ACL mode setting: %w", result.Error)
 	} else {
 		s.mode = ParseACLMode(modeSetting.Value)
 	}
@@
 		for _, r := range defaultRooms {
 			targetRooms[r] = struct{}{}
-			s.db.Create(&Room{RoomID: r, ListType: lt})
+			if err := s.db.Create(&Room{RoomID: r, ListType: lt}).Error; err != nil {
+				s.mu.Unlock()
+				return fmt.Errorf("failed to initialize ACL room %q: %w", r, err)
+			}
 		}
 	} else {
```

### 2-C. `SetEnabled` / `SetMode` 를 DB-first 로 변경

```diff
@@
 func (s *Service) SetEnabled(ctx context.Context, enabled bool) error {
-	s.mu.Lock()
-	s.enabled = enabled
-	s.mu.Unlock()
-
 	result := s.db.Where("key = ?", dbKeyEnabled).Assign(Settings{Value: fmt.Sprintf("%t", enabled)}).FirstOrCreate(&Settings{Key: dbKeyEnabled})
 	if result.Error != nil {
 		return fmt.Errorf("failed to save ACL enabled setting: %w", result.Error)
 	}
+
+	s.mu.Lock()
+	s.enabled = enabled
+	s.mu.Unlock()

 	if err := s.syncSettingsToValkey(ctx); err != nil {
 		return fmt.Errorf("sync acl settings to cache: %w", err)
 	}
@@
 func (s *Service) SetMode(ctx context.Context, mode ACLMode) error {
-	s.mu.Lock()
-	s.mode = mode
-	s.mu.Unlock()
-
 	result := s.db.Where("key = ?", dbKeyMode).Assign(Settings{Value: string(mode)}).FirstOrCreate(&Settings{Key: dbKeyMode})
 	if result.Error != nil {
 		return fmt.Errorf("failed to save ACL mode setting: %w", result.Error)
 	}
+
+	s.mu.Lock()
+	s.mode = mode
+	s.mu.Unlock()

 	if err := s.syncModeToValkey(ctx); err != nil {
 		return fmt.Errorf("sync acl mode to cache: %w", err)
 	}
```

### 2-D. `AddRoom` / `RemoveRoom` 는 캡처한 모드 기준으로 rollback / Valkey full-sync

여기서 핵심은 incremental `SAdd/SRem` 대신 **이미 있는 `syncRoomsToValkey(ctx, mode)`** 를 쓰는 겁니다.  
이 코드베이스는 `service_cache_sync.go` 에 이미 원자적 전체 교체 루틴이 있는데, 정작 mutation path 에서는 그걸 안 씁니다.

```diff
@@
 func (s *Service) AddRoom(ctx context.Context, room string) (bool, error) {
 	room = stringutil.TrimSpace(room)
 	if room == "" {
 		return false, nil
 	}

 	s.mu.Lock()
-	targetRooms := s.activeRoomsMap()
-	lt := string(s.mode)
+	mode := s.mode
+	targetRooms := s.roomsMapForMode(mode)
+	lt := string(mode)

 	if _, exists := targetRooms[room]; exists {
 		s.mu.Unlock()
 		return false, nil // 이미 존재
 	}
@@
 	result := s.db.Create(&Room{RoomID: room, ListType: lt})
 	if result.Error != nil {
 		s.mu.Lock()
-		delete(s.activeRoomsMap(), room)
+		delete(s.roomsMapForMode(mode), room)
 		s.mu.Unlock()

 		return false, fmt.Errorf("failed to add room to database: %w", result.Error)
 	}

-	valkeyKey := s.valkeyKeyForMode(s.currentMode())
-
-	if _, err := s.cache.SAdd(ctx, valkeyKey, []string{room}); err != nil {
-		return false, fmt.Errorf("sync acl room add to cache: %w", err)
+	if err := s.syncRoomsToValkey(ctx, mode); err != nil {
+		return false, fmt.Errorf("sync acl room add to cache (%s): %w", mode, err)
 	}
@@
 func (s *Service) RemoveRoom(ctx context.Context, room string) (bool, error) {
 	room = stringutil.TrimSpace(room)
 	if room == "" {
 		return false, nil
 	}

 	s.mu.Lock()
-	targetRooms := s.activeRoomsMap()
-	lt := string(s.mode)
+	mode := s.mode
+	targetRooms := s.roomsMapForMode(mode)
+	lt := string(mode)

 	if _, exists := targetRooms[room]; !exists {
 		s.mu.Unlock()
 		return false, nil // 존재하지 않음
 	}
@@
 	result := s.db.Where("room_id = ? AND list_type = ?", room, lt).Delete(&Room{})
 	if result.Error != nil {
 		s.mu.Lock()
-		s.activeRoomsMap()[room] = struct{}{}
+		s.roomsMapForMode(mode)[room] = struct{}{}
 		s.mu.Unlock()

 		return false, fmt.Errorf("failed to remove room from database: %w", result.Error)
 	}

-	valkeyKey := s.valkeyKeyForMode(s.currentMode())
-
-	if _, err := s.cache.SRem(ctx, valkeyKey, []string{room}); err != nil {
-		return false, fmt.Errorf("sync acl room removal to cache: %w", err)
+	if err := s.syncRoomsToValkey(ctx, mode); err != nil {
+		return false, fmt.Errorf("sync acl room removal to cache (%s): %w", mode, err)
 	}
```

## 이 ACL 패치의 의미

이 패치는 세 가지를 동시에 막습니다.

- DB 실패 시 메모리만 바뀌는 문제
- 모드가 바뀌는 타이밍에 잘못된 Valkey 키를 갱신하는 문제
- mutation path 에서만 원자적 full-sync 를 쓰지 않아 cache drift 가 생기는 문제

## 추가 테스트

기존 `service_test.go` / `service_db_test.go` 에 아래를 추가해야 합니다.

- `TestACLService_SetEnabled_DoesNotMutateMemoryOnDBFailure`
- `TestACLService_SetMode_DoesNotMutateMemoryOnDBFailure`
- `TestACLService_AddRoom_UsesCapturedModeForValkeySync`
- `TestACLService_RemoveRoom_UsesCapturedModeForValkeySync`
- `TestACLService_LoadFromDatabase_ReturnsInitCreateError`

---

# 3) `hololive-kakao-bot-go/internal/service/notification/alarm_service.go` + `alarm_persistence.go`

이 부분은 “느낌상 별로”가 아니라, **설계상 durability가 깨져 있습니다.**

## 현재 구조

`AddAlarm()`:

1. Valkey `SAdd` 로 room alarm set 갱신
2. registry / type subscriber / member/user/room name cache 갱신
3. **그 다음에** `persistAlarmAsync()` 로 DB 영속화
4. DB 실패는 로그만 남기고 사용자에게는 성공처럼 보임

`RemoveAlarm()` 과 `ClearRoomAlarms()` 도 본질적으로 같습니다.

문제 구간:

- `alarm_service.go`: `AddAlarm()` 179~280
- `alarm_service.go`: `RemoveAlarm()` 282~349
- `alarm_service.go`: `ClearRoomAlarms()` 447~513
- `alarm_persistence.go`: `persistAlarmAsync()` 71~88
- `alarm_persistence.go`: `removeAlarmAsync()` 90~107
- `alarm_persistence.go`: `clearRoomAlarmsAsync()` 109~124

## 왜 이게 위험한가

실패 시나리오가 너무 쉽습니다.

- 사용자가 알람 추가
- Valkey 갱신 성공
- API는 성공 응답
- 비동기 DB 저장 실패
- 프로세스 재시작
- `WarmCacheFromDB()` 는 DB만 신뢰하므로 방금 알람이 사라짐

즉, **현재 구조는 “즉시 보이는 성공”과 “재시작 후 남는 성공”이 다릅니다.**

사용자 CRUD는 핫패스가 아닙니다.  
여기는 지연 최적화보다 **내구성** 이 우선입니다.

## 바로 적용할 패치 방향

여기는 diff 한 덩어리보다 **수정 순서**를 정확히 따르는 게 중요합니다.

### 3-A. DB를 authoritative source 로 바꾸고, CRUD는 동기 영속화로 전환

`alarm_persistence.go` 에 아래 동기 helper 를 추가합니다.

```go
func (as *AlarmService) persistAlarm(ctx context.Context, alarm *domain.Alarm) error {
	if as.alarmWriter == nil || alarm == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	persistCtx, cancel := context.WithTimeout(ctx, alarmPersistTaskTimeout)
	defer cancel()

	if err := as.alarmWriter.Add(persistCtx, alarm); err != nil {
		return fmt.Errorf("persist alarm: %w", err)
	}
	return nil
}

func (as *AlarmService) deleteAlarm(ctx context.Context, roomID, channelID string) error {
	if as.alarmWriter == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	persistCtx, cancel := context.WithTimeout(ctx, alarmPersistTaskTimeout)
	defer cancel()

	if err := as.alarmWriter.Remove(persistCtx, roomID, channelID); err != nil {
		return fmt.Errorf("delete alarm: %w", err)
	}
	return nil
}

func (as *AlarmService) deleteRoomAlarms(ctx context.Context, roomID string) error {
	if as.alarmWriter == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	persistCtx, cancel := context.WithTimeout(ctx, alarmPersistTaskTimeout)
	defer cancel()

	if _, err := as.alarmWriter.ClearByRoom(persistCtx, roomID); err != nil {
		return fmt.Errorf("delete room alarms: %w", err)
	}
	return nil
}
```

### 3-B. `AddAlarm()` 은 **DB 먼저**, 그 다음 cache

기존 `as.persistAlarmAsync(...)` 호출은 삭제하고, 아래 순서로 바꿉니다.

1. `alarmRecord := &domain.Alarm{...}` 생성
2. `if err := as.persistAlarm(ctx, alarmRecord); err != nil { return false, err }`
3. 그 다음 기존 cache mutation 수행
4. cache mutation 중 일부가 실패하면 `rebuildSubscriberCacheFromRepository(...)` 로 전면 복구

핵심 편집 포인트는 이겁니다.

```go
alarmRecord := &domain.Alarm{
	RoomID:     roomID,
	UserID:     req.UserID,
	ChannelID:  channelID,
	MemberName: memberName,
	RoomName:   roomName,
	UserName:   userName,
	AlarmTypes: alarmTypes,
}

if err := as.persistAlarm(ctx, alarmRecord); err != nil {
	opErr = fmt.Errorf("persist alarm before cache write: %w", err)
	as.logger.Error("Failed to persist alarm before cache write",
		slog.Any("error", opErr),
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
	)
	return false, opErr
}
```

### 3-C. `RemoveAlarm()` / `ClearRoomAlarms()` 도 같은 원칙

- `RemoveAlarm()` 은 `deleteAlarm(ctx, roomID, channelID)` 를 먼저 실행
- `ClearRoomAlarms()` 는 가능하면 **cache 대신 repository 기준으로 channel list를 가져오고**, cache cleanup 은 그 결과를 기준으로 처리
- cache cleanup 실패 시 `RebuildSubscriberCacheFromRepository()` 실행

## 이 경로에서 반드시 같이 바꿔야 하는 것

현재 `persistExecutor`, `submitPersistTask`, `stripedExecutor` 는 CRUD write-through 경로에서 사실상 필요 없어집니다.  
바로 삭제해도 되지만, 충격을 줄이려면 2단계로 가는 편이 안전합니다.

### 단계 1
- CRUD 경로는 동기 영속화로 전환
- 기존 async 함수와 executor 는 남겨두되 더 이상 CRUD 에서 사용하지 않음

### 단계 2
- `persistExecutor` / `submitPersistTask()` / 관련 ordering test 정리
- Close 경로 단순화

## 이걸 꼭 바꿔야 하는 이유

이 코드는 “빠르다”가 아니라 “성공한 척한다” 쪽입니다.  
알람 추가/삭제는 초당 수천 건 처리하는 핫패스가 아니니, 여기서는 1~2개의 DB round trip 보다 **재시작 후 상태 보존**이 훨씬 중요합니다.

## 같이 추가할 테스트

- `TestAddAlarm_PersistFailureDoesNotPolluteCache`
- `TestRemoveAlarm_PersistFailureDoesNotDeleteCache`
- `TestClearRoomAlarms_UsesRepositoryAsAuthorityWhenConfigured`
- `TestAddAlarm_PartialCacheFailure_RebuildsFromRepository`

---

# 4) `hololive-kakao-bot-go/internal/service/twitch/client.go`

## 문제 요약

`GetStreams()` 에서 401 이 오면 토큰을 refresh 한 뒤 **자기 자신을 재귀 호출**합니다.

```go
if resp.StatusCode == http.StatusUnauthorized {
    c.invalidateToken()

    if refreshErr := c.refreshToken(ctx); refreshErr != nil {
        return nil, fmt.Errorf("refresh token after 401: %w", refreshErr)
    }

    return c.GetStreams(ctx, userLogins)
}
```

이건 “한 번만 더 시도”처럼 보이지만, 실제로는 401 이 계속 나오는 환경에서 계속 이어질 수 있습니다.

예를 들면:

- 잘못된 app credentials
- Twitch 쪽 앱 상태 문제
- 요청 헤더 형식 mismatch
- refresh 는 성공하지만 API 호출 자격은 계속 401

그때는 **stack depth + circuit breaker semantics + request time budget** 이 전부 꼬입니다.

## 같이 보이는 네트워크 계층 문제

`NewClient()` 는 공용 `httputil.NewExternalAPIClient()` 를 안 쓰고 bare `http.Client{Timeout: ...}` 만 씁니다.  
이 저장소에는 이미 `shared-go/pkg/httputil/client.go` 가 있고, 다른 서비스는 그걸 쓰는 곳도 있습니다.  
지금 Twitch 는 transport policy 가 분기된 상태입니다.

## 최소 안전 패치

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/service/twitch/client.go b/hololive/hololive-kakao-bot-go/internal/service/twitch/client.go
index 5555555..6666666 100644
--- a/hololive/hololive-kakao-bot-go/internal/service/twitch/client.go
+++ b/hololive/hololive-kakao-bot-go/internal/service/twitch/client.go
@@
 import (
 	"context"
 	"errors"
 	"fmt"
 	"log/slog"
 	"net/http"
 	"net/url"
 	"strings"
 	"sync"
 	"sync/atomic"
 	"time"

 	"github.com/kapu/hololive-shared/pkg/constants"
+	"github.com/park285/llm-kakao-bots/shared-go/pkg/httputil"
 	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
 	"github.com/park285/llm-kakao-bots/shared-go/pkg/jsonutil"
@@
 type ClientConfig struct {
+	HTTPClient   *http.Client
 	ClientID     string
 	ClientSecret string
 }
@@
 func NewClient(cfg ClientConfig, logger *slog.Logger) *Client {
+	if logger == nil {
+		logger = slog.Default()
+	}
+
+	httpClient := cfg.HTTPClient
+	if httpClient == nil {
+		httpClient = httputil.NewExternalAPIClient(constants.TwitchConfig.Timeout)
+	}
+
 	c := &Client{
-		httpClient: &http.Client{
-			Timeout: constants.TwitchConfig.Timeout,
-		},
+		httpClient:   httpClient,
 		clientID:     cfg.ClientID,
 		clientSecret: cfg.ClientSecret,
-		logger:       logger,
+		logger:       logger,
 	}
 	c.tokenExpiry.Store(time.Time{})
 	c.circuitOpenedAt.Store(time.Time{})

 	return c
 }

 func (c *Client) IsConfigured() bool {
 	return c != nil && c.clientID != "" && c.clientSecret != ""
 }

 func (c *Client) GetStreams(ctx context.Context, userLogins []string) (*StreamsResponse, error) {
+	return c.getStreams(ctx, userLogins, true)
+}
+
+func (c *Client) getStreams(ctx context.Context, userLogins []string, allowRefreshRetry bool) (*StreamsResponse, error) {
 	if !c.IsConfigured() {
 		return nil, errors.New("twitch client not configured")
 	}
@@
 	if resp.StatusCode == http.StatusUnauthorized {
+		c.recordFailure()
 		c.invalidateToken()

+		if !allowRefreshRetry {
+			return nil, &apperrors.APIError{
+				Operation:  "twitch_get_streams",
+				StatusCode: http.StatusUnauthorized,
+				Err:        errors.New("unauthorized after token refresh"),
+			}
+		}
+
 		if refreshErr := c.refreshToken(ctx); refreshErr != nil {
 			return nil, fmt.Errorf("refresh token after 401: %w", refreshErr)
 		}

-		return c.GetStreams(ctx, userLogins)
+		return c.getStreams(ctx, userLogins, false)
 	}
@@
 	c.recordSuccess()

 	return &result, nil
 }
```

## 추가 테스트

기존 `client_test.go` 는 circuit breaker 정도만 봅니다. 여기에 아래를 추가해야 합니다.

- `TestClient_GetStreams_Retries401OnlyOnce`
- `TestClient_GetStreams_UsesProvidedHTTPClient`
- `TestClient_GetStreams_UsesProfiledClientWhenHTTPClientNil`

---

# 5) `hololive-llm-sched/internal/service/majorevent/summarizer/summarizer.go`

## 문제 요약

현재 cache key:

```go
cacheKey := fmt.Sprintf("majorevent:summary:%s:%s:%s", promptVersion, summaryType, periodKey)
```

입력 이벤트 목록이 바뀌어도, 같은 `promptVersion + summaryType + periodKey` 면 같은 키를 씁니다.

이건 조용히 잘못된 요약을 재사용할 수 있습니다.

예:

- 2026-04 주간 요약을 오전 9시에 생성
- 오전 11시에 같은 주간에 새 이벤트 2개 추가
- 다시 요약 요청
- 캐시는 24시간 TTL 이라 오전 9시 결과를 그대로 돌려줌

LLM 캐시는 원문 입력이 key 에 포함돼야 안전합니다.

## 최소 안전 패치

### 5-A. `summarizer.go` 수정

```diff
diff --git a/hololive/hololive-llm-sched/internal/service/majorevent/summarizer/summarizer.go b/hololive/hololive-llm-sched/internal/service/majorevent/summarizer/summarizer.go
index 7777777..8888888 100644
--- a/hololive/hololive-llm-sched/internal/service/majorevent/summarizer/summarizer.go
+++ b/hololive/hololive-llm-sched/internal/service/majorevent/summarizer/summarizer.go
@@
-	cacheKey := fmt.Sprintf("majorevent:summary:%s:%s:%s", promptVersion, summaryType, periodKey)
+	cacheKey := buildSummaryCacheKey(events, summaryType, periodKey)
```

### 5-B. 새 파일 추가: `summarizer_cache_key.go`

```diff
diff --git a/hololive/hololive-llm-sched/internal/service/majorevent/summarizer/summarizer_cache_key.go b/hololive/hololive-llm-sched/internal/service/majorevent/summarizer/summarizer_cache_key.go
new file mode 100644
--- /dev/null
+++ b/hololive/hololive-llm-sched/internal/service/majorevent/summarizer/summarizer_cache_key.go
@@
+package summarizer
+
+import (
+	"crypto/sha256"
+	"encoding/hex"
+	"fmt"
+	"sort"
+
+	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
+
+	"github.com/kapu/hololive-shared/pkg/domain"
+)
+
+type summaryCacheEvent struct {
+	Title     string `json:"title"`
+	DateStr   string `json:"date"`
+	Members   string `json:"members,omitempty"`
+	EventType string `json:"type"`
+	Link      string `json:"link"`
+}
+
+func buildSummaryCacheKey(events []domain.MajorEvent, summaryType SummaryType, periodKey string) string {
+	return fmt.Sprintf(
+		"majorevent:summary:%s:%s:%s:%s",
+		promptVersion,
+		summaryType,
+		periodKey,
+		buildSummaryInputHash(events),
+	)
+}
+
+func buildSummaryInputHash(events []domain.MajorEvent) string {
+	if len(events) == 0 {
+		return "empty"
+	}
+
+	projected := make([]summaryCacheEvent, 0, len(events))
+	for i := range events {
+		e := &events[i]
+		projected = append(projected, summaryCacheEvent{
+			Title:     e.Title,
+			DateStr:   formatEventDateForPrompt(e.EventStartDate, e.EventEndDate),
+			Members:   joinMembers(e.Members),
+			EventType: string(e.Type),
+			Link:      e.Link,
+		})
+	}
+
+	sort.Slice(projected, func(i, j int) bool {
+		if projected[i].DateStr != projected[j].DateStr {
+			return projected[i].DateStr < projected[j].DateStr
+		}
+		if projected[i].Title != projected[j].Title {
+			return projected[i].Title < projected[j].Title
+		}
+		return projected[i].Link < projected[j].Link
+	})
+
+	payload, _ := json.Marshal(projected)
+	sum := sha256.Sum256(payload)
+	return hex.EncodeToString(sum[:8])
+}
```

## 추가 테스트

기존 `summarizer_test.go` 에 반드시 추가:

- `TestBuildSummaryCacheKey_ChangesWhenEventsChange`
- `TestBuildSummaryCacheKey_IsOrderInsensitive`
- 기존 `TestSummarize_CacheKeyContainsPromptVersion` 는 그대로 유지

## 이 패치가 중요한 이유

이건 LLM 쪽에서 흔한 냄새입니다.  
“프롬프트 버전만 올리면 캐시 무효화는 된다”라는 식으로 가다 보면, **입력 데이터 드리프트**를 못 잡습니다.  
현 상태는 딱 그 단계입니다.

---

# 6) 네트워크 계층 일관성: `shared-go/pkg/httputil/client.go` 는 있는데, 일부 클라이언트는 안 씀

이 저장소는 네트워크 정책이 완전히 나쁘진 않습니다.  
오히려 좋은 부분도 있습니다.

- 대부분 `http.NewRequestWithContext(...)` 사용
- 대부분 응답 body 는 `jsonutil.ReadAllLimit(...)` 로 제한
- `io.ReadAll` 남용은 거의 없음

문제는 **정책이 통일되지 않았다는 점**입니다.

## 확인한 split-brain

- 공용 transport helper 존재: `shared-go/pkg/httputil/client.go`
- 그런데 아래는 bare `http.Client{Timeout: ...}` 직접 생성
  - `hololive-shared/pkg/service/holodex/scraper.go`
  - `hololive-kakao-bot-go/internal/service/twitch/client.go`
- 반면 다른 서비스는 이미 공용 helper 나 injected client 를 사용

이러면 나중에 다음 정책 변경이 아주 비싸집니다.

- dial timeout 조정
- keep-alive pool 조정
- HTTP/2 off
- trace round-tripper 삽입
- 프록시 정책 통합

## 최소 패치

### 6-A. `holodex/scraper.go`

```diff
diff --git a/hololive/hololive-shared/pkg/service/holodex/scraper.go b/hololive/hololive-shared/pkg/service/holodex/scraper.go
index 9999999..aaaaaaa 100644
--- a/hololive/hololive-shared/pkg/service/holodex/scraper.go
+++ b/hololive/hololive-shared/pkg/service/holodex/scraper.go
@@
 import (
 	"context"
 	"errors"
 	"fmt"
 	"log/slog"
 	"net/http"
 	"strings"
 	"sync"
 	"time"

 	"github.com/PuerkitoBio/goquery"
+	"github.com/park285/llm-kakao-bots/shared-go/pkg/httputil"
 	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
 	"golang.org/x/sync/singleflight"
@@
 func NewScraperService(
 	cacheSvc cache.Client,
 	membersData domain.MemberDataProvider,
 	youtubeProxyConfig scraper.ProxyConfig,
 	sharedRL *scraper.RateLimiter,
 	logger *slog.Logger,
 ) *ScraperService {
+	if logger == nil {
+		logger = slog.Default()
+	}
+
 	nameMap := make(map[string]string)
@@
 	return &ScraperService{
-		httpClient: &http.Client{
-			Timeout: constants.OfficialScheduleConfig.Timeout,
-		},
+		httpClient:     httputil.NewExternalAPIClient(constants.OfficialScheduleConfig.Timeout),
 		cache:          cacheSvc,
 		membersData:    membersData,
 		memberNameMap:  nameMap,
 		logger:         logger,
 		baseURL:        constants.OfficialScheduleConfig.BaseURL,
```

### 6-B. Twitch 는 4번 패치에 포함

Twitch 쪽은 이미 위 4번 diff에 `httputil.NewExternalAPIClient(...)` 적용을 같이 넣었습니다.

---

# 7) `hololive-stream-ingester/internal/ops` 는 패키지 단위 거대 객체입니다

이 패키지는 **9530 LOC / 46 files** 입니다.  
여기서 눈에 띈 건 단순히 크기만이 아닙니다.

## 보이는 패턴

아래 렌더러들은 문자열 빌더 기반 Markdown 생성이 거의 손으로 복붙된 형태로 반복됩니다.

- `community_shorts_alarm_sent_history_dataset_render.go`
- `community_shorts_latency_cause_render.go`
- `community_shorts_continuous_observation_render.go`
- `community_shorts_send_counts_render.go`

대표적으로 `builder.WriteString(...)` 횟수만 세어도:

- `community_shorts_alarm_sent_history_dataset_render.go` — **196회**
- `community_shorts_latency_cause_render.go` — **125회**
- `community_shorts_continuous_observation_render.go` — **119회**
- `community_shorts_send_counts_render.go` — **77회**

가장 긴 비테스트 함수도 이 패키지에서 나옵니다.

- `RenderCommunityShortsAlarmSentHistoryDatasetMarkdown()` — **284 LOC**
- `RenderCommunityShortsLatencyCauseMarkdown()` — **169 LOC**

이건 전형적인 “AI가 복붙으로 생산하기 쉬운 구조” 냄새가 있습니다.  
문제가 AI 사용 자체가 아니라, **공통 규약 없이 파일이 늘어나고 diff surface 가 커지는 상태**라는 점입니다.

## 왜 유지보수가 급격히 나빠지는가

- 표 머리글이나 escape 규칙을 바꾸려면 여러 파일을 동시에 수정해야 함
- 같은 개념의 summary/header/table/footer 가 파일마다 조금씩 다름
- 렌더링 버그를 테스트로 막기 어려워짐
- “하나의 보고서 파일”이 수집/가공/검증/출력까지 다 품게 됨

## 바로 나눌 수 있는 최소 단위

새 파일: `hololive/hololive-stream-ingester/internal/ops/report_markdown.go`

```go
package ops

import (
	"fmt"
	"strings"
)

func writeMarkdownTitle(b *strings.Builder, title string) {
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
}

func writeMarkdownKV(b *strings.Builder, key, value string) {
	b.WriteString("- ")
	b.WriteString(key)
	b.WriteString(": `")
	b.WriteString(value)
	b.WriteString("`\n")
}

func writeMarkdownTable(b *strings.Builder, headers []string, rows [][]string) {
	if len(headers) == 0 {
		return
	}

	b.WriteString("\n| ")
	b.WriteString(strings.Join(headers, " | "))
	b.WriteString(" |\n| ")
	for i := range headers {
		if i > 0 {
			b.WriteString(" | ")
		}
		b.WriteString("---")
	}
	b.WriteString(" |\n")

	for _, row := range rows {
		b.WriteString("| ")
		b.WriteString(strings.Join(row, " | "))
		b.WriteString(" |\n")
	}
}

func markdownCode(v string) string {
	return "`" + strings.ReplaceAll(v, "`", "\\`") + "`"
}

func markdownInt64(v int64) string {
	return fmt.Sprintf("%d", v)
}
```

그 다음 첫 번째 단계로 아래 두 파일부터 바꾸는 게 좋습니다.

- `community_shorts_send_counts_render.go`
- `community_shorts_latency_cause_render.go`

### 실제 리팩토링 순서

1. 렌더 함수 안에서 직접 `builder.WriteString("| ...")` 하는 부분을 전부 제거
2. 먼저 DTO → `[][]string` row builder 로 분리
3. 마지막에 `writeMarkdownTable(...)` 호출
4. header / summary 도 `writeMarkdownKV(...)` 로 통일
5. golden test 추가

이 작업은 성능보다 **수정 비용**을 크게 줄입니다.  
그리고 이 패키지는 “운영 보고서 생성기”라서, 지금 가장 무서운 건 CPU 3ms보다 **표 컬럼 하나 수정할 때 4개 파일이 조금씩 깨지는 것**입니다.

---

# 8) `cmd/*/main.go` 5개가 거의 같은 코드입니다

실제로 유사도만 봐도:

- `stream-ingester/main.go` ↔ `youtube-scraper/main.go`: **97.7%**
- `llm-scheduler/main.go` ↔ stream-ingester 쪽: **95%대**
- `dispatcher/main.go` ↔ 위 둘: **94%대**

공통 구조가 거의 똑같습니다.

- `automaxprocs.Init(nil)`
- `health.Init(Version)`
- `exitCode` + `defer os.Exit(...)`
- config load
- file logger init
- startup log
- `context.WithTimeout(context.Background(), 1*time.Minute)`
- build runtime
- `defer runtime.Close()`
- `runtime.Run()`

## 문제

이건 지금은 그냥 보기 불편한 수준이 아니라, **부팅 정책 변경 시 5곳을 같이 고쳐야 하는 구조**입니다.

예를 들어 다음 변경이 생기면 전부 중복 패치가 납니다.

- build timeout 변경
- startup log 공통 attrs 추가
- panic/recover
- tracing bootstrap
- runtime close error handling

## 권장 추출안

새 패키지: `shared-go/pkg/runtime/bootstrap`

```go
package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/kapu/hololive-shared/pkg/health"
	sharedlogging "github.com/park285/llm-kakao-bots/shared-go/pkg/logging"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/automaxprocs"
)

type Runtime interface {
	Run()
	Close() error
}

type Options[C any, R Runtime] struct {
	Version      string
	BuildTimeout time.Duration
	LogFileName  string

	LoadConfig   func() (C, error)
	LoggingOf    func(C) (sharedlogging.Config, string)
	BuildRuntime func(context.Context, C, *slog.Logger) (R, error)
	StartupLog   func(*slog.Logger, C)
}

func Run[C any, R Runtime](opts Options[C, R]) {
	automaxprocs.Init(nil)
	health.Init(opts.Version)

	var exitCode int
	defer func() { os.Exit(exitCode) }()

	cfg, err := opts.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		exitCode = 1
		return
	}

	logCfg, level := opts.LoggingOf(cfg)
	logger, err := sharedlogging.EnableFileLoggingWithLevel(logCfg, opts.LogFileName, level)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		exitCode = 1
		return
	}

	if opts.StartupLog != nil {
		opts.StartupLog(logger, cfg)
	}

	buildCtx, cancel := context.WithTimeout(context.Background(), opts.BuildTimeout)
	runtime, err := opts.BuildRuntime(buildCtx, cfg, logger)
	cancel()
	if err != nil {
		logger.Error("Failed to build runtime", slog.Any("error", err))
		exitCode = 1
		return
	}
	defer runtime.Close()

	runtime.Run()
}
```

각 main 은 15~20줄 수준으로 줄어듭니다.

이건 P1 입니다. 당장 버그는 아니지만, 현재 중복률이 너무 높아서 **다음 변경 때마다 실수 유발점**이 됩니다.

---

# 9) `summarizer_prompt_assets.go` 는 AI 냄새가 강하고, 유지보수 구조도 좋지 않습니다

문제 파일:

- `hololive-llm-sched/internal/service/majorevent/summarizer/summarizer_prompt_assets.go`

여기는 거대한 raw string 프롬프트가 Go 코드 안에 박혀 있습니다.

- `promptVersion = "v3"`
- `domainContextPart1`
- `domainContextPart2`
- 기타 prompt 조립용 상수들

## 왜 이게 나쁜가

### 9-1. 프롬프트 변경과 코드 변경이 같은 diff 에 섞임

프롬프트 문장 하나 고치려고 Go 파일을 건드리면, 리뷰어가 **로직 변경인지 문구 변경인지** 분리해서 보기 어려워집니다.

### 9-2. `promptVersion` 이 수동 관리

현재는 `v3` 를 사람이 기억해서 올려야 합니다.  
이건 언젠가 반드시 빠집니다. 이미 cache key 는 promptVersion 을 쓰고 있으니, 버전 bump 누락 = 잘못된 캐시 재사용입니다.

### 9-3. 테스트 포인트가 이상해짐

지금은 prompt 자체를 golden asset 으로 보기보다, Go constant 결과 문자열로만 검증하게 됩니다.  
결국 diff noise 가 커지고, 작은 문구 변경도 코드 리뷰가 무거워집니다.

## 권장 구조

```text
internal/service/majorevent/summarizer/
  prompts/
    system_domain_context_1.tmpl
    system_domain_context_2.tmpl
    response_schema.tmpl
    examples/
      weekly.json
      monthly.json
  graduated_members.json
  prompt_assets.go
  prompt_render.go
```

`prompt_assets.go` 에서는 `embed.FS` 로만 읽고,  
`promptVersion` 은 아래 둘 중 하나로 바꿔야 합니다.

### 방법 A. 완전 자동
임베드된 템플릿 바이트 전체의 SHA-256 앞 8자리

### 방법 B. 반자동
`promptVersionManual = "v3"` + 테스트에서 asset digest 가 바뀌면 실패

지금 단계에서는 B 도 충분히 낫습니다.

---

# 10) `hololive-shared/pkg/service/holodex/scraper.go` 는 서비스 단위 거대 객체입니다

현재 `ScraperService` 가 섞고 있는 책임:

- member alias 매핑 구축
- cache 조회/저장
- YouTube HTML scraper 사용
- official schedule fallback
- DOM parsing
- stream mapping
- singleflight + page cache

이걸 한 서비스가 다 들고 있습니다.

## 당장 쪼개야 할 파일 단위

- `member_matcher.go`
- `official_schedule_fetcher.go`
- `youtube_fallback_client.go`
- `stream_mapper.go`
- `scraper_service.go` (오케스트레이션만)

## 지금 구조가 왜 위험한가

- fallback 정책 바꾸려면 파싱/매핑/캐시와 한 파일에서 씨름해야 함
- 네트워크 transport 정책 변경이 constructor/field/사용처에 흩어짐
- member alias matching 버그와 schedule fetch 버그가 같은 파일 diff 로 섞임

이 파일은 LOC 자체도 크지만, 더 큰 문제는 **책임이 서로 다른 층을 동시에 품고 있다는 점**입니다.

---

# 11) 이번 스캔에서 “덜 걱정해도 되는 것”

비판만 하면 오히려 초점이 흐려져서, 덜 위험한 것도 같이 적습니다.

## I/O 남용은 생각보다 심하지 않음

비테스트 코드에서 raw `io.ReadAll` 사용은 매우 적고, 대부분 body limit 가 걸려 있습니다.  
즉, 이 저장소의 핵심 문제를 “무제한 response read” 쪽으로 몰고 가는 건 정확하지 않습니다.

## 네트워크 요청의 context 전달도 기본은 괜찮음

`http.NewRequestWithContext(...)` 사용이 상당수 경로에 이미 들어가 있습니다.  
그러니 네트워크 계층의 주된 문제는 “context 전파 부재”보다 **transport policy 통일 실패, retry semantics, bare client 분기** 쪽입니다.

## 가장 큰 성능 이슈는 CPU 미세튜닝보다 상태 복구 비용

현재 구조에서는 3ms 빠른 것보다,  
실패 시 **DB/Valkey/메모리 중 어디가 진실인지 모르는 상태**가 훨씬 더 비쌉니다.

---

# 실제 패치 적용 순서

이 순서대로 가면 충돌이 적습니다.

1. `scheduler_alerts.go` 부분 성공 = 전체 성공 버그 수정
2. `acl/service.go` 의 DB-first / mode-capture / full-sync 패치
3. `twitch/client.go` 재귀 제거 + profiled client 적용
4. `summarizer.go` cache key 개선 + `summarizer_cache_key.go` 추가
5. `holodex/scraper.go` transport helper 적용
6. `notification/alarm_service.go` CRUD write-through 동기화
7. `stream-ingester/internal/ops` 공통 Markdown writer 추출
8. `cmd/*/main.go` bootstrap 공통화
9. `summarizer_prompt_assets.go` 프롬프트 자산 파일 분리

---

# 최종 판단

이 저장소는 “코드가 전반적으로 나쁘다” 쪽은 아닙니다.  
오히려 좋은 의도가 있는 부분도 많습니다. 예를 들면:

- 공용 HTTP helper 존재
- fallback / telemetry / ops tooling 을 따로 뽑으려는 흔적
- 테스트 파일 수 자체는 적지 않음

문제는 그 위에 **기능이 빠르게 쌓이면서, 성공 조건이 다른 하위 계층들을 한 덩어리 서비스가 책임지게 된 것**입니다.  
그래서 지금 가장 시급한 건 미세 최적화가 아니라 아래 둘입니다.

- **부분 성공을 전체 성공으로 오판하는 코드 제거**
- **DB/메모리/Valkey 중 authoritative source 를 분명히 하고 mutation 순서를 바로잡기**

이 두 줄기만 먼저 잡아도, 운영 중에 “왜 어떤 방은 알림이 안 왔지?”, “왜 재시작하니 알람이 사라졌지?”, “왜 whitelist 방이 black/white 키에 엇갈려 있지?” 같은 종류의 문제를 크게 줄일 수 있습니다.
