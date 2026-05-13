# Phase 08. Active channel tiering, rollout, tests

## 목표

전체 채널을 같은 cadence로 때리지 않고, 최근 활동 가능성이 높은 채널을 우선합니다.

tier:

- `active`: 최근 24시간 live/upcoming/recent/community/shorts 이벤트가 있음
- `warm`: 최근 7일 활동 있음
- `cold`: 그 외

## 코드 레벨 의사결정

1. 처음부터 scheduler 내부를 크게 바꾸지 않습니다.
2. target list를 active/warm/cold로 분리한 registration을 만드는 방식으로 시작합니다.
3. budget 계산은 tier별 registration을 그대로 합산하게 둡니다.
4. tier 계산은 DB/state 기반으로 하고, 실패 health와 독립시킵니다.
5. `SCRAPER_POLL_TIERING_ENABLED=true`일 때만 tiered registration을 적용합니다.

## 구현 방향

현재 stream ingester는 registration을 만들고 scheduler에 channel/poller/interval을 등록합니다. 이 구조에서는 channel set을 분리한 registration을 추가하는 방식이 가장 안전합니다.

권장 작업:

1. `youtube_poll_target_tiering.go` 신규
2. `youtubePollTargets`에 active/warm/cold slice 추가 또는 helper result로 분리
3. `buildStreamIngesterChannelPollerRegistrationsWithClient`에서 tier별 registration 생성
4. cold channel은 live/videos/community interval을 늘림
5. active channel은 live/upcoming 성격의 poller를 우선

## 예시 코드

```go
type youtubePollTier string

const (
	youtubePollTierActive youtubePollTier = "active"
	youtubePollTierWarm   youtubePollTier = "warm"
	youtubePollTierCold   youtubePollTier = "cold"
)

type youtubeTieredPollTargets struct {
	ActiveNotificationChannelIDs []string
	WarmNotificationChannelIDs   []string
	ColdNotificationChannelIDs   []string
	StatsChannelIDs              []string
}

func classifyYouTubePollTargetsByActivity(
	ctx context.Context,
	db *gorm.DB,
	targets youtubePollTargets,
	now time.Time,
) (youtubeTieredPollTargets, error) {
	// 기준:
	// active: streams table 또는 outbox/event table에서 24h 이내 활동
	// warm: 7d 이내 활동
	// cold: 나머지
	//
	// 실제 테이블명은 repo schema 기준으로 LLM 작업자가 확인해야 합니다.
	return youtubeTieredPollTargets{}, nil
}
```

registration 예시:

```go
func buildTieredLivePollerRegistrations(
	livePoller poller.Poller,
	poll config.ScraperPoll,
	targets youtubeTieredPollTargets,
) []providers.ChannelPollerRegistration {
	return []providers.ChannelPollerRegistration{
		providers.NewChannelPollerRegistration(livePoller, poller.PriorityHigh, poll.Live).
			WithChannelIDs(targets.ActiveNotificationChannelIDs).
			WithTargetGroup(providers.ChannelTargetGroupNotification).
			WithWorstCaseAttempts(scraper.FetchPageMaxAttempts).
			WithWorstCaseRequestUnitsPerRun(float64(scraper.FetchPageMaxAttempts)),

		providers.NewChannelPollerRegistration(livePoller, poller.PriorityNormal, poll.Live*2).
			WithChannelIDs(targets.WarmNotificationChannelIDs).
			WithTargetGroup(providers.ChannelTargetGroupNotification).
			WithWorstCaseAttempts(scraper.FetchPageMaxAttempts).
			WithWorstCaseRequestUnitsPerRun(float64(scraper.FetchPageMaxAttempts)),

		providers.NewChannelPollerRegistration(livePoller, poller.PriorityLow, poll.Live*6).
			WithChannelIDs(targets.ColdNotificationChannelIDs).
			WithTargetGroup(providers.ChannelTargetGroupNotification).
			WithWorstCaseAttempts(scraper.FetchPageMaxAttempts).
			WithWorstCaseRequestUnitsPerRun(float64(scraper.FetchPageMaxAttempts)),
	}
}
```

주의점:

- 이 phase는 DB schema 의존성이 커서 LLM 작업자가 먼저 table/model을 확인해야 합니다.
- phase 01~07이 merge된 뒤 적용합니다.
- budget validation은 기존 `summarizeYouTubeScraperBudget`을 그대로 태우면 됩니다.
- active/warm/cold 분리로 registration 수가 늘어나므로 log에 tier를 넣는 것이 좋습니다.

## Rollout 순서

1. 배포 1: failure taxonomy + metric만 켜기
   - `SCRAPER_CHANNEL_HEALTH_ENABLED=false`
   - `SCRAPER_SNAPSHOT_ENABLED=false`

2. 배포 2: channel health 켜기
   - parser drift만 backoff 적용
   - timeout/transport는 관찰 후 적용

3. 배포 3: snapshot 켜기
   - `SCRAPER_SNAPSHOT_ENABLED=true`
   - `SCRAPER_SNAPSHOT_MAX_BODY_BYTES=524288`
   - disk usage alert 필수

4. 배포 4: browser diagnostic 수동 호출만 허용

5. 배포 5: active/warm/cold tiering 적용
   - `SCRAPER_POLL_TIERING_ENABLED=true`

## 테스트 명령

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/scraper
go test ./hololive/hololive-shared/pkg/service/youtube
go test ./hololive/hololive-shared/pkg/service/youtube/poller
go test ./hololive/hololive-stream-ingester/internal/runtime
```

## 대시보드 확인 항목

- `youtube_scraper_channel_failures_total{reason="parser_drift"}`
- `youtube_scraper_channel_failures_total{reason="rate_limited"}`
- `youtube_scraper_channel_recoveries_total{recovery_source="api"}`
- API quota 사용량
- scheduler job count
- scraper RPM budget
- snapshot artifact count
- snapshot artifact directory disk usage

## 최종 완료 기준

- 실패 reason이 운영 로그와 metric에 분리되어 보입니다.
- API fallback은 “무엇을 복구했는지” 기록합니다.
- parser drift 시 raw fixture가 생성됩니다.
- channel/source별 backoff가 scheduler next run에 반영됩니다.
- browser diagnostic은 기본 수집 경로로 들어가지 않습니다.
- active/warm/cold tiering 후 전체 RPM이 줄어듭니다.
- registry version이 변하지 않아도 tier가 주기적으로 재분류됩니다.
