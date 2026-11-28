INSERT INTO bot_reply_outbox_resolution_audit (outbox_id, decision, observed_iris_state, actor, reason, recorded_at)
VALUES (?1, 'discarded_without_replay', ?2, ?3, ?4, ?5)
