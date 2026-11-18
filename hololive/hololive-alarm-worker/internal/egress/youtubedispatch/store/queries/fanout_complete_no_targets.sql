WITH clock AS MATERIALIZED (
    SELECT clock_timestamp() AS at
)
UPDATE youtube_notification_outbox
SET status = $1,
    locked_at = NULL,
    sent_at = (SELECT at FROM clock),
    terminal_at = (SELECT at FROM clock),
    error = ''
WHERE id = $2
  AND status = $3
  AND locked_at = $4;
