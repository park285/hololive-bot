# YouTube Community Shorts Delivery Logs

유튜브 커뮤니티/쇼츠 알람의 발송 결과를 alarm-worker 구조화 로그와 PostgreSQL의 현재 발송 상태로 확인하는 운영 절차입니다.

## Scope

- 대상은 `COMMUNITY_POST`, `NEW_SHORT` outbox와 그 room별 delivery입니다.
- 시도 단위 이력은 alarm-worker 구조화 로그에만 남습니다. DB에는 delivery별 현재 상태, room별 확정 원장, 게시물별 추적 시각만 남습니다.

## 감사 기록 계약

alarm-worker는 발송 시도마다 DB 행을 쓰지 않습니다. 전이 트랜잭션이 `youtube_notification_delivery_telemetry`에 시도 행을 넣고 배경 처리기가 그 행을 방출·삭제하던 경로, 시도 단위 `telemetry_source = persistent_buffer` 로그, `latency_classification` 분류는 모두 제거했습니다. 표와 기존 행은 남지만 이 변경이 들어간 image부터는 쓰지 않으므로 그 이전 시도만 담은 동결 자료입니다. 표에서 아직 방출되지 않았던 행(`logged_at IS NULL`)은 로그로 방출되지 않습니다.

| 근거 | 단위 | 내용 |
| --- | --- | --- |
| `message="YouTube community/shorts delivery attempt started"` | delivery 시도 | claim한 delivery의 발송 준비(메시지 포맷·요청 고정)와 SENDING 전이가 성공한 뒤 delivery마다 한 줄을 남깁니다. 준비 단계에서 실패한 claim은 이 줄 없이 delivery result 줄만 남고, grouped 발송이 room별 발송으로 나뉠 때 room별 발송도 이 줄을 따로 남기지 않으므로, 이 줄로 claim 횟수나 최초 대기 시간을 복원할 수 없습니다. `attempt_ordinal`은 claim 시점 `attempt_count + 1`이며, revive가 `attempt_count`를 0으로 되돌리면 1부터 다시 셉니다. 같은 `(delivery_id, attempt_ordinal)`이 두 번 나오면 `attempt_started_at`으로 시도를 구분합니다. |
| `message="YouTube community/shorts delivery result"` | 발송 한 번(grouped 또는 per_room) | 대상·성공·실패 알람 수와 room 수, room이 하나면 `room_id`, 실패면 `failure_reason`(100자까지)을 남깁니다. |
| `message="YouTube community/shorts delivery audit"`, `telemetry_source = outbox_final_result` | 게시물(outbox) | delivery 발송을 거쳐 outbox가 SENT 또는 FAILED로 끝나면 한 줄을 남깁니다. 아래 필드 표를 따릅니다. fan-out 단계에서 끝난 outbox(구독 room 조회 재시도 소진으로 delivery 없이 FAILED, 구독 room 없이 SENT)는 이 줄을 남기지 않고, PENDING delivery가 `delivery freshness expired`로 만료돼 FAILED가 된 outbox는 aggregate sync가 먼저 정산하지 않는 한 이 줄을 남기지 않으므로 Fallback SQL의 `outbox_status`와 delivery `error`로 확인합니다. |
| `youtube_notification_delivery` | room별 delivery | `status`, `attempt_count`, `sent_at`, `error`의 현재 값입니다. outbox가 끝나고 `youtube_delivery.cleanup_after_ms`가 지나면 outbox와 함께 지워집니다. |
| `youtube_notification_delivery_ledger` | `(kind, logical_id, room_id)` | room별 `SENT`·`QUARANTINED` 확정 원장입니다. `logical_id`는 `short:…`, `community:…` 형식입니다. |
| `youtube_content_alarm_tracking` | 게시물 | `actual_published_at`, `detected_at`, `alarm_sent_at`, `alarm_latency_millis`, `alarm_latency_exceeded`입니다. |

- 최종 결과 줄의 `send_result = success`는 Iris가 요청을 접수(202)해 delivery가 SENT로 바뀐 시점의 판정입니다. 그 뒤 handoff 확인(`youtube-delivery-handoff-reconciler`)이 정산하면 delivery·outbox 상태와 SENT 원장이 바뀝니다. 재발급이면 outbox가 PENDING으로 돌아가 다시 발송한 뒤 같은 `outbox_id`의 최종 결과 줄이 한 번 더 남으므로, 같은 `outbox_id`의 마지막 줄이 최신 판정입니다. 재시도 소진·격리로 정산되면 최종 결과 줄은 다시 남지 않습니다.
- 결과 불명 시도(오래 남은 SENDING)는 stale sweep이 `QUARANTINED`로 격리하고 건수를 로그로 남깁니다. 재발송하지 않으며, 운영자는 해당 delivery를 전송 증거로 검토합니다.
- 시도 단위 결과를 게시물과 잇는 공통 식별자는 없습니다. attempt started 줄에는 `delivery_id`·`outbox_id`·`post_id`·`room_id`·`attempt_started_at`이 있고, delivery result 줄에는 발송 한 번의 room(대상 room이 하나일 때 `room_id`)과 결과만 있습니다. grouped 발송은 한 room의 여러 게시물을 요청 하나로 보내므로 같은 결과를 공유합니다. 그래서 같은 `room_id`와 시각으로 두 줄을 대조하고, 게시물별 최종 결과는 최종 결과 줄과 delivery의 현재 상태로 확인합니다.
- 결과 줄이 남지 않는 경로가 있습니다. per-room 메시지 누락은 지표만, 발송 전 식별자 오류는 `Failed to resolve delivery logical identity before send` Warn 줄과 지표만 남기고, stale sweep 격리는 건수만 남깁니다. 이때는 delivery의 `status`와 마지막 실패 사유 `error`(시도마다 덮어씀)로 확인합니다.
- `post_id`는 content_id와 payload `canonical_post_id`가 일치함을 검증한 logical key(`short:…`, `community:…`)입니다. 식별자가 잘못되면 대체 ID 없이 `invalid:<kind>:<reason>` 라벨을 남깁니다.

## Canonical Validation Log

- 아래 SQL은 outbox의 `content_id`를 worker와 같은 규칙(앞뒤 공백 제거, `short:`·`community:` 접두사, 커뮤니티 URL의 `post/` 뒤 ID)으로 정규화한 `post_id`로 tracking의 `canonical_content_id`와 잇습니다. 다른 runbook의 SQL도 같은 `outbox_post` CTE를 씁니다.
- 게시물 단위 결과의 1차 근거는 `message="YouTube community/shorts delivery audit"`, `telemetry_source = outbox_final_result` 줄입니다. 이 줄은 Iris 접수 시점의 판정이므로, 합격 판정 전에 아래 Fallback SQL로 delivery의 현재 상태와 handoff 확인 상태를 함께 확인합니다.
- 시도별 실패·재시도 흐름은 `delivery attempt started`와 `delivery result` 줄을 room과 시각으로 대조해 읽습니다. 게시물 단위로 정확히 잇는 근거는 아니므로 위 감사 기록 계약의 한계를 함께 봅니다.
- 2분 SLA는 최종 결과 줄의 `alarm_latency_exceeded`와 `alarm_latency_millis`로 판정합니다. 재발급 뒤 다시 남은 줄도 처음 접수 시각을 유지하므로(outbox `sent_at`은 재발급에도 유지되고 tracking `alarm_sent_at`은 더 이른 값으로만 바뀝니다) 재발송까지 걸린 지연은 이 값에 반영되지 않습니다. 이 경우 같은 `outbox_id`의 마지막 줄 `sent_at`과 room별 delivery `sent_at`을 비교합니다. 외부 수집과 내부 전달 구간 분리는 [Latency Cause Report](YOUTUBE_COMMUNITY_SHORTS_LATENCY_CAUSE_REPORT.md)를 따릅니다.

## 최종 결과 줄 필드

| Field | Meaning | Type | Example |
| --- | --- | --- | --- |
| `message` | 검증 대상 로그 식별자입니다. | `string` | `YouTube community/shorts delivery audit` |
| `telemetry_source` | 최종 결과 줄임을 나타냅니다. 다른 값은 더 나오지 않습니다. | `string enum` | `outbox_final_result` |
| `alarm_type` | 대상 알람 유형입니다. | `string enum` | `COMMUNITY` |
| `channel_id` | YouTube 채널 식별자입니다. | `string` | `UC1DCedRgGHBdm81E1llLhOQ` |
| `post_id` | 게시물 logical key입니다. 정확히 1회 발송 검증의 기본 키입니다. | `string` | `community:UgkxExampleCanonicalPostId12345` |
| `content_id` | 원본 콘텐츠 식별자입니다. | `string` | `UgkxExampleCanonicalPostId12345` |
| `outbox_id` | 게시물 fan-out의 outbox 식별자입니다. | `integer(int64)` | `98123` |
| `dedupe_key` | 중복 방지 키입니다. | `string` | `youtube-notification:COMMUNITY_POST:UgkxExampleCanonicalPostId12345` |
| `send_result` | 게시물 최종 결과입니다(`success`, `failure`). | `string enum` | `success` |
| `delivery_path` | 발송 경로입니다. 값은 `youtube_outbox_dispatcher` 하나입니다. | `string` | `youtube_outbox_dispatcher` |
| `delivery_mode` | 최종 결과 줄은 `final_result`입니다. 시도 줄은 `grouped`, `per_room`입니다. | `string enum` | `final_result` |
| `sent_at` | outbox가 SENT면 그 발송 시각, 아니면 최종 결과를 기록한 시각입니다. | `RFC3339 timestamp string` | `2026-04-10T00:02:05Z` |
| `target_room_count`, `successful_room_count`, `failed_room_count` | 게시물의 대상·성공·실패 room 수입니다. | `integer` | `3` |
| `actual_published_at` | tracking의 실제 게시 시각입니다. 없으면 빠집니다. | `RFC3339 timestamp string` | `2026-04-10T00:01:10Z` |
| `alarm_sent_at` | tracking의 최초 발송 완료 시각입니다. Iris 접수 시각이거나, 같은 게시물의 다른 outbox가 구독 room 없이 끝나 hololive-api canonical writer가 옮겨 적은 그 outbox의 `sent_at`입니다. handoff 확인이 실패로 정산돼도 지워지지 않습니다. tracking 행이 없으면 outbox `sent_at`입니다. tracking 조회가 실패하면 `actual_published_at`·`alarm_sent_at`·`alarm_latency_millis`·`alarm_latency_exceeded`가 모두 빠지고 `YouTube community/shorts final result tracking unavailable` Warn 줄이 `outbox_ids`와 함께 남습니다. | `RFC3339 timestamp string` | `2026-04-10T00:02:05Z` |
| `alarm_latency_millis` | `alarm_sent_at - actual_published_at`입니다. 둘 중 하나라도 없으면 빠집니다. | `integer(int64)` | `55000` |
| `alarm_latency_exceeded` | 실제 게시 시각 기준 2분(120000ms) 초과 여부입니다. 내부 원인이어도 늦게나마 1회 발송돼야 하며 이 값은 기록용입니다. | `bool` | `false` |
| `failure_reason` | 실패한 room들의 사유를 모은 값입니다. 성공이면 빠집니다. | `string` | `provider_rate_limited` |

## Fallback SQL

1차 경로는 위 alarm-worker 구조화 로그입니다. 로그로 판단할 수 없을 때만 DB를 조회합니다. 이 조회는 시도 이력이 아니라 delivery별 현재 상태를 보여 줍니다.
DB 조회는 stack-platform-ops의 guarded read 경로(iris-stack
`.agents/skills/stack-platform-ops/references/postgres.md`)를 따릅니다. 중앙 `hololive-seoul`의
`holo-postgres`에 컨테이너 socket으로 접속하고, 조회 전에 read-only guard를 먼저 증명합니다.
비밀 env 파일을 셸에 source하거나 `PGPASSWORD`를 쓰지 않습니다.

```bash
# 1. read-only guard 증명: 결과가 정확히 `on`이 아니면 중단합니다.
ssh 100.100.1.8 \
  'sudo podman exec -e PGOPTIONS="-c default_transaction_read_only=on -c statement_timeout=5s" holo-postgres psql -U hololive_runtime -d hololive --no-psqlrc -v ON_ERROR_STOP=1 -At -c "show transaction_read_only"'

# 2. 같은 guard로 조회합니다. SQL을 표준 입력으로 넘기므로 podman exec에 -i를 씁니다.
ssh 100.100.1.8 \
  'sudo podman exec -i -e PGOPTIONS="-c default_transaction_read_only=on -c statement_timeout=5s" holo-postgres psql -U hololive_runtime -d hololive --no-psqlrc -v ON_ERROR_STOP=1' <<'SQL'
WITH outbox_post AS (
    SELECT
        o.*,
        CASE o.kind
            WHEN 'NEW_SHORT' THEN 'short:' || regexp_replace(regexp_replace(raw.id, '^short:', ''), space.re, '', 'g')
            ELSE 'community:' || regexp_replace(COALESCE(substring(community.id FROM '(?:^|/)post/([^"?#&/]+)'), community.id), space.re, '', 'g')
        END AS post_id
    FROM youtube_notification_outbox AS o
    CROSS JOIN (SELECT '^[ \t\n\v\f\r\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+|[ \t\n\v\f\r\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+$' AS re) AS space
    CROSS JOIN LATERAL (SELECT regexp_replace(o.content_id, space.re, '', 'g') AS id) AS raw
    CROSS JOIN LATERAL (SELECT replace(regexp_replace(regexp_replace(raw.id, '^community:', ''), space.re, '', 'g'), '\/', '/') AS id) AS community
    WHERE o.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
)
SELECT
    o.kind,
    o.channel_id,
    o.content_id,
    o.id AS outbox_id,
    o.status AS outbox_status,
    o.sent_at AS outbox_sent_at,
    d.id AS delivery_id,
    d.room_id,
    d.status AS delivery_status,
    d.attempt_count,
    d.sent_at,
    d.error,
    request.member_ids[1] AS handoff_first_delivery_id,
    request.generation AS handoff_generation,
    request.handoff_request_id,
    request.handoff_next_check_at,
    request.handoff_checked_at,
    track.actual_published_at,
    track.detected_at,
    track.alarm_sent_at
FROM outbox_post AS o
LEFT JOIN youtube_notification_delivery AS d
    ON d.outbox_id = o.id
LEFT JOIN youtube_notification_delivery_ledger AS ledger
    ON d.status = 'SENT'
   AND d.send_request_id IS NULL
   AND ledger.kind = o.kind
   AND ledger.logical_id = o.post_id
   AND ledger.room_id = d.room_id
   AND ledger.status = 'SENT'
LEFT JOIN youtube_notification_delivery AS source
    ON source.id = ledger.source_delivery_id
LEFT JOIN youtube_notification_send_request AS request
    ON request.base_id = COALESCE(d.send_request_id, source.send_request_id)
LEFT JOIN youtube_content_alarm_tracking AS track
    ON track.kind = o.kind
   AND track.canonical_content_id = o.post_id
WHERE o.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
  AND COALESCE(track.actual_published_at, track.detected_at, o.created_at) >= NOW() - INTERVAL '24 hours'
ORDER BY COALESCE(track.actual_published_at, track.detected_at, o.created_at) DESC, o.id ASC, d.room_id ASC
LIMIT 200;
SQL
```

## Readout

- `delivery_status = SENT`: Iris가 해당 room 요청을 접수했습니다. `sent_at`이 접수 시각입니다. `handoff_next_check_at`이 있으면 handoff 확인을 기다리는 중입니다. `handoff_checked_at`은 확인 절차가 닫힌 시각일 뿐 성공 판정이 아닙니다. 같은 게시물·room의 별칭 delivery는 요청 없이 실제 발송 delivery와 함께 SENT가 되므로, 그 행의 handoff 열은 원장 `source_delivery_id`가 가리키는 실제 발송 delivery의 요청을 보여 줍니다. 실제 발송 delivery가 이미 정리돼 요청을 찾지 못하면 handoff 열이 비고, 이때 handoff 확인 상태는 알 수 없습니다. 확인이 실패로 정산되면 delivery가 SENT에서 벗어나지만, frozen member가 바뀌어 정산하지 못하고 닫힌 경우(`youtube delivery handoff check closed; frozen members changed since admission` Error 로그)에는 SENT와 `handoff_checked_at`이 함께 남습니다. reconciler 로그에는 Iris 요청 ID가 없고 `delivery_ids.first_id`·`generation`만 있으므로, SENT를 확정하려면 `handoff_first_delivery_id`·`handoff_generation`과 같은 값의 로그에 이 닫힘 로그나 `youtube delivery handoff failed`·`handoff outcome unknown` 로그가 없는지 확인합니다. 그 기간의 로그를 볼 수 없으면 결과를 미확정으로 둡니다.
- `delivery_status`가 비어 있는 행: delivery 없이 끝난 outbox입니다. `outbox_status = FAILED`면 구독 room 조회 실패로 발송하지 못한 누락이고, `outbox_status = SENT`면 구독 room이 없었던 정상 종료입니다. `outbox_status = PENDING`이면 아직 fan-out 전입니다. outbox 생성 뒤 활성 프로필의 `queue.max_age`가 지났으면 fan-out이 다시 잡지 않고 정리 작업이 발송 없이 지우므로 누락으로 봅니다.
- `delivery_status = PENDING`: 아직 보내지 않았거나 재시도를 기다립니다. 재시도를 기다리는 행은 `error`에 마지막 실패 사유가 남지만, revive가 되살린 행은 `error`가 비고 `attempt_count`가 0이므로 이전 실패는 로그로 확인합니다.
- `delivery_status = SENDING`: 발송 중입니다. 오래 남으면 stale sweep이 `QUARANTINED`로 격리합니다.
- `delivery_status = FAILED`: 영구 실패 또는 재시도 한도 소진입니다. `youtube_delivery.revive_enabled`가 켜져 있으면 revive가 최근 FAILED 중 부모 outbox의 `sent_at`(`outbox_sent_at`)이 비어 있고 `error`가 `delivery freshness expired`·`client request id generations exhausted`가 아닌 행만 다시 PENDING으로 돌립니다. 부모 outbox가 SENT에 도달해 `outbox_sent_at`이 채워진 뒤 handoff 실패로 FAILED가 된 room은 되살아나지 않지만, 다른 room이 아직 진행 중이라 `outbox_sent_at`이 비어 있을 때 handoff 실패로 FAILED가 된 room은 되살아날 수 있습니다. `error`로 사유를 확인합니다.
- `delivery_status = QUARANTINED`: 결과 불명으로 격리됐습니다. 재발송하지 않으므로 전송 증거로 검토합니다.
- `attempt_count > 0`: 실패한 시도가 그만큼 있었습니다. 성공은 이 값을 올리지 않고 revive가 0으로 되돌리므로 실패 횟수의 하한입니다.
- 행이 없으면 outbox가 끝나고 `youtube_delivery.cleanup_after_ms`가 지나 지워졌을 수 있습니다. 이때는 `youtube_notification_delivery_ledger`의 SENT·QUARANTINED room으로 확인합니다. tracking의 `alarm_sent_at`은 최초 발송 완료 시각이라 handoff 실패 뒤에도 남으므로 성공 근거로 쓰지 않습니다.
