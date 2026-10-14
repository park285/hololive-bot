-- 새 세션 기본값과 큰 가변 길이 열의 압축을 함께 지정합니다. SET COMPRESSION은
-- 기존 TOAST 값을 재압축하지 않습니다. 삭제된 공간 반환은 별도 점검 창의 VACUUM FULL이
-- 소유하며 이 migration은 잠금 대기를 제한합니다.
SET lock_timeout = '3s';
DO $compression$
BEGIN
    EXECUTE pg_catalog.format(
        'ALTER DATABASE %I SET default_toast_compression = %L',
        pg_catalog.current_database(), 'lz4'
    );
END
$compression$;
ALTER TABLE public.youtube_live_reconciliation_heads ALTER COLUMN ignored_absence_scheduled_for SET COMPRESSION lz4;
ALTER TABLE public.youtube_live_absence_slots ALTER COLUMN coverage SET COMPRESSION lz4;
ALTER TABLE public.youtube_content_absence_slots ALTER COLUMN coverage SET COMPRESSION lz4;
ALTER TABLE public.youtube_live_review_receipts ALTER COLUMN original_snapshot SET COMPRESSION lz4;
ALTER TABLE public.youtube_live_review_receipt_facts ALTER COLUMN facts SET COMPRESSION lz4;
ALTER TABLE public.alarm_dispatch_events ALTER COLUMN payload SET COMPRESSION lz4;
ALTER TABLE public.alarm_upcoming_candidates ALTER COLUMN notification SET COMPRESSION lz4;
ALTER TABLE public.bot_webhook_inbox ALTER COLUMN payload SET COMPRESSION lz4;
ALTER TABLE public.bot_reply_outbox ALTER COLUMN payload SET COMPRESSION lz4;
RESET lock_timeout;

-- 각 keyset 배치를 독립 commit하므로 중단 뒤 재실행해도 이미 정리한 값을 다시 쓰지 않습니다.
-- ENDED의 부재 이력은 reducer가 읽지 않지만, 종료 후보가 남은 pending은 계속 보호합니다.
CREATE OR REPLACE PROCEDURE public.clean_ended_live_storage_v305()
LANGUAGE plpgsql
AS $cleanup$
DECLARE
    previous_video_id TEXT;
    candidate_ids TEXT[];
BEGIN
    LOOP
        SELECT array_agg(candidate.video_id ORDER BY candidate.video_id)
        INTO candidate_ids
        FROM (
            SELECT video_id FROM public.youtube_live_reconciliation_heads
            WHERE previous_video_id IS NULL OR video_id > previous_video_id
            ORDER BY video_id LIMIT 1000
        ) candidate;
        EXIT WHEN candidate_ids IS NULL;

        PERFORM set_config('lock_timeout', '3s', true);
        UPDATE public.youtube_live_reconciliation_heads head
        SET ignored_absence_scheduled_for = '{}'::timestamptz[]
        WHERE video_id = ANY(candidate_ids)
          AND status = 'ENDED'
          AND cardinality(ignored_absence_scheduled_for) > 0
          -- canonical 상태가 있으면 head보다 우선한다. head만 남았을 때만 head의 ENDED를 사용한다.
          AND NOT EXISTS (
              SELECT 1 FROM public.youtube_live_sessions session
              WHERE session.video_id = head.video_id AND session.status <> 'ENDED'
          );
        previous_video_id := candidate_ids[array_length(candidate_ids, 1)];
        COMMIT;
    END LOOP;

    previous_video_id := NULL;
    LOOP
        SELECT array_agg(candidate.video_id ORDER BY candidate.video_id)
        INTO candidate_ids
        FROM (
            SELECT video_id FROM public.youtube_live_pending_ends
            WHERE previous_video_id IS NULL OR video_id > previous_video_id
            ORDER BY video_id LIMIT 1000
        ) candidate;
        EXIT WHEN candidate_ids IS NULL;

        PERFORM set_config('lock_timeout', '3s', true);
        DELETE FROM public.youtube_live_pending_ends pending
        USING public.youtube_live_sessions session
        WHERE pending.video_id = ANY(candidate_ids)
          AND session.video_id = pending.video_id
          AND session.status = 'ENDED'
          AND NOT EXISTS (
              SELECT 1 FROM public.youtube_live_reconciliation_heads head
              WHERE head.video_id = session.video_id
                AND (head.end_candidate_observation_id IS NOT NULL OR head.next_end_check_at IS NOT NULL)
          );
        previous_video_id := candidate_ids[array_length(candidate_ids, 1)];
        COMMIT;
    END LOOP;
END
$cleanup$;
REVOKE ALL ON PROCEDURE public.clean_ended_live_storage_v305() FROM PUBLIC;
CALL public.clean_ended_live_storage_v305();
DROP PROCEDURE IF EXISTS public.clean_ended_live_storage_v305();
