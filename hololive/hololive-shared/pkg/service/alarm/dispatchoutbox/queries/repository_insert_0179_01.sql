WITH input AS (
	SELECT send_unit_key, dispatch_group_key, room_id, client_request_id
	FROM jsonb_to_recordset($1::jsonb) AS x(
		event_id BIGINT,
		room_id TEXT,
		dedupe_key TEXT,
		claim_keys JSONB,
		delivery_context JSONB,
		dispatch_group_key TEXT,
		send_unit_key TEXT,
		client_request_id TEXT,
		status TEXT
	)
), unit_input AS (
	SELECT DISTINCT send_unit_key, dispatch_group_key, room_id, client_request_id
	FROM input
	WHERE send_unit_key <> ''
)
INSERT INTO alarm_dispatch_send_units (unit_key, dispatch_group_key, room_id, client_request_id)
SELECT send_unit_key, dispatch_group_key, room_id, client_request_id
FROM unit_input
ON CONFLICT (unit_key) DO NOTHING
