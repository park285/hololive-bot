-- 봇 reply outbox shape CHECK를 엄격형으로 바꾼다(4b-2). lease는 submitting만 가지며, accepted는 lease 없이
-- accepted_at과 handoff 확인 예정 시각을 가져야 한다. 326의 완화형이 허용하던 leased accepted 대안을 지운다.
-- 전제는 직전 릴리스(326·333과 handoff reconciler)가 운영에서 돌아 이전 바이너리의 leased accepted가 0행인
-- 것이다. 한 행이라도 남아 있으면 제약을 건드리지 않고 중단한다. 사전 조회와 제약 추가 사이에 leased accepted가
-- 새로 쓰이지 않도록 표를 ACCESS EXCLUSIVE로 잠그며, 잠금은 runner의 lock_timeout(10s) 안에서 얻어야 한다.
-- 엄격형 판정은 정의 안의 `claim_token IS NOT NULL`이 submitting 분기 하나뿐인지로 한다.
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
          AND regexp_count(pg_get_constraintdef(oid), 'claim_token IS NOT NULL') = 1
    ) THEN
        LOCK TABLE public.bot_reply_outbox IN ACCESS EXCLUSIVE MODE;

        IF EXISTS (
            SELECT 1
            FROM public.bot_reply_outbox
            WHERE status = 'accepted' AND claim_token IS NOT NULL
        ) THEN
            RAISE EXCEPTION 'bot_reply_outbox still has leased accepted rows; keep the previous release running until lease reclaim moves them to manual_review, then retry migration 332'
                USING ERRCODE = '55000';
        END IF;

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
                        AND claim_token IS NULL
                        AND lease_until IS NULL
                        AND accepted_at IS NOT NULL
                        AND handoff_next_check_at IS NOT NULL
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
