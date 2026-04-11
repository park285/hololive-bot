# YouTube Community/Shorts Channel Route Verification

## 목적

운영 DB와 현재 런타임 설정을 기준으로 유튜브 커뮤니티/쇼츠 알람의 배포 대상, 활성 상태, 실제 사용 경로를 채널별로 한 번에 대조하는 Markdown 리포트를 생성합니다.

이 리포트는 다음 세 가지 근거를 같은 출력에 묶습니다.

- 배포 대상: 현재 런타임 env가 해석한 `final_delivery_owner`
- 활성 상태: `members` + `alarms` 기준 채널/타입별 현재 활성 여부
- 실제 사용 경로: `youtube_content_alarm_tracking` + `youtube_notification_delivery_telemetry` 기준 최근 게시물의 관측 경로

## 실행

리포지토리 루트에서 실행합니다.

```bash
go run ./hololive/hololive-stream-ingester/cmd/youtube-community-shorts-route-report
```

더 긴 관측 창이 필요하면 `-window` 를 지정합니다.

```bash
go run ./hololive/hololive-stream-ingester/cmd/youtube-community-shorts-route-report -window=72h
```

필요 조건:

- `config.Load()` 가 성공할 수 있도록 운영과 동일한 DB/env 구성이 필요합니다.
- 기본 관측 창은 최근 `24h` 입니다.
- 리포트는 stdout으로 Markdown을 출력합니다.

## 출력 해석

헤더는 전체 운영 상태를 요약합니다.

- `runtime final owner`: 현재 배포 대상 owner입니다. 운영 목표값은 `youtube-scraper` 입니다.
- `big-bang enabled`: 운영 목표값은 `true` 입니다.
- `telemetry path expectation`: 실제 전송 텔레메트리에서 기대하는 신규 경로 값입니다. 현재 기준값은 `youtube_outbox_dispatcher` 입니다.
- `summary`: 채널/라우트 수를 보여 주고, 실제 사용 상태 집계는 현재 활성 라우트 기준으로 계산합니다.

채널 섹션의 각 라인은 `COMMUNITY` 또는 `SHORTS` 한 라우트를 뜻합니다.

- `activation`: 현재 활성 상태입니다.
  - `new_only`: 해당 타입 알람이 켜져 있고 신규 경로가 최종 경로입니다.
  - `disabled`: 현재 typed 알람 구독이 없습니다.
  - `pending_cutover`: 컷오버 시각 전이라 신규 경로 단일 활성로 판정하면 안 됩니다.
- `deployment`: 현재 런타임이 최종 owner로 해석한 경로입니다. 운영 목표값은 `youtube-scraper.youtube_outbox_dispatcher` 입니다.
- `actual`: 최근 관측 창에서 실제 게시물의 텔레메트리 경로를 요약한 상태입니다.
  - `new_only_verified`: 최근 게시물들이 모두 `youtube_outbox_dispatcher` 로만 관측됐습니다.
  - `no_recent_posts`: 최근 창에 해당 채널/타입 게시물이 없어 실제 경로를 관측하지 못했습니다.
  - `no_path_observed`: 최근 게시물은 있었지만 경로 텔레메트리가 없었습니다.
  - `unexpected_path_detected`: `youtube_outbox_dispatcher` 외 단일 경로가 관측됐습니다.
  - `mixed_paths_detected`: 같은 라우트 창에서 복수 경로가 함께 관측됐습니다.
- `observed_paths`: 최근 창에서 실제 관측된 `delivery_path` 집합입니다.

## 운영 판독 기준

다음 조건이면 “현재 운영 경로는 신규 경로만 사용 중” 근거로 사용할 수 있습니다.

- 헤더에서 `runtime final owner = youtube-scraper`
- 헤더에서 `big-bang enabled = true`
- 대상 라우트의 `activation = new_only`
- 대상 라우트의 `deployment = youtube-scraper.youtube_outbox_dispatcher`
- 대상 라우트의 `actual = new_only_verified`
- 대상 라우트의 `observed_paths` 에 `youtube_outbox_dispatcher` 외 값이 없음

다음 상태는 즉시 합격 근거로 쓰면 안 됩니다.

- `pending_cutover`: 컷오버 전 상태
- `no_recent_posts`: 실제 사용 경로를 아직 관측하지 못한 상태
- `no_path_observed`: 게시물은 있었지만 텔레메트리 근거가 빈 상태
- `unexpected_path_detected`, `mixed_paths_detected`: 신규 경로 단일 사용 위반 후보

`no_recent_posts` 가 많으면 관측 창을 늘려 다시 실행합니다.

## 근거 코드

- 리포트 수집/렌더링: `hololive/hololive-stream-ingester/internal/app/community_shorts_route_report.go`
- baseline SSOT: `hololive/hololive-stream-ingester/internal/app/community_shorts_target_baseline_build.go`
- 신규 경로 fan-out: `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher_send.go`
- 실제 경로 조회: `hololive/hololive-shared/pkg/service/youtube/outbox/delivery_path_usage.go`
- 게시물별 발송 집계: `hololive/hololive-shared/pkg/service/youtube/outbox/delivery_post_send_counts.go`

## 로컬 검증

- `go test ./hololive/hololive-stream-ingester/internal/app -run '^TestBuildCommunityShortsRouteVerificationReport$'`
- `go test ./hololive/hololive-stream-ingester/cmd/...`

관련 참고:

- `docs/current/YOUTUBE_COMMUNITY_SHORTS_TARGET_BASELINE.md`
- `docs/current/runbooks/YOUTUBE_COMMUNITY_SHORTS_ROUTE_USAGE_LAST_24H.md`
- `docs/current/runbooks/YOUTUBE_COMMUNITY_SHORTS_SEND_COUNTS_LAST_24H.md`
