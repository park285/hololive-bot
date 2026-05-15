-- Payload room-agnostic guard.
-- 기존 constraint가 있으면 migration 전략에 맞게 name을 조정하십시오.

ALTER TABLE alarm_dispatch_events
ADD CONSTRAINT alarm_dispatch_events_payload_notification_room_agnostic_check
CHECK (
    NOT (payload ? 'room_id')
    AND NOT (payload ? 'roomId')
    AND NOT (payload ? 'room')
    AND NOT (payload ? 'users')
    AND NOT ((payload -> 'notification') ? 'room_id')
    AND NOT ((payload -> 'notification') ? 'roomId')
    AND NOT ((payload -> 'notification') ? 'room')
    AND NOT ((payload -> 'notification') ? 'users')
);

-- 주의:
-- 이 constraint는 현재 payload schema에 맞춘 bounded check입니다.
-- JSON 전체 recursive scan은 insert hot path에서 피합니다.
