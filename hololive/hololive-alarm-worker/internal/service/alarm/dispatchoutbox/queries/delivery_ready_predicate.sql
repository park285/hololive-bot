d.send_unit_id IS NOT NULL
AND d.status IN ('pending', 'retry')
AND d.next_attempt_at <= (SELECT clock.now FROM clock)
