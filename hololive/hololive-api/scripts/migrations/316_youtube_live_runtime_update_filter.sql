-- 소비·검토 입력이 같은 UPDATE는 동기 모델의 재조회와 행 잠금을 만들지 않는다.
-- BEFORE guard는 유지하여 refresh를 생략하는 문장도 같은 MVCC 직렬화 경계를 따른다.
BEGIN;
SET LOCAL lock_timeout = '3s';
SELECT public.lock_youtube_live_runtime_state();

CREATE OR REPLACE FUNCTION public.refresh_youtube_live_runtime_state_statement()
RETURNS trigger LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $statement$
DECLARE
    video_ids text[];
BEGIN
    IF TG_OP='INSERT' THEN
        SELECT array_agg(video_id ORDER BY video_id COLLATE "C") INTO video_ids
        FROM (SELECT DISTINCT video_id::text FROM runtime_new) changed;
    ELSIF TG_OP='DELETE' THEN
        SELECT array_agg(video_id ORDER BY video_id COLLATE "C") INTO video_ids
        FROM (SELECT DISTINCT video_id::text FROM runtime_old) changed;
    ELSIF TG_OP='UPDATE' THEN
        -- 동기 모델과 영수증 판정의 입력만 비교하며 큰 진단 배열은 읽지 않는다.
        -- identity 변경은 양쪽 key를 남겨 이전 모델의 삭제와 새 모델의 생성을 함께 반영한다.
        IF TG_RELID='public.youtube_live_sessions'::regclass THEN
            SELECT array_agg(COALESCE(n.video_id,o.video_id)::text
                       ORDER BY COALESCE(n.video_id,o.video_id)::text COLLATE "C") INTO video_ids
            FROM runtime_old o FULL JOIN runtime_new n USING (video_id)
            WHERE o.video_id IS NULL OR n.video_id IS NULL
               OR ROW(o.channel_id,o.status,o.lifecycle_origin,o.title,o.topic_id,o.is_premiere,o.scheduled_start_time,o.started_at,o.ended_at,o.live_first_seen_at)
                  IS DISTINCT FROM ROW(n.channel_id,n.status,n.lifecycle_origin,n.title,n.topic_id,n.is_premiere,n.scheduled_start_time,n.started_at,n.ended_at,n.live_first_seen_at);
        ELSIF TG_RELID='public.youtube_live_reconciliation_heads'::regclass THEN
            SELECT array_agg(COALESCE(n.video_id,o.video_id)::text
                       ORDER BY COALESCE(n.video_id,o.video_id)::text COLLATE "C") INTO video_ids
            FROM runtime_old o FULL JOIN runtime_new n USING (video_id)
            WHERE o.video_id IS NULL OR n.video_id IS NULL
               OR ROW(o.status,o.last_upcoming_positive_at,o.last_upcoming_positive_seen_at,o.last_live_positive_at,o.last_live_positive_seen_at,o.last_end_evidence_at,o.last_complete_absence_at,o.last_absence_scheduled_for,o.first_absence_scheduled_for,o.second_absence_scheduled_for,o.consecutive_absence_slots,o.last_absence_observation_id,o.end_candidate_kind,o.end_candidate_observation_id,o.ended_at,o.end_reason,o.unresolvable_since)
                  IS DISTINCT FROM ROW(n.status,n.last_upcoming_positive_at,n.last_upcoming_positive_seen_at,n.last_live_positive_at,n.last_live_positive_seen_at,n.last_end_evidence_at,n.last_complete_absence_at,n.last_absence_scheduled_for,n.first_absence_scheduled_for,n.second_absence_scheduled_for,n.consecutive_absence_slots,n.last_absence_observation_id,n.end_candidate_kind,n.end_candidate_observation_id,n.ended_at,n.end_reason,n.unresolvable_since);
        ELSIF TG_RELID='public.youtube_live_pending_ends'::regclass THEN
            SELECT array_agg(COALESCE(n.video_id,o.video_id)::text
                       ORDER BY COALESCE(n.video_id,o.video_id)::text COLLATE "C") INTO video_ids
            FROM runtime_old o FULL JOIN runtime_new n USING (video_id)
            WHERE o.video_id IS NULL OR n.video_id IS NULL
               OR ROW(o.channel_id,o.kind,o.observation_id,o.effective_at,o.scheduled_for,o.ended_at,o.negative_eligible,o.scope_covers)
                  IS DISTINCT FROM ROW(n.channel_id,n.kind,n.observation_id,n.effective_at,n.scheduled_for,n.ended_at,n.negative_eligible,n.scope_covers);
        ELSIF TG_RELID='public.youtube_video_availability'::regclass THEN
            SELECT array_agg(COALESCE(n.video_id,o.video_id)::text
                       ORDER BY COALESCE(n.video_id,o.video_id)::text COLLATE "C") INTO video_ids
            FROM runtime_old o FULL JOIN runtime_new n USING (video_id)
            WHERE o.video_id IS NULL OR n.video_id IS NULL
               OR ROW(o.availability,o.effective_at,o.observed_at)
                  IS DISTINCT FROM ROW(n.availability,n.effective_at,n.observed_at);
        ELSE
            SELECT array_agg(video_id ORDER BY video_id COLLATE "C") INTO video_ids
            FROM (SELECT video_id::text FROM runtime_old UNION SELECT video_id::text FROM runtime_new) changed;
        END IF;
    ELSE
        SELECT array_agg(video_id ORDER BY video_id COLLATE "C") INTO video_ids
        FROM (SELECT video_id FROM public.youtube_live_runtime_state
              UNION SELECT video_id FROM public.youtube_live_sessions
              UNION SELECT video_id FROM public.youtube_live_reconciliation_heads) changed;
    END IF;
    IF video_ids IS NOT NULL THEN
        PERFORM public.refresh_youtube_live_runtime_state(video_ids);
    END IF;
    RETURN NULL;
END
$statement$;

COMMIT;
