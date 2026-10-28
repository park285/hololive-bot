-- 새 YouTube Markdown 요청의 richtext route를 writer 배포 전에 허용한다.
-- YouTube 원장이 소유하는 기존 markdown 행은 body·semantic hash·요청 ID를 바꾸지 않는다.
-- 기존 행이 모두 drain된 뒤에만 markdown 읽기 경로와 허용 값을 제거한다. sender 어휘는 기존 계약대로 둔다.
-- 329와 같이 제약 교체와 전 행 검증의 트랜잭션을 분리한다. 중간 실패 뒤 재실행해도 안전하다.
BEGIN;
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE youtube_notification_send_request
    DROP CONSTRAINT IF EXISTS youtube_notification_send_request_route_check,
    ADD CONSTRAINT youtube_notification_send_request_route_check CHECK (route IN ('text', 'markdown', 'richtext', 'sender')) NOT VALID;

COMMIT;

DO $$
BEGIN
    PERFORM set_config('lock_timeout', '3s', true);
    ALTER TABLE youtube_notification_send_request VALIDATE CONSTRAINT youtube_notification_send_request_route_check;
END
$$;
