(CASE WHEN d.last_error_code ~ '^[A-Za-z0-9_.:-]{1,128}$' THEN d.last_error_code WHEN d.last_error_code = '' THEN '' ELSE 'unclassified' END)
