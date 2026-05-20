# YouTube Community Shorts Send Counts

최근 24시간 recent window 또는 지정한 24시간 observation window 동안 실제 게시된 유튜브 커뮤니티/쇼츠 게시물별 알람 발송 횟수와 중복 여부를 운영자가 확인할 때 사용하는 조회 절차입니다.

## Scope

- 대상은 `COMMUNITY_POST`, `NEW_SHORT` 두 종류만 포함합니다.
- recent window 조회는 `youtube_content_alarm_tracking.actual_published_at` 를 우선 사용하고, 값이 비어 있으면 `detected_at` 으로 대체해 최근 24시간 게시물을 고릅니다.
- observation window 조회는 `runtime_name + bigbang_cutover_at` 로 식별한 `youtube_community_shorts_observation_windows` 레코드를 사용합니다. 관찰 구간이 아직 열려 있으면 누적 관찰 범위는 `[observation_started_at, min(now, observation_ended_at))` 이고, 관찰 구간이 닫힌 뒤에는 finalized baseline 기준 `[observation_started_at, observation_ended_at)` 전체를 재현합니다.
- `0회 발송` 게시물도 결과에 남아야 하므로 텔레메트리만이 아니라 추적 테이블(`youtube_content_alarm_tracking`)을 기준 집합으로 사용합니다.
- 게시물 식별자는 저장된 `youtube_notification_delivery_telemetry.post_id` 를 우선 사용하고, 비어 있으면 canonical `content_id` 로 대체합니다.

## Execute

recent window:

```bash
go run ./hololive/hololive-youtube-producer/cmd/ops/youtube-community-shorts send-counts -window 24h
go run ./hololive/hololive-youtube-producer/cmd/ops/youtube-community-shorts send-counts -window 24h -format json
```

observation window:

```bash
go run ./hololive/hololive-youtube-producer/cmd/ops/youtube-community-shorts send-counts \
  -observation-runtime youtube-producer \
  -observation-cutover 2026-04-10T00:00:00Z

go run ./hololive/hololive-youtube-producer/cmd/ops/youtube-community-shorts send-counts \
  -observation-runtime youtube-producer \
  -observation-cutover 2026-04-10T00:00:00Z \
  -format json
```

- 기본 출력은 Markdown 표이며, 운영자가 중복/누락 후보를 바로 읽을 수 있도록 `status`, `alarm_type`, `channel_id`, `post_id`, `actual_published_at`, `alarm_sent_at`, `delay_seconds`, 발송 횟수, room 수와 함께 `delay_source`, `publish_to_detect_ms`, `internal_delay_cause`, `queue_wait_ms`, `retry_accumulation_ms`, `job_failure_detected`를 함께 보여줍니다. observation query를 관찰 창이 열려 있는 동안 실행하면 같은 observation key의 누적 관찰치만 반환합니다.
- 요약 바로 아래에 `duplicate alarm verdict` 줄이 추가되며 운영 기준은 `duplicate_success_posts == 0` 입니다. 이 값이 `pass` 이고 `duplicate_posts = 0` 이어야 중복 알람 0건으로 판정합니다.
- `-window` recent query와 observation query는 서로 배타적입니다.
- `-observation-runtime`, `-observation-cutover` 는 반드시 함께 줘야 합니다.
- `-format json` 은 자동 수집이나 후처리에 사용할 수 있습니다. JSON row에는 `alarm_type`, `channel_id`, `post_id`, `actual_published_at`, `alarm_sent_at`, `delay_seconds` 가 함께 들어갑니다.

## Readout

- `status = ok`: 성공 발송이 room별 1회씩만 기록된 정상 상태입니다.
- `duplicate alarm verdict = pass`: 집계 윈도우에서 `duplicate_success_posts = 0` 이 확인된 상태입니다. 운영 기준의 중복 알람 0건 판정은 이 줄을 기준으로 합니다.
- `status = duplicate_success`: 같은 게시물에서 `success_send_count > success_room_count` 가 관측된 건입니다. 즉 최소 1개 room에 2회 이상 성공 발송이 남은 중복 알람입니다.
- `status = no_success`: 성공 발송이 없습니다. `last_event_at` 도 비어 있으면 시도 자체가 없었던 것입니다.
- `status = outbox_missing`: 게시물 추적 레코드는 있으나 outbox row가 만들어지지 않았습니다. 전송 파이프라인 진입 전 누락 후보입니다.
- `status = failed_attempts`: 성공은 있었지만 실패 또는 재시도 이벤트도 함께 남아 있습니다.
- `delay_source = external_collection`: 실제 게시 이후 최초 감지까지(`actual_published_at -> detected_at`)가 대표 지연 구간인 케이스입니다.
- `delay_source = internal_delivery`: 감지 이후 내부 전달(`detected_at -> alarm_sent_at`)이 더 큰 비중을 차지한 케이스입니다.
- `delay_source = mixed`: 외부 수집과 내부 전달 구간이 같은 크기로 지연에 기여했거나 둘 다 무시할 수 없게 관측된 케이스입니다.
- `actual_published_at`, `detected_at`, `alarm_sent_at`, `delay_seconds`: 실제 게시 시각, 내부 최초 감지 시각, canonical 알람 발송 시각, 초 단위 지연을 함께 기록합니다. `delay_seconds > 120` 이면 2분 초과 후보이며, 최종 판정은 `latency_classification.status` 로 함께 확인합니다.
