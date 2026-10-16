-- 감사 행의 entity_kind(약 18B)·decision(약 8B)은 각각 열 개 남짓의 코드 상수라 사전 표의 smallint 코드로 저장한다.
-- 공개 SQL/Go 표현은 그대로 문자열이며 삽입·조회 경로가 resolver 함수와 사전 표를 거친다.
-- 이 파일은 코드 열을 채우기만 한다. 새 UNIQUE index는 319, 구 index 제거는 320, text 열 제거는 321이 맡는다.
CREATE TABLE IF NOT EXISTS public.source_observation_entity_kinds (
    id smallint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name text NOT NULL,
    CONSTRAINT chk_source_observation_entity_kind_bounds CHECK (length(name) BETWEEN 1 AND 64),
    CONSTRAINT uq_source_observation_entity_kind UNIQUE (name)
);
CREATE TABLE IF NOT EXISTS public.source_observation_decisions (
    id smallint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name text NOT NULL,
    CONSTRAINT chk_source_observation_decision_bounds CHECK (length(name) BETWEEN 1 AND 128),
    CONSTRAINT uq_source_observation_decision UNIQUE (name)
);
REVOKE ALL ON TABLE public.source_observation_entity_kinds FROM PUBLIC;
REVOKE ALL ON TABLE public.source_observation_decisions FROM PUBLIC;

-- 사전 행은 SECURITY DEFINER 함수만 만든다. 동시 추가 패자는 새 snapshot에서 다시 읽고 끝내 못 보면 40001이다.
CREATE OR REPLACE FUNCTION public.source_observation_entity_kind_id(requested_name text)
RETURNS smallint
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path = pg_catalog
AS $function$
DECLARE
    resolved smallint;
BEGIN
    FOR attempt IN 1..3 LOOP
        SELECT kind.id INTO resolved FROM public.source_observation_entity_kinds kind WHERE kind.name = requested_name;
        IF FOUND THEN
            RETURN resolved;
        END IF;
        INSERT INTO public.source_observation_entity_kinds (name) VALUES (requested_name)
        ON CONFLICT (name) DO NOTHING
        RETURNING id INTO resolved;
        IF resolved IS NOT NULL THEN
            RETURN resolved;
        END IF;
    END LOOP;
    RAISE EXCEPTION 'source observation entity kind could not be resolved' USING ERRCODE = '40001';
END
$function$;
CREATE OR REPLACE FUNCTION public.source_observation_decision_id(requested_name text)
RETURNS smallint
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path = pg_catalog
AS $function$
DECLARE
    resolved smallint;
BEGIN
    FOR attempt IN 1..3 LOOP
        SELECT decision.id INTO resolved FROM public.source_observation_decisions decision WHERE decision.name = requested_name;
        IF FOUND THEN
            RETURN resolved;
        END IF;
        INSERT INTO public.source_observation_decisions (name) VALUES (requested_name)
        ON CONFLICT (name) DO NOTHING
        RETURNING id INTO resolved;
        IF resolved IS NOT NULL THEN
            RETURN resolved;
        END IF;
    END LOOP;
    RAISE EXCEPTION 'source observation decision could not be resolved' USING ERRCODE = '40001';
END
$function$;
REVOKE ALL ON FUNCTION public.source_observation_entity_kind_id(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.source_observation_decision_id(text) FROM PUBLIC;
DO $roles$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT ON TABLE public.source_observation_entity_kinds TO hololive_runtime;
        GRANT SELECT ON TABLE public.source_observation_decisions TO hololive_runtime;
        GRANT EXECUTE ON FUNCTION public.source_observation_entity_kind_id(text) TO hololive_runtime;
        GRANT EXECUTE ON FUNCTION public.source_observation_decision_id(text) TO hololive_runtime;
    END IF;
END
$roles$;

-- 사전과 nullable 코드 열을 먼저 확정하고 기존 text 열은 cutover까지 보존한다.
DO $prepare$
BEGIN
    PERFORM set_config('lock_timeout', '3s', true);
    IF NOT EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid = 'public.source_observation_applications'::regclass
                   AND attname = 'entity_kind_id' AND NOT attisdropped) THEN
        INSERT INTO public.source_observation_entity_kinds (name)
        SELECT DISTINCT application.entity_kind FROM public.source_observation_applications application
        ON CONFLICT (name) DO NOTHING;
        INSERT INTO public.source_observation_decisions (name)
        SELECT DISTINCT application.decision FROM public.source_observation_applications application
        ON CONFLICT (name) DO NOTHING;
        ALTER TABLE public.source_observation_applications
            ADD COLUMN entity_kind_id smallint,
            ADD COLUMN decision_id smallint;
    END IF;
END
$prepare$;

-- 5,000행마다 commit하여 후속 실패가 완료된 backfill을 되돌리지 않게 한다.
DO $backfill$
DECLARE
    previous_id bigint;
    upper_id bigint;
    picked_ids bigint[];
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid = 'public.source_observation_applications'::regclass
                   AND attname = 'entity_kind' AND NOT attisdropped) THEN
        RETURN;
    END IF;
    SELECT max(id) INTO upper_id FROM public.source_observation_applications;
    LOOP
        PERFORM set_config('lock_timeout', '3s', true);
        IF previous_id IS NULL THEN
            SELECT array_agg(id ORDER BY id) INTO picked_ids
            FROM (SELECT id FROM public.source_observation_applications
                  WHERE id <= upper_id ORDER BY id LIMIT 5000) picked;
        ELSE
            SELECT array_agg(id ORDER BY id) INTO picked_ids
            FROM (SELECT id FROM public.source_observation_applications
                  WHERE id > previous_id AND id <= upper_id ORDER BY id LIMIT 5000) picked;
        END IF;
        EXIT WHEN picked_ids IS NULL;
        UPDATE public.source_observation_applications application
        SET entity_kind_id = kind.id, decision_id = decision.id
        FROM public.source_observation_entity_kinds kind, public.source_observation_decisions decision
        WHERE application.id = ANY(picked_ids)
          AND kind.name = application.entity_kind AND decision.name = application.decision
          AND ROW(application.entity_kind_id, application.decision_id) IS DISTINCT FROM ROW(kind.id, decision.id);
        previous_id := picked_ids[cardinality(picked_ids)];
        COMMIT;
    END LOOP;
END
$backfill$;

DO $constraints$
BEGIN
    PERFORM set_config('lock_timeout', '3s', true);
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'public.source_observation_applications'::regclass
                   AND conname = 'source_application_entity_kind_nn') THEN
        ALTER TABLE public.source_observation_applications ADD CONSTRAINT source_application_entity_kind_nn
            CHECK (entity_kind_id IS NOT NULL) NOT VALID;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'public.source_observation_applications'::regclass
                   AND conname = 'source_application_decision_nn') THEN
        ALTER TABLE public.source_observation_applications ADD CONSTRAINT source_application_decision_nn
            CHECK (decision_id IS NOT NULL) NOT VALID;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'public.source_observation_applications'::regclass
                   AND conname = 'fk_source_application_entity_kind') THEN
        ALTER TABLE public.source_observation_applications ADD CONSTRAINT fk_source_application_entity_kind
            FOREIGN KEY (entity_kind_id) REFERENCES public.source_observation_entity_kinds(id) ON DELETE RESTRICT NOT VALID;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'public.source_observation_applications'::regclass
                   AND conname = 'fk_source_application_decision') THEN
        ALTER TABLE public.source_observation_applications ADD CONSTRAINT fk_source_application_decision
            FOREIGN KEY (decision_id) REFERENCES public.source_observation_decisions(id) ON DELETE RESTRICT NOT VALID;
    END IF;
END
$constraints$;
DO $validate$
DECLARE
    pending text;
BEGIN
    FOR pending IN
        SELECT conname FROM pg_constraint
        WHERE conrelid = 'public.source_observation_applications'::regclass AND NOT convalidated
          AND conname IN ('source_application_entity_kind_nn', 'source_application_decision_nn',
                          'fk_source_application_entity_kind', 'fk_source_application_decision')
    LOOP
        EXECUTE format('ALTER TABLE public.source_observation_applications VALIDATE CONSTRAINT %I', pending);
    END LOOP;
END
$validate$;
BEGIN;
SET LOCAL lock_timeout = '3s';
ALTER TABLE public.source_observation_applications
    ALTER COLUMN entity_kind_id SET NOT NULL,
    ALTER COLUMN decision_id SET NOT NULL,
    DROP CONSTRAINT IF EXISTS source_application_entity_kind_nn,
    DROP CONSTRAINT IF EXISTS source_application_decision_nn;
COMMIT;
