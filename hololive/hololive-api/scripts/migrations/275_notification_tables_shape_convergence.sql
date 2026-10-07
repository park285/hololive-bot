-- 알림 테이블의 운영·fresh 모양을 맞춘다. 운영 테이블은 레거시 애플리케이션이 먼저 만들어 epoch-1의
-- CREATE TABLE IF NOT EXISTS가 적용되지 않았다.
-- - notification_delivery_outbox.attempt_count는 운영의 bigint를 따른다. 운영은 이미 bigint라 바꾸지 않고 fresh만 넓힌다.
-- - youtube_notification_outbox의 운영 전용 status CHECK(DISPATCHED 포함)는 chk_youtube_notification_outbox_status_vocab보다
--   느슨해 실효가 없고, 운영 전용 dispatched_at은 읽거나 쓰는 코드가 없다(2026-10-08 운영 56행 모두 NULL).
--   NULL이 아닌 값이 생겼으면 저장소 밖에서 쓰기 시작했다는 뜻이므로 열을 지우지 않고 실패한다.
-- - notification_template_revisions의 template FK(ON DELETE CASCADE)는 운영에만 없다. 템플릿 삭제가 revision 이력까지
--   지운다는 268의 전제를 운영에서도 성립시킨다. 고아 revision은 지우지 않으며, 있으면 VALIDATE가 실패해 적용이 멈춘다
--   (2026-10-08 운영 고아 0).
-- 실패하면 ledger에 남지 않아 다음 실행에서 파일 전체를 다시 적용하므로 모든 변경은 현재 catalog를 확인한 뒤 실행한다.
-- 테이블마다 별도 트랜잭션으로 잠가 youtube_notification_outbox(hot table)의 잠금 시간을 dispatched_at 검사와
-- 한 ALTER로 한정한다.
BEGIN;
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';
DO $migration$
DECLARE
    column_type text;
BEGIN
    SELECT pg_catalog.format_type(attribute.atttypid, attribute.atttypmod) INTO column_type
    FROM pg_catalog.pg_attribute AS attribute
    WHERE attribute.attrelid = 'public.notification_delivery_outbox'::regclass
      AND attribute.attname = 'attempt_count'
      AND NOT attribute.attisdropped;
    IF column_type = 'integer' THEN
        ALTER TABLE public.notification_delivery_outbox ALTER COLUMN attempt_count TYPE bigint;
    ELSIF column_type IS DISTINCT FROM 'bigint' THEN
        RAISE EXCEPTION 'notification_delivery_outbox.attempt_count has unexpected type %', column_type;
    END IF;
END
$migration$;
COMMIT;

BEGIN;
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';
DO $migration$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_catalog.pg_attribute AS attribute
        WHERE attribute.attrelid = 'public.youtube_notification_outbox'::regclass
          AND attribute.attname = 'dispatched_at'
          AND NOT attribute.attisdropped
    ) THEN
        RETURN;
    END IF;
    -- 검사와 DROP COLUMN이 같은 잠금을 쓰게 처음부터 ACCESS EXCLUSIVE를 잡는다. 아래 조회는 열이 있을 때만 계획된다.
    LOCK TABLE public.youtube_notification_outbox IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (SELECT 1 FROM public.youtube_notification_outbox WHERE dispatched_at IS NOT NULL) THEN
        RAISE EXCEPTION 'youtube_notification_outbox.dispatched_at has values; column kept';
    END IF;
END
$migration$;
ALTER TABLE public.youtube_notification_outbox
    DROP CONSTRAINT IF EXISTS youtube_notification_outbox_status_check,
    DROP COLUMN IF EXISTS dispatched_at;
COMMIT;

BEGIN;
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';
DO $migration$
DECLARE
    constraint_text text;
    expected_text text;
BEGIN
    -- regclass 출력과 pg_get_constraintdef는 같은 search_path 규칙으로 스키마를 붙이므로 경로와 무관하게 비교된다.
    expected_text := pg_catalog.format(
        'FOREIGN KEY (template_id) REFERENCES %s(id) ON DELETE CASCADE',
        'public.notification_templates'::regclass
    );
    SELECT pg_catalog.pg_get_constraintdef(con.oid) INTO constraint_text
    FROM pg_catalog.pg_constraint AS con
    WHERE con.conrelid = 'public.notification_template_revisions'::regclass
      AND con.conname = 'notification_template_revisions_template_id_fkey';
    IF NOT FOUND THEN
        ALTER TABLE public.notification_template_revisions
            ADD CONSTRAINT notification_template_revisions_template_id_fkey
            FOREIGN KEY (template_id) REFERENCES public.notification_templates (id) ON DELETE CASCADE NOT VALID;
    ELSIF constraint_text NOT IN (expected_text, expected_text || ' NOT VALID') THEN
        RAISE EXCEPTION 'notification_template_revisions_template_id_fkey has unexpected definition %', constraint_text;
    END IF;
END
$migration$;
COMMIT;

-- 쓰기를 막지 않는 잠금으로 기존 revision을 검증한다. 이미 검증된 제약이면 아무것도 하지 않는다.
ALTER TABLE public.notification_template_revisions VALIDATE CONSTRAINT notification_template_revisions_template_id_fkey;
