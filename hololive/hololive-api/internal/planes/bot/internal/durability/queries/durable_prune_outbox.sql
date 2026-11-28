WITH terminal AS (
    SELECT id, updated_at
    FROM bot_reply_outbox
    WHERE status IN ('handoff_completed', 'dead', 'permanent_conflict', 'discarded')
      AND updated_at < ?1
    ORDER BY updated_at ASC, id ASC
    LIMIT ?3
), review AS (
    SELECT id, updated_at
    FROM bot_reply_outbox
    WHERE status = 'manual_review'
      AND updated_at < ?2
    ORDER BY updated_at ASC, id ASC
    LIMIT ?3
)
DELETE FROM bot_reply_outbox
WHERE id IN (
    SELECT id
    FROM (SELECT id, updated_at FROM terminal UNION ALL SELECT id, updated_at FROM review)
    ORDER BY updated_at ASC, id ASC
    LIMIT ?3
)
