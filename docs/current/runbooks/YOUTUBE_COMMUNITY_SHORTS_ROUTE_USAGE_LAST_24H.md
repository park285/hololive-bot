# YouTube Community Shorts Route Usage Last 24h

최근 24시간 동안 실제 게시된 유튜브 커뮤니티/쇼츠 게시물이 alarm-worker 발송 경로를 거쳤는지 채널별로 확인하는 운영 절차입니다.

## Scope

- 대상은 `COMMUNITY_POST`, `NEW_SHORT` 두 종류만 포함합니다.
- 최근 24시간 판단 기준은 `youtube_content_alarm_tracking.actual_published_at`이고, 실제 게시 시각이 비어 있으면 `detected_at`로 대체합니다.
- 발송 경로 값은 행에 저장하지 않습니다. 경로 근거는 alarm-worker 감사 로그의 `delivery_path`이며, 값은 `youtube_outbox_dispatcher` 하나입니다. [Delivery Logs](YOUTUBE_COMMUNITY_SHORTS_DELIVERY_LOGS.md)의 최종 결과 줄에서 읽습니다.
- 아래 조회는 게시물이 outbox와 room별 delivery로 진입했고 성공했는지를 보여 줍니다. 경로 값 자체는 로그로 대조합니다.

## Execute
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
    track.kind AS outbox_kind,
    CASE track.kind WHEN 'COMMUNITY_POST' THEN 'COMMUNITY' ELSE 'SHORTS' END AS alarm_type,
    track.channel_id,
    track.content_id,
    track.actual_published_at,
    track.detected_at,
    COUNT(DISTINCT o.id) AS outbox_count,
    COUNT(DISTINCT o.id) FILTER (WHERE o.status = 'PENDING') AS pending_outbox_count,
    COUNT(DISTINCT o.id) FILTER (WHERE o.status = 'SENT') AS sent_outbox_count,
    COUNT(DISTINCT o.id) FILTER (WHERE o.status = 'FAILED') AS failed_outbox_count,
    MAX(o.created_at) FILTER (WHERE o.status = 'FAILED' AND o.sent_at IS NULL) AS last_unsent_failed_outbox_created_at,
    COUNT(d.id) AS delivery_count,
    MIN(d.sent_at) FILTER (WHERE d.status = 'SENT') AS first_success_at,
    MAX(d.sent_at) FILTER (WHERE d.status = 'SENT') AS last_success_at,
    COUNT(d.id) FILTER (WHERE d.status = 'SENT') AS success_send_count,
    COUNT(DISTINCT d.room_id) FILTER (WHERE d.status = 'SENT') AS success_room_count,
    (SELECT COUNT(*) FROM youtube_notification_delivery_ledger AS l
      WHERE l.kind = track.kind AND l.logical_id = track.canonical_content_id AND l.status = 'SENT') AS ledger_sent_room_count,
    (SELECT COUNT(*) FROM youtube_notification_delivery_ledger AS l
      WHERE l.kind = track.kind AND l.logical_id = track.canonical_content_id AND l.status = 'QUARANTINED') AS ledger_quarantined_room_count
FROM youtube_content_alarm_tracking AS track
LEFT JOIN outbox_post AS o
    ON o.kind = track.kind
   AND o.post_id = track.canonical_content_id
LEFT JOIN youtube_notification_delivery AS d
    ON d.outbox_id = o.id
WHERE track.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
  AND COALESCE(track.actual_published_at, track.detected_at) >= NOW() - INTERVAL '24 hours'
GROUP BY
    track.kind,
    track.channel_id,
    track.content_id,
    track.actual_published_at,
    track.detected_at,
    track.canonical_content_id
ORDER BY track.channel_id ASC, track.content_id ASC;
SQL
```

## Readout

- `success_send_count > 0`: outbox delivery 발송이 성공했습니다. 같은 `outbox_id`의 최종 결과 로그에서 `delivery_path = youtube_outbox_dispatcher`를 확인합니다.
- `pending_outbox_count > 0`이고 `delivery_count = 0`: outbox가 room별 delivery로 아직 fan-out되지 않았습니다. 미발송 후보입니다.
- `outbox_count > 0`, `sent_outbox_count = outbox_count`, `delivery_count = 0`, `ledger_sent_room_count = 0`, `ledger_quarantined_room_count = 0`: 구독 room이 없어 outbox가 delivery 없이 SENT로 끝난 게시물입니다. 미발송이 아닙니다. 원장 room이 있으면 정리된 다른 outbox가 그 room에 발송했거나 격리된 것이므로 [Send Counts](YOUTUBE_COMMUNITY_SHORTS_SEND_COUNTS_LAST_24H.md)로 판정합니다.
- `failed_outbox_count > 0`, `delivery_count = 0`, `ledger_sent_room_count = 0`: 구독 조회 등 fan-out이 재시도 한도까지 실패해 room별 delivery 없이 끝났습니다. `youtube_delivery.revive_enabled`가 켜져 있으면 `last_unsent_failed_outbox_created_at`부터 `youtube_delivery.revive_freshness_window_ms`가 지나기 전까지 revive가 outbox를 PENDING으로 되돌려 fan-out을 다시 하므로 아직 최종이 아닙니다. 그 뒤에도 남으면 발송 누락이므로 outbox의 `error`를 확인합니다. `ledger_sent_room_count > 0`이면 정리된 다른 outbox가 이미 그 room에 발송했으므로 [Send Counts](YOUTUBE_COMMUNITY_SHORTS_SEND_COUNTS_LAST_24H.md)로 판정합니다.
- `outbox_count = 0`: outbox가 없습니다. alarm-worker 경로에 진입하지 않았거나, 끝난 outbox가 `youtube_delivery.cleanup_after_ms`를 넘겨 지워졌습니다. 원장 room 수(`ledger_sent_room_count`, `ledger_quarantined_room_count`)가 있으면 정리된 경우입니다([Send Counts](YOUTUBE_COMMUNITY_SHORTS_SEND_COUNTS_LAST_24H.md)의 `sent_outbox_cleaned`). 원장 room도 없으면 진입 전 누락과 실패 뒤 정리를 DB로 구분할 수 없으므로 [Send Counts](YOUTUBE_COMMUNITY_SHORTS_SEND_COUNTS_LAST_24H.md)의 `outbox_missing` 기준대로 로그로 확인합니다.