-- youtube_notification_send_request(고정 요청)에 Iris 202 admission 뒤 handoff 확인 상태를 둔다.
-- handoff_request_id는 202 응답의 Iris requestId, handoff_next_check_at은 reconciler의 다음 확인 시각이자 soft
-- claim CAS 토큰, handoff_checked_at은 확인을 끝낸 시각이다. "admission 완료·handoff 미확인"은 handoff_request_id가
-- 있고 handoff_checked_at이 없는 요청이다. CHECK는 그런 요청이 항상 다음 확인 시각을 갖고, 다음 확인 시각은 그런
-- 요청에만 있게 한다. 그래서 확인 대기 요청은 reconciler가 반드시 다시 고른다. 재발급은 세 열을 함께 비우므로 새
-- 세대 발송 전 요청은 handoff 없는 행이다. delivery·ledger 어휘와 기존 CHECK는 바꾸지 않는다.
-- 세 열 모두 nullable이고 기존 행은 backfill하지 않는다. 이전 바이너리는 이 열을 읽거나 쓰지 않으므로 migration을
-- 먼저 적용해도 된다. 과거 SENT 요청은 확인 대상이 아니다.
-- 열 추가와 NOT VALID CHECK는 ACCESS EXCLUSIVE가 필요하다. 이전 worker의 요청 고정·claim·CompleteSent가 도는 중에
-- 적용되므로 짧은 트랜잭션에서 잠금 대기를 3초로 끊는다(291·301 선례). VALIDATE는 그 잠금을 이어받지 않도록 블록 밖
-- autocommit 문장으로 두고(178 선례), 잠금 대기 3초는 DO 블록 안 set_config(..., true)로 그 문장의 트랜잭션에만 걸어
-- runner가 세션에 둔 lock_timeout을 바꾸지 않는다(308 선례). 모든 문장은 재실행해도 안전하다.
BEGIN;
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE public.youtube_notification_send_request
    ADD COLUMN IF NOT EXISTS handoff_request_id TEXT,
    ADD COLUMN IF NOT EXISTS handoff_next_check_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS handoff_checked_at TIMESTAMPTZ;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.youtube_notification_send_request'::regclass
          AND conname = 'chk_youtube_send_request_handoff_shape'
    ) THEN
        ALTER TABLE public.youtube_notification_send_request
            ADD CONSTRAINT chk_youtube_send_request_handoff_shape CHECK (
                (handoff_request_id IS NULL OR length(handoff_request_id) BETWEEN 1 AND 256)
                AND (
                    handoff_next_check_at IS NULL
                    OR (handoff_request_id IS NOT NULL AND handoff_checked_at IS NULL)
                )
                AND (handoff_checked_at IS NULL OR handoff_request_id IS NOT NULL)
                AND (
                    handoff_request_id IS NULL
                    OR handoff_checked_at IS NOT NULL
                    OR handoff_next_check_at IS NOT NULL
                )
            ) NOT VALID;
    END IF;
END
$$;

COMMIT;

DO $$
BEGIN
    PERFORM set_config('lock_timeout', '3s', true);
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.youtube_notification_send_request'::regclass
          AND conname = 'chk_youtube_send_request_handoff_shape'
          AND NOT convalidated
    ) THEN
        ALTER TABLE public.youtube_notification_send_request
            VALIDATE CONSTRAINT chk_youtube_send_request_handoff_shape;
    END IF;
END
$$;
