-- 봇 reply outbox의 accepted를 lease 없는 handoff 확인 대기 상태로 바꾼다(4b-1).
-- accepted_at은 admission 정산 시각, handoff_next_check_at은 reconciler의 다음 확인 시각이자 soft claim CAS
-- 토큰이다. 두 열 모두 nullable이고 기존 행은 backfill하지 않는다.
-- shape CHECK는 완화형이다. 이전 바이너리가 migration 뒤에도 남긴 leased accepted(claim_token·lease_until·
-- first_attempt_at)와 새 바이너리의 unleased accepted(accepted_at·handoff_next_check_at)를 모두 허용한다.
-- leased 대안은 다음 릴리스의 엄격 CHECK가 지운다. 상태 어휘 CHECK는 바꾸지 않는다.
ALTER TABLE public.bot_reply_outbox
    ADD COLUMN IF NOT EXISTS accepted_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS handoff_next_check_at TIMESTAMPTZ;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.bot_reply_outbox'::regclass
          AND conname = 'chk_bot_reply_outbox_state_shape_next'
    ) AND NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.bot_reply_outbox'::regclass
          AND conname = 'chk_bot_reply_outbox_state_shape'
          AND pg_get_constraintdef(oid) LIKE '%handoff_next_check_at%'
    ) THEN
        ALTER TABLE public.bot_reply_outbox
            ADD CONSTRAINT chk_bot_reply_outbox_state_shape_next CHECK (
                (
                    status <> 'submitting'
                    OR (
                        claim_token IS NOT NULL
                        AND lease_until IS NOT NULL
                        AND first_attempt_at IS NOT NULL
                    )
                )
                AND (
                    status <> 'accepted'
                    OR (
                        length(iris_request_id) > 0
                        AND (
                            (
                                claim_token IS NOT NULL
                                AND lease_until IS NOT NULL
                                AND first_attempt_at IS NOT NULL
                            )
                            OR (
                                claim_token IS NULL
                                AND lease_until IS NULL
                                AND accepted_at IS NOT NULL
                                AND handoff_next_check_at IS NOT NULL
                            )
                        )
                    )
                )
                AND (status = 'accepted' OR handoff_next_check_at IS NULL)
                AND (
                    status IN ('handoff_completed', 'dead', 'permanent_conflict', 'discarded')
                    OR payload IS NOT NULL
                )
            ) NOT VALID;
    END IF;
END
$$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.bot_reply_outbox'::regclass
          AND conname = 'chk_bot_reply_outbox_state_shape_next'
          AND NOT convalidated
    ) THEN
        ALTER TABLE public.bot_reply_outbox
            VALIDATE CONSTRAINT chk_bot_reply_outbox_state_shape_next;
    END IF;
END
$$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.bot_reply_outbox'::regclass
          AND conname = 'chk_bot_reply_outbox_state_shape_next'
    ) THEN
        ALTER TABLE public.bot_reply_outbox
            DROP CONSTRAINT IF EXISTS chk_bot_reply_outbox_state_shape;
        ALTER TABLE public.bot_reply_outbox
            RENAME CONSTRAINT chk_bot_reply_outbox_state_shape_next
            TO chk_bot_reply_outbox_state_shape;
    END IF;
END
$$;
