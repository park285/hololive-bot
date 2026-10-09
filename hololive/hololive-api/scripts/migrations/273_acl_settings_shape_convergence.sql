-- 운영 acl_settings는 레거시 애플리케이션이 먼저 만들어 epoch-1의 CREATE TABLE IF NOT EXISTS가 적용되지 않았고,
-- 그래서 fresh와 모양이 다르다. 두 쪽을 다음 모양으로 맞춘다.
-- - id와 acl_settings_id_seq는 운영의 bigint를 따른다. 운영은 이미 bigint라 바꾸지 않고 fresh만 넓힌다.
-- - key는 fresh처럼 NOT NULL이고 UNIQUE는 acl_settings_key_key 제약이다. 운영의 같은 열 unique 인덱스
--   idx_acl_settings_key를 제약으로 승격하므로 인덱스를 새로 만들지 않는다.
-- 2026-10-08 서울 운영 조회: 2행, key NULL 0, 중복 key 0.
-- 실패하면 ledger에 남지 않아 다음 실행에서 파일 전체를 다시 적용하므로 모든 변경은 현재 catalog를 확인한 뒤 실행한다.
-- 같은 이름의 객체가 다른 정의로 있으면 덮어쓰지 않고 중단한다.
BEGIN;
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';
DO $migration$
DECLARE
    column_type text;
    sequence_type text;
    constraint_kind "char";
    constraint_text text;
    index_text text;
BEGIN
    SELECT pg_catalog.format_type(attribute.atttypid, attribute.atttypmod) INTO column_type
    FROM pg_catalog.pg_attribute AS attribute
    WHERE attribute.attrelid = 'public.acl_settings'::regclass
      AND attribute.attname = 'id'
      AND NOT attribute.attisdropped;
    IF column_type = 'integer' THEN
        ALTER TABLE public.acl_settings ALTER COLUMN id TYPE bigint;
    ELSIF column_type IS DISTINCT FROM 'bigint' THEN
        RAISE EXCEPTION 'acl_settings.id has unexpected type %', column_type;
    END IF;

    -- 최대값이 integer 최대값(기본값)이면 타입을 바꿀 때 bigint 최대값으로 함께 바뀌어 운영 시퀀스와 같아진다.
    SELECT pg_catalog.format_type(seq.seqtypid, NULL) INTO sequence_type
    FROM pg_catalog.pg_sequence AS seq
    WHERE seq.seqrelid = 'public.acl_settings_id_seq'::regclass;
    IF sequence_type = 'integer' THEN
        ALTER SEQUENCE public.acl_settings_id_seq AS bigint;
    ELSIF sequence_type IS DISTINCT FROM 'bigint' THEN
        RAISE EXCEPTION 'acl_settings_id_seq has unexpected type %', sequence_type;
    END IF;

    -- SET NOT NULL이 전체 스캔을 생략하도록 NOT VALID CHECK를 먼저 둔다. 이미 NOT NULL이면 만들지 않는다.
    SELECT con.contype, pg_catalog.pg_get_expr(con.conbin, con.conrelid) INTO constraint_kind, constraint_text
    FROM pg_catalog.pg_constraint AS con
    WHERE con.conrelid = 'public.acl_settings'::regclass
      AND con.conname = 'acl_settings_key_nn';
    IF FOUND THEN
        IF constraint_kind <> 'c' OR constraint_text IS DISTINCT FROM '(key IS NOT NULL)' THEN
            RAISE EXCEPTION 'acl_settings_key_nn has unexpected definition %', constraint_text;
        END IF;
    ELSIF NOT (
        SELECT attribute.attnotnull
        FROM pg_catalog.pg_attribute AS attribute
        WHERE attribute.attrelid = 'public.acl_settings'::regclass
          AND attribute.attname = 'key'
          AND NOT attribute.attisdropped
    ) THEN
        ALTER TABLE public.acl_settings ADD CONSTRAINT acl_settings_key_nn CHECK (key IS NOT NULL) NOT VALID;
    END IF;

    SELECT con.contype, pg_catalog.pg_get_constraintdef(con.oid) INTO constraint_kind, constraint_text
    FROM pg_catalog.pg_constraint AS con
    WHERE con.conrelid = 'public.acl_settings'::regclass
      AND con.conname = 'acl_settings_key_key';
    IF FOUND THEN
        IF constraint_kind <> 'u' OR constraint_text IS DISTINCT FROM 'UNIQUE (key)' THEN
            RAISE EXCEPTION 'acl_settings_key_key has unexpected definition %', constraint_text;
        END IF;
    ELSE
        SELECT pg_catalog.pg_get_indexdef(index_class.oid) INTO index_text
        FROM pg_catalog.pg_class AS index_class
        WHERE index_class.oid = pg_catalog.to_regclass('public.idx_acl_settings_key');
        IF index_text IS DISTINCT FROM 'CREATE UNIQUE INDEX idx_acl_settings_key ON public.acl_settings USING btree (key)' THEN
            RAISE EXCEPTION 'acl_settings has no acl_settings_key_key and no promotable idx_acl_settings_key: %', index_text;
        END IF;
        -- 인덱스 이름이 제약 이름으로 바뀌는 catalog 변경이며 인덱스를 다시 만들지 않는다.
        ALTER TABLE public.acl_settings ADD CONSTRAINT acl_settings_key_key UNIQUE USING INDEX idx_acl_settings_key;
    END IF;
END
$migration$;
COMMIT;

-- 쓰기를 막지 않는 잠금으로 검증한다. 재실행 때 이미 지운 CHECK는 건너뛴다.
DO $validate$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_catalog.pg_constraint AS con
        WHERE con.conrelid = 'public.acl_settings'::regclass
          AND con.conname = 'acl_settings_key_nn'
    ) THEN
        ALTER TABLE public.acl_settings VALIDATE CONSTRAINT acl_settings_key_nn;
    END IF;
END
$validate$;

BEGIN;
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';
DO $migration$
BEGIN
    -- 검증된 acl_settings_key_nn이 NULL 부재를 증명하므로 SET NOT NULL은 테이블을 다시 읽지 않는다.
    IF NOT (
        SELECT attribute.attnotnull
        FROM pg_catalog.pg_attribute AS attribute
        WHERE attribute.attrelid = 'public.acl_settings'::regclass
          AND attribute.attname = 'key'
          AND NOT attribute.attisdropped
    ) THEN
        ALTER TABLE public.acl_settings ALTER COLUMN key SET NOT NULL;
    END IF;
END
$migration$;
ALTER TABLE public.acl_settings DROP CONSTRAINT IF EXISTS acl_settings_key_nn;
COMMIT;
