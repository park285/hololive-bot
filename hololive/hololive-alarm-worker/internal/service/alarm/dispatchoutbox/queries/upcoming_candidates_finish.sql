UPDATE alarm_upcoming_candidates SET outcome = $2, terminal_at = $3, checked_at = $3
WHERE dedupe_key = $1 AND outcome = 'pending'
