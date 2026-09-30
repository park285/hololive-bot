-- 발송을 시작한 기존 행의 본문은 추정하지 않는다. NULL은 운영 전환 분류 대상으로 보존한다.
ALTER TABLE alarm_dispatch_send_units
    ADD COLUMN IF NOT EXISTS request_body TEXT,
    ADD COLUMN IF NOT EXISTS request_route TEXT,
    ADD COLUMN IF NOT EXISTS request_body_hash TEXT,
    ADD COLUMN IF NOT EXISTS request_delivery_ids BIGINT[],
    ADD COLUMN IF NOT EXISTS base_client_request_id TEXT,
    ADD COLUMN IF NOT EXISTS request_generation INTEGER NOT NULL DEFAULT 0;

DO $migration$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'alarm_dispatch_send_units'::regclass AND conname = 'chk_alarm_send_request_shape') THEN
        ALTER TABLE alarm_dispatch_send_units ADD CONSTRAINT chk_alarm_send_request_shape CHECK (
            request_generation BETWEEN 0 AND 2 AND (
                (request_body IS NULL AND request_route IS NULL AND request_body_hash IS NULL AND request_delivery_ids IS NULL AND base_client_request_id IS NULL AND request_generation = 0)
                OR (request_body IS NOT NULL AND request_route IS NOT NULL AND request_route IN ('text', 'markdown')
                    AND request_body_hash IS NOT NULL AND request_body_hash ~ '^[0-9a-f]{64}$'
                    AND request_delivery_ids IS NOT NULL AND cardinality(request_delivery_ids) BETWEEN 1 AND 10
                    AND base_client_request_id IS NOT NULL AND base_client_request_id ~ '^[A-Za-z0-9._:-]{8,160}$')
            )
        ) NOT VALID;
    END IF;
END
$migration$;
ALTER TABLE alarm_dispatch_send_units VALIDATE CONSTRAINT chk_alarm_send_request_shape;
