SELECT cardinality(retention_window.deleted_ids), retention_window.passed, retention_window.traversal_completed
FROM public.delete_source_observation_retention_window($1, $2, $3) retention_window
