SELECT subject_key, min(poll_interval_ms), max(poll_interval_ms), max(priority)
FROM extension_benchmark.targets
WHERE generation = 1 AND kind IN (0, 1) AND enabled
GROUP BY subject_key;
