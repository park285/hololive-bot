-- migration 249가 고정 요청 이전 행을 구분하려고 추가한 request_snapshot_allowed를 지운다.
-- 7.2.0부터 이 열을 읽는 코드가 없다. 2026-10-02 운영 조회에서 FALSE 행은 SENT 54, FAILED 5(revive 창 밖)뿐이었다.
-- FALSE인데 아직 끝나지 않은 행이 있으면 그 행의 재전송 판단 근거가 사라지므로 지우지 않고 실패한다.
BEGIN;
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';
DO $migration$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'youtube_notification_delivery'
          AND column_name = 'request_snapshot_allowed'
    ) THEN
        RETURN;
    END IF;
    -- 검사와 DROP이 같은 잠금을 쓰게 처음부터 ACCESS EXCLUSIVE를 잡는다. SHARE에서 올리면 동시 쓰기와 교착할 수 있다.
    LOCK TABLE public.youtube_notification_delivery IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (
        SELECT 1 FROM public.youtube_notification_delivery
        WHERE request_snapshot_allowed IS FALSE
          AND status IN ('PENDING', 'SENDING', 'QUARANTINED')
    ) THEN
        RAISE EXCEPTION 'unfinished delivery rows still carry request_snapshot_allowed=false; column kept';
    END IF;
END
$migration$;
ALTER TABLE public.youtube_notification_delivery DROP COLUMN IF EXISTS request_snapshot_allowed;
COMMIT;
