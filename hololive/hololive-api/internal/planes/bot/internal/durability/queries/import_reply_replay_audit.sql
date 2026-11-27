INSERT INTO bot_reply_outbox_replay_audit (
    id, outbox_id, grant_number, event_type, actor, reason, recorded_at
) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)
