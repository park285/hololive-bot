-- API/collector를 함께 중단·drain한 뒤 적용하는 비호환 cutover다. 구 binary와 rolling 혼용하지 않는다.
-- 259/260은 이미 적용된 ledger 계약이다. membership 만료 조회 외 lease·retention 함수와 ACL은 바꾸지 않는다.
-- target별 expiry를 없애고 CURRENT header만 유효기간의 정본으로 사용한다.
BEGIN;

ALTER TABLE public.youtube_collection_projection_generations
    ADD COLUMN IF NOT EXISTS eligibility_version BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS validity_refreshed_at TIMESTAMPTZ;

-- 기존 header의 TTL/갱신 시각을 역산하지 않는다. NULL은 첫 성공 refresh가 supplied now로 수립한다.

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'public.youtube_collection_projection_generations'::regclass
          AND conname = 'chk_youtube_projection_eligibility_version'
    ) THEN
        ALTER TABLE public.youtube_collection_projection_generations
            ADD CONSTRAINT chk_youtube_projection_eligibility_version
            CHECK (eligibility_version > 0) NOT VALID;
    END IF;
END;
$$;

-- 신규 admission의 not_before와 별개로 이미 잡은 job은 연속 membership만 검증한다.
-- 만료는 header 한 곳에서 검사하고 이전 count/epoch/subject 규칙을 보존한다.
CREATE OR REPLACE FUNCTION public.youtube_collection_membership_valid(
    requested_kinds TEXT[],
    requested_exact_subject BOOLEAN,
    requested_subject_key TEXT,
    requested_target_count INTEGER,
    requested_generation BIGINT
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
SET search_path = pg_catalog
AS $function$
    SELECT COALESCE(requested_target_count > 0, FALSE)
       AND COALESCE(cardinality(requested_kinds) > 0, FALSE)
       AND COALESCE(requested_generation > 0, FALSE)
       AND requested_exact_subject IS NOT NULL
       AND EXISTS (
           SELECT 1
           FROM public.youtube_collection_projection_generations AS projection
           CROSS JOIN LATERAL (
               SELECT count(target.subject_key) AS target_count,
                      count(target.subject_key) FILTER (
                          WHERE target.member_since_generation BETWEEN 1 AND requested_generation
                      ) AS member_count
               FROM public.youtube_collection_targets AS target
               WHERE target.projection_generation = projection.generation
                 AND target.observation_kind = ANY (requested_kinds)
                 AND target.enabled
                 AND (NOT requested_exact_subject OR target.subject_key = requested_subject_key)
           ) AS scope
           WHERE projection.status = 'CURRENT'
             AND projection.valid_until > statement_timestamp()
             AND scope.target_count = requested_target_count
             AND scope.member_count = requested_target_count
       )
$function$;
ALTER TABLE public.youtube_collection_targets DROP COLUMN IF EXISTS valid_until;


COMMIT;

ALTER TABLE public.youtube_collection_projection_generations
    VALIDATE CONSTRAINT chk_youtube_projection_eligibility_version;
