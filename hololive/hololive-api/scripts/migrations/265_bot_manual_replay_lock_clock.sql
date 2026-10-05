-- 함수 권한과 감사 원자성을 유지하며 잠금 대기 전 시각으로 cutoff를 우회하지 못하게 합니다.
CREATE OR REPLACE FUNCTION public.grant_bot_reply_outbox_manual_replay(requested_outbox_id bigint, operator_actor text, operator_reason text) RETURNS text
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $_$
DECLARE
    granted_at TIMESTAMPTZ;
    normalized_actor TEXT := btrim(operator_actor);
    normalized_reason TEXT := btrim(operator_reason);
    target_id BIGINT;
    target_status TEXT;
    target_created_at TIMESTAMPTZ;
    target_replay_grants INTEGER;
    next_grant_number INTEGER;
BEGIN
    SELECT id, status, created_at, operator_replay_grants
    INTO target_id, target_status, target_created_at, target_replay_grants
    FROM public.bot_reply_outbox
    WHERE id = requested_outbox_id
    FOR UPDATE;

    IF NOT FOUND THEN
        RETURN 'not_found';
    END IF;
    IF target_status <> 'manual_review' THEN
        RETURN 'not_manual_review';
    END IF;
    -- 잠금 대기가 끝난 시각으로 재발급 제한과 감사 시각을 함께 판정합니다.
    granted_at := clock_timestamp();
    IF granted_at >= target_created_at + interval '144 hours' THEN
        RETURN 'cutoff_expired';
    END IF;
    IF normalized_actor !~ '^[A-Za-z0-9._:@-]{1,64}$'
        OR octet_length(normalized_reason) NOT BETWEEN 1 AND 256
        OR normalized_reason ~ '[[:cntrl:]]'
    THEN
        RETURN 'invalid_operator_metadata';
    END IF;

    next_grant_number := target_replay_grants + 1;
    INSERT INTO public.bot_reply_outbox_replay_audit (
        outbox_id, grant_number, event_type, actor, reason, recorded_at
    ) VALUES (
        target_id, next_grant_number, 'granted', normalized_actor, normalized_reason, granted_at
    );

    UPDATE public.bot_reply_outbox
    SET status = 'pending',
        claim_token = NULL,
        lease_until = NULL,
        last_error = '',
        operator_replay_grants = next_grant_number,
        available_at = granted_at,
        updated_at = granted_at
    WHERE id = target_id;

    RETURN 'replayed';
END
$_$;
