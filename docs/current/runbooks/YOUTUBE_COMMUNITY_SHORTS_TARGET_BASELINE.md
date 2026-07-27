# YouTube Community/Shorts Target Baseline

## 목적

운영 `members` 기준으로 유튜브 커뮤니티/쇼츠 알람의 운영 채널 목록을 확정하고, `alarms` 테이블 기준 typed 활성 상태와 canonical delivery owner/path/mode를 JSON 기준 데이터로 검증합니다.

## 수집 명령

리포지토리 루트에서 다음 명령을 실행합니다.

```bash
go run ./hololive/hololive-youtube-producer/cmd/ops/youtube-community-shorts target-baseline
```

운영자용 Markdown 리포트가 필요하면 다음 명령을 사용합니다.

```bash
go run ./hololive/hololive-youtube-producer/cmd/ops/youtube-community-shorts route-report
```

필요 조건:
- `settings.LoadYouTubeProducerRuntime()`가 성공할 수 있도록 운영과 동일한 DB 환경 변수가 설정되어 있어야 합니다.
- 명령은 `members` 테이블을 읽어 활성 운영 채널을 계산합니다.
- 명령은 `alarms` 테이블을 읽어 채널별 `COMMUNITY`/`SHORTS` typed room 활성 수를 계산합니다.

## 출력 의미

- `runtime.final_delivery_owner`
  - `alarm-worker`
- `path_mappings`
  - `final_delivery_owner=alarm-worker` 와 `final_delivery_path=alarm-worker.youtube_outbox_dispatcher` 가 canonical fan-out 계약입니다.
  - `effective_delivery_mode` 는 typed room 활성 상태를 `new_only`, `pending_cutover`, `disabled` 중 하나로 나타냅니다.
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

- 운영 채널 SSOT: `hololive/hololive-youtube-producer/internal/communityshorts/target_baseline.go`
- baseline 수집: `hololive/hololive-youtube-producer/internal/communityshorts/target_baseline.go`
- typed key SSOT: `hololive/hololive-shared/pkg/service/alarm/keys/keys.go`
- canonical 경로 fan-out: `hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatch/dispatcher.go`
- cutover 라우팅 정책: `hololive/hololive-youtube-producer/internal/communityshorts/route_policy.go`
- 운영 런타임 owner: `hololive/hololive-youtube-producer/internal/runtime/internal/producerruntime/bootstrap_youtube_producer.go`

## 검증

다음 조건을 만족하면 전체 운영 채널의 canonical 경로 상태를 검증할 수 있습니다.

- 모든 `path_mappings[*].new_path_configured` 가 `true`
- 모든 `path_mappings[*].final_delivery_owner` 와 `final_delivery_path` 가 현재 runtime 계약과 일치
- `cutover_pending=true` 인 항목이 하나라도 있으면 아직 “신규 경로 단일 활성” 완료로 판정하지 않음
- cutover가 끝난 상태에서는 모든 `channels[*].routes[*].effective_delivery_mode` 가 `new_only` 또는 `disabled` 중 하나
- 실제 활성화된 알람만 보려면 `channels[*].routes[*].alarm_enabled=true` 인 항목을 확인

로컬 검증 명령:

- `go test ./hololive/hololive-youtube-producer/internal/communityshorts -run '^TestBuildTargetBaseline$'`
- `go test ./hololive/hololive-youtube-producer/cmd/...`
