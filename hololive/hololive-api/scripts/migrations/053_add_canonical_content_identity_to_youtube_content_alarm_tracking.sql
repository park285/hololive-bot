-- 053_add_canonical_content_identity_to_youtube_content_alarm_tracking.sql
-- 커뮤니티/쇼츠 알람 추적 저장소에 canonical 게시물 식별자 무결성을 추가한다.

ALTER TABLE youtube_content_alarm_tracking
    ADD COLUMN IF NOT EXISTS canonical_content_id VARCHAR(50);

WITH normalized_rows AS (
    SELECT
        t.ctid,
        CASE
            WHEN t.kind = 'NEW_SHORT' THEN
                CONCAT(
                    'short:',
                    CASE
                        WHEN BTRIM(t.content_id) LIKE 'short:%' THEN NULLIF(BTRIM(SUBSTRING(BTRIM(t.content_id) FROM 7)), '')
                        ELSE NULLIF(BTRIM(t.content_id), '')
                    END
                )
            WHEN t.kind = 'COMMUNITY_POST' THEN
                CONCAT(
                    'community:',
                    COALESCE(
                        NULLIF(
                            BTRIM(
                                SUBSTRING(
                                    REPLACE(
                                        CASE
                                            WHEN BTRIM(t.content_id) LIKE 'community:%' THEN SUBSTRING(BTRIM(t.content_id) FROM 11)
                                            ELSE BTRIM(t.content_id)
                                        END,
                                        E'\\/',
                                        '/'
                                    )
                                    FROM '(?:^|/)post/([^"?#&/]+)'
                                )
                            ),
                            ''
                        ),
                        NULLIF(
                            BTRIM(
                                CASE
                                    WHEN BTRIM(t.content_id) LIKE 'community:%' THEN SUBSTRING(BTRIM(t.content_id) FROM 11)
                                    ELSE BTRIM(t.content_id)
                                END
                            ),
                            ''
                        )
                    )
                )
            ELSE NULLIF(BTRIM(t.content_id), '')
        END AS canonical_content_id
    FROM youtube_content_alarm_tracking AS t
)
UPDATE youtube_content_alarm_tracking AS t
SET canonical_content_id = n.canonical_content_id
FROM normalized_rows AS n
WHERE t.ctid = n.ctid
  AND n.canonical_content_id IS NOT NULL;

UPDATE youtube_content_alarm_tracking
SET canonical_content_id = BTRIM(content_id)
WHERE canonical_content_id IS NULL
  AND NULLIF(BTRIM(content_id), '') IS NOT NULL;

WITH base_rows AS (
    SELECT
        t.ctid,
        t.kind,
        t.content_id,
        t.canonical_content_id,
        t.channel_id,
        t.actual_published_at,
        t.detected_at,
        t.alarm_sent_at,
        t.latency_classification_status,
        t.delay_source,
        t.internal_delay_cause,
        t.created_at,
        t.updated_at,
        EXISTS (
            SELECT 1
            FROM youtube_notification_outbox AS o
            WHERE o.kind = t.kind
              AND o.content_id = t.content_id
        ) AS has_matching_outbox
    FROM youtube_content_alarm_tracking AS t
    WHERE t.canonical_content_id IS NOT NULL
), ranked_rows AS (
    SELECT
        b.ctid,
        b.kind,
        b.canonical_content_id,
        ROW_NUMBER() OVER (
            PARTITION BY b.kind, b.canonical_content_id
            ORDER BY
                CASE WHEN b.has_matching_outbox THEN 0 ELSE 1 END,
                CASE WHEN b.content_id = b.canonical_content_id THEN 0 ELSE 1 END,
                CASE WHEN b.alarm_sent_at IS NULL THEN 1 ELSE 0 END,
                b.detected_at ASC,
                b.created_at ASC,
                b.content_id ASC
        ) AS survivor_rank
    FROM base_rows AS b
), merged_rows AS (
    SELECT
        b.kind,
        b.canonical_content_id,
        (ARRAY_REMOVE(
            ARRAY_AGG(NULLIF(BTRIM(b.channel_id), '') ORDER BY
                CASE WHEN b.has_matching_outbox THEN 0 ELSE 1 END,
                CASE WHEN b.content_id = b.canonical_content_id THEN 0 ELSE 1 END,
                b.updated_at DESC NULLS LAST,
                b.created_at DESC NULLS LAST,
                b.content_id ASC
            ),
            NULL
        ))[1] AS channel_id,
        MIN(b.actual_published_at) FILTER (WHERE b.actual_published_at IS NOT NULL) AS actual_published_at,
        MIN(b.detected_at) AS detected_at,
        MIN(b.alarm_sent_at) FILTER (WHERE b.alarm_sent_at IS NOT NULL) AS alarm_sent_at,
        (ARRAY_REMOVE(ARRAY_AGG(NULLIF(BTRIM(b.latency_classification_status), '') ORDER BY b.updated_at DESC NULLS LAST, b.created_at DESC NULLS LAST), NULL))[1] AS latency_classification_status,
        (ARRAY_REMOVE(ARRAY_AGG(NULLIF(BTRIM(b.delay_source), '') ORDER BY b.updated_at DESC NULLS LAST, b.created_at DESC NULLS LAST), NULL))[1] AS delay_source,
        (ARRAY_REMOVE(ARRAY_AGG(NULLIF(BTRIM(b.internal_delay_cause), '') ORDER BY b.updated_at DESC NULLS LAST, b.created_at DESC NULLS LAST), NULL))[1] AS internal_delay_cause
    FROM base_rows AS b
    GROUP BY b.kind, b.canonical_content_id
)
UPDATE youtube_content_alarm_tracking AS t
SET canonical_content_id = m.canonical_content_id,
    channel_id = COALESCE(NULLIF(m.channel_id, ''), t.channel_id),
    actual_published_at = m.actual_published_at,
    detected_at = m.detected_at,
    alarm_sent_at = m.alarm_sent_at,
    alarm_latency_millis = CASE
        WHEN m.actual_published_at IS NULL OR m.alarm_sent_at IS NULL THEN NULL
        ELSE CAST(ROUND(EXTRACT(EPOCH FROM (m.alarm_sent_at - m.actual_published_at)) * 1000) AS BIGINT)
    END,
    alarm_latency_exceeded = CASE
        WHEN m.actual_published_at IS NULL OR m.alarm_sent_at IS NULL THEN NULL
        WHEN CAST(ROUND(EXTRACT(EPOCH FROM (m.alarm_sent_at - m.actual_published_at)) * 1000) AS BIGINT) > 120000 THEN TRUE
        ELSE FALSE
    END,
    delivery_status = CASE
        WHEN m.alarm_sent_at IS NULL THEN 'PENDING'
        ELSE 'SENT'
    END,
    latency_classification_status = m.latency_classification_status,
    delay_source = m.delay_source,
    internal_delay_cause = m.internal_delay_cause,
    updated_at = GREATEST(t.updated_at, NOW())
FROM ranked_rows AS r
JOIN merged_rows AS m
  ON m.kind = r.kind
 AND m.canonical_content_id = r.canonical_content_id
WHERE t.ctid = r.ctid
  AND r.survivor_rank = 1;

WITH base_rows AS (
    SELECT
        t.ctid,
        t.kind,
        t.canonical_content_id,
        EXISTS (
            SELECT 1
            FROM youtube_notification_outbox AS o
            WHERE o.kind = t.kind
              AND o.content_id = t.content_id
        ) AS has_matching_outbox,
        t.content_id,
        t.alarm_sent_at,
        t.detected_at,
        t.created_at
    FROM youtube_content_alarm_tracking AS t
    WHERE t.canonical_content_id IS NOT NULL
), ranked_rows AS (
    SELECT
        b.ctid,
        ROW_NUMBER() OVER (
            PARTITION BY b.kind, b.canonical_content_id
            ORDER BY
                CASE WHEN b.has_matching_outbox THEN 0 ELSE 1 END,
                CASE WHEN b.content_id = b.canonical_content_id THEN 0 ELSE 1 END,
                CASE WHEN b.alarm_sent_at IS NULL THEN 1 ELSE 0 END,
                b.detected_at ASC,
                b.created_at ASC,
                b.content_id ASC
        ) AS survivor_rank
    FROM base_rows AS b
)
DELETE FROM youtube_content_alarm_tracking AS t
USING ranked_rows AS r
WHERE t.ctid = r.ctid
  AND r.survivor_rank > 1;

ALTER TABLE youtube_content_alarm_tracking
    ALTER COLUMN canonical_content_id SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_ycat_kind_canonical_content
    ON youtube_content_alarm_tracking(kind, canonical_content_id);

COMMENT ON COLUMN youtube_content_alarm_tracking.canonical_content_id IS '게시물당 1회 발송 보장을 위한 canonical 게시물 식별자';
