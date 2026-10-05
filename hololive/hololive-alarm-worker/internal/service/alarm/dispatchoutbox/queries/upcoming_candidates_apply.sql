UPDATE alarm_upcoming_candidates c
SET outcome = $2, checked_at = $4, terminal_at = $3
WHERE c.dedupe_key = ANY($1::text[]) AND c.outcome = 'pending'
