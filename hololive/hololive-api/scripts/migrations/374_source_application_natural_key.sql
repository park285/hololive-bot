BEGIN;
SET LOCAL lock_timeout = '3s';

-- 세 키 열은 이미 NOT NULL이고 유효한 unique 인덱스가 있으므로 전 행 스캔·재작성 없이 승격한다.
-- ACCESS EXCLUSIVE는 COMMIT까지 유지된다. 잠금 대기가 길면 전체 cutover를 롤백한다.
DO $migration$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_catalog.pg_attribute
        WHERE attrelid = 'public.source_observation_applications'::regclass
          AND attname = 'id' AND NOT attisdropped
    ) THEN
        ALTER TABLE public.source_observation_applications
            DROP CONSTRAINT source_observation_applications_pkey,
            ADD CONSTRAINT source_observation_applications_pkey
                PRIMARY KEY USING INDEX uq_source_observation_application_entity,
            -- BIGSERIAL의 OWNED BY 의존성으로 sequence도 함께 제거된다.
            DROP COLUMN id;
    END IF;
END
$migration$;
COMMIT;
