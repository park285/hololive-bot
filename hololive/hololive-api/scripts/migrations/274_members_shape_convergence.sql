-- 운영 members는 레거시 애플리케이션이 먼저 만들어 epoch-1의 CREATE TABLE IF NOT EXISTS가 적용되지 않았고,
-- 그래서 fresh와 모양이 다르다. 두 쪽을 다음 모양으로 맞춘다.
-- - english_name·japanese_name·korean_name은 varchar(200)이다. 운영의 varchar(100)을 넓히며 테이블을 다시 쓰지 않는다.
-- - is_graduated는 NOT NULL이고 DEFAULT false를 유지한다.
-- - aliases는 NOT NULL이고 기본값은 빈 ko·ja 배열이다. 조회 코드·GIN 인덱스·관리 화면이 공유하는 "ko와 ja 배열을
--   가진 객체" 계약을 저장소 소유 CHECK chk_members_aliases_shape로 둔다.
-- - 운영에만 있는 check_aliases_structure는 위 CHECK로 대체하고, check_status는 더 엄격한
--   chk_members_status_vocab에 포함되므로 지운다.
-- - 운영에만 있는 created_at·updated_at은 읽거나 쓰는 코드가 없어 지운다. 운영 135행의 시각 값은 DB에서 사라진다.
--   마지막 전체 DB 복원 검증은 2026-10-07이고 그 뒤 백업은 중단됐다(DEPLOYMENT_BASELINE.md). 적용 시점의 값을
--   남기려면 운영 적용 전에 승인된 읽기 전용 세션에서 SELECT id, slug, created_at, updated_at FROM public.members
--   결과를 추출해 보관 위치를 기록한다.
-- 2026-10-08 서울 운영 조회: 135행, 이름 최대 길이 25자, is_graduated·aliases NULL 0, 형식이 어긋난 aliases 0.
-- 이름 열의 typmod가 바뀌므로 이 열을 결과로 반환하는 문장을 POSTGRES_QUERY_EXEC_MODE=cache_statement로 이미 준비한
-- 연결은 첫 트랜잭션 COMMIT 뒤 그 문장의 다음 실행에서 한 번 cached plan must not change result type(0A000)로
-- 실패한다. pgx가 실패한 문장을 캐시에서 지워 그다음 실행부터 회복하지만, 이 오류를 줄이려면 db-migrate 직후
-- members를 읽는 hololive-api와 hololive-alarm-worker를 모두 순차 재기동한다. 단일 서비스 재배포
-- (compose-redeploy-service.sh hololive-api)도 전체 migration을 먼저 실행하므로 alarm-worker 재기동을 따로 한다.
-- 실패하면 ledger에 남지 않아 다음 실행에서 파일 전체를 다시 적용하므로 모든 변경은 현재 catalog를 확인한 뒤 실행한다.
-- 같은 이름의 객체가 다른 정의로 있으면 덮어쓰지 않고 중단한다. 행 검증이 실패하면 열과 제약을 지우기 전에 멈춘다.
BEGIN;
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';
DO $migration$
DECLARE
    target_column text;
    column_type text;
    pending record;
    constraint_kind "char";
    constraint_text text;
BEGIN
    -- varchar 확대만 허용한다. 다른 타입이면 의도하지 않은 drift이므로 중단한다.
    FOREACH target_column IN ARRAY ARRAY['english_name', 'japanese_name', 'korean_name'] LOOP
        SELECT pg_catalog.format_type(attribute.atttypid, attribute.atttypmod) INTO column_type
        FROM pg_catalog.pg_attribute AS attribute
        WHERE attribute.attrelid = 'public.members'::regclass
          AND attribute.attname = target_column
          AND NOT attribute.attisdropped;
        IF column_type = 'character varying(100)' THEN
            EXECUTE pg_catalog.format('ALTER TABLE public.members ALTER COLUMN %I TYPE varchar(200)', target_column);
        ELSIF column_type IS DISTINCT FROM 'character varying(200)' THEN
            RAISE EXCEPTION 'members.% has unexpected type %', target_column, column_type;
        END IF;
    END LOOP;

    -- SET NOT NULL이 전체 스캔을 생략하도록 NOT VALID CHECK를 먼저 둔다. 이미 NOT NULL인 열에는 만들지 않는다.
    FOR pending IN
        SELECT required.column_name, required.constraint_name
        FROM (VALUES ('is_graduated', 'members_is_graduated_nn'), ('aliases', 'members_aliases_nn'))
            AS required(column_name, constraint_name)
    LOOP
        SELECT con.contype, pg_catalog.pg_get_expr(con.conbin, con.conrelid) INTO constraint_kind, constraint_text
        FROM pg_catalog.pg_constraint AS con
        WHERE con.conrelid = 'public.members'::regclass
          AND con.conname = pending.constraint_name;
        IF FOUND THEN
            IF constraint_kind <> 'c'
               OR constraint_text IS DISTINCT FROM pg_catalog.format('(%s IS NOT NULL)', pending.column_name) THEN
                RAISE EXCEPTION '% has unexpected definition %', pending.constraint_name, constraint_text;
            END IF;
        ELSIF NOT (
            SELECT attribute.attnotnull
            FROM pg_catalog.pg_attribute AS attribute
            WHERE attribute.attrelid = 'public.members'::regclass
              AND attribute.attname = pending.column_name
              AND NOT attribute.attisdropped
        ) THEN
            EXECUTE pg_catalog.format(
                'ALTER TABLE public.members ADD CONSTRAINT %I CHECK (%I IS NOT NULL) NOT VALID',
                pending.constraint_name,
                pending.column_name
            );
        END IF;
    END LOOP;

    SELECT con.contype, pg_catalog.pg_get_expr(con.conbin, con.conrelid) INTO constraint_kind, constraint_text
    FROM pg_catalog.pg_constraint AS con
    WHERE con.conrelid = 'public.members'::regclass
      AND con.conname = 'chk_members_aliases_shape';
    IF NOT FOUND THEN
        ALTER TABLE public.members ADD CONSTRAINT chk_members_aliases_shape CHECK (
            jsonb_typeof(aliases) = 'object'
            AND aliases ? 'ko'
            AND aliases ? 'ja'
            AND jsonb_typeof(aliases -> 'ko') = 'array'
            AND jsonb_typeof(aliases -> 'ja') = 'array'
        ) NOT VALID;
    ELSIF constraint_kind <> 'c'
          OR constraint_text IS DISTINCT FROM
             '((jsonb_typeof(aliases) = ''object''::text) AND (aliases ? ''ko''::text) AND (aliases ? ''ja''::text)'
             ' AND (jsonb_typeof((aliases -> ''ko''::text)) = ''array''::text)'
             ' AND (jsonb_typeof((aliases -> ''ja''::text)) = ''array''::text))' THEN
        RAISE EXCEPTION 'chk_members_aliases_shape has unexpected definition %', constraint_text;
    END IF;
END
$migration$;
-- 운영 기본값과 같다. CreateMember는 aliases를 항상 명시하지만 열을 생략한 seed도 계약을 만족하게 한다.
ALTER TABLE public.members ALTER COLUMN aliases SET DEFAULT '{"ja": [], "ko": []}'::jsonb;
COMMIT;

-- 쓰기를 막지 않는 잠금으로 검증한다. 재실행 때 이미 지운 NOT NULL용 CHECK는 건너뛴다.
DO $validate$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_catalog.pg_constraint AS con
        WHERE con.conrelid = 'public.members'::regclass
          AND con.conname = 'members_is_graduated_nn'
    ) THEN
        ALTER TABLE public.members VALIDATE CONSTRAINT members_is_graduated_nn;
    END IF;
END
$validate$;
DO $validate$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_catalog.pg_constraint AS con
        WHERE con.conrelid = 'public.members'::regclass
          AND con.conname = 'members_aliases_nn'
    ) THEN
        ALTER TABLE public.members VALIDATE CONSTRAINT members_aliases_nn;
    END IF;
END
$validate$;
ALTER TABLE public.members VALIDATE CONSTRAINT chk_members_aliases_shape;

BEGIN;
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';
DO $migration$
DECLARE
    target_column text;
BEGIN
    -- 검증된 NOT NULL용 CHECK가 NULL 부재를 증명하므로 SET NOT NULL은 테이블을 다시 읽지 않는다.
    FOREACH target_column IN ARRAY ARRAY['is_graduated', 'aliases'] LOOP
        IF NOT (
            SELECT attribute.attnotnull
            FROM pg_catalog.pg_attribute AS attribute
            WHERE attribute.attrelid = 'public.members'::regclass
              AND attribute.attname = target_column
              AND NOT attribute.attisdropped
        ) THEN
            EXECUTE pg_catalog.format('ALTER TABLE public.members ALTER COLUMN %I SET NOT NULL', target_column);
        END IF;
    END LOOP;
END
$migration$;
ALTER TABLE public.members
    DROP CONSTRAINT IF EXISTS members_is_graduated_nn,
    DROP CONSTRAINT IF EXISTS members_aliases_nn,
    DROP CONSTRAINT IF EXISTS check_aliases_structure,
    DROP CONSTRAINT IF EXISTS check_status,
    DROP COLUMN IF EXISTS created_at,
    DROP COLUMN IF EXISTS updated_at;
COMMIT;
