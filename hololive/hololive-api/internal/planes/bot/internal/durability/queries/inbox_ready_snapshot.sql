SELECT count(due.id), coalesce(max(?1 - min(due.available_at), 0), 0)
{{inbox_ready_relation}}
