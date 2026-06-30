# 코드 기반 성능 최적화·스크래핑 안정화 리팩토링안 (2026-06-30)

## 목적

이 문서는 기존 운영 문서나 실측 지표를 근거로 삼지 않고, 현재 저장소의 Go 코드와 SQL 호출 경로만 기준으로 병목 가능성이 명확한 구간을 정리합니다. 대상은 홀로라이브 봇의 핵심 런타임인 `hololive-youtube-producer`, 공용 YouTube scraper, poller batch repository, delivery/outbox tracking 저장소입니다.

핵심 판단 기준은 다음과 같습니다.

- 루프 내부 DB 조회·갱신으로 인해 입력 배치 크기에 선형으로 쿼리 수가 증가하는 코드
- 동적 identity predicate 또는 큰 `IN` 조건으로 인해 PostgreSQL planner가 안정적으로 인덱스를 타기 어려운 쿼리
- YouTube HTML/차단/동의 페이지를 정상 HTML로 오판할 수 있는 scraper 경로
- fetch engine 생성·종료 비용이 요청마다 반복되는 코드
- hot path에서 반복 문자열 정규화·동적 SQL 문자열 생성이 발생하는 코드

## 분석 범위

주요 분석 파일은 아래와 같습니다.

- `hololive/hololive-shared/pkg/service/youtube/scraper/internal/scraping/client_http.go`
- `hololive/hololive-shared/pkg/service/youtube/scraper/internal/scraping/client_resolve.go`
- `hololive/hololive-shared/pkg/service/youtube/scraper/internal/scraping/goscrapy_fetcher.go`
- `hololive/hololive-shared/pkg/service/youtube/scraper/internal/scraping/community.go`
- `hololive/hololive-shared/pkg/service/youtube/poller/internal/pollers/community_poller.go`
- `hololive/hololive-shared/pkg/service/youtube/poller/internal/batchrepo/repository_batch.go`
- `hololive/hololive-shared/pkg/service/youtube/poller/internal/batchrepo/repository_batch_writes.go`
- `hololive/hololive-shared/pkg/service/youtube/poller/internal/batchrepo/repository_batch_delivery_state.go`
- `hololive/hololive-shared/pkg/service/youtube/poller/internal/batchrepo/repository_batch_persisted_state.go`
- `hololive/hololive-shared/pkg/service/youtube/tracking/internal/observation/repository_identity.go`
- `hololive/hololive-shared/pkg/service/youtube/tracking/internal/observation/alarm_state_repository.go`

## 결론 요약

최우선 병목은 `repository_batch_delivery_state.go`의 알림 재활성화/중복 방지 경로입니다. `insertNotificationsChunk`가 `prepareNotificationInsertChunk`를 호출하고, 그 안에서 `loadCompletedNotificationSentAtByIdentity`가 후보 알림을 순회하며 후보마다 `FindByIdentity`와 `FindAlarmStateByPostID`를 호출합니다. 이 구조는 배치 크기가 50이면 한 트랜잭션 안에서 최대 100개 안팎의 SELECT가 추가될 수 있습니다. 이는 데이터량이 많을 때만 느려지는 문제가 아니라, 코드 구조상 배치 크기에 비례해서 쿼리 수가 늘어나는 명확한 병목입니다.

스크래핑 안정성에서 가장 위험한 구간은 `validateSuccessfulFetchBody`입니다. 최종 URL이 Google/YouTube sorry 또는 consent 페이지로 리다이렉트되면 `ErrBlockedResponse`로 처리하지만, 본문에 block signature가 포함된 경우에는 경고만 남기고 성공으로 처리합니다. 이 경우 후속 parser가 차단 페이지를 정상 YouTube HTML로 파싱하려고 시도하여 parser drift, 빈 결과, 잘못된 cooldown 판단으로 이어질 수 있습니다. 단, 기존 회귀 테스트가 고정한 불변식인 “body substring만으로 fleet-wide/global hard cooldown을 만들지 않는다”는 정책은 유지해야 합니다.

## 리뷰 반영 메모

이 문서는 1차 리뷰에서 지적된 세 가지 정확도 보강 사항을 반영합니다.

- bulk sent-state query 예시에 `content_id`, `canonical_content_id`, `raw_content_id`의 의미를 모두 명시합니다.
- `identityRepository.findByIdentityRecords`는 단순 `(kind = ? AND content_id = ?) OR ...` 패턴이 아니라 `kind = ? AND (canonical_content_id = ? OR content_id IN (...))` 형태이므로, P1 표현을 “동적 identity lookup”으로 좁힙니다.
- blocked body 강화안은 기존 `TestHB05BodySubstringDoesNotTriggerBlockCooldown_9e234216` 계열 불변식과 `TestBodyLooksBlockedByYouTubeIgnoresGenericContentWords`의 의도를 침해하지 않도록, source-level failure와 global hard cooldown의 경계를 분리합니다.

## P0. 알림 sent-state 확인 경로 N+1 SQL 제거

### 현재 코드 흐름

현재 `BatchInsertNotifications`의 내부 흐름은 다음과 같습니다.

```text
BatchInsertNotifications
  -> insertNotificationsChunk
    -> prepareNotificationInsertChunk
      -> loadCompletedNotificationSentAtByIdentity
        -> for each candidate
          -> recordCompletedNotificationSentAtByCandidate
            -> trackingrepo.FindByIdentity
            -> trackingrepo.FindAlarmStateByPostID (payload에서 post_id 해석 가능한 경우)
      -> loadFailedNotificationOutboxRows
      -> finalizeCompletedFailedNotificationRows
```

문제가 되는 지점은 `loadCompletedNotificationSentAtByIdentity`입니다. 이 함수는 후보를 dedupe한 뒤에도 후보마다 tracking row 조회와 alarm state 조회를 개별 SQL로 실행합니다. 커뮤니티/쇼츠 배치 크기가 `PollerBatchMaxSize=50`으로 제한되어 있어도, 이 경로는 배치당 쿼리 수가 입력 개수에 비례합니다.

### 리팩토링 목표

후보 알림 목록을 먼저 정규화한 뒤, 아래 두 테이블을 set-based query로 한 번씩만 조회하도록 바꿉니다.

- `youtube_content_alarm_tracking`
- `youtube_community_shorts_alarm_states`

그 결과 `loadCompletedNotificationSentAtByIdentity`의 SQL 호출 수는 후보 수와 무관하게 최대 2회로 고정됩니다.

### 후보 identity 모델

후속 구현에서 가장 중요한 점은 raw/canonical fallback 의미를 보존하는 것입니다. 기존 `trackingIdentityCandidates`는 canonical ID와 raw ID 후보를 함께 고려합니다. 따라서 bulk input도 단순 `content_id` 하나만 들고 있으면 안 됩니다.

```go
type completedNotificationSentStateInput struct {
    Kind               domain.OutboxKind
    IdentityKey        string
    RequestedContentID string
    CanonicalContentID string
    RawContentID       string
    ReactivationPostID string
}
```

각 필드의 의미는 아래처럼 고정합니다.

- `RequestedContentID`: notification/outbox에 현재 들어온 `content_id`. 이미 canonical prefix가 붙었을 수도 있고 raw ID일 수도 있습니다.
- `CanonicalContentID`: `ytcontentid.ForOutboxKind(kind, RequestedContentID)` 결과입니다. 실패하면 기존 `canonicalTrackingIdentity`처럼 원본 trim 값을 fallback으로 둡니다.
- `RawContentID`: `NormalizeShortVideoID` 또는 `NormalizeCommunityPostID` 결과입니다. 이미 canonical prefix가 붙은 값도 raw resource ID로 되돌립니다.
- `IdentityKey`: 기존 `notificationIdentityKey(kind, RequestedContentID)` 기준을 유지합니다. 결과 map의 key가 바뀌면 dedupe와 sent-state merge 의미가 달라질 수 있습니다.
- `ReactivationPostID`: payload에서 추출한 post/video canonical post ID 우선, 실패 시 기존처럼 `content_id` fallback을 사용합니다.

### input 수집 의사 코드

```go
func collectCompletedNotificationSentStateInputs(
    notifications []*domain.YouTubeNotificationOutbox,
) []completedNotificationSentStateInput {
    inputs := make([]completedNotificationSentStateInput, 0, len(notifications))
    seen := make(map[string]struct{}, len(notifications))

    for _, notification := range notifications {
        candidate, ok := completedNotificationIdentityCandidateFor(notification)
        if !ok {
            continue
        }
        if _, ok := seen[candidate.identityKey]; ok {
            continue
        }
        seen[candidate.identityKey] = struct{}{}

        canonicalContentID := canonicalTrackingIdentity(candidate.notification.Kind, candidate.contentID)
        rawContentID := rawTrackingIdentity(candidate.notification.Kind, candidate.contentID)
        reactivationPostID := resolveNotificationReactivationPostID(
            candidate.notification.Kind,
            candidate.contentID,
            candidate.notification.Payload,
        )

        inputs = append(inputs, completedNotificationSentStateInput{
            Kind:               candidate.notification.Kind,
            IdentityKey:        candidate.identityKey,
            RequestedContentID: candidate.contentID,
            CanonicalContentID: canonicalContentID,
            RawContentID:       rawContentID,
            ReactivationPostID: reactivationPostID,
        })
    }
    return inputs
}
```

`rawTrackingIdentity`는 `trackingIdentityCandidates`를 직접 재사용하거나, 중복을 피하기 위해 `trackingIdentityCandidatePair`와 같은 helper를 public/internal helper로 승격하는 방식이 안전합니다. 목표는 “bulk 전환 후에도 기존 `FindByIdentity`가 보던 후보 집합을 줄이지 않는 것”입니다.

### tracking row bulk query 예시

`dbx.PostgresPlaceholders`가 `?`를 `$n`으로 변환하므로 repository 내부 SQL은 기존 스타일을 유지할 수 있습니다.

```sql
WITH input(kind, identity_key, requested_content_id, canonical_content_id, raw_content_id) AS (
  VALUES (?, ?, ?, ?, ?), (?, ?, ?, ?, ?)
)
SELECT i.identity_key,
       MIN(t.alarm_sent_at) AS sent_at
FROM input i
JOIN youtube_content_alarm_tracking t
  ON t.kind = i.kind
 AND (
      t.canonical_content_id = i.canonical_content_id
      OR t.content_id = i.requested_content_id
      OR t.content_id = i.canonical_content_id
      OR (i.raw_content_id <> '' AND t.content_id = i.raw_content_id)
 )
WHERE t.alarm_sent_at IS NOT NULL
GROUP BY i.identity_key;
```

`OR t.content_id = i.canonical_content_id`는 legacy row가 `content_id`에 canonical prefix 값을 들고 있는 경우를 보존하기 위한 방어 조건입니다. 기존 `FindByIdentity`는 `canonical_content_id = preferredContentID OR content_id IN (candidates)` 형태로 canonical/raw 양쪽 후보를 봅니다. bulk query도 이 후보 의미를 약화하면 안 됩니다.

### alarm state bulk query 예시

```sql
WITH input(kind, identity_key, post_id) AS (
  VALUES (?, ?, ?), (?, ?, ?)
)
SELECT i.identity_key,
       MIN(s.alarm_sent_at) AS sent_at
FROM input i
JOIN youtube_community_shorts_alarm_states s
  ON s.kind = i.kind
 AND s.post_id = i.post_id
WHERE i.post_id <> ''
  AND s.alarm_sent_at IS NOT NULL
GROUP BY i.identity_key;
```

두 결과를 같은 `identityKey` 기준으로 merge하면서 더 이른 `sent_at`을 선택하면 기존 `recordCompletedNotificationTrackingSentAt` / `recordCompletedNotificationAlarmStateSentAt`의 의미를 유지할 수 있습니다.

### 테스트 포인트

- 같은 `(kind, content_id)` 후보가 여러 번 들어와도 한 번만 조회 입력에 포함됩니다.
- tracking row와 alarm state row가 둘 다 있으면 더 이른 `sent_at`이 선택됩니다.
- payload JSON 파싱 실패 시 기존처럼 `content_id`를 post_id fallback으로 사용합니다.
- raw ID와 canonical ID가 섞여 있어도 기존 `trackingIdentityCandidates`의 의미가 깨지지 않습니다.
- legacy row가 `content_id = raw`, `content_id = canonical`, `canonical_content_id = canonical` 중 어느 형태로 존재해도 sent state를 찾습니다.
- `NEW_VIDEO`, `LIVE_STREAM`, `MILESTONE`은 기존처럼 community/shorts reactivation 대상에서 제외됩니다.

## P0. 완료된 failed outbox row finalize의 row-by-row UPDATE 제거

`finalizeCompletedFailedNotificationRows`는 completed failed outbox row를 순회하며 row마다 다음 두 UPDATE를 실행합니다.

- `updateCompletedFailedNotificationOutboxRow`
- `updateCompletedFailedNotificationDeliveryRows`

failed row가 누적된 상황에서 이 경로는 `2N` UPDATE가 됩니다. 이미 `rearmFailedDeliveryRows`는 `outbox_id IN (...)` 기반 bulk update를 사용하고 있으므로 finalize 경로도 같은 수준으로 묶는 편이 맞습니다.

### 제안 SQL

```sql
WITH input(id, sent_at) AS (
  VALUES (?, ?), (?, ?)
)
UPDATE youtube_notification_outbox o
SET status = ?,
    locked_at = NULL,
    sent_at = CASE
      WHEN o.sent_at IS NULL OR o.sent_at > i.sent_at THEN i.sent_at
      ELSE o.sent_at
    END,
    error = ''
FROM input i
WHERE o.id = i.id
  AND o.status = ?;
```

`youtube_notification_delivery`도 같은 `input(id, sent_at)` CTE를 공유해 `outbox_id = i.id`로 bulk update합니다.

### 구현 시 주의점

bulk finalize와 rearm은 서로 다른 상태 전이입니다.

- completed failed row: 이미 tracking/alarm state에 `alarm_sent_at`이 존재하므로 outbox/delivery를 `SENT`로 정리합니다.
- reactivation row: sent state가 없으므로 failed delivery row를 `PENDING`으로 되살립니다.

따라서 `partitionFailedNotificationOutboxRows`의 결과를 합치지 말고, completed와 reactivation을 끝까지 분리해서 처리해야 합니다.

## P1. 동적 identity lookup을 VALUES CTE JOIN으로 교체

현재 다음 함수들은 입력 identity 수에 따라 SQL predicate가 동적으로 커집니다.

- `failedNotificationOutboxQueryArgs` + `loadFailedNotificationOutboxRows`: `(kind = ? AND content_id = ?)` clause를 여러 개 만든 뒤 `OR`로 결합합니다.
- `collectTrackingIdentityClauses` + `loadPersistedOutboxSentState`: 위와 같은 `(kind = ? AND content_id = ?)` OR-list입니다.
- `identityRepository.findByIdentityRecords`: 단순 OR-list가 아니라 `kind = ? AND (canonical_content_id = ? OR content_id IN (...))` 형태입니다.

즉 세 번째 경로를 “`(kind = ? AND content_id = ?) OR ...`”로 설명하면 부정확합니다. 더 정확한 표현은 “입력 identity 후보 집합에 따라 SQL predicate가 바뀌는 동적 identity lookup”입니다.

이 패턴은 입력이 작을 때는 단순하지만, 조건 수가 늘어날수록 SQL 문자열이 커지고 planner가 composite index 사용을 안정적으로 선택하기 어렵습니다. 동일한 입력을 `VALUES` CTE로 올리고 대상 테이블과 JOIN하면 쿼리 형태가 고정되고, 인덱스 조건도 명확해집니다.

### failed outbox row lookup 예시

```sql
WITH input(kind, content_id) AS (
  VALUES (?, ?), (?, ?), (?, ?)
)
SELECT o.id, o.kind, o.content_id
FROM input i
JOIN youtube_notification_outbox o
  ON o.kind = i.kind
 AND o.content_id = i.content_id
WHERE o.status = ?;
```

### tracking identity lookup 예시

```sql
WITH input(kind, preferred_content_id, candidate_content_id) AS (
  VALUES (?, ?, ?), (?, ?, ?)
)
SELECT t.kind, t.content_id, t.canonical_content_id, t.channel_id,
       t.actual_published_at, t.detected_at, t.alarm_sent_at,
       t.alarm_latency_millis, t.alarm_latency_exceeded, t.delivery_status,
       COALESCE(t.latency_classification_status, '') AS latency_classification_status,
       COALESCE(t.delay_source, '') AS delay_source,
       COALESCE(t.internal_delay_cause, '') AS internal_delay_cause,
       t.created_at, t.updated_at
FROM input i
JOIN youtube_content_alarm_tracking t
  ON t.kind = i.kind
 AND (
      t.canonical_content_id = i.preferred_content_id
      OR t.content_id = i.candidate_content_id
 )
```

이 예시는 구조를 보여주기 위한 것입니다. 실제 구현에서는 한 identity가 canonical/raw 후보를 둘 다 가질 수 있으므로 `input`을 후보 단위 row로 펼친 뒤, 결과를 `preferTrackingIdentityRecord`와 같은 우선순위 규칙으로 다시 접어야 합니다.

권장 인덱스 확인 항목은 다음과 같습니다.

- `youtube_notification_outbox(status, kind, content_id)` 또는 `WHERE status = 'FAILED'` partial index
- `youtube_notification_outbox(kind, content_id)` unique/index
- `youtube_content_alarm_tracking(kind, canonical_content_id)`
- legacy fallback이 유지되는 동안 `youtube_content_alarm_tracking(kind, content_id)`
- `youtube_community_shorts_alarm_states(kind, post_id)`
- `youtube_notification_delivery(outbox_id, status)`

## P1. scraper blocked body 판정 강화

### 현재 위험

`validateSuccessfulFetchBody`는 다음 순서로 성공 응답을 검증합니다.

1. body가 비어 있으면 `ErrEmptyResponse`
2. final URL이 block/consent/sorry 계열이면 `ErrBlockedResponse`
3. body에 block signature가 있으면 경고만 기록하고 성공 처리

3번이 문제입니다. YouTube가 status 200으로 challenge HTML을 반환하는 경우 final URL만으로는 차단을 식별하지 못합니다. 본문 signature가 잡혔는데도 성공으로 넘기면 parser가 정상 `ytInitialData`를 찾지 못하고, 이 실패가 parser drift나 빈 데이터로 오염됩니다.

### 기존 불변식

현재 테스트 정책은 body substring만으로 전역 cooldown을 걸지 않는 방향입니다. 특히 아래 회귀 불변식은 유지해야 합니다.

- `TestHB05BodySubstringDoesNotTriggerBlockCooldown_9e234216` 계열: body 내부 문자열만으로 fleet-wide/global hard cooldown을 만들면 안 됩니다.
- `TestBodyLooksBlockedByYouTubeIgnoresGenericContentWords`: 일반 영상/댓글/설명에 `captcha`, `cookies`, `unusual traffic`, `before you continue` 같은 문구가 들어가도 정상 콘텐츠일 수 있으므로 block으로 오판하면 안 됩니다.
- `TestFetchPageDoesNotRetryBlockedSuccessfulResponse`: final URL이 sorry/consent 계열로 확정된 경우는 `ErrBlockedResponse`이며 재시도하지 않는 것이 맞습니다.

따라서 “blocked body를 더 강하게 본다”는 말은 “문자열이 보이면 즉시 global hard cooldown”이 아닙니다. 더 정확한 구현 목표는 “정상 parser 성공 경로에서 challenge HTML을 제거하되, 전역 cooldown은 final URL, 429, 403, Retry-After처럼 신뢰도 높은 신호에만 연결한다”입니다.

### 제안 정책

blocked body signature는 바로 전역 cooldown으로 연결하지 말고, fetch result를 source-level suspicious/blocked classification으로 승격합니다. 구현상 `ErrBlockedResponse`를 그대로 사용할 경우 `shouldRetryFetchPage`와 `recordFetchPageTransientError`가 global hard cooldown을 만들지 않는지 반드시 테스트해야 합니다. 더 안전한 대안은 별도 에러를 두는 것입니다.

```go
var ErrBlockedBodySignature = errors.New("youtube response body contains block signature")
```

권장 흐름은 다음입니다.

```text
nethttp/goscrapy success body
  -> final URL blocked
    -> ErrBlockedResponse
    -> no retry
    -> no body parser
    -> current hard-block policy 유지
  -> body signature suspicious
    -> generic-content false positive guard 통과 여부 확인
    -> browser_snapshot fetcher가 있으면 snapshot fallback 1회
    -> fallback 성공이면 snapshot body 사용
    -> fallback도 suspicious이면 channel-source failure로 기록
    -> global hard cooldown은 만들지 않음
```

### 구현 조건

- `bodyLooksBlockedByYouTube`의 signature 목록은 generic phrase가 아니라 URL/action/form 수준의 강한 signal만 유지합니다.
- body signature만으로 `backoffState.RecordErrorWithSuggestedCooldown` 또는 hard cooldown 계층을 호출하지 않습니다.
- channel health/source health에는 실패를 기록하되, producer 전체를 멈추는 cooldown으로 전파하지 않습니다.
- browser snapshot fallback이 없는 구성에서는 parser drift로 흘려보내지 말고 source-level failure로 빠르게 반환합니다.
- `ClassifyFailure`에 별도 reason이 필요하면 `FailureReasonBlockedResponse`와 구분되는 `FailureReasonSuspiciousBlockedBody`를 추가하는 편이 운영 분석에 더 낫습니다.

이렇게 하면 차단 페이지를 정상 HTML로 오판하지 않으면서도, 단일 채널/단일 요청의 challenge가 전체 producer를 과도하게 멈추는 문제를 피할 수 있습니다.

## P1. community parser의 tab title 의존 제거

`extractCommunityPostsContent`는 tab title이 `Posts` 또는 `Community`일 때만 community posts content를 찾습니다. YouTube는 UI 문자열, 지역화, renderer 계층을 자주 바꾸므로 title string은 안정적인 selector가 아닙니다.

### 제안

1차는 기존 tab path를 유지합니다. 2차 fallback으로 `ytInitialData` 전체에서 `backstagePostThreadRenderer.post.backstagePostRenderer`가 포함된 section을 찾아 반환합니다. 즉, UI label이 아니라 실제 renderer type을 selector로 사용합니다.

```text
extractCommunityPostsContent
  -> tab title 기반 fast path
  -> renderer type 기반 fallback
  -> 둘 다 실패하면 parser drift snapshot 기록
```

이 변경은 `/posts` URL 변경, localized tab title, minor YouTube UI 개편에 대한 내성을 올립니다.

### 코드 레벨 fallback 예시

```go
func extractCommunityPostsContent(data *gjson.Result) gjson.Result {
    if postsContent := extractCommunityPostsContentByTabTitle(data); postsContent.Exists() {
        return postsContent
    }
    return extractCommunityPostsContentByRenderer(data)
}

func extractCommunityPostsContentByRenderer(data *gjson.Result) gjson.Result {
    var found gjson.Result
    data.Get("contents").ForEach(func(_, value gjson.Result) bool {
        if value.Get("backstagePostThreadRenderer.post.backstagePostRenderer").Exists() {
            found = value
            return false
        }
        return true
    })
    return found
}
```

위 코드는 개념 예시입니다. 실제 `gjson` traversal은 현재 JSON tree 깊이에 맞춰 recursive walker 또는 제한 깊이 DFS helper로 작성해야 합니다. 무제한 재귀는 큰 `ytInitialData`에서 비용이 커질 수 있으므로 최대 depth와 최대 node scan count를 두는 것이 안전합니다.

## P2. GoScrapy fetcher lifecycle 비용 축소

`defaultGoscrapyRunner.Run`은 요청마다 `newGoScrapyFetchApp`을 생성하고, `app.Start`를 goroutine으로 띄운 뒤 10ms ticker로 active count를 polling합니다. 완료 후 `waitGoScrapyEngine`은 100ms까지만 engine 종료를 기다립니다.

이 구조의 문제는 다음과 같습니다.

- 요청마다 app/engine 초기화 비용이 반복됩니다.
- 10ms polling ticker가 fetch concurrency 증가 시 불필요한 wake-up을 만듭니다.
- engine shutdown이 100ms를 넘으면 호출자는 반환하지만 내부 goroutine 정리가 늦어질 수 있습니다.

### 제안

- 단기: ticker polling 대신 result channel / engine done channel / ctx done channel만 기다리는 event-driven wait로 변경합니다.
- 중기: goscrapy app 생성 비용이 크다면 fetcher instance 단위로 재사용 가능한 runner pool을 둡니다.
- 장기: `FetcherEngineGoScrapy`를 fallback engine이 아니라 명확한 browser-like fetch strategy로 격리하고, nethttp 실패 원인별로 goscrapy/browser_snapshot fallback 우선순위를 명시합니다.

## P2. hot path 소형 allocation 제거

`CommunityPoller.matchesKeywords`는 매 호출마다 keyword를 `strings.ToLower(keyword)`로 변환합니다. keyword 목록은 poller 생성 후 자주 바뀌지 않는 설정값이므로 `NewCommunityPoller`에서 한 번만 lower/trim 정규화하는 편이 맞습니다.

권장 변경은 다음과 같습니다.

```go
func normalizeKeywords(keywords []string) []string {
    out := make([]string, 0, len(keywords))
    seen := make(map[string]struct{}, len(keywords))
    for _, keyword := range keywords {
        normalized := strings.ToLower(strings.TrimSpace(keyword))
        if normalized == "" {
            continue
        }
        if _, ok := seen[normalized]; ok {
            continue
        }
        seen[normalized] = struct{}{}
        out = append(out, normalized)
    }
    return out
}
```

그 뒤 `matchesKeywords`는 `lowerText := strings.ToLower(text)` 한 번만 수행하고, 이미 정규화된 keyword와 비교합니다.

## 적용 순서

1. `repository_batch_delivery_state.go`의 completed sent-state 조회를 batch query로 변경합니다. 이때 `requested_content_id`, `canonical_content_id`, `raw_content_id` 후보 의미를 모두 보존합니다.
2. completed failed outbox finalize를 bulk update로 변경합니다.
3. 동적 identity lookup을 `VALUES` CTE JOIN 패턴으로 통일하되, `identityRepository.findByIdentityRecords`의 canonical/raw 우선순위는 유지합니다.
4. blocked body signature를 parser 성공 경로에서 제거하고 browser snapshot fallback 또는 source-level failure로 라우팅합니다. 이 변경은 global hard cooldown으로 전파하지 않습니다.
5. community parser에 renderer-type fallback을 추가합니다.
6. GoScrapy runner wait loop를 event-driven 구조로 바꿉니다.
7. keyword normalize 같은 소형 hot path allocation을 제거합니다.

## 검증 계획

- `go test ./hololive/hololive-shared/pkg/service/youtube/poller/internal/batchrepo/...`
- `go test ./hololive/hololive-shared/pkg/service/youtube/tracking/internal/observation/...`
- `go test ./hololive/hololive-shared/pkg/service/youtube/scraper/internal/scraping/...`
- `go test ./hololive/hololive-youtube-producer/...`
- 기존 local CI: `./scripts/ci/local-ci.sh`

추가로 batchrepo 테스트에는 다음 케이스를 넣습니다.

- 50개 후보 알림 입력 시 completed sent state를 set-based로 동일하게 재구성하는지 확인
- tracking row와 alarm state row의 sent_at이 다를 때 더 이른 값을 선택하는지 확인
- raw/canonical/legacy content ID 조합에서 기존 `FindByIdentity`와 bulk 결과가 동일한지 확인
- failed outbox row finalize 이후 outbox/delivery row가 모두 `SENT`로 정리되는지 확인
- reactivation 대상 failed row는 기존처럼 `PENDING`으로 rearm되는지 확인

추가로 scraper 테스트에는 다음 케이스를 넣습니다.

- `TestHB05BodySubstringDoesNotTriggerBlockCooldown_9e234216` 계열 불변식을 유지합니다.
- generic phrase가 들어간 정상 body는 block으로 분류하지 않습니다.
- final URL 기반 block은 기존처럼 `ErrBlockedResponse`를 반환하고 retry하지 않습니다.
- body signature 기반 suspicious response는 parser 성공 경로로 넘기지 않되 global hard cooldown은 만들지 않습니다.
- browser snapshot fallback이 성공하면 fallback body를 사용하고 channel-source failure를 기록하지 않습니다.

## 기대 효과

이 리팩토링은 측정값이 없어도 코드 구조상 확실한 개선입니다. 가장 큰 변화는 notification insert transaction 내부의 DB round-trip 수를 후보 개수에 비례하는 구조에서 batch 단위 상수 쿼리 구조로 바꾸는 것입니다. scraper 쪽은 차단/동의/challenge HTML을 정상 HTML로 오인하지 않도록 하되, 기존 회귀 테스트가 보장하는 global hard cooldown 불변식은 유지하는 방향입니다.
