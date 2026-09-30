SELECT COALESCE(request_body, ''), COALESCE(request_route, ''), COALESCE(request_body_hash, ''),
       request_delivery_ids, COALESCE(base_client_request_id, ''), client_request_id, request_generation, room_id
FROM alarm_dispatch_send_units WHERE id = $1 FOR UPDATE
