# YouTube Community Shorts Send Counts

최근 24시간 동안 실제 게시된 유튜브 커뮤니티/쇼츠 게시물별 알람 발송 횟수와 중복 여부를 운영자가 확인할 때 사용하는 조회 절차입니다.

## Scope

- 대상은 `COMMUNITY_POST`, `NEW_SHORT` 두 종류만 포함합니다.
- recent window 조회는 `youtube_content_alarm_tracking.actual_published_at`를 우선 사용하고, 값이 비어 있으면 `detected_at`으로 대체해 최근 24시간 게시물을 고릅니다.
- `0회 발송` 게시물도 결과에 남아야 하므로 추적 테이블(`youtube_content_alarm_tracking`)을 기준 집합으로 사용합니다.
- 발송 횟수는 room별 delivery(`youtube_notification_delivery`)의 현재 상태로 셉니다. 시도마다 남던 telemetry 행은 더 쓰지 않으므로, 실패 흔적은 `FAILED`·`QUARANTINED` 상태와 `attempt_count > 0`으로만 남습니다. `attempt_count`는 실패한 시도에서만 올라가고 성공은 올리지 않으며, revive가 0으로 되돌립니다.
- 게시물은 outbox `content_id`를 worker와 같은 규칙으로 정규화한 `post_id`(tracking의 `canonical_content_id`와 같은 값)로 outbox와 잇습니다. 공백이 다른 `content_id` 같은 별칭 outbox가 한 게시물에 여럿일 수 있고, 같은 room의 별칭 delivery는 실제 발송 없이 fulfilled로 SENT가 되므로 `success_send_count`는 실제 발송 횟수보다 클 수 있습니다. 게시물·room당 성공은 원장 `youtube_notification_delivery_ledger`의 기본 키 `(kind, logical_id, room_id)`로 한 번만 기록되므로 room 단위 판단은 `success_room_count`와 `ledger_sent_room_count`로 합니다. 중복 발송 위험은 provider가 받았는지 모르는 시도에 남으며, 이런 delivery는 `QUARANTINED`로 격리되고 재발송하지 않습니다.
- delivery 행은 outbox가 끝나고 `youtube_delivery.cleanup_after_ms`가 지나면 지워집니다. 지워진 뒤에는 원장(`youtube_notification_delivery_ledger`, `logical_id`는 tracking의 `canonical_content_id`와 같은 값)의 SENT·QUARANTINED room 수로 판정합니다. 실패한 room과 delivery 없이 FAILED로 끝난 outbox는 원장을 남기지 않으므로, 정리된 뒤에는 그 실패가 DB에서 사라지고 alarm-worker 로그로만 확인됩니다. tracking의 `alarm_sent_at`은 최초 발송 완료 시각이라 handoff 실패 뒤에도 남으므로 그것만 남은 게시물은 결과를 알 수 없습니다.

## Execute

전용 리포트 CLI는 없으며 PostgreSQL을 직접 조회합니다.
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
),
per_post AS (
    SELECT
        track.kind,
        track.channel_id,
        track.content_id,
        track.canonical_content_id,
        track.actual_published_at,
        track.detected_at,
        track.alarm_sent_at,
        track.alarm_latency_millis,
        track.alarm_latency_exceeded,
        COUNT(DISTINCT o.id) AS outbox_count,
        COUNT(DISTINCT o.id) FILTER (WHERE o.status = 'PENDING') AS pending_outbox_count,
        COUNT(DISTINCT o.id) FILTER (WHERE o.status = 'SENT') AS sent_outbox_count,
        MAX(o.created_at) FILTER (WHERE o.status = 'FAILED' AND o.sent_at IS NULL AND (d.id IS NULL OR (d.status = 'FAILED' AND COALESCE(d.error, '') NOT IN ('delivery freshness expired', 'client request id generations exhausted')))) AS last_revivable_outbox_created_at,
        COUNT(DISTINCT o.id) FILTER (WHERE o.status = 'PENDING' AND d.id IS NULL) AS undelivered_pending_outbox_count,
        COUNT(DISTINCT o.id) FILTER (WHERE o.status = 'PENDING' AND d.id IS NULL AND o.locked_at IS NOT NULL) AS claimed_undelivered_pending_outbox_count,
        MAX(o.created_at) FILTER (WHERE o.status = 'PENDING' AND d.id IS NULL) AS last_undelivered_pending_outbox_created_at,
        COUNT(d.id) FILTER (WHERE d.status IN ('PENDING', 'SENDING')) AS in_progress_delivery_count,
        COUNT(d.id) FILTER (WHERE d.status = 'SENT' AND request.handoff_next_check_at IS NOT NULL) AS handoff_pending_delivery_count,
        COUNT(d.id) FILTER (WHERE d.status = 'SENT') AS success_send_count,
        COUNT(DISTINCT d.room_id) FILTER (WHERE d.status = 'SENT') AS success_room_count,
        COUNT(DISTINCT d.room_id) AS target_room_count,
        COUNT(d.id) FILTER (WHERE d.status = 'FAILED') AS failed_delivery_count,
        COUNT(d.id) FILTER (WHERE d.status = 'QUARANTINED') AS quarantined_delivery_count,
        COUNT(d.id) FILTER (WHERE d.attempt_count > 0) AS failed_attempt_delivery_count,
        MAX(d.sent_at) AS last_sent_at
    FROM youtube_content_alarm_tracking AS track
    LEFT JOIN outbox_post AS o
        ON o.kind = track.kind
       AND o.post_id = track.canonical_content_id
    LEFT JOIN youtube_notification_delivery AS d
        ON d.outbox_id = o.id
    LEFT JOIN youtube_notification_send_request AS request
        ON request.base_id = d.send_request_id
    WHERE track.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
      AND COALESCE(track.actual_published_at, track.detected_at) >= NOW() - INTERVAL '24 hours'
    GROUP BY
        track.kind,
        track.channel_id,
        track.content_id,
        track.canonical_content_id,
        track.actual_published_at,
        track.detected_at,
        track.alarm_sent_at,
        track.alarm_latency_millis,
        track.alarm_latency_exceeded
),
ledger_request AS (
    SELECT DISTINCT ON (l.kind, l.logical_id, l.room_id)
        l.kind,
        l.logical_id,
        l.status,
        r.handoff_next_check_at
    FROM youtube_notification_delivery_ledger AS l
    LEFT JOIN (
        youtube_notification_send_request AS r
        CROSS JOIN LATERAL unnest(r.member_ids) AS member(delivery_id)
    ) ON member.delivery_id = l.source_delivery_id
    WHERE (l.kind, l.logical_id) IN (SELECT kind, canonical_content_id FROM per_post)
    ORDER BY l.kind, l.logical_id, l.room_id, r.created_at DESC, r.base_id DESC
),
ledger_post AS (
    SELECT
        kind,
        logical_id,
        COUNT(*) FILTER (WHERE status = 'QUARANTINED') AS ledger_quarantined_room_count,
        COUNT(*) FILTER (WHERE status = 'SENT') AS ledger_sent_room_count,
        COUNT(*) FILTER (WHERE status = 'SENT' AND handoff_next_check_at IS NOT NULL) AS ledger_handoff_pending_room_count
    FROM ledger_request
    GROUP BY kind, logical_id
),
with_ledger AS (
    SELECT
        p.*,
        COALESCE(lp.ledger_quarantined_room_count, 0) AS ledger_quarantined_room_count,
        COALESCE(lp.ledger_sent_room_count, 0) AS ledger_sent_room_count,
        COALESCE(lp.ledger_handoff_pending_room_count, 0) AS ledger_handoff_pending_room_count
    FROM per_post AS p
    LEFT JOIN ledger_post AS lp
        ON lp.kind = p.kind
       AND lp.logical_id = p.canonical_content_id
)
SELECT
    CASE
        WHEN quarantined_delivery_count > 0 OR ledger_quarantined_room_count > 0 THEN 'quarantined'
        WHEN outbox_count = 0 AND ledger_handoff_pending_room_count > 0 THEN 'in_progress'
        WHEN outbox_count = 0 AND ledger_sent_room_count > 0 THEN 'sent_outbox_cleaned'
        WHEN outbox_count = 0 AND alarm_sent_at IS NOT NULL THEN 'tracking_only_outbox_cleaned'
        WHEN outbox_count = 0 THEN 'outbox_missing'
        WHEN pending_outbox_count > 0 OR in_progress_delivery_count > 0 OR handoff_pending_delivery_count > 0 OR ledger_handoff_pending_room_count > 0 THEN 'in_progress'
        WHEN target_room_count = 0 AND sent_outbox_count = outbox_count AND ledger_sent_room_count = 0 THEN 'no_target_rooms'
        WHEN success_send_count = 0 AND ledger_sent_room_count = 0 THEN 'no_success'
        WHEN failed_delivery_count > 0 OR failed_attempt_delivery_count > 0 OR outbox_count > sent_outbox_count + pending_outbox_count THEN 'failed_attempts'
        ELSE 'ok'
    END AS status,
    CASE kind WHEN 'COMMUNITY_POST' THEN 'COMMUNITY' ELSE 'SHORTS' END AS alarm_type,
    channel_id,
    content_id,
    actual_published_at,
    detected_at,
    alarm_sent_at,
    ROUND(alarm_latency_millis / 1000.0) AS delay_seconds,
    alarm_latency_exceeded,
    ROUND(EXTRACT(EPOCH FROM (detected_at - actual_published_at)) * 1000)::BIGINT AS publish_to_detect_ms,
    ROUND(EXTRACT(EPOCH FROM (alarm_sent_at - detected_at)) * 1000)::BIGINT AS detect_to_sent_ms,
    outbox_count,
    pending_outbox_count,
    sent_outbox_count,
    undelivered_pending_outbox_count,
    claimed_undelivered_pending_outbox_count,
    target_room_count,
    success_send_count,
    success_room_count,
    in_progress_delivery_count,
    handoff_pending_delivery_count,
    ledger_handoff_pending_room_count,
    quarantined_delivery_count,
    ledger_quarantined_room_count,
    ledger_sent_room_count,
    failed_delivery_count,
    failed_attempt_delivery_count,
    last_revivable_outbox_created_at,
    last_undelivered_pending_outbox_created_at,
    last_sent_at
FROM with_ledger
ORDER BY COALESCE(actual_published_at, detected_at) DESC, channel_id ASC, content_id ASC;
SQL
```

운영 기준은 `quarantined` 행 0건이며, `in_progress` 행이 남아 있으면 최종 판정이 아닙니다. `youtube_delivery.revive_enabled`가 켜져 있으면 `last_revivable_outbox_created_at`부터 `youtube_delivery.revive_freshness_window_ms`가 지나지 않은 행도 revive가 FAILED outbox나 room을 다시 PENDING으로 돌릴 수 있으므로 최종 판정이 아닙니다. 이 값은 `sent_at` 없이 FAILED로 끝난 outbox 가운데 revive 대상(delivery 없는 fan-out 실패, 또는 `error`가 `delivery freshness expired`·`client request id generations exhausted`가 아닌 FAILED delivery)이 있는 outbox의 가장 나중 생성 시각이며, 비어 있으면 revive로 바뀔 실패가 없습니다. 같은 CTE 뒤에 `SELECT COUNT(*) FILTER (WHERE quarantined_delivery_count > 0 OR ledger_quarantined_room_count > 0) AS quarantined_posts FROM with_ledger;`를 붙이면 한 줄로 판정할 수 있습니다.

## Readout

- `status = ok`: 진행 중·실패·격리 room 없이 대상 room이 모두 SENT이고 handoff 확인을 기다리는 room도 없습니다. 성공한 outbox가 정리되고 delivery 없이 SENT로 끝난 별칭 outbox만 남아 원장 SENT로 성공이 확인되는 게시물도 여기에 듭니다. frozen member가 바뀌어 정산하지 못하고 닫힌 확인도 SENT로 남으므로, 확정하려면 [Delivery Logs](YOUTUBE_COMMUNITY_SHORTS_DELIVERY_LOGS.md)의 handoff 로그 대조를 따릅니다.
- `quarantined_posts = 0`: 조회 시점에 집계 창에서 격리된 delivery나 원장 항목이 없다는 뜻입니다. provider 응답 타임아웃 같은 결과 불명 시도는 stale sweep이 격리하기 전까지 SENDING(`in_progress`)으로 보이므로, `in_progress` 행이 있으면 stale sweep 뒤에 다시 판정합니다.
- `status = quarantined`: provider가 받았는지 모르는 시도가 격리됐습니다. 재발송하지 않으므로 room에 도착했는지 전송 증거로 검토합니다. 받았다면 1회, 받지 못했다면 누락입니다.
- `status = no_target_rooms`: 구독 room이 없어 outbox가 delivery 없이 SENT로 끝났고 원장 SENT room도 없습니다. 발송할 대상이 없었던 정상 상태입니다.
- `status = no_success`: 진행 중인 room 없이 SENT delivery도 원장 SENT room도 없습니다. room이 모두 실패했거나, fan-out이 실패해 delivery 없이 outbox가 FAILED로 끝났습니다. 후자는 outbox `error`로 확인합니다. `last_revivable_outbox_created_at`이 revive 창 안이면(위 운영 기준) 다시 발송될 수 있습니다.
- `status = outbox_missing`: 게시물 추적 레코드는 있으나 outbox row도, 원장 room도, `alarm_sent_at`도 없습니다. 전송 파이프라인에 진입하지 못했거나, 실패로 끝난 outbox가 `youtube_delivery.cleanup_after_ms` 뒤 정리됐거나, 구독 room 없이 SENT로 끝난 outbox가 hololive-api가 그 완료 시각을 tracking `alarm_sent_at`에 옮기기 전에 정리됐습니다. 실패한 room, fan-out 실패, 구독 room 없는 종료는 원장을 남기지 않으므로 셋은 DB로 구분되지 않습니다. alarm-worker 로그에 그 게시물의 fan-out 실패나 room 실패 결과가 있으면 실패 후 정리된 것입니다. 구독 room 없는 종료는 게시물별 로그를 남기지 않으므로, 실패 로그가 없으면 그 채널을 구독한 room이 있었는지로 진입 전 누락과 구분합니다. 어느 쪽이든 room에 발송되지 않았습니다.
- `status = sent_outbox_cleaned`: 끝난 outbox가 보존 기간을 넘겨 지워졌고 원장 SENT가 남아 있으며, 그 room의 요청이 handoff 확인을 기다리지 않습니다. 기다리는 요청이 남아 있으면 `in_progress`입니다.
- `status = tracking_only_outbox_cleaned`: outbox와 원장 SENT 없이 tracking `alarm_sent_at`만 남았습니다. 구독 room 없이 끝난 outbox의 `sent_at`이 복사됐거나, Iris가 접수한 뒤 handoff 실패로 재시도가 소진돼 원장 SENT가 지워졌을 수 있으므로 최종 결과는 알 수 없습니다.
- `status = in_progress`: outbox fan-out을 기다리거나, `PENDING`·`SENDING` room이 남았거나, SENT room의 handoff 확인(`handoff_pending_delivery_count`, 또는 원장 `source_delivery_id`를 member로 가진 가장 나중 요청이 확인을 기다리는 `ledger_handoff_pending_room_count`)이 끝나지 않았습니다. 요청 없이 함께 SENT가 된 별칭 delivery만 남거나 실제 발송 delivery가 정리돼도 요청이 남아 있으면 원장 쪽 값으로 잡힙니다. 확인 결과에 따라 재발급·실패·격리로 바뀔 수 있습니다. delivery 없는 PENDING outbox는 생성 뒤 활성 프로필의 `queue.max_age`가 지나면 fan-out이 다시 잡지 않고 정리 작업이 발송 없이 지웁니다. 다만 fan-out이 이미 잡은 outbox(`claimed_undelivered_pending_outbox_count`, outbox `locked_at`이 있음)는 그 뒤에도 끝날 수 있으므로, 이 값이 0이 될 때까지 기다린 뒤 다시 판정합니다. `last_undelivered_pending_outbox_created_at`(가장 나중에 만든 delivery 없는 PENDING outbox)부터 그 시간이 지났고 `claimed_undelivered_pending_outbox_count`, `in_progress_delivery_count`, `handoff_pending_delivery_count`, `ledger_handoff_pending_room_count`가 모두 0이면, delivery 없는 PENDING outbox `undelivered_pending_outbox_count`개는 모두 발송 없이 지워집니다. 그 수를 `outbox_count`와 `pending_outbox_count`에서 빼고 `sent_outbox_count`와 나머지 값으로 위 판정 순서를 다시 적용합니다. 남는 outbox가 없으면 `outbox_count = 0` 분기를 따르되, 마지막 `outbox_missing` 대신 `no_success`로 읽습니다. 파이프라인에는 진입했지만 fan-out되지 못했기 때문입니다.
- `status = failed_attempts`: 성공(SENT delivery 또는 원장 SENT room)은 있었지만 `FAILED` delivery나 실패한 시도가 있었던 delivery, 또는 delivery 없이 FAILED로 끝난 다른 outbox(같은 게시물의 별칭 outbox fan-out 실패)도 있습니다. `last_revivable_outbox_created_at`이 revive 창 안이면 실패한 room이나 outbox가 다시 시도될 수 있습니다. 시도별 사유는 [Delivery Logs](YOUTUBE_COMMUNITY_SHORTS_DELIVERY_LOGS.md)의 로그로 확인합니다.
- `publish_to_detect_ms`, `detect_to_sent_ms`: 외부 수집 구간과 내부 전달 구간입니다. `delay_seconds > 120`이면 2분 초과이며, 구간 판정은 [Latency Cause Report](YOUTUBE_COMMUNITY_SHORTS_LATENCY_CAUSE_REPORT.md)를 따릅니다.
