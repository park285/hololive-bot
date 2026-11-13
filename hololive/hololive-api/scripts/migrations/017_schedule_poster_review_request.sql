-- review_requested_at은 후보를 관리자 검토로 넘긴 시각이고, Console 알림(summary의 최신 후보)은 이 시각이 있는 행만 본다.
-- 자동 검토를 켠 수집은 NULL로 넣고, 판정이 확정·거절하지 못한 행만 채운다. 자동 확정·거절한 행은 NULL로 남는다.
-- 기본값이 now()라 자동 검토를 끈 수집과 이전 image가 넣는 행은 바로 넘긴 행이 된다. 기존 행은 생성 시각으로 채워 배포 때 최신 후보가 바뀌지 않게 한다.
-- 수천 행 이하라 한 트랜잭션에서 채운다.
BEGIN;

ALTER TABLE public.schedule_posters ADD COLUMN IF NOT EXISTS review_requested_at timestamptz;
UPDATE public.schedule_posters SET review_requested_at = created_at WHERE review_requested_at IS NULL;
ALTER TABLE public.schedule_posters ALTER COLUMN review_requested_at SET DEFAULT now();

COMMIT;
