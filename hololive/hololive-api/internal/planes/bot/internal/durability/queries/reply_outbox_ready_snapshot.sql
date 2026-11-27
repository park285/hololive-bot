SELECT count(candidate.id), coalesce(max(?1 - min(candidate.available_at), 0), 0)
{{reply_ready_relation}}
  AND NOT {{safety_expired}}
