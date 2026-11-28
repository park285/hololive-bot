INSERT INTO bot_reply_outbox_resolution_audit (
    id, outbox_id, decision, observed_iris_state, actor, reason, recorded_at
) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)
