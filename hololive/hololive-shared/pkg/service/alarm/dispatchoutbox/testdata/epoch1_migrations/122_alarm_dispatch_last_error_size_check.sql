-- 122_alarm_dispatch_last_error_size_check.sql
-- 8192는 app chokepoint(error_store.go)의 2048B truncation보다 넓다 —
-- app 한도를 8KiB까지는 migration 없이 올릴 수 있게 둔 방어선이다.

ALTER TABLE alarm_dispatch_deliveries
    DROP CONSTRAINT IF EXISTS chk_alarm_dispatch_deliveries_last_error_size;
ALTER TABLE alarm_dispatch_deliveries
    ADD CONSTRAINT chk_alarm_dispatch_deliveries_last_error_size
    CHECK (octet_length(last_error) <= 8192) NOT VALID;
ALTER TABLE alarm_dispatch_deliveries
    VALIDATE CONSTRAINT chk_alarm_dispatch_deliveries_last_error_size;
