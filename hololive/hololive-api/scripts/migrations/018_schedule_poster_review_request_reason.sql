-- review_request_reason은 자동 검토가 후보를 관리자 검토로 넘긴 이유이고, reviewed_by는 확정·거절을 자동 검토(auto)와
-- 관리자(admin) 가운데 누가 했는지다. 둘 다 Console에 보이기만 하고 판정 규칙(week_coverage)은 계속 review_requested_at을 본다.
-- 이유는 자동 검토를 끈 수집이 바로 넘긴 행과 이 migration 전에 넘긴 행에서 NULL이고, 자동 검토를 끈 뒤 남은 후보를 넘기면 auto_review_off다.
-- 기존 확정·거절 행은 관리자 검토로 넘긴 적이 있으면 admin, 없으면 auto로 채운다(자동 판정 행을 관리자가 그 뒤 거절했으면 auto로 남는다).
-- 이전 image는 두 열을 쓰지 않으므로 롤백해도 된다. 수천 행 이하라 한 트랜잭션에서 채운다.
BEGIN;

ALTER TABLE public.schedule_posters ADD COLUMN IF NOT EXISTS review_request_reason text;
ALTER TABLE public.schedule_posters ADD COLUMN IF NOT EXISTS reviewed_by text;
UPDATE public.schedule_posters
SET reviewed_by = CASE WHEN review_requested_at IS NULL THEN 'auto' ELSE 'admin' END
WHERE reviewed_by IS NULL AND review_status IN ('confirmed', 'rejected');
ALTER TABLE public.schedule_posters DROP CONSTRAINT IF EXISTS chk_schedule_posters_review_request_reason;
ALTER TABLE public.schedule_posters ADD CONSTRAINT chk_schedule_posters_review_request_reason CHECK (
    review_request_reason IS NULL
    OR (review_requested_at IS NOT NULL AND review_request_reason IN (
        'uncertain', 'unjudgeable', 'image_changed', 'period_unread', 'week_rejected', 'week_overlapping', 'stale', 'auto_review_off'
    ))
);
ALTER TABLE public.schedule_posters DROP CONSTRAINT IF EXISTS chk_schedule_posters_reviewed_by;
ALTER TABLE public.schedule_posters ADD CONSTRAINT chk_schedule_posters_reviewed_by CHECK (reviewed_by IS NULL OR reviewed_by IN ('auto', 'admin'));

COMMIT;
