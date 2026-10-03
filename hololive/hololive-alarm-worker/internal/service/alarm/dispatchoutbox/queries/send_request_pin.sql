UPDATE alarm_dispatch_send_units SET request_body = $2, request_route = $3, request_body_hash = $4,
       request_delivery_ids = $5, base_client_request_id = $6 WHERE id = $1 AND request_body IS NULL
