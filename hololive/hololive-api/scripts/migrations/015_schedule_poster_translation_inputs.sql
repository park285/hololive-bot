-- 재번역이 원본 판독과 번역표를 다시 만들지 않도록, 번역 시도가 남긴 둘과 그 입력(그림, 판독·텍스트 모델, 지시문, thinking level)의
-- sha256(32바이트)을 함께 남긴다. 셋은 함께 채우거나 함께 비운다. 이전 image는 이 열을 읽지도 쓰지도 않는다.
-- 수백 행 이하라 한 트랜잭션에서 검증한다.
BEGIN;

ALTER TABLE public.schedule_posters ADD COLUMN IF NOT EXISTS translation_inputs_sha256 bytea;
ALTER TABLE public.schedule_posters ADD COLUMN IF NOT EXISTS source_transcript text[];
ALTER TABLE public.schedule_posters ADD COLUMN IF NOT EXISTS translation_table text;
ALTER TABLE public.schedule_posters DROP CONSTRAINT IF EXISTS chk_schedule_posters_translation_inputs;
ALTER TABLE public.schedule_posters ADD CONSTRAINT chk_schedule_posters_translation_inputs CHECK (
    (translation_inputs_sha256 IS NULL) = (source_transcript IS NULL)
    AND (translation_inputs_sha256 IS NULL) = (translation_table IS NULL)
    AND (translation_inputs_sha256 IS NULL OR octet_length(translation_inputs_sha256) = 32)
    AND (source_transcript IS NULL OR cardinality(source_transcript) BETWEEN 1 AND 200)
    AND (translation_table IS NULL OR octet_length(translation_table) BETWEEN 1 AND 262144)
);

COMMIT;
