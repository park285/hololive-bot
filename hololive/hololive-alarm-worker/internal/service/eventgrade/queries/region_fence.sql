SELECT pg_advisory_xact_lock_shared(20261121, CASE $1::text WHEN 'jp' THEN 1 WHEN 'us' THEN 2 ELSE 3 END)
