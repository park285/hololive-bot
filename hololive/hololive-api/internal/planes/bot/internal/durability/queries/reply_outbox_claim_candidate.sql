SELECT candidate.id, {{safety_expired}}
{{reply_ready_relation}}
ORDER BY candidate.available_at ASC, candidate.id ASC
LIMIT 1
