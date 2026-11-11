-- 발송 route 생산자는 text·richtext만 만들고 구독 저장은 빈 alarm_types를 기본값으로 바꿔 쓴다. 읽기 쪽은 빈 배열을
-- 해석하지 않으므로 DB가 같은 불변식을 거절한다. 세 표 모두 수백 행 이하라 한 트랜잭션에서 검증한다.
BEGIN;

ALTER TABLE public.alarm_dispatch_send_units DROP CONSTRAINT IF EXISTS chk_alarm_send_request_shape;
ALTER TABLE public.alarm_dispatch_send_units ADD CONSTRAINT chk_alarm_send_request_shape CHECK ((((request_generation >= 0) AND (request_generation <= 2)) AND (((request_body IS NULL) AND (request_route IS NULL) AND (request_body_hash IS NULL) AND (request_delivery_ids IS NULL) AND (base_client_request_id IS NULL) AND (request_generation = 0)) OR ((request_body IS NOT NULL) AND (request_route IS NOT NULL) AND (request_route = ANY (ARRAY['text'::text, 'richtext'::text])) AND (request_body_hash IS NOT NULL) AND (request_body_hash ~ '^[0-9a-f]{64}$'::text) AND (request_delivery_ids IS NOT NULL) AND ((cardinality(request_delivery_ids) >= 1) AND (cardinality(request_delivery_ids) <= 10)) AND (base_client_request_id IS NOT NULL) AND (base_client_request_id ~ '^[A-Za-z0-9._:-]{8,160}$'::text)))));

ALTER TABLE public.youtube_notification_send_request DROP CONSTRAINT IF EXISTS youtube_notification_send_request_route_check;
ALTER TABLE public.youtube_notification_send_request ADD CONSTRAINT youtube_notification_send_request_route_check CHECK ((route = ANY (ARRAY['text'::text, 'richtext'::text])));

ALTER TABLE public.alarms DROP CONSTRAINT IF EXISTS chk_alarms_alarm_types_nonempty;
ALTER TABLE public.alarms ADD CONSTRAINT chk_alarms_alarm_types_nonempty CHECK ((cardinality(alarm_types) >= 1));

COMMIT;
