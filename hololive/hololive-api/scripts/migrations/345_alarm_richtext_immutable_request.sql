-- 새 Markdown 요청은 richtext로 고정한다. writer 배포 전에 이 제약 확장을 적용한다.
-- alarm 원장이 소유하는 기존 markdown 행은 같은 ID의 body·semantic hash 계약 때문에
-- 갱신하지 않는다. 기존 행이 모두 drain된 뒤에만 markdown 읽기 경로와 허용 값을 제거한다.
-- 제약 교체는 짧은 트랜잭션으로 원자화하고, 전 행 검증은 ACCESS EXCLUSIVE를 놓은 뒤 수행한다.
-- 중간 실패 뒤 파일 전체를 재실행해도 데이터와 제약 의미가 달라지지 않는다(329 선례).
BEGIN;
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE alarm_dispatch_send_units
    DROP CONSTRAINT IF EXISTS chk_alarm_send_request_shape,
    ADD CONSTRAINT chk_alarm_send_request_shape CHECK (
        request_generation BETWEEN 0 AND 2 AND (
            (request_body IS NULL AND request_route IS NULL AND request_body_hash IS NULL AND request_delivery_ids IS NULL AND base_client_request_id IS NULL AND request_generation = 0)
            OR (request_body IS NOT NULL AND request_route IS NOT NULL AND request_route IN ('text', 'markdown', 'richtext')
                AND request_body_hash IS NOT NULL AND request_body_hash ~ '^[0-9a-f]{64}$'
                AND request_delivery_ids IS NOT NULL AND cardinality(request_delivery_ids) BETWEEN 1 AND 10
                AND base_client_request_id IS NOT NULL AND base_client_request_id ~ '^[A-Za-z0-9._:-]{8,160}$')
        )
    ) NOT VALID;

COMMIT;

DO $$
BEGIN
    PERFORM set_config('lock_timeout', '3s', true);
    ALTER TABLE alarm_dispatch_send_units VALIDATE CONSTRAINT chk_alarm_send_request_shape;
END
$$;
