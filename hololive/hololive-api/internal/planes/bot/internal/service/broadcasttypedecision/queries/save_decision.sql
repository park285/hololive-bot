INSERT INTO youtube_broadcast_type_decisions (video_id, title, outcome, broadcast_type, confidence, model, decided_at)
VALUES ($1, $2, $3, $4, $5, $6, now())
ON CONFLICT (video_id) DO UPDATE
SET title = EXCLUDED.title,
    outcome = EXCLUDED.outcome,
    broadcast_type = EXCLUDED.broadcast_type,
    confidence = EXCLUDED.confidence,
    model = EXCLUDED.model,
    decided_at = EXCLUDED.decided_at
