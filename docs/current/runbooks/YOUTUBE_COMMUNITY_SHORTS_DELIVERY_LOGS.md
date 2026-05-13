# YouTube Community Shorts Delivery Logs

최근 구간 또는 지정한 관찰 구간의 유튜브 커뮤니티/쇼츠 알람 발송 로그만 별도로 조회할 때 사용하는 운영 절차입니다.

## Scope

- 대상은 `COMMUNITY`, `SHORTS` 발송 로그만 포함합니다.
- 최근 조회의 기준 시각은 `youtube_notification_delivery_telemetry.actual_published_at` 이며, 값이 없으면 `detected_at`, 둘 다 없으면 `event_at` 으로 대체합니다.
- 지정 관찰 구간 조회는 `runtime_name + bigbang_cutover_at` 로 식별한 `youtube_community_shorts_observation_windows` 레코드를 사용합니다. 관찰 창이 열려 있으면 현재까지 누적된 matched 로그만 반환하고, 관찰 창이 닫힌 뒤에는 finalized baseline 기준으로 종료 이후 최초 감지 게시물을 제외한 최종 로그 집합을 반환합니다.
- 결과는 발송 시도 로그 단위이며, 성공/실패 시도와 재시도 흔적이 모두 유지됩니다.

## Canonical Validation Log

- 운영 검증의 기준 원시 로그는 `message="YouTube community/shorts delivery audit"` 구조화 로그입니다.
- `telemetry_source = persistent_buffer` 또는 `direct_fallback` 는 시도 단위 감사 로그입니다. 내부 버퍼 flush 성공 여부만 다르고 필드 의미는 같습니다.
- `telemetry_source = outbox_final_result` 는 게시물 단위 최종 결과 로그입니다. 이 라인에서 `latency_classification.*` 로 2분 SLA 판정과 내부/외부 지연 분류를 읽습니다.
- `message="YouTube community/shorts delivery result"` 와 `message="YouTube community/shorts delivery attempt started"` 는 보조 근거입니다. 합격 판정과 중복/누락 판단은 `delivery audit` 로그를 우선 사용합니다.
- 이 runbook의 조회 결과에서 `event_at` 컬럼은 원시 로그의 `sent_at` 를 정규화해 보여 주는 값입니다.
- 이 runbook의 조회 결과에서 `publish_to_event_ms` 는 저장 필드가 아니라 `actual_published_at -> event_at` 차이를 계산한 파생값입니다.

## Required Validation Fields

운영 검증 시 아래 필드는 반드시 함께 확인합니다.

### 1. 공통 필수 필드

| Field | Meaning | Type | Example |
| --- | --- | --- | --- |
| `message` | 검증 대상 로그 식별자. `delivery audit` 로그만 합격 판정의 기준으로 사용합니다. | `string` | `YouTube community/shorts delivery audit` |
| `telemetry_source` | 로그가 어디서 방출됐는지 구분합니다. 시도 로그인지 최종 결과 로그인지 판정할 때 필요합니다. | `string enum` | `persistent_buffer` |
| `alarm_type` | 대상 알람 유형입니다. 범위 밖 알람이 섞이지 않았는지 확인합니다. | `string enum` | `COMMUNITY` |
| `channel_id` | YouTube 채널 식별자입니다. 채널별 집계와 누락 판정 키로 사용합니다. | `string` | `UC1DCedRgGHBdm81E1llLhOQ` |
| `post_id` | 게시물 기준 canonical 식별자입니다. 정확히 1회 발송 검증의 기본 키입니다. | `string` | `UgkxExampleCanonicalPostId12345` |
| `content_id` | 원본 콘텐츠 식별자입니다. `post_id` 해석이 애매할 때 역추적용으로 사용합니다. | `string` | `dQw4w9WgXcQ` |
| `room_id` | 실제 발송 대상 Kakao room 식별자입니다. 룸 단위 중복 발송 여부를 확인합니다. | `string` | `4130277163930951` |
| `delivery_id` | room 단위 delivery row 식별자입니다. 동일 시도의 중복 로그인지 새 delivery row인지 구분합니다. | `integer(int64)` | `182345` |
| `outbox_id` | 게시물 fan-out의 상위 outbox 식별자입니다. 게시물 단위 최종 결과와 연결할 때 사용합니다. | `integer(int64)` | `98123` |
| `dedupe_key` | 중복 방지 키입니다. 같은 게시물이 같은 dedupe key로만 발송되는지 확인합니다. | `string` | `youtube-notification:COMMUNITY_POST:UgkxExampleCanonicalPostId12345` |
| `attempt_ordinal` | 해당 `delivery_id` 의 몇 번째 시도인지 나타냅니다. 재시도 누적과 최종 성공 이전 실패 이력을 읽을 때 필요합니다. | `integer` | `1` |
| `send_result` | 해당 로그가 성공인지 실패인지 나타냅니다. 성공 로그는 게시물-룸 조합당 정확히 1건이어야 합니다. | `string enum` | `success` |
| `delivery_path` | 실제 발송 경로입니다. 운영 목표값은 신규 경로 `youtube_outbox_dispatcher` 하나입니다. | `string` | `youtube_outbox_dispatcher` |
| `delivery_mode` | 발송 모드입니다. grouped fan-out, 복구 backfill, 최종 결과 로그를 구분합니다. | `string enum` | `grouped` |
| `actual_published_at` | 실제 유튜브 게시 시각입니다. 내부 지연 2분 계산의 시작점입니다. | `RFC3339 timestamp string` | `2026-04-10T00:01:10Z` |
| `detected_at` | 스크래퍼가 게시물을 최초 감지한 시각입니다. 외부 수집 지연과 내부 지연을 분리할 때 사용합니다. | `RFC3339 timestamp string` | `2026-04-10T00:01:42Z` |
| `sent_at` | 해당 감사 로그가 가리키는 발송 완료 또는 실패 시점입니다. 이 runbook의 조회 결과에서는 `event_at` 로 표시됩니다. | `RFC3339 timestamp string` | `2026-04-10T00:02:05Z` |
| `observation_status` | 지정 관찰 구간에 속하는지 여부입니다. big-bang 적용 후 검증 창 포함 여부를 판정합니다. | `string enum` | `matched` |
| `failure_reason` | 실패 시도일 때의 축약 원인입니다. 실패 후 재시도/최종 성공 여부를 해석할 때 사용합니다. 성공 로그에서는 비어 있을 수 있습니다. | `string` | `send message` |

### 2. 관찰 구간 매칭 필드

| Field | Meaning | Type | Example |
| --- | --- | --- | --- |
| `observation_runtime_name` | 매칭된 24시간 관찰 구간의 runtime 이름입니다. 전체 운영 채널이 `youtube-scraper` 로 수렴했는지 확인합니다. | `string` | `youtube-scraper` |
| `observation_bigbang_cutover_at` | 관찰 구간을 식별하는 big-bang cutover 시각입니다. 같은 운영 전환 창 로그만 묶을 때 사용합니다. | `RFC3339 timestamp string` | `2026-04-10T00:00:00Z` |
| `observation_started_at` | 매칭된 관찰 구간 시작 시각입니다. | `RFC3339 timestamp string` | `2026-04-10T00:00:00Z` |
| `observation_ended_at` | 매칭된 관찰 구간 종료 시각입니다. | `RFC3339 timestamp string` | `2026-04-11T00:00:00Z` |

`observation_status` 값 해석:

| Value | Meaning |
| --- | --- |
| `matched` | 실제 게시 시각 기준으로 현재 관찰 구간에 포함됩니다. |
| `outside_observation_window` | 실제 게시 시각은 있으나 현재 관찰 구간 밖입니다. |
| `missing_actual_published_at` | 실제 게시 시각이 비어 있어 SLA 시작점을 확정하지 못했습니다. |
| `tracking_not_found` | tracking row를 찾지 못해 게시물 기준 정보 결합에 실패했습니다. |
| `unclassified` | 분류 전 또는 backfill 보완 전 상태입니다. |

### 3. 2분 SLA 판정 필드

`telemetry_source = outbox_final_result` 인 `delivery audit` 로그에서는 아래 하위 필드를 추가로 확인합니다.

| Field | Meaning | Type | Example |
| --- | --- | --- | --- |
| `latency_classification.status` | 최종 지연 판정 결과입니다. 2분 초과 여부를 직접 판정합니다. | `string enum` | `within_target` |
| `latency_classification.threshold_millis` | 합격선 임계값입니다. 현재 고정값은 2분 = 120000ms 입니다. | `integer(int64)` | `120000` |
| `latency_classification.delay_source` | 2분 초과 주원인이 외부 수집인지 내부 전달인지 구분합니다. | `string enum` | `internal_delivery` |
| `latency_classification.internal_delay_cause` | 내부 전달 지연일 때 대표 원인을 표준화한 값입니다. | `string enum` | `queue_wait` |
| `latency_classification.reason_code` | 외부 원인과 내부 원인을 단일 코드로 바로 구분하기 위한 운영용 사유 코드입니다. | `string enum` | `external_collection` |
| `latency_classification.evidence` | 판정 근거 세부 목록입니다. `millis` 또는 `bool` 값으로 세부 원인을 설명합니다. | `array<object>` | `[{"key":"alarm_latency","millis":55000,"selected":true}]` |

`latency_classification.status` 값 해석:

| Value | Meaning |
| --- | --- |
| `within_target` | 실제 게시 시각 기준 2분 이내입니다. |
| `exceeded` | 실제 게시 시각 기준 2분을 초과했습니다. 내부 원인이면 늦더라도 1회 발송돼야 하며 이 값은 기록용입니다. |
| `insufficient_evidence` | 실제 게시 시각 또는 최종 성공 시각이 부족해 확정 판정을 내릴 수 없습니다. |

`latency_classification.delay_source` 값 해석:

| Value | Meaning |
| --- | --- |
| `none` | 2분 초과가 아니거나 지연 원인을 특정할 필요가 없는 상태입니다. |
| `external_collection` | YouTube 노출/스크래핑 감지 지연이 우세한 상태입니다. 실패 판정으로 보지 않습니다. |
| `internal_delivery` | 내부 큐 적체, 재시도 누적, job failure 등 내부 전달 지연이 우세한 상태입니다. |
| `mixed` | 외부 수집과 내부 전달 지연이 함께 크게 관측된 상태입니다. |

`latency_classification.reason_code` 값 해석:

| Value | Meaning |
| --- | --- |
| `external_collection` | 외부 시스템 노출 또는 스크래핑 감지 지연이 대표 원인입니다. 내부 실패로 판정하지 않습니다. |
| `mixed` | 외부 수집 지연과 내부 전달 지연이 함께 크게 관측됐습니다. |
| `internal_delivery` | 내부 전달 구간 지연이 관측됐지만 세부 원인은 특정되지 않았습니다. |
| `queue_wait` | 내부 큐 대기 또는 첫 시도 시작 전 적체가 대표 원인입니다. |
| `retry_accumulation` | 내부 재시도 누적이 대표 원인입니다. |
| `job_failure` | 내부 발송 job 실패 흔적이 대표 원인입니다. |
| `insufficient_evidence` | 실제 게시 시각 또는 최종 성공 시각 근거가 부족합니다. |
| `none` | 별도 대표 원인을 붙일 필요가 없는 상태입니다. |

`latency_classification.internal_delay_cause` 값 해석:

| Value | Meaning |
| --- | --- |
| `none` | 내부 지연 대표 원인이 없습니다. |
| `queue_wait` | 감지 이후 큐 대기 또는 첫 시도 시작 전 적체가 지배적입니다. |
| `retry_accumulation` | 실패 후 재시도 누적으로 지연이 커졌습니다. |
| `job_failure` | 발송 job 실패 흔적이 직접 감지됐습니다. |

### 4. Runbook 조회 컬럼 매핑

| Runbook output column | Raw log field / derivation |
| --- | --- |
| `published_at` | `actual_published_at` |
| `detected_at` | `detected_at` |
| `event_at` | `sent_at` |
| `publish_to_event_ms` | `sent_at - actual_published_at` |
| `send_result` | `send_result` |
| `delivery_path` | `delivery_path` |
| `observation_status` | `observation_status` |
| `failure_reason` | `failure_reason` |

## Execute

repo root에서 실행합니다.

```bash
go run ./hololive/hololive-stream-ingester/cmd/ops/youtube-community-shorts delivery-logs -window 24h
go run ./hololive/hololive-stream-ingester/cmd/ops/youtube-community-shorts delivery-logs -window 30m -limit 500 -format json
```

특정 관찰 구간만 보려면 observation key를 함께 지정합니다.

```bash
go run ./hololive/hololive-stream-ingester/cmd/ops/youtube-community-shorts delivery-logs \
  -observation-runtime youtube-scraper \
  -observation-cutover 2026-04-10T00:00:00Z

go run ./hololive/hololive-stream-ingester/cmd/ops/youtube-community-shorts delivery-logs \
  -observation-runtime youtube-scraper \
  -observation-cutover 2026-04-10T00:00:00Z \
  -limit 1000 \
  -format json
```

- `-window`: recent 조회 창. observation query를 쓰지 않을 때만 적용됩니다.
- `-observation-runtime`, `-observation-cutover`: 특정 24시간 관찰 구간을 선택합니다. 둘 중 하나만 주면 실패합니다.
- `-limit`: 반환할 최대 로그 row 수입니다. 기본값은 `200` 입니다.
- `-format json`: 자동 수집이나 후처리용 JSON 출력을 사용합니다.

## Readout

- `published_at`, `detected_at`, `event_at` 을 함께 비교해 실제 게시 이후 어느 시점의 시도 로그인지 판단합니다.
- `publish_to_event_ms` 가 있으면 실제 게시 시각 기준으로 해당 시도까지 걸린 시간입니다.
- `send_result = success`: 해당 시도에서 실제 발송이 성공했습니다.
- `send_result != success`: 실패 또는 재시도 전 시도입니다. `failure_reason` 으로 이유를 확인합니다.
- `observation_status = matched`: 지정 관찰 구간에 속한 게시물입니다.
- `truncated = true`: `limit` 때문에 전체 로그가 잘렸습니다. 더 큰 `-limit` 로 다시 조회합니다.

## Fallback SQL

compose 운영 기준 Postgres는 `localhost:5433` 입니다.

최근 구간 조회:

```bash
set -a
source .env
set +a

PGPASSWORD="$DB_PASSWORD" psql -h localhost -p 5433 -U "${HOLOLIVE_DB_USER:-hololive_runtime}" -d hololive <<'SQL'
SELECT
    alarm_type,
    channel_id,
    COALESCE(NULLIF(post_id, ''), content_id) AS post_id,
    room_id,
    attempt_ordinal,
    actual_published_at,
    detected_at,
    event_at,
    send_result,
    delivery_path,
    observation_status,
    failure_reason
FROM youtube_notification_delivery_telemetry
WHERE alarm_type IN ('COMMUNITY', 'SHORTS')
  AND COALESCE(actual_published_at, detected_at, event_at) >= NOW() - INTERVAL '24 hours'
ORDER BY COALESCE(actual_published_at, detected_at, event_at) DESC, event_at ASC, id ASC
LIMIT 200;
SQL
```

지정 관찰 구간 조회:

```bash
set -a
source .env
set +a

PGPASSWORD="$DB_PASSWORD" psql -h localhost -p 5433 -U "${HOLOLIVE_DB_USER:-hololive_runtime}" -d hololive <<'SQL'
SELECT
    alarm_type,
    channel_id,
    COALESCE(NULLIF(post_id, ''), content_id) AS post_id,
    room_id,
    attempt_ordinal,
    actual_published_at,
    detected_at,
    event_at,
    send_result,
    delivery_path,
    observation_status,
    failure_reason
FROM youtube_notification_delivery_telemetry
WHERE observation_status = 'matched'
  AND observation_runtime_name = 'youtube-scraper'
  AND observation_bigbang_cutover_at = '2026-04-10T00:00:00Z'
ORDER BY event_at ASC, id ASC
LIMIT 200;
SQL
```
