-- 방송 이력 정렬값 COALESCE(ended_at, started_at, scheduled_start_time, last_seen_at)를 저장 생성 열로 둔다.
-- 정렬 인덱스가 이 식을 직접 담으면 live 세션 upsert가 last_seen_at만 바꿔도 인덱스가 참조하는 열이 바뀌어 HOT 갱신이
-- 막힌다. 생성 열을 key로 쓰면 저장된 값이 그대로일 때 HOT이 가능하다. 예정·시작·종료 시각 중 하나가 있는 세션은
-- last_seen_at이 바뀌어도 이 값이 바뀌지 않는다. 같은 rewrite에서 fillfactor를 90으로 낮춰 같은 페이지 안 갱신에
-- 쓸 여유를 남긴다. 2026-10-11 운영 조회: 9,837행·313 페이지, 누적 UPDATE 중 HOT 6.1%.
-- 열 추가는 ACCESS EXCLUSIVE로 테이블을 다시 쓴다. upsert가 초당 수 회라 잠금 대기를 3초로 끊는다.
BEGIN;
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '60s';
ALTER TABLE public.youtube_live_sessions
    SET (fillfactor = 90),
    ADD COLUMN IF NOT EXISTS history_sort_at timestamptz
        GENERATED ALWAYS AS (COALESCE(ended_at, started_at, scheduled_start_time, last_seen_at)) STORED;
COMMIT;
