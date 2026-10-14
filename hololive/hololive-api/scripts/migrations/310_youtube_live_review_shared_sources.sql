-- append-only 영수증의 의미 사실을 기록 시 한 번 typed 값으로 투영한다. 현재 원본 캐시가 아니다.
-- 현재 원본은 membership에서 읽은 scalar를 그대로 쓰며 head의 대형 진단 배열은 읽지 않는다.
BEGIN;
SET LOCAL lock_timeout = '3s';

-- 숫자는 전부 numeric으로 둬 JSONB의 1=1.0과 epoch 정밀도·무한대를 보존한다.
-- 이 타입에는 시간대·표시 설정에 의존하는 타입을 추가하지 않는다. decoder의 IMMUTABLE 전제다.
DO $types$
DECLARE
    expected record;
BEGIN
    IF to_regtype('public.youtube_live_review_session_value') IS NULL THEN
        CREATE TYPE public.youtube_live_review_session_value AS (
            video_id text,
            channel_id text,
            status text,
            lifecycle_origin text,
            title text,
            topic_id text,
            is_premiere boolean,
            scheduled_start_time numeric,
            started_at numeric,
            ended_at numeric,
            live_first_seen_at numeric);
    END IF;
    IF to_regtype('public.youtube_live_review_head_value') IS NULL THEN
        CREATE TYPE public.youtube_live_review_head_value AS (
            status text,
            last_upcoming_positive_at numeric,
            last_upcoming_positive_seen_at numeric,
            last_live_positive_at numeric,
            last_live_positive_seen_at numeric,
            last_end_evidence_at numeric,
            last_complete_absence_at numeric,
            last_absence_scheduled_for numeric,
            first_absence_scheduled_for numeric,
            second_absence_scheduled_for numeric,
            consecutive_absence_slots numeric,
            last_absence_observation_id numeric,
            end_candidate_kind text,
            end_candidate_observation_id numeric,
            ended_at numeric,
            end_reason text);
    END IF;
    IF to_regtype('public.youtube_live_review_pending_value') IS NULL THEN
        CREATE TYPE public.youtube_live_review_pending_value AS (
            channel_id text,
            kind text,
            observation_id numeric,
            effective_at numeric,
            scheduled_for numeric,
            ended_at numeric,
            negative_eligible boolean,
            scope_covers boolean);
    END IF;
    IF to_regtype('public.youtube_live_review_value') IS NULL THEN
        CREATE TYPE public.youtube_live_review_value AS (
            session public.youtube_live_review_session_value,
            head public.youtube_live_review_head_value,
            pending public.youtube_live_review_pending_value,
            availability text);
    END IF;
    -- 이름만 같은 기존 타입을 받아들이면 위치 기반 ROW와 key 기반 decoder가 다른 사실을 만든다.
    FOR expected IN
        SELECT definition.type_name,definition.attribute_names,definition.attribute_types FROM (VALUES
            ('public.youtube_live_review_session_value',
             ARRAY['video_id','channel_id','status','lifecycle_origin','title','topic_id','is_premiere','scheduled_start_time','started_at','ended_at','live_first_seen_at'],
             ARRAY['text','text','text','text','text','text','boolean','numeric','numeric','numeric','numeric']::regtype[]),
            ('public.youtube_live_review_head_value',
             ARRAY['status','last_upcoming_positive_at','last_upcoming_positive_seen_at','last_live_positive_at','last_live_positive_seen_at','last_end_evidence_at','last_complete_absence_at','last_absence_scheduled_for','first_absence_scheduled_for','second_absence_scheduled_for','consecutive_absence_slots','last_absence_observation_id','end_candidate_kind','end_candidate_observation_id','ended_at','end_reason'],
             ARRAY['text','numeric','numeric','numeric','numeric','numeric','numeric','numeric','numeric','numeric','numeric','numeric','text','numeric','numeric','text']::regtype[]),
            ('public.youtube_live_review_pending_value',
             ARRAY['channel_id','kind','observation_id','effective_at','scheduled_for','ended_at','negative_eligible','scope_covers'],
             ARRAY['text','text','numeric','numeric','numeric','numeric','boolean','boolean']::regtype[]),
            ('public.youtube_live_review_value',
             ARRAY['session','head','pending','availability'],
             ARRAY['public.youtube_live_review_session_value','public.youtube_live_review_head_value','public.youtube_live_review_pending_value','text']::regtype[])
        ) AS definition(type_name,attribute_names,attribute_types)
    LOOP
        IF NOT EXISTS (
            SELECT 1
            FROM pg_catalog.pg_type type
            JOIN pg_catalog.pg_class relation ON relation.oid=type.typrelid
            JOIN pg_catalog.pg_attribute attribute ON attribute.attrelid=relation.oid AND attribute.attnum>0
            LEFT JOIN pg_catalog.pg_type field_type ON field_type.oid=attribute.atttypid
            WHERE type.oid=to_regtype(expected.type_name) AND type.typtype='c' AND relation.relkind='c'
            GROUP BY type.oid
            HAVING array_agg(attribute.attname::text ORDER BY attribute.attnum)=expected.attribute_names
               AND array_agg(attribute.atttypid::regtype ORDER BY attribute.attnum)=expected.attribute_types
               AND bool_and(NOT attribute.attisdropped AND attribute.atttypmod=-1
                   AND NOT attribute.attnotnull AND attribute.attcollation=field_type.typcollation)
        ) THEN
            RAISE EXCEPTION 'incompatible existing review composite type: %',expected.type_name;
        END IF;
    END LOOP;
END
$types$;
REVOKE ALL ON TYPE public.youtube_live_review_session_value, public.youtube_live_review_head_value, public.youtube_live_review_pending_value, public.youtube_live_review_value FROM PUBLIC;

-- 왕복이 정확한 문서만 typed 비교를 허용한다. 누락/추가 key, 잘못된 scalar 타입,
-- JSON null과 모든 속성이 null인 object의 차이를 묵시적 형변환으로 없애지 않는다.
CREATE OR REPLACE FUNCTION public.youtube_live_review_receipt_value(p_facts jsonb)
RETURNS public.youtube_live_review_value
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE
AS $decode$
DECLARE
    decoded public.youtube_live_review_value;
BEGIN
    IF jsonb_typeof(p_facts)<>'object' THEN
        RETURN NULL;
    END IF;
    IF NOT jsonb_populate_record_valid(NULL::public.youtube_live_review_value,p_facts) THEN
        RETURN NULL;
    END IF;
    decoded := jsonb_populate_record(NULL::public.youtube_live_review_value,p_facts);
    IF to_jsonb(decoded)=p_facts THEN
        RETURN decoded;
    END IF;
    RETURN NULL;
END
$decode$;
REVOKE ALL ON FUNCTION public.youtube_live_review_receipt_value(jsonb) FROM PUBLIC;

ALTER TABLE public.youtube_live_review_receipt_facts
    ADD COLUMN IF NOT EXISTS semantic_value public.youtube_live_review_value
    GENERATED ALWAYS AS (public.youtube_live_review_receipt_value(facts)) STORED;

-- 커밋 후 ledger 기록 전 재실행은 허용하되, 동명 일반 열이나 다른 generated 식은 거부한다.
DO $column$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_catalog.pg_attribute attribute
        JOIN pg_catalog.pg_attrdef definition ON definition.adrelid=attribute.attrelid AND definition.adnum=attribute.attnum
        WHERE attribute.attrelid='public.youtube_live_review_receipt_facts'::regclass
          AND attribute.attname='semantic_value' AND NOT attribute.attisdropped
          AND attribute.atttypid='public.youtube_live_review_value'::regtype
          AND attribute.atttypmod=-1 AND NOT attribute.attnotnull AND attribute.attgenerated='s'
          AND pg_catalog.pg_get_expr(definition.adbin,definition.adrelid) IN (
              'youtube_live_review_receipt_value(facts)','public.youtube_live_review_receipt_value(facts)')
          AND EXISTS (
              SELECT 1 FROM pg_catalog.pg_depend dependency
              WHERE dependency.classid='pg_catalog.pg_attrdef'::regclass AND dependency.objid=definition.oid
                AND dependency.refclassid='pg_catalog.pg_proc'::regclass
                AND dependency.refobjid='public.youtube_live_review_receipt_value(jsonb)'::regprocedure
          )
    ) THEN
        RAISE EXCEPTION 'incompatible existing review receipt semantic_value column';
    END IF;
END
$column$;
COMMENT ON COLUMN public.youtube_live_review_receipt_facts.semantic_value IS
    '불변 facts의 정확한 typed 투영. 원본 상태·신선도는 저장하지 않으며 누락/추가 key나 타입이 다른 문서는 NULL이다. '
    'decoder·composite 타입·의미 facts 정의를 바꾸는 migration은 같은 파일에서 이 generated 열도 기존 facts로 다시 계산해야 한다.';

-- 같은 scalar 생성자가 runtime 비교와 기존 JSON current_facts 계약을 함께 소유한다.
CREATE OR REPLACE FUNCTION public.youtube_live_review_source_value(
    p_session_video_id text,
    p_session_channel_id text,
    p_session_status text,
    p_session_lifecycle_origin text,
    p_session_title text,
    p_session_topic_id text,
    p_session_is_premiere boolean,
    p_session_scheduled_start_time timestamptz,
    p_session_started_at timestamptz,
    p_session_ended_at timestamptz,
    p_session_live_first_seen_at timestamptz,
    p_head_video_id text,
    p_head_status text,
    p_head_last_upcoming_positive_at timestamptz,
    p_head_last_upcoming_positive_seen_at timestamptz,
    p_head_last_live_positive_at timestamptz,
    p_head_last_live_positive_seen_at timestamptz,
    p_head_last_end_evidence_at timestamptz,
    p_head_last_complete_absence_at timestamptz,
    p_head_last_absence_scheduled_for timestamptz,
    p_head_first_absence_scheduled_for timestamptz,
    p_head_second_absence_scheduled_for timestamptz,
    p_head_consecutive_absence_slots smallint,
    p_head_last_absence_observation_id bigint,
    p_head_end_candidate_kind text,
    p_head_end_candidate_observation_id bigint,
    p_head_ended_at timestamptz,
    p_head_end_reason text,
    p_pending_video_id text,
    p_pending_channel_id text,
    p_pending_kind text,
    p_pending_observation_id bigint,
    p_pending_effective_at timestamptz,
    p_pending_scheduled_for timestamptz,
    p_pending_ended_at timestamptz,
    p_pending_negative_eligible boolean,
    p_pending_scope_covers boolean,
    p_availability_availability text)
RETURNS public.youtube_live_review_value
LANGUAGE sql STABLE PARALLEL SAFE
AS $value$
SELECT ROW(
    ROW(
        p_session_video_id,
        p_session_channel_id,
        p_session_status,
        p_session_lifecycle_origin,
        p_session_title,
        p_session_topic_id,
        p_session_is_premiere,
        extract(epoch FROM p_session_scheduled_start_time),
        extract(epoch FROM p_session_started_at),
        extract(epoch FROM p_session_ended_at),
        extract(epoch FROM p_session_live_first_seen_at))::public.youtube_live_review_session_value,
    CASE WHEN p_head_video_id IS NOT NULL THEN ROW(
        p_head_status,
        extract(epoch FROM p_head_last_upcoming_positive_at),
        extract(epoch FROM p_head_last_upcoming_positive_seen_at),
        extract(epoch FROM p_head_last_live_positive_at),
        extract(epoch FROM p_head_last_live_positive_seen_at),
        extract(epoch FROM p_head_last_end_evidence_at),
        extract(epoch FROM p_head_last_complete_absence_at),
        extract(epoch FROM p_head_last_absence_scheduled_for),
        extract(epoch FROM p_head_first_absence_scheduled_for),
        extract(epoch FROM p_head_second_absence_scheduled_for),
        p_head_consecutive_absence_slots,
        p_head_last_absence_observation_id,
        p_head_end_candidate_kind,
        p_head_end_candidate_observation_id,
        extract(epoch FROM p_head_ended_at),
        p_head_end_reason)::public.youtube_live_review_head_value END,
    CASE WHEN p_pending_video_id IS NOT NULL THEN ROW(
        p_pending_channel_id,
        p_pending_kind,
        p_pending_observation_id,
        extract(epoch FROM p_pending_effective_at),
        extract(epoch FROM p_pending_scheduled_for),
        extract(epoch FROM p_pending_ended_at),
        p_pending_negative_eligible,
        p_pending_scope_covers)::public.youtube_live_review_pending_value END,
    p_availability_availability)::public.youtube_live_review_value;
$value$;
REVOKE ALL ON FUNCTION public.youtube_live_review_source_value(text, text, text, text, text, text, boolean, timestamptz, timestamptz, timestamptz, timestamptz, text, text, timestamptz, timestamptz, timestamptz, timestamptz, timestamptz, timestamptz, timestamptz, timestamptz, timestamptz, smallint, bigint, text, bigint, timestamptz, text, text, text, text, bigint, timestamptz, timestamptz, timestamptz, boolean, boolean, text) FROM PUBLIC;

CREATE OR REPLACE FUNCTION public.youtube_live_review_current_facts(p_video_ids TEXT[])
RETURNS TABLE (video_id TEXT, facts jsonb, reviewable boolean)
LANGUAGE sql STABLE
AS $facts$
SELECT session.video_id,
       to_jsonb(public.youtube_live_review_source_value(
           session.video_id,session.channel_id,session.status,session.lifecycle_origin,
           session.title,session.topic_id,session.is_premiere,session.scheduled_start_time,
           session.started_at,session.ended_at,session.live_first_seen_at,head.video_id,
           head.status,head.last_upcoming_positive_at,head.last_upcoming_positive_seen_at,head.last_live_positive_at,
           head.last_live_positive_seen_at,head.last_end_evidence_at,head.last_complete_absence_at,head.last_absence_scheduled_for,
           head.first_absence_scheduled_for,head.second_absence_scheduled_for,head.consecutive_absence_slots,head.last_absence_observation_id,
           head.end_candidate_kind,head.end_candidate_observation_id,head.ended_at,head.end_reason,
           pending.video_id,pending.channel_id,pending.kind,pending.observation_id,
           pending.effective_at,pending.scheduled_for,pending.ended_at,pending.negative_eligible,
           pending.scope_covers,availability.availability)),
       session.status = 'UPCOMING'
           AND (head.video_id IS NULL OR head.status = session.status)
           AND (session.lifecycle_origin <> 'observed' OR head.video_id IS NOT NULL)
           AND availability.video_id IS NOT NULL
           AND NOT COALESCE(GREATEST(head.last_upcoming_positive_at,head.last_live_positive_at)
               >= availability.effective_at,false)
FROM public.youtube_live_sessions session
LEFT JOIN public.youtube_live_reconciliation_heads head USING (video_id)
LEFT JOIN public.youtube_live_pending_ends pending USING (video_id)
LEFT JOIN public.youtube_video_availability availability USING (video_id)
WHERE session.video_id = ANY(p_video_ids);
$facts$;
REVOKE ALL ON FUNCTION public.youtube_live_review_current_facts(TEXT[]) FROM PUBLIC;

DO $roles$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT USAGE ON TYPE public.youtube_live_review_session_value, public.youtube_live_review_head_value, public.youtube_live_review_pending_value, public.youtube_live_review_value TO hololive_runtime;
        GRANT EXECUTE ON FUNCTION public.youtube_live_review_receipt_value(jsonb),
            public.youtube_live_review_source_value(text, text, text, text, text, text, boolean, timestamptz, timestamptz, timestamptz, timestamptz, text, text, timestamptz, timestamptz, timestamptz, timestamptz, timestamptz, timestamptz, timestamptz, timestamptz, timestamptz, smallint, bigint, text, bigint, timestamptz, text, text, text, text, bigint, timestamptz, timestamptz, timestamptz, boolean, boolean, text) TO hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE ALL ON TYPE public.youtube_live_review_session_value, public.youtube_live_review_head_value, public.youtube_live_review_pending_value, public.youtube_live_review_value FROM hololive_scraper;
        REVOKE ALL ON FUNCTION public.youtube_live_review_receipt_value(jsonb),
            public.youtube_live_review_source_value(text, text, text, text, text, text, boolean, timestamptz, timestamptz, timestamptz, timestamptz, text, text, timestamptz, timestamptz, timestamptz, timestamptz, timestamptz, timestamptz, timestamptz, timestamptz, timestamptz, smallint, bigint, text, bigint, timestamptz, text, text, text, text, bigint, timestamptz, timestamptz, timestamptz, boolean, boolean, text) FROM hololive_scraper;
    END IF;
END
$roles$;

COMMIT;
