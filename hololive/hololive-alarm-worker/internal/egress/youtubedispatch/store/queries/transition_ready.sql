SELECT EXISTS (
    SELECT 1
    FROM youtube_notification_delivery_ledger_state
    WHERE singleton
      AND schema_version = $1
      AND completed_at IS NOT NULL
);
