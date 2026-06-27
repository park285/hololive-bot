-- 051_normalize_legacy_youtube_short_content_ids.sql
-- 쇼츠 identity를 short:<video_id> canonical form으로 정렬한다.
-- 이미 canonical row가 존재하는 legacy raw row는 코드 레벨 alias 재사용이 처리하므로 여기서는 건너뛴다.

WITH canonicalizable_outbox AS (
    SELECT
        o.id,
        CONCAT('short:', BTRIM(o.content_id)) AS canonical_content_id
    FROM youtube_notification_outbox AS o
    WHERE o.kind = 'NEW_SHORT'
      AND NULLIF(BTRIM(o.content_id), '') IS NOT NULL
      AND BTRIM(o.content_id) NOT LIKE 'short:%'
), updatable_outbox AS (
    SELECT c.id, c.canonical_content_id
    FROM canonicalizable_outbox AS c
    WHERE NOT EXISTS (
        SELECT 1
        FROM youtube_notification_outbox AS existing
        WHERE existing.kind = 'NEW_SHORT'
          AND existing.content_id = c.canonical_content_id
    )
)
UPDATE youtube_notification_outbox AS o
SET content_id = u.canonical_content_id
FROM updatable_outbox AS u
WHERE o.id = u.id;

WITH canonicalizable_tracking AS (
    SELECT
        t.kind,
        t.content_id,
        CONCAT('short:', BTRIM(t.content_id)) AS canonical_content_id
    FROM youtube_content_alarm_tracking AS t
    WHERE t.kind = 'NEW_SHORT'
      AND NULLIF(BTRIM(t.content_id), '') IS NOT NULL
      AND BTRIM(t.content_id) NOT LIKE 'short:%'
), updatable_tracking AS (
    SELECT c.kind, c.content_id, c.canonical_content_id
    FROM canonicalizable_tracking AS c
    WHERE NOT EXISTS (
        SELECT 1
        FROM youtube_content_alarm_tracking AS existing
        WHERE existing.kind = c.kind
          AND existing.content_id = c.canonical_content_id
    )
)
UPDATE youtube_content_alarm_tracking AS t
SET content_id = u.canonical_content_id
FROM updatable_tracking AS u
WHERE t.kind = u.kind
  AND t.content_id = u.content_id;

UPDATE youtube_content_watermarks
SET last_content_id = CONCAT('short:', BTRIM(last_content_id))
WHERE watermark_type = 'SHORT'
  AND NULLIF(BTRIM(last_content_id), '') IS NOT NULL
  AND BTRIM(last_content_id) NOT LIKE 'short:%';

UPDATE youtube_notification_delivery_telemetry
SET content_id = CONCAT('short:', BTRIM(content_id))
WHERE alarm_type = 'SHORTS'
  AND NULLIF(BTRIM(content_id), '') IS NOT NULL
  AND BTRIM(content_id) NOT LIKE 'short:%';

UPDATE youtube_notification_delivery_telemetry
SET post_id = CONCAT('short:', BTRIM(post_id))
WHERE alarm_type = 'SHORTS'
  AND NULLIF(BTRIM(post_id), '') IS NOT NULL
  AND BTRIM(post_id) NOT LIKE 'short:%';

UPDATE youtube_notification_delivery_telemetry
SET dedupe_key = CONCAT('youtube-notification:NEW_SHORT:', BTRIM(content_id))
WHERE alarm_type = 'SHORTS'
  AND NULLIF(BTRIM(content_id), '') IS NOT NULL;
