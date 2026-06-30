# 코드 기반 성능 최적화·스크래핑 안정화 리팩토링안 (2026-06-30)

## 목적

이 문서는 기존 운영 문서나 실측 지표를 근거로 삼지 않고, 현재 저장소의 Go 코드와 SQL 호출 경로만 기준으로 병목 가능성이 명확한 구간을 정리합니다. 대상은 홀로라이브 봇의 핵심 런타임인 `hololive-youtube-producer`, 공용 YouTube scraper, poller batch repository, delivery/outbox tracking 저장소입니다.

핵심 판단 기준은 다음과 같습니다.

- 루프 내부 DB 조회·갱신으로 인해 입력 배치 크기에 선형으로 쿼리 수가 증가하는 코드
- 동적 `OR` 조건 또는 큰 `IN` 조건으로 인해 PostgreSQL planner가 안정적으로 인덱스를 타기 어려운 쿼리
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

스크래핑 안정성에서 가장 위험한 구간은 `validateSuccessfulFetchBody`입니다. 최종 URL이 Google/YouTube sorry 또는 consent 페이지로 리다이렉트되면 `ErrBlockedResponse`로 처리하지만, 본문에 block signature가 포함된 경우에는 경고만 남기고 성공으로 처리합니다. 이 경우 후속 parser가 차단 페이지를 정상 YouTube HTML로 파싱하려고 시도하여 parser drift, 빈 결과, 잘못된 cooldown 판단으로 이어질 수 있습니다.

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

### 제안 인터페이스

```go
type completedNotificationSentStateInput struct {
    Kind               domain.OutboxKind
    ContentID          string
    CanonicalContentID string
    ReactivationPostID string
    IdentityKey        string
}

func loadCompletedNotificationSentAtByIdentityBatch(
    ctx context.Context,
    tx batchDB,
    notifications []*domain.YouTubeNotificationOutbox,
) (map[string]time.Time, error)
```

### tracking row bulk query 예시

`dbx.PostgresPlaceholders`가 `?`를 `$n`으로 변환하므로 repository 내부 SQL은 기존 스타일을 유지할 수 있습니다.

```sql
WITH input(kind, content_id, canonical_content_id) AS (
  VALUES (?, ?, ?), (?, ?, ?)
)
SELECT i.kind,
       i.content_id,
       MIN(t.alarm_sent_at) AS sent_at
FROM input i
JOIN youtube_content_alarm_tracking t
  ON t.kind = i.kind
 AND (
      t.canonical_content_id = i.canonical_content_id
      OR t.content_id = i.content_id
 )
WHERE t.alarm_sent_at IS NOT NULL
GROUP BY i.kind, i.content_id;
```

### alarm state bulk query 예시

```sql
WITH input(kind, identity_content_id, post_id) AS (
  VALUES (?, ?, ?), (?, ?, ?)
)
SELECT i.kind,
       i.identity_content_id,
       MIN(s.alarm_sent_at) AS sent_at
FROM input i
JOIN youtube_community_shorts_alarm_states s
  ON s.kind = i.kind
 AND s.post_id = i.post_id
WHERE i.post_id <> ''
  AND s.alarm_sent_at IS NOT NULL
GROUP BY i.kind, i.identity_content_id;
```

두 결과를 같은 `identityKey` 기준으로 merge하면서 더 이른 `sent_at`을 선택하면 기존 `recordCompletedNotificationTrackingSentAt` / `recordCompletedNotificationAlarmStateSentAt`의 의미를 유지할 수 있습니다.

### 테스트 포인트

- 같은 `(kind, content_id)` 후보가 여러 번 들어와도 한 번만 조회 입력에 포함됩니다.
- tracking row와 alarm state row가 둘 다 있으면 더 이른 `sent_at`이 선택됩니다.
- payload JSON 파싱 실패 시 기존처럼 `content_id`를 post_id fallback으로 사용합니다.
- raw ID와 canonical ID가 섞여 있어도 기존 `trackingIdentityCandidates`의 의미가 깨지지 않습니다.
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

## P1. 동적 OR-list identity lookup을 VALUES CTE JOIN으로 교체

현재 다음 함수들은 `(kind = ? AND content_id = ?)` clause를 여러 개 만든 뒤 `OR`로 결합합니다.

- `failedNotificationOutboxQueryArgs` + `loadFailedNotificationOutboxRows`
- `collectTrackingIdentityClauses` + `loadPersistedOutboxSentState`
- `identityRepository.findByIdentityRecords`의 후보 조회

이 패턴은 입력이 작을 때는 단순하지만, 조건 수가 늘어날수록 SQL 문자열이 커지고 planner가 composite index 사용을 안정적으로 선택하기 어렵습니다. 동일한 입력을 `VALUES` CTE로 올리고 대상 테이블과 JOIN하면 쿼리 형태가 고정되고, 인덱스 조건도 명확해집니다.

### 예시

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

### 제안 정책

blocked body signature는 바로 전역 cooldown으로 연결하지 말고, fetch result를 `ErrBlockedResponse` 계열의 별도 source failure로 승격합니다.

권장 흐름은 다음입니다.

```text
nethttp/goscrapy success body
  -> block body signature detected
    -> browser_snapshot fetcher가 있으면 snapshot fallback 1회
    -> fallback도 block이면 channel-source failure로 기록
    -> global hard cooldown은 429/403/Retry-After 중심으로만 유지
```

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

1. `repository_batch_delivery_state.go`의 completed sent-state 조회를 batch query로 변경합니다.
2. completed failed outbox finalize를 bulk update로 변경합니다.
3. OR-list query를 `VALUES` CTE JOIN 패턴으로 통일합니다.
4. blocked body signature를 parser 성공 경로에서 제거하고 browser snapshot fallback 또는 source-level failure로 라우팅합니다.
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
- failed outbox row finalize 이후 outbox/delivery row가 모두 `SENT`로 정리되는지 확인
- reactivation 대상 failed row는 기존처럼 `PENDING`으로 rearm되는지 확인

## 기대 효과

이 리팩토링은 측정값이 없어도 코드 구조상 확실한 개선입니다. 가장 큰 변화는 notification insert transaction 내부의 DB round-trip 수를 후보 개수에 비례하는 구조에서 batch 단위 상수 쿼리 구조로 바꾸는 것입니다. scraper 쪽은 차단/동의/challenge HTML을 정상 HTML로 오인하지 않도록 하여 parser drift와 잘못된 source health 판정을 줄이는 방향입니다.
