DELETE FROM bot_reply_outbox
WHERE id IN (
    SELECT id
    FROM bot_reply_outbox
    WHERE (
        status IN ('handoff_completed', 'dead', 'permanent_conflict', 'discarded')
        AND updated_at < ?1
    ) OR (
        status = 'manual_review'
        AND updated_at < ?2
    )
    ORDER BY updated_at ASC, id ASC
    LIMIT ?3
)
