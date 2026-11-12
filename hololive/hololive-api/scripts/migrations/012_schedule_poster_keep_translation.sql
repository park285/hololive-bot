-- 재번역을 접수하거나 재번역이 실패해도 마지막 번역본을 계속 게시하므로, 번역본은 succeeded 상태가 아니라 세 열의 짝으로 묶는다.
-- 이전 image의 쓰기(succeeded일 때만 번역본을 두고 재번역·거절 때 비움)도 새 제약을 만족한다. 수백 행 이하라 한 트랜잭션에서 검증한다.
BEGIN;

ALTER TABLE public.schedule_posters DROP CONSTRAINT IF EXISTS chk_schedule_posters_translated_image;
ALTER TABLE public.schedule_posters ADD CONSTRAINT chk_schedule_posters_translated_image CHECK (
    (translated_image IS NULL) = (translated_mime IS NULL)
    AND (translated_image IS NULL) = (translated_at IS NULL)
    AND (translation_status <> 'succeeded' OR translated_image IS NOT NULL)
    AND (translation_status <> 'not_requested' OR translated_image IS NULL)
);

ALTER TABLE public.schedule_posters DROP CONSTRAINT IF EXISTS chk_schedule_posters_translated_mime;
ALTER TABLE public.schedule_posters ADD CONSTRAINT chk_schedule_posters_translated_mime CHECK (translated_mime IS NULL OR translated_mime = 'image/jpeg');

COMMIT;
