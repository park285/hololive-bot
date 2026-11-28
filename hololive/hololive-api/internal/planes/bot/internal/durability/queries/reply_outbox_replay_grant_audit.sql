INSERT INTO bot_reply_outbox_replay_audit (outbox_id, grant_number, event_type, actor, reason, recorded_at)
VALUES (?1, ?2, 'granted', ?3, ?4, ?5)
