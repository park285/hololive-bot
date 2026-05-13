# YouTube Community/Shorts Target Baseline

## 목적

운영 `members` 기준으로 유튜브 커뮤니티/쇼츠 알람의 운영 채널 목록을 확정하고, `alarms` 테이블 기준 typed 활성 상태까지 함께 수집해 legacy 경로는 비활성화되고 신규 경로가 실제 활성인지, 아니면 cutover 대기 중인지 같은 JSON 기준 데이터로 검증합니다.

## 수집 명령

리포지토리 루트에서 다음 명령을 실행합니다.

```bash
go run ./hololive/hololive-stream-ingester/cmd/ops/youtube-community-shorts target-baseline
```

운영자용 Markdown 리포트가 필요하면 다음 명령을 사용합니다.

```bash
go run ./hololive/hololive-stream-ingester/cmd/ops/youtube-community-shorts route-report
```

필요 조건:
- `config.Load()`가 성공할 수 있도록 운영과 동일한 DB 환경 변수가 설정되어 있어야 합니다.
- 명령은 `members` 테이블을 읽어 활성 운영 채널을 계산합니다.
- 명령은 `alarms` 테이블을 읽어 채널별 `COMMUNITY`/`SHORTS` typed room 활성 수를 계산합니다.

## 출력 의미

- `runtime.final_delivery_owner`
  - `YOUTUBE_COMMUNITY_SHORTS_BIGBANG_ENABLED=true` 이면 `youtube-scraper`
  - 아니면 `stream-ingester`
- `path_mappings`
  - `legacy_alarm_queue` 는 `COMMUNITY`/`SHORTS` 에 대해 차단된 legacy 경로입니다.
  - `legacy_path_active=false` 이면 구 경로가 비활성화된 상태입니다.
  - `youtube_outbox_dispatcher` 가 현재 신규 경로입니다.
  - `new_path_configured=true` 이면 신규 경로가 최종 fan-out 경로로 배치된 상태입니다.
  - `cutover_pending=true` 이면 신규 owner는 배치되었지만 cutover 시각 전이라 활성화 판정을 내리면 안 됩니다.
  - `alarm_enabled_channel_count` 와 `alarm_enabled_room_count` 는 실제 typed 알람이 켜져 있는 운영 채널 수와 room 수입니다.
- `channels`
  - 활성 운영 채널별 `community_subscribers_key`, `shorts_subscribers_key` 기준 목록입니다.
  - `routes[].alarm_enabled` 는 해당 채널/타입 조합에 실제 typed 알람이 켜져 있는지 나타냅니다.
  - `routes[].effective_delivery_mode`
    - `new_only`: 해당 채널/타입 알람이 활성화되어 있고 신규 경로만 사용합니다.
    - `pending_cutover`: 알람 구독은 존재하지만 cutover 이전이라 신규 경로가 아직 활성화되지 않았습니다.
    - `disabled`: 해당 채널/타입 알람 구독이 없어 fan-out이 비활성화된 상태입니다.

## 기준 코드

- 운영 채널 SSOT: `hololive/hololive-stream-ingester/internal/app/channel_target_validation.go`
- baseline 수집: `hololive/hololive-stream-ingester/internal/app/community_shorts_target_baseline_build.go`
- typed key SSOT: `hololive/hololive-shared/pkg/service/alarm/keys/keys.go`
- legacy 차단: `hololive/hololive-shared/pkg/domain/alarm.go`
- 신규 경로 fan-out: `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher.go`
- cutover 라우팅 정책: `hololive/hololive-stream-ingester/internal/app/community_shorts_route_policy.go`
- 운영 런타임 owner: `hololive/hololive-stream-ingester/internal/app/bootstrap_stream_ingester.go`

## 검증

다음 조건을 만족하면 전체 운영 채널에서 구 경로 비활성화와 신규 경로 상태를 검증할 수 있습니다.

- 모든 `path_mappings[*].legacy_path_active` 가 `false`
- 모든 `path_mappings[*].new_path_configured` 가 `true`
- `cutover_pending=true` 인 항목이 하나라도 있으면 아직 “신규 경로 단일 활성” 완료로 판정하지 않음
- cutover가 끝난 상태에서는 모든 `channels[*].routes[*].effective_delivery_mode` 가 `new_only` 또는 `disabled` 중 하나
- 실제 활성화된 알람만 보려면 `channels[*].routes[*].alarm_enabled=true` 인 항목을 확인

로컬 검증 명령:

- `go test ./hololive/hololive-stream-ingester/internal/app -run '^TestBuildCommunityShortsTargetBaseline$'`
- `go test ./hololive/hololive-stream-ingester/cmd/...`
