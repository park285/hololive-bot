-- 운영 roster 밖 채널에 남은 legacy LIVE 세션을 종료 기록으로 정리한다.
-- 채널·영상 수집 대상은 운영 roster(members.is_graduated=false)로 한정되므로 이 세션들은 종료를 관측할 경로가 없고,
-- head가 없는 legacy 행이라 부재 판정도 적용되지 않아 LIVE로 남는다.
-- 종료 증거가 없으므로 종료 시각은 마지막 관측 시각(last_seen_at)을 관측값으로 남기고 lifecycle_origin은 바꾸지 않는다.
-- status_observed_at을 적용 시각으로 두어 시각 확정 신규 대상에 들어가게 한다. 영상 확인이 아카이브 시각으로 확정하거나
-- 확인할 수 없으면 exhausted로 끝낸다. 다시 적용해도 LIVE 조건에 맞는 행이 없어 바뀌지 않는다.
BEGIN;
SET LOCAL lock_timeout = '3s';

UPDATE public.youtube_live_sessions AS session
SET status = 'ENDED',
    ended_at = COALESCE(session.ended_at, session.last_seen_at),
    status_observed_at = statement_timestamp()
WHERE session.status = 'LIVE'
  AND session.lifecycle_origin = 'legacy_unknown'
  AND NOT EXISTS (
      SELECT 1 FROM public.youtube_live_reconciliation_heads AS head
      WHERE head.video_id = session.video_id
  )
  AND NOT EXISTS (
      SELECT 1 FROM public.youtube_live_pending_ends AS pending
      WHERE pending.video_id = session.video_id
  )
  AND NOT EXISTS (
      SELECT 1 FROM public.members AS member
      WHERE member.channel_id = session.channel_id
        AND member.is_graduated = FALSE
  );

COMMIT;
