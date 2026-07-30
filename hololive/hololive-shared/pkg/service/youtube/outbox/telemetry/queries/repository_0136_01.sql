
		INSERT INTO youtube_notification_delivery_telemetry (
			delivery_id, attempt_ordinal, outbox_id, channel_id, content_id, post_id, room_id, alarm_type,
			actual_published_at, alarm_sent_at, alarm_latency_millis, detected_at,
			dedupe_key, delivery_path, delivery_mode, send_result, failure_reason,
			attempt_started_at, attempt_finished_at, event_at, next_attempt_at, locked_at, logged_at, error
		) VALUES 