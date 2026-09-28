# YouTube Community Shorts Delivery Logs

최근 구간의 유튜브 커뮤니티/쇼츠 알람 발송 로그만 별도로 조회할 때 사용하는 운영 절차입니다.

## Scope

- 대상은 `COMMUNITY`, `SHORTS` 발송 로그만 포함합니다.
- 최근 조회의 기준 시각은 `youtube_notification_delivery_telemetry.actual_published_at` 이며, 값이 없으면 `detected_at`, 둘 다 없으면 `event_at` 으로 대체합니다.
- 결과는 발송 시도 로그 단위이며, 성공/실패 시도와 재시도 흔적이 모두 유지됩니다.

## Telemetry 기록 계약

`DEC-20260926-hololive-delivery-telemetry-single-path`에 따라 시도 telemetry는 alarm-worker `TransitionStore`가 lifecycle 전이 트랜잭션(`CompleteSent`, 실패 전이, stale SENDING 격리) 안에서 owner 시도 하나당 한 행만 기록합니다. commit 뒤 직접 enqueue, `direct_fallback` 로그, delivery 테이블을 역산하던 backfill(`delivery_mode = recovered`)은 삭제했습니다.

- 기록 대상은 owner의 provider 성공·실패와 prepared failure 전체(`message_missing`, `pre_send_claim` 포함)입니다. follower·fulfilled·전파 전이는 시도가 아니라 기록하지 않습니다.
- 결과 불명 시도는 공백으로 두지 않고 stale SENDING 격리 트랜잭션이 `send_result = outcome_unknown`, `delivery_mode = stale_sweep`, `failure_reason = stale_sending_outcome_unknown`으로 기록합니다.
- `failure_reason`은 lifecycle Reason 코드 어휘(`provider_rate_limited`, `provider_transport`, `format_message` 등)를 씁니다.
- `attempt_ordinal`은 telemetry 버퍼에 남은 delivery별 최대 순번 다음 값과 claim 시점 `attempt_count + 1`(attempt started 로그의 값) 중 큰 값입니다. revive가 `attempt_count`를 0으로 되돌려도 버퍼에 남은 순번 뒤로 이어지므로 revive 뒤에는 `attempt_count + 1`보다 클 수 있고, telemetry processor가 retention(기본 24h, profile `youtube_delivery.telemetry_retention_ms`)이 지난 방출 행을 지운 뒤에도 `attempt_count`가 하한이라 되돌아가지 않습니다. 한계: 버퍼 행이 retention으로 지워진 뒤 revive된 delivery는 두 값이 모두 초기화되어 1부터 다시 셉니다. 그래서 같은 `(delivery_id, attempt_ordinal)` 감사 로그가 retention보다 긴 간격을 두고 두 번 나올 수 있으며, 이때는 `sent_at`으로 시도를 구분합니다. 버퍼에 남은 행과 같은 `(delivery_id, attempt_ordinal)`이 다시 들어오면 조용히 건너뛰지 않고 오류로 전이 트랜잭션을 rollback합니다.
- `post_id`는 content_id와 payload `canonical_post_id`가 일치함을 검증한 logical key(`short:…`, `community:…`)입니다.
- 알려진 공백: grouped 발송이 permanent로 실패해 개별 발송으로 넘어간 경우 그 grouped 시도는 전이가 없어 기록되지 않습니다. 이어지는 개별 발송 시도는 `per_room`으로 기록됩니다.
- fail-closed 비용: telemetry INSERT가 실패하면 전이 트랜잭션 전체가 rollback됩니다. `CompleteSent`가 이렇게 실패하면 provider가 이미 받은 발송이 SENDING으로 남고 stale sweep이 QUARANTINED로 격리합니다. 재발송하지 않고 결과 불명으로 드러나며, 운영자는 해당 delivery를 전송 증거로 검토합니다.

## Canonical Validation Log

- 운영 검증의 기준 원시 로그는 `message="YouTube community/shorts delivery audit"` 구조화 로그입니다.
- `telemetry_source = persistent_buffer` 는 시도 단위 감사 로그입니다. 위 기록 계약대로 전이 트랜잭션이 저장한 행만 flush해 방출합니다.
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
| `send_result` | 해당 시도의 결과입니다(`success`, `failure`, `outcome_unknown`). 성공 로그는 게시물-룸 조합당 정확히 1건이어야 합니다. | `string enum` | `success` |
| `delivery_path` | 실제 발송 경로입니다. 운영 목표값은 신규 경로 `youtube_outbox_dispatcher` 하나입니다. | `string` | `youtube_outbox_dispatcher` |
| `delivery_mode` | 발송 모드입니다. `per_room`, `grouped`, 결과 불명 격리 `stale_sweep`, 최종 결과 로그 `final_result`를 구분합니다. | `string enum` | `grouped` |
| `actual_published_at` | 실제 유튜브 게시 시각입니다. 내부 지연 2분 계산의 시작점입니다. | `RFC3339 timestamp string` | `2026-04-10T00:01:10Z` |
| `detected_at` | 스크래퍼가 게시물을 최초 감지한 시각입니다. 외부 수집 지연과 내부 지연을 분리할 때 사용합니다. | `RFC3339 timestamp string` | `2026-04-10T00:01:42Z` |
| `sent_at` | 해당 감사 로그가 가리키는 발송 완료 또는 실패 시점입니다. 이 runbook의 조회 결과에서는 `event_at` 로 표시됩니다. | `RFC3339 timestamp string` | `2026-04-10T00:02:05Z` |
| `failure_reason` | 실패 시도일 때의 lifecycle Reason 코드입니다. 실패 후 재시도/최종 성공 여부를 해석할 때 사용합니다. 성공 로그에서는 비어 있습니다. | `string` | `provider_rate_limited` |

### 2. 2분 SLA 판정 필드

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

### 3. Runbook 조회 컬럼 매핑

| Runbook output column | Raw log field / derivation |
| --- | --- |
| `published_at` | `actual_published_at` |
| `detected_at` | `detected_at` |
| `event_at` | `sent_at` |
| `publish_to_event_ms` | `sent_at - actual_published_at` |
| `send_result` | `send_result` |
| `delivery_path` | `delivery_path` |
| `failure_reason` | `failure_reason` |

## Execute

standalone producer ops CLI는 Task 9에서 모듈과 함께 제거됐다. `youtube-collector`에는 `cmd/ops` 리포트 바이너리가 없다.

우선 경로는 alarm-worker의 `message="YouTube community/shorts delivery audit"` 구조화 로그다. 보조 근거는 `youtube_notification_delivery_telemetry`다.

## Readout

- `published_at`, `detected_at`, `event_at` 을 함께 비교해 실제 게시 이후 어느 시점의 시도 로그인지 판단합니다.
- `publish_to_event_ms` 가 있으면 실제 게시 시각 기준으로 해당 시도까지 걸린 시간입니다.
- `send_result = success`: 해당 시도에서 실제 발송이 성공했습니다.
- `send_result != success`: 실패 또는 재시도 전 시도입니다. `failure_reason` 으로 이유를 확인합니다.
- `truncated = true`: `limit` 때문에 전체 로그가 잘렸습니다. 더 큰 `-limit` 로 다시 조회합니다.

## Fallback SQL

1차 경로는 위 alarm-worker 구조화 로그입니다. 로그로 판단할 수 없을 때만 DB를 조회합니다.
DB 조회는 stack-platform-ops의 guarded read 경로(iris-stack
`.agents/skills/stack-platform-ops/references/postgres.md`)를 따릅니다. `hololive-osaka`의
`holo-postgres`에 컨테이너 socket으로 접속하고, 조회 전에 read-only guard를 먼저 증명합니다.
비밀 env 파일을 셸에 source하거나 `PGPASSWORD`를 쓰지 않습니다.

최근 구간 조회:

```bash
# 1. read-only guard 증명: 결과가 정확히 `on`이 아니면 중단합니다.
ssh 100.100.1.8 \
  'sudo docker exec -e PGOPTIONS="-c default_transaction_read_only=on -c statement_timeout=5s" holo-postgres psql -U hololive_runtime -d hololive --no-psqlrc -v ON_ERROR_STOP=1 -At -c "show transaction_read_only"'

# 2. 같은 guard로 조회합니다. SQL을 표준 입력으로 넘기므로 docker exec에 -i를 씁니다.
ssh 100.100.1.8 \
  'sudo docker exec -i -e PGOPTIONS="-c default_transaction_read_only=on -c statement_timeout=5s" holo-postgres psql -U hololive_runtime -d hololive --no-psqlrc -v ON_ERROR_STOP=1' <<'SQL'
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
    failure_reason
FROM youtube_notification_delivery_telemetry
WHERE alarm_type IN ('COMMUNITY', 'SHORTS')
  AND COALESCE(actual_published_at, detected_at, event_at) >= NOW() - INTERVAL '24 hours'
ORDER BY COALESCE(actual_published_at, detected_at, event_at) DESC, event_at ASC, id ASC
LIMIT 200;
SQL
```
