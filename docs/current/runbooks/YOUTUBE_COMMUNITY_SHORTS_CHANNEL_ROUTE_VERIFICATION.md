# YouTube Community/Shorts Channel Route Verification

## 목적

운영 DB와 현재 런타임 설정을 기준으로 유튜브 커뮤니티/쇼츠 알람의 배포 대상, 활성 상태, 실제 사용 경로를 채널별로 한 번에 대조하는 Markdown 리포트를 생성합니다.

이 리포트는 다음 세 가지 근거를 같은 출력에 묶습니다.

- 배포 대상: 현재 런타임 env가 해석한 `final_delivery_owner`
- 활성 상태: `members` + `alarms` 기준 채널/타입별 현재 활성 여부
- 실제 사용 경로: `youtube_content_alarm_tracking` + alarm-worker가 만든 `youtube_notification_delivery` 행과 감사 로그 `delivery_path` 기준 최근 게시물의 관측 경로

## 실행

전용 리포트 CLI는 없으며 PostgreSQL을 직접 조회한다.

Community/shorts 수집 evidence는 `collector` observation, canonical consume는 `hololive-api` YouTube plane, 최종 발송은 `alarm-worker`다. 운영 조회는 `members`/`alarms`와 `youtube_content_alarm_tracking` + `youtube_notification_delivery`를 직접 대조하고, 경로 값은 alarm-worker 감사 로그의 `delivery_path`로 확인한다.

필요 조건:

- 운영과 동일한 PostgreSQL 구성이 필요합니다.
- 기본 관측 창은 최근 `24h` 입니다.

## 출력 해석

헤더는 전체 운영 상태를 요약합니다.

- `runtime final owner`: 현재 최종 발송 owner입니다. 운영 목표값은 `alarm-worker` 입니다.
- `big-bang enabled`: 운영 목표값은 `true` 입니다.
- `path expectation`: 감사 로그 `delivery_path`에서 기대하는 경로 값입니다. 현재 기준값은 `youtube_outbox_dispatcher` 입니다.
- `summary`: 채널/라우트 수를 보여 주고, 실제 사용 상태 집계는 현재 활성 라우트 기준으로 계산합니다.

채널 섹션의 각 라인은 `COMMUNITY` 또는 `SHORTS` 한 라우트를 뜻합니다.

- `activation`: 현재 활성 상태입니다.
  - `new_only`: 해당 타입 알람이 켜져 있고 신규 경로가 최종 경로입니다.
  - `disabled`: 현재 typed 알람 구독이 없습니다.
  - `pending_cutover`: 컷오버 시각 전이라 신규 경로 단일 활성로 판정하면 안 됩니다.
- `deployment`: 현재 런타임이 최종 owner로 해석한 경로입니다. 운영 목표값은 `alarm-worker.youtube_outbox_dispatcher` 입니다.
- `actual`: 최근 관측 창에서 실제 게시물이 alarm-worker outbox·delivery를 거쳤는지 요약한 상태입니다. 경로 값은 행에 저장하지 않고 코드 상수(`youtube_outbox_dispatcher`) 하나만 로그에 쓰므로, 다른 경로나 복수 경로는 관측될 수 없습니다.
  - `delivery_observed`: 최근 게시물이 outbox와 room별 delivery로 진입했습니다.
  - `no_recent_posts`: 최근 창에 해당 채널/타입 게시물이 없어 실제 사용을 관측하지 못했습니다.
  - `no_delivery_observed`: 최근 게시물은 있었지만 outbox 또는 delivery가 없었습니다. 구독 room이 없는 outbox는 delivery 없이 SENT로 끝나므로 [Route Usage](YOUTUBE_COMMUNITY_SHORTS_ROUTE_USAGE_LAST_24H.md)의 `pending_outbox_count`와 함께 봅니다.

## 운영 판독 기준

다음 조건이면 “현재 운영 경로는 신규 경로만 사용 중” 근거로 사용할 수 있습니다.

- 헤더에서 `runtime final owner = alarm-worker`
- 헤더에서 `big-bang enabled = true`
- 대상 라우트의 `activation = new_only`
- 대상 라우트의 `deployment = alarm-worker.youtube_outbox_dispatcher`
- 대상 라우트의 `actual = delivery_observed`

다음 상태는 즉시 합격 근거로 쓰면 안 됩니다.

- `pending_cutover`: 컷오버 전 상태
- `no_recent_posts`: 실제 사용 경로를 아직 관측하지 못한 상태
- `no_delivery_observed`: 게시물은 있었지만 outbox·delivery 근거가 빈 상태

`no_recent_posts` 가 많으면 관측 창을 늘려 다시 실행합니다.

## 근거 코드

- 신규 경로 상수: `hololive/hololive-alarm-worker/internal/egress/youtubeoutbox/telemetry/classifiers.go`
- 실제 경로 조회: `docs/current/runbooks/YOUTUBE_COMMUNITY_SHORTS_ROUTE_USAGE_LAST_24H.md`의 SQL (저장소 코드에는 경로별 조회 메서드가 없음)
- 최종 egress owner: `alarm-worker`

## 로컬 검증

- `go -C hololive/hololive-alarm-worker test ./internal/egress/youtubedispatch/...`

관련 참고:

- `docs/current/runbooks/YOUTUBE_COMMUNITY_SHORTS_TARGET_BASELINE.md`
- `docs/current/runbooks/YOUTUBE_COMMUNITY_SHORTS_ROUTE_USAGE_LAST_24H.md`
- `docs/current/runbooks/YOUTUBE_COMMUNITY_SHORTS_SEND_COUNTS_LAST_24H.md`
