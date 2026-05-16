# YouTube Community Shorts Post-Deploy First 24h Verification

`youtube-scraper` 빅뱅 배포 직후 24시간 동안 유튜브 커뮤니티/쇼츠 알람이 실제 게시 시각 기준 2분 SLA와 게시물당 정확히 1회 발송 조건을 유지하는지 확인하는 운영 검증 문서입니다.

## 검증 개요

| 항목 | 명시 내용 |
| --- | --- |
| 수행 기간 | 같은 `observation key` 로 고정한 `youtube_community_shorts_observation_windows` 1개 row의 `[observation_started_at, observation_ended_at)` 전체 24시간 구간을 대조 검증 대상으로 사용합니다. 관찰 창이 열려 있는 동안에는 같은 key로 누적 관찰치를 계속 수집하고, 창이 닫히면 finalized baseline 기준으로 동일한 24시간 구간 전체를 다시 대조합니다. |
| 데이터 원천 | 기준 집합은 `youtube-community-shorts target-baseline`, `youtube_community_shorts_observation_post_baselines`, `youtube-community-shorts alarm-sent-history-dataset` 을 사용합니다. 게시물별 exact-once 및 누락 판정은 `youtube-community-shorts send-counts`, `youtube-community-shorts delivery-logs`, `youtube_content_alarm_tracking`, `youtube_notification_delivery_telemetry` 를 사용하고, 운영 요약은 `youtube-community-shorts continuous-observation-report`, `youtube-community-shorts channel-summary` 로 확인합니다. SLA 대조는 `youtube-community-shorts latency-period-summary`, `youtube-community-shorts latency-cause-report` 를 함께 사용하고, `route-report`, `route-usage` 는 상호 검증용 보조 증적으로만 사용합니다. |
| 비교 범위 | 비교 대상은 `COMMUNITY_POST`, `NEW_SHORT` 두 알람 유형뿐입니다. 같은 observation key 안에서 `alarm_enabled = true` 인 운영 채널의 `channel_id + alarm_type` route subset만 포함하고, 게시물 대조 단위는 canonical key `alarm_type + channel_id + post_id` 로 고정합니다. 다른 알람 유형, 비대상 route, UI 변경 여부는 이 대조 검증 범위에 포함하지 않습니다. |
| 판정 기준 | SLA 시작점은 `actual_published_at` 이며 값이 없을 때만 `detected_at` 을 fallback 으로 사용합니다. exact-once 합격선은 각 canonical post key마다 final success 증적이 정확히 1회 존재하고 `duplicate_success_posts = 0`, `no_success = 0`, `outbox_missing = 0` 인 상태입니다. 내부 원인 기준 `actual_published_at -> alarm_sent_at` 지연이 2분을 넘기면 SLA 실패로 기록하되 알람은 늦더라도 정확히 1회 발송돼야 하며, 외부 수집 지연(`delay_source = external_collection`)은 기록 대상이지만 실패 판정에는 넣지 않습니다. |

## Scope

- 대상 알람은 `COMMUNITY_POST`, `NEW_SHORT` 만 포함합니다.
- 운영 검증 창은 배포 시점에 기록한 `youtube_community_shorts_observation_windows` 의 `[observation_started_at, observation_ended_at)` 24시간 구간을 기준으로 삼습니다.
- 게시물별 지연 시작점은 `actual_published_at` 이고, 실제 게시 시각이 비어 있을 때만 `detected_at` 을 fallback 으로 사용합니다.
- 2분 초과 건은 운영 알림이나 자동 중지 없이 계속 기록하고 추적합니다.
- 외부 수집 지연은 기록 대상이지만 실패 판정으로 보지 않습니다.
- 내부 원인으로 2분을 넘긴 경우에도 게시물은 늦더라도 정확히 1회 발송돼야 하며, 누락 판단은 success 증적이 생길 때까지 열어 둡니다.

## Verification Rhythm

- 배포 직후 `0h-1h`: `youtube-community-shorts continuous-observation-report -watch` 는 같은 observation key로 즉시 시작하고, 운영자는 `latest.md` 또는 `latest.json` 의 channel summary / send counts 섹션을 `5분` 간격으로 다시 확인합니다. 수동 재판독이 필요할 때는 `send-counts` 와 `latency-period-summary` 를 같은 observation key로 재수집합니다.
- 배포 후 `1h-24h`: continuous observation snapshot은 계속 누적하되 운영자 판독은 `15분` 간격으로 수행합니다.
- `route-report` 와 `route-usage` 확인은 배포 직후 즉시 1회 수행하고, 최근 게시물이 실제로 잡히기 전까지 `15분` 간격으로 다시 확인합니다.
- `delivery-logs` 는 `detectedUnsent`, `pending`, `duplicate`, `over_2m_posts` 같은 이상 징후가 보일 때 즉시 조회합니다.

## First 24h Step-By-Step Procedure

아래 절차는 배포 직후부터 `24시간` 동안 운영자가 같은 observation key를 유지한 채 로그와 지표의 누락 여부를 먼저 확인하고, 그 다음 게시물 누락/중복/SLA 상태를 닫는 순서입니다.

### 1. 배포 직후 observation key를 고정합니다

1. 배포 완료 시각을 `CUTOVER_AT` 으로 기록합니다.
2. 이후 `send-counts`, `delivery-logs`, `latency-cause-report` 등 observation query는 모두 같은 `-observation-runtime youtube-scraper -observation-cutover "$CUTOVER_AT"` 조합으로 실행합니다. 운영자는 가능하면 `youtube-community-shorts continuous-observation-report -watch` 를 먼저 켜서 같은 observation key의 baseline, channel summary, send counts, delivery logs, latency report snapshot을 계속 파일로 남깁니다.
3. `youtube-community-shorts continuous-observation-report -watch` 가 첫 snapshot을 썼는지 확인하고, `latest.md` 또는 `latest.json` 에 channel summary / send counts / latency report 섹션이 모두 보이는지 확인합니다.
4. `route-report` 를 먼저 1회 실행해 `runtime final owner = alarm-worker`, `big-bang enabled = true` 를 확인합니다.

### 2. 배포 직후 기준 스냅샷을 채집합니다

1. 같은 observation key로 `youtube-community-shorts continuous-observation-report` 를 1회 실행하거나 `-watch` 로 계속 수집합니다. 필요 시 `send-counts`, `delivery-logs`, `latency-period-summary`, `latency-cause-report` 를 개별 명령으로 재실행합니다.
2. `YOUTUBE_COMMUNITY_SHORTS_ROUTE_USAGE_LAST_24H.md` 의 route usage 조회를 1회 수행합니다.
3. 아래 다섯 가지 증적이 모두 준비된 상태에서만 누락 여부 판정을 시작합니다.

| Evidence source | Immediate check | Missing judgment |
| --- | --- | --- |
| `continuous-observation-report` snapshot | 같은 observation key의 `latest.*` 또는 새 snapshot 파일에 channel summary, send counts, latency report 섹션이 모두 기록됩니다. | snapshot이 생성되지 않거나, 필요한 섹션이 비어 있거나, 새 수집 시각이 `15분` 이상 갱신되지 않으면 `metric evidence missing` 으로 둡니다. |
| `send-counts` | 같은 observation key summary와 post row가 출력됩니다. | observation snapshot에 recent post/채널 변화가 보이는데 결과가 비어 있거나, summary row와 post row 수 불일치가 즉시 재실행 후에도 남아 있으면 `metric evidence missing` 으로 둡니다. |
| `delivery-logs` | 같은 observation key row가 나오고 `truncated = true` 가 아닙니다. | `send-counts` 에 send activity가 있는데 같은 post의 `delivery audit` 로그가 없거나, 결과가 잘린 상태면 `log evidence missing` 으로 둡니다. |
| `latency-period-summary` | `last_15m`, `last_1h`, `last_24h` period row가 모두 나옵니다. | 필요한 period row가 없거나 `exceededPostCount` 변화가 period summary와 맞지 않으면 `metric evidence missing` 으로 둡니다. |
| `latency-cause-report` | 같은 observation key의 `observation_window` row와 `over_2m` detail table이 나옵니다. | `observation_window` row가 없거나 `over_2m_posts` 와 detail row/원인 집계가 맞지 않으면 `metric evidence missing` 으로 둡니다. |
| `route-report` / `route-usage` | 신규 경로 증적이 조회되고 recent post의 path를 읽을 수 있습니다. | route report가 비어 있거나 recent post가 있는데 path 증적이 전혀 없으면 `route evidence missing` 으로 둡니다. |

4. 위 증적 중 하나라도 비어 있으면 즉시 같은 명령을 한 번 더 재실행하고, 여전히 비어 있으면 해당 시점 판정은 열어 둔 채 다음 주기에 다시 확인합니다.

### 3. 배포 후 첫 1시간은 5분 간격으로 반복 점검합니다

1. continuous observation snapshot의 channel summary / send counts / latency sections에서 `detectedPostCount`, `successPostCount`, `detectedUnsentPostCount`, `pendingPostCount`, `exceededPostCount` 성격의 집계를 먼저 읽습니다.
2. 아래 조건 중 하나라도 보이면 같은 observation key로 `send-counts` 를 즉시 다시 실행합니다.
   - `successPostCount < detectedPostCount`
   - `detectedUnsentPostCount > 0`
   - `pendingPostCount > 0`
   - `exceededPostCount > 0`
   - 채널 행 상태가 `미발송 추적 필요`, `미발송 + SLA 초과`, `실패 시도 존재`
3. `send-counts` 에서 이상 게시물이 보이면 그 `post_id` 를 기준으로 `delivery-logs` 를 즉시 실행해 final success, retry, duplicate 여부를 확인합니다.
4. `exceededPostCount > 0` 이면 `latency-period-summary` 를 다시 실행해 `last_15m` 과 `last_1h` 의 `over_2m_posts`, `pending_posts`, `max_latency_ms` 를 함께 기록하고, 같은 observation key로 `latency-cause-report` 를 실행해 `observation_window` 초과 목록과 `internal_system_cause_posts`, `excluded_external_delay_posts`, `queue_wait_cause_posts`, `retry_accumulation_cause_posts`, `job_failure_cause_posts` 를 함께 남깁니다. `excluded_external_delay_posts` 는 참고용 제외 건수로만 기록하고 실패 집계와 섞지 않습니다.
5. recent post가 신규 경로로 아직 확정되지 않았으면 `route-report` 또는 `route-usage` 를 다시 확인합니다.

### 4. 배포 후 1시간부터 24시간까지는 15분 간격으로 같은 절차를 반복합니다

1. continuous observation snapshot 누적 수집은 계속 유지합니다.
2. 운영자 판독 주기만 `15분` 으로 낮추고, 3단계의 조회 순서와 판정 순서를 그대로 유지합니다.
3. recent post가 충분히 쌓여 `actual = new_only_verified` 와 `delivery_path = youtube_outbox_dispatcher` 가 안정적으로 보일 때까지 route 증적 확인을 계속합니다.
4. 어느 시점이든 증적 누락이 다시 발생하면 그 시점 판정은 닫지 말고, 동일한 observation key로 재수집 후 이어서 확인합니다.

### 5. 게시물별 누락 후보와 로그·지표 누락을 같은 순서로 판정합니다

| Situation | Primary source | Judgment | Next action |
| --- | --- | --- | --- |
| `status = no_success` 또는 `status = outbox_missing` | `send-counts` | 게시물 누락 후보입니다. | 같은 `post_id` 로 `delivery-logs` 를 열고 final success 증적이 생길 때까지 열린 상태로 둡니다. |
| `status = failed_attempts` 이고 `last_success_at` 이 비어 있음 | `send-counts` | 아직 닫히지 않은 전달 후보입니다. | `delivery-logs` 에서 retry 이후 success가 생겼는지 확인합니다. success가 없으면 누락 후보를 유지합니다. |
| `duplicate alarm verdict != pass` 또는 `duplicate_success_posts > 0` | `send-counts` | exact-once 실패입니다. | 같은 `post_id + room_id` success row를 추적해 중복 성공이 사라졌는지 확인할 때까지 합격으로 닫지 않습니다. |
| `send-counts` 에 send activity가 있는데 `delivery audit` 로그가 없음 | `delivery-logs` | 로그 증적 누락입니다. | `-limit` 를 늘려 재조회하고, 그래도 없으면 `log evidence missing` 으로 기록한 채 판정을 보류합니다. |
| `delay_source = external_collection` 이고 final success가 존재함 | `send-counts`, `delivery-logs` | 외부 수집 지연 기록입니다. 실패 판정은 아닙니다. | 지연만 기록하고 exact-once 검증은 success 1회 여부로 닫습니다. |
| `delay_source = internal_delivery` 또는 `job_failure` 흔적이 있지만 final success가 정확히 1회 존재함 | `send-counts`, `delivery-logs` | 내부 원인 지연이지만 누락은 아닙니다. | `2분 초과 기록` 으로 남기고, duplicate가 없으면 exact-once 는 합격으로 닫습니다. |
| observation snapshot/period summary/route evidence 가 비어 있어 상호 검증이 안 됨 | `continuous-observation-report`, `latency-period-summary`, route docs | 지표 증적 누락입니다. | 즉시 재수집하고, 다음 주기에도 비어 있으면 해당 시점 판정을 열어 둡니다. |

### 6. 24시간 종료 시점에 최종 클로즈아웃을 수행합니다

1. `24시간` 시점에 `send-counts`, `delivery-logs`, `latency-period-summary`, `route-report`, route usage 조회를 한 번 더 전부 실행합니다.
2. 증적 누락 없이 조회가 끝났는지 먼저 확인합니다.
3. 그 다음 아래 두 조건을 동시에 만족할 때만 “배포 직후 24시간 동안 운영 검증 완료”로 닫습니다.
   - 이 문서의 `Omission Closure Rule` 을 모두 만족합니다.
   - 위 2단계 표의 `metric/log/route evidence missing` 상태가 하나도 남아 있지 않습니다.
4. 아직 열린 게시물 누락 후보나 증적 누락이 남아 있으면 24시간 창은 종료됐더라도 판정은 보류하고, final success 1회와 로그·지표 증적이 모두 확인될 때까지 계속 추적합니다.

## Metrics To Check For The First 24 Hours

| Metric / Signal | Primary Source | Unit | Expected Collection Cadence | Omission Judgment |
| --- | --- | --- | --- | --- |
| `detectedPostCount`, `successPostCount`, `detectedUnsentPostCount` | `YOUTUBE_COMMUNITY_SHORTS_CONTINUOUS_OBSERVATION_REPORT.md`, `YOUTUBE_COMMUNITY_SHORTS_CHANNEL_SUMMARY_LAST_24H.md` | posts | observation snapshot refresh `5m` for first hour then `15m`; channel summary on demand | `successPostCount < detectedPostCount` or `detectedUnsentPostCount > 0` 이면 누락 후보입니다. 즉시 `send-counts` 로 게시물 목록을 좁힙니다. |
| `pendingPostCount` | `YOUTUBE_COMMUNITY_SHORTS_CONTINUOUS_OBSERVATION_REPORT.md`, `YOUTUBE_COMMUNITY_SHORTS_SEND_COUNTS_LAST_24H.md` | posts | observation snapshot refresh `5m` for first hour then `15m`; send-counts on demand | `pendingPostCount > 0` 이면 아직 `alarm_sent_at` 가 닫히지 않은 게시물이 있다는 뜻입니다. 다음 수집 주기에도 같은 post가 남아 있으면 누락 후보를 계속 유지하고 success가 생길 때까지 닫지 않습니다. |
| `duplicate alarm verdict`, `duplicate_posts`, `duplicate_success_posts` | `YOUTUBE_COMMUNITY_SHORTS_SEND_COUNTS_LAST_24H.md` | verdict, posts | `5m` for first hour then `15m`, plus on demand when dashboard totals mismatch | 기대값은 `verdict = pass`, `duplicate_posts = 0`, `duplicate_success_posts = 0` 입니다. 중복은 누락과 별도 실패이지만, 같은 post가 `status != ok` 이면 발송 상태를 미해결로 유지합니다. |
| per-post `status` (`ok`, `no_success`, `outbox_missing`, `failed_attempts`, `duplicate_success`) | `YOUTUBE_COMMUNITY_SHORTS_SEND_COUNTS_LAST_24H.md` | post status | `5m` for first hour then `15m`, plus on demand on anomaly | `no_success` 또는 `outbox_missing` 은 즉시 누락 후보입니다. `failed_attempts` 는 최종 success가 있으면 회복된 시도로 보되, success가 없으면 누락 후보로 유지합니다. |
| `exceededPostCount`, `averageLatencyMillis`, `maxLatencyMillis` | `YOUTUBE_COMMUNITY_SHORTS_CONTINUOUS_OBSERVATION_REPORT.md`, `YOUTUBE_COMMUNITY_SHORTS_LATENCY_PERIOD_SUMMARY.md` | posts, milliseconds | observation snapshot refresh `5m` for first hour then `15m`; latency summary `5m` for first hour then `15m` | 2분 초과는 누락 자체가 아니라 지연 기록입니다. 다만 같은 post가 `exceeded` 이면서 success가 없으면 누락 후보로 계속 추적합니다. |
| `publish_to_detect_ms`, `publish_to_event_ms`, `delay_source`, `internal_delay_cause` | `YOUTUBE_COMMUNITY_SHORTS_SEND_COUNTS_LAST_24H.md`, `YOUTUBE_COMMUNITY_SHORTS_DELIVERY_LOGS.md` | milliseconds, enums | on demand whenever a post is pending, unsent, or over 2 minutes | `delay_source = external_collection` 단독이면 누락으로 보지 않습니다. `internal_delivery` 또는 `job_failure` 흔적과 함께 final success가 없으면 누락 후보를 유지합니다. |
| `internal_system_cause_posts`, `excluded_external_delay_posts`, `queue_wait_cause_posts`, `retry_accumulation_cause_posts`, `job_failure_cause_posts`, over-2m detail rows | `YOUTUBE_COMMUNITY_SHORTS_LATENCY_CAUSE_REPORT.md` | posts, milliseconds, enums | on demand whenever `exceededPostCount > 0` or final 24h closeout | `excluded_external_delay_posts` 는 외부 시스템 지연 참고 건수이며 실패 집계에는 넣지 않습니다. observation key의 `observation_window` row가 없거나, 초과 게시물 detail row와 집계 bucket이 맞지 않으면 2분 초과 판정을 닫지 않습니다. |
| `actual`, `observed_paths`, `delivery_path` | `YOUTUBE_COMMUNITY_SHORTS_CHANNEL_ROUTE_VERIFICATION.md`, `YOUTUBE_COMMUNITY_SHORTS_ROUTE_USAGE_LAST_24H.md` | route state, path count | immediately after deploy, then `15m` until recent posts are observed on the new path | 경로 이상 자체가 누락 확정은 아닙니다. 다만 `new_only_verified` 가 아니거나 `youtube_outbox_dispatcher` 외 경로가 보이면 같은 post를 `send-counts` 와 묶어 누락 후보 여부를 다시 판정합니다. |

## Correlation Keys And Mapping Rules

운영 검증은 항상 하나의 `observation key` 안에서 `게시물 식별자 -> fan-out 이벤트 -> room 시도 -> 집계 지표` 순서로 추적합니다. recent window 결과와 observation window 결과를 섞지 말고, 같은 상관 키를 끝까지 유지합니다.

### 1. Canonical Correlation Keys

| Layer | Canonical key | Primary source | Mapping rule |
| --- | --- | --- | --- |
| Observation window | `observation_runtime_name + observation_bigbang_cutover_at` | `youtube_community_shorts_observation_windows`, `youtube_notification_delivery_telemetry` | `-observation-runtime`, `-observation-cutover` 와 동일한 값입니다. `send-counts`, `delivery-logs`, `route-report` 를 모두 같은 observation key로 고정합니다. |
| Post identity | `alarm_type + channel_id + post_id` | `YOUTUBE_COMMUNITY_SHORTS_SEND_COUNTS_LAST_24H.md`, `YOUTUBE_COMMUNITY_SHORTS_DELIVERY_LOGS.md`, `YouTube community/shorts delivery audit` 로그 | 운영자가 보는 canonical 게시물 키입니다. 같은 `post_id` 라도 `alarm_type` 과 `channel_id` 가 다르면 다른 게시물로 취급합니다. |
| Tracking fallback | `kind + content_id` | `youtube_content_alarm_tracking` | raw DB row에서 `post_id` 가 바로 안 보일 때 쓰는 내부 조인 키입니다. `COMMUNITY_POST -> COMMUNITY`, `NEW_SHORT -> SHORTS` 로 변환한 뒤 운영 post key에 대응시킵니다. |
| Fan-out root | `outbox_id` | `youtube_notification_outbox`, `youtube_notification_delivery_telemetry` | 게시물 1건의 room fan-out 묶음입니다. 같은 게시물의 발송 시도 로그를 묶을 때는 먼저 `outbox_id` 로 모읍니다. |
| Room attempt | `delivery_id + attempt_ordinal` | `youtube_notification_delivery_telemetry` | 같은 room delivery의 재시도 체인을 구분하는 키입니다. retry는 허용되지만, 같은 `post_id + room_id` 에 success가 2건 이상이면 duplicate입니다. |

- `post_id` 해석 규칙:
  - 커뮤니티는 `youtube_community_posts.post_id` 를 그대로 canonical ID로 사용합니다.
  - 쇼츠는 텔레메트리 적재 시 `canonical_post_id -> post_id -> video_id -> outbox.content_id -> telemetry.content_id` 순서로 채운 값을 canonical ID로 사용합니다.
  - `send-counts` 와 `delivery-logs` 에서 보이는 `post_id` 는 위 정규화가 끝난 값으로 읽습니다.
- `dedupe_key` 는 `youtube-notification:<kind>:<content_id>` 규칙으로 생성됩니다. 운영자가 보는 canonical post key는 `post_id` 이지만, dedupe root가 흔들렸는지 확인할 때는 `dedupe_key` 와 `outbox_id` 를 함께 봅니다.

### 2. Metric-To-Event Mapping

| Metric / Decision | Metric source | Post correlation key | Event evidence to join | Mapping rule |
| --- | --- | --- | --- | --- |
| `detectedPostCount` | continuous observation report, channel summary | `alarm_type + channel_id + post_id` | `send-counts` 의 post row 1건 | 같은 observation key에서 summary 집계는 `send-counts` row 수와 1:1로 대응해야 합니다. 게시물 기준 시각은 `COALESCE(actual_published_at, detected_at)` 입니다. |
| `alarmSentPostCount` | continuous observation report, channel summary | same post key | `outbox_id` 존재 또는 delivery telemetry 활동 존재 | 게시물이 전송 파이프라인에 진입했는지 보는 지표입니다. `outbox_count > 0` 이거나 success/failure attempt 흔적이 있으면 send activity가 있다고 봅니다. |
| `successPostCount` | continuous observation report, channel summary | same post key | `delivery audit` success 또는 `last_success_at` | 이 값은 “최소 1회 성공이 있었는가”를 뜻합니다. 정확히 1회 보장은 이 지표만으로 닫지 말고 `duplicate alarm verdict` 를 반드시 같이 봅니다. |
| `duplicate alarm verdict`, `duplicate_success_posts` | `send-counts` | same post key + `room_id` | success audit rows grouped by `post_id + room_id` | exact-once 합격선입니다. 같은 게시물에서 `success_send_count > success_room_count` 이면 duplicate로 판정합니다. |
| `detectedUnsentPostCount` | continuous observation report, channel summary | same post key | `send-counts.status in (no_success, outbox_missing)` | final success 증적이 아직 없는 게시물 후보를 좁히는 첫 단계입니다. 이 값이 0이 아니면 반드시 post row 목록으로 내려갑니다. |
| `pendingPostCount` | continuous observation report, send-counts | same post key | tracking row `alarm_sent_at`, final result log | `pending` 은 tracking closeout 기준입니다. `send-counts` 에서 먼저 `last_success_at` 이 비어 있는 post를 보고, 값이 어긋나면 tracking row와 `outbox_final_result` 로그로 최종 닫힘 여부를 확인합니다. |
| `exceededPostCount` | continuous observation report, latency summary | same post key | `latency_classification.status = exceeded`, `latency_classification.*` | 2분 초과 post를 좁히는 지표입니다. 원인 판정은 `delay_source`, `internal_delay_cause`, `publish_to_detect_ms`, `queue_wait_ms`, `retry_accumulation_ms`, `job_failure_detected` 로 이어서 읽습니다. |
| external vs internal delay 판정 | `send-counts`, `delivery-logs` | same post key | `telemetry_source = outbox_final_result` + `latency_classification.delay_source` | `external_collection` 단독이면 기록 대상이지 실패 판정은 아닙니다. `internal_delivery` 또는 `job_failure` 면 늦더라도 final success 1회 증적이 반드시 있어야 합니다. |

### 3. Operator Trace Order

1. 먼저 observation key를 고정합니다. `send-counts`, `delivery-logs`, `route-report` 에 서로 다른 `cutover` 를 섞지 않습니다.
2. 대시보드에서 이상 지표를 찾으면 같은 observation key로 `send-counts` 를 열고 `alarm_type + channel_id + post_id` 단위로 게시물 목록을 좁힙니다.
3. 문제가 된 post는 `delivery-logs` 또는 raw `delivery audit` 로그에서 같은 `post_id` 를 찾고, 그 안에서 `outbox_id` 로 fan-out 묶음을 확인한 뒤 `delivery_id + attempt_ordinal` 로 재시도 체인을 풉니다.
4. exact-once 판정은 항상 `post_id + room_id` success row 개수로 닫습니다. success가 여러 번 있어도 같은 room이면 duplicate입니다.
5. 2분 초과 post는 `latency_classification.status = exceeded` 를 기준으로만 묶고, `delay_source = external_collection` 은 기록만 남기며 실패로 닫지 않습니다.
6. 누락 판정은 `successPostCount == detectedPostCount` 와 `duplicate_success_posts == 0` 이 동시에 만족할 때만 닫습니다. 둘 중 하나라도 어긋나면 post row 기준 추적을 유지합니다.

## Omission Closure Rule

아래 조건을 모두 만족해야 “배포 직후 24시간 동안 누락 0건”으로 닫습니다.

1. continuous observation snapshot 또는 channel summary 기준 `successPostCount == detectedPostCount`, `detectedUnsentPostCount == 0`, `pendingPostCount == 0`
2. `send-counts` 기준 `duplicate alarm verdict = pass`, `duplicate_success_posts = 0`
3. `send-counts` 결과에 `status = no_success` 또는 `status = outbox_missing` 인 post 가 없음
4. 2분 초과 post 는 모두 `latency-cause-report` 또는 `delivery-logs`/`send-counts` 로 원인이 기록돼 있고, final success 증적이 존재함

`exceededPostCount > 0` 만으로 누락으로 판정하지는 않습니다. 누락 판정은 언제나 “감지된 post에 final success 증적이 끝까지 생기지 않았는가”로 닫습니다.

## Recommended Commands

배포 시점의 observation key를 이미 기록해 두었다면 아래 명령을 기준 절차로 사용합니다.

```bash
go run ./hololive/hololive-stream-ingester/cmd/ops/youtube-community-shorts continuous-observation-report \
  -observation-runtime youtube-scraper \
  -observation-cutover <CUTOVER_AT> \
  -watch

go run ./hololive/hololive-stream-ingester/cmd/ops/youtube-community-shorts send-counts \
  -observation-runtime youtube-scraper \
  -observation-cutover <CUTOVER_AT>

go run ./hololive/hololive-stream-ingester/cmd/ops/youtube-community-shorts delivery-logs \
  -observation-runtime youtube-scraper \
  -observation-cutover <CUTOVER_AT>

go run ./hololive/hololive-stream-ingester/cmd/ops/youtube-community-shorts latency-cause-report \
  -observation-runtime youtube-scraper \
  -observation-cutover <CUTOVER_AT>

go run ./hololive/hololive-stream-ingester/cmd/ops/youtube-community-shorts route-report -window=24h

go run ./hololive/hololive-stream-ingester/cmd/ops/youtube-community-shorts latency-period-summary \
  -period last_15m=15m \
  -period last_1h=1h \
  -period last_24h=24h
```

운영 요약은 `youtube-community-shorts continuous-observation-report` snapshot과 `youtube-community-shorts channel-summary` 결과를 사용합니다.
route usage 상세 SQL은 `YOUTUBE_COMMUNITY_SHORTS_ROUTE_USAGE_LAST_24H.md` 절차를 그대로 사용합니다.

## Audit Trail

사후 감사에서 같은 판정을 재현하려면 운영자는 같은 observation key마다 검증 실행 기록과 증적 위치를 함께 남겨야 합니다. 감사 추적은 `youtube-community-shorts continuous-observation-report` 의 산출물 루트를 기준 보관 경로로 삼고, KST 운영 기록이 필요하더라도 observation key와 재조인을 위해 UTC 원문(`RFC3339`)을 함께 남깁니다.

| Audit field | Required record |
| --- | --- |
| 검증 실행 일시 | `verification_started_at_kst`, `verification_started_at_utc`, `verification_closed_at_kst`, `verification_closed_at_utc` 를 모두 기록합니다. 중간 재실행이 있으면 같은 row에 append 하거나 별도 row를 추가합니다. |
| 작성자 | 실제 검증 실행자 이름 또는 핸들을 남기고, 별도 검토자가 있으면 `reviewer` 로 함께 적습니다. |
| observation key | `runtime_name`, `bigbang_cutover_at`, 가능하면 `observation_window_id` 까지 남깁니다. 모든 쿼리와 증적은 이 key와 같은 값으로 재조회 가능해야 합니다. |
| 보관 루트 | 기본 경로는 `artifacts/youtube-community-shorts-continuous-observation/<runtime>-<cutover>/` 를 사용합니다. 수동 경로를 썼다면 실제 절대/상대 경로를 그대로 남깁니다. |
| 쿼리 증적 위치 | 실행한 CLI, SQL, dashboard 재확인 시각, 재실행 이유를 `audit/queries.md` 또는 동등한 파일에 원문 그대로 저장합니다. 명령 옵션과 `-observation-runtime`, `-observation-cutover` 값이 빠지면 안 됩니다. |
| 로그 증적 위치 | `delivery audit` 추출본, 필요한 서비스 로그, `rg`/`grep` 재조회 결과를 `audit/logs/` 아래 또는 동등 경로에 저장하고 사용한 파일명을 적습니다. |
| 화면/렌더 증적 위치 | route 화면 또는 rendered snapshot을 판정 근거로 썼다면 `audit/screenshots/` 아래 캡처 파일 경로와 캡처 시각을 남깁니다. 사용하지 않았으면 `not used` 를 명시합니다. |
| snapshot 증적 위치 | 판정에 사용한 `latest.md`, `latest.json`, `snapshot-YYYYMMDD-HHMMSS.*` 파일명을 남깁니다. final closeout은 finalized snapshot 파일을 반드시 포함합니다. |
| 최종 판정 요약 | `exact_once_verdict`, `internal_over_2m_verdict`, `external_collection_delay_posts`, `open_missing_candidates`, `open_evidence_gaps` 를 함께 적어 최종 closeout 당시 열린 항목이 있었는지 재현 가능하게 남깁니다. |

감사 추적 파일은 배포 직후 시작 시점에 1회 생성하고, 첫 1시간 집중 관찰 종료 시점과 24시간 최종 closeout 시점에 반드시 갱신합니다. 증적 누락으로 판정을 열어 둔 경우에는 그 상태와 후속 재수집 시각도 같은 파일에 이어서 남깁니다.

권장 보관 레이아웃:

- `artifacts/youtube-community-shorts-continuous-observation/<runtime>-<cutover>/audit/audit-trail.md`: 검증 실행 row와 최종 판정 요약
- `artifacts/youtube-community-shorts-continuous-observation/<runtime>-<cutover>/audit/queries.md`: 실제 실행한 CLI/SQL/query 원문과 재실행 사유
- `artifacts/youtube-community-shorts-continuous-observation/<runtime>-<cutover>/audit/logs/`: delivery audit 발췌, 서비스 로그, 검색 결과
- `artifacts/youtube-community-shorts-continuous-observation/<runtime>-<cutover>/audit/screenshots/`: dashboard 및 route 검증 캡처
- `artifacts/youtube-community-shorts-continuous-observation/<runtime>-<cutover>/latest.*`, `snapshot-*.{md,json}`: continuous observation snapshot 원본

권장 감사 추적 템플릿:

```yaml
verification_started_at_kst: <YYYY-MM-DD HH:MM:SS KST>
verification_started_at_utc: <YYYY-MM-DDTHH:MM:SSZ>
verification_closed_at_kst: <YYYY-MM-DD HH:MM:SS KST>
verification_closed_at_utc: <YYYY-MM-DDTHH:MM:SSZ>
author: <name-or-handle>
reviewer: <name-or-handle-or-n/a>
observation_key:
  runtime_name: youtube-scraper
  bigbang_cutover_at: <CUTOVER_AT RFC3339 UTC>
  observation_window_id: <id-or-n/a>
artifact_root: artifacts/youtube-community-shorts-continuous-observation/<runtime>-<cutover>/
query_evidence:
  - audit/queries.md
snapshot_evidence:
  - latest.md
  - snapshot-YYYYMMDD-HHMMSS.md
log_evidence:
  - audit/logs/<file>
screenshot_evidence:
  - audit/screenshots/<file-or-not-used>
final_verdict:
  exact_once_verdict: <pass|fail|open>
  internal_over_2m_verdict: <pass|fail|open>
  external_collection_delay_posts: <count>
  open_missing_candidates: <count-or-none>
  open_evidence_gaps: <count-or-none>
notes: <rerun reason, unresolved items, follow-up timestamp>
```

## Related Runbooks

- `YOUTUBE_COMMUNITY_SHORTS_CONTINUOUS_OBSERVATION_REPORT.md`
- `YOUTUBE_COMMUNITY_SHORTS_CHANNEL_SUMMARY_LAST_24H.md`
- `YOUTUBE_COMMUNITY_SHORTS_SEND_COUNTS_LAST_24H.md`
- `YOUTUBE_COMMUNITY_SHORTS_LATENCY_PERIOD_SUMMARY.md`
- `YOUTUBE_COMMUNITY_SHORTS_LATENCY_CAUSE_REPORT.md`
- `YOUTUBE_COMMUNITY_SHORTS_DELIVERY_LOGS.md`
- `YOUTUBE_COMMUNITY_SHORTS_ROUTE_USAGE_LAST_24H.md`
- `YOUTUBE_COMMUNITY_SHORTS_CHANNEL_ROUTE_VERIFICATION.md`
