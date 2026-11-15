-- 포스터 그림 바이트(image_data)와 내용으로 판정한 형식(image_mime)을 둔다. 썸네일은 같은 주소에서 그림만 바뀌므로, 자동 검토와
-- 일정표 행 추출은 저장한 그림이 있으면 원본 주소를 다시 받지 않는다. 썸네일 후보는 수집 때 받은 그림을 후보와 같은 문장으로 넣고,
-- 그림이 없는 행(기존 행, X·커뮤니티 후보)은 처음 원본을 받을 때 채운다. 기존 행은 NULL로 둔다.
-- 이전 image는 이 열을 읽지도 쓰지도 않고, 이 열 없이 넣는 행은 두 열이 모두 NULL이라 CHECK를 지킨다. 되돌릴 때 이 migration은 되돌리지 않는다.
-- 수백 행 이하이고 새 열이 모두 NULL이라 한 트랜잭션에서 검증한다.
BEGIN;

ALTER TABLE public.schedule_posters ADD COLUMN IF NOT EXISTS image_data bytea;
ALTER TABLE public.schedule_posters ADD COLUMN IF NOT EXISTS image_mime text;
ALTER TABLE public.schedule_posters DROP CONSTRAINT IF EXISTS chk_schedule_posters_image;
ALTER TABLE public.schedule_posters ADD CONSTRAINT chk_schedule_posters_image CHECK (
    (image_data IS NULL AND image_mime IS NULL)
    OR (image_data IS NOT NULL AND image_mime IS NOT NULL AND octet_length(image_data) >= 1
        AND image_mime IN ('image/jpeg', 'image/png', 'image/webp'))
);

COMMIT;
