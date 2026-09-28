-- 검토한 격리 send unit의 원본과 재발송하지 않는 결정을 영구 보존한다.
-- receipt는 delivery/event retention과 독립적이며 UPDATE/DELETE를 허용하지 않는다.
CREATE TABLE IF NOT EXISTS public.alarm_dispatch_closeout_receipts (
    receipt_id uuid PRIMARY KEY,
    send_unit_id bigint NOT NULL UNIQUE,
    addressed_delivery_id bigint NOT NULL,
    target_ids bigint[] NOT NULL,
    target_revisions jsonb NOT NULL,
    status_metadata jsonb NOT NULL,
    original_sha256 text NOT NULL,
    operator_id text NOT NULL,
    reason text NOT NULL,
    disposition text NOT NULL DEFAULT 'closed_without_replay',
    recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT alarm_dispatch_closeout_receipts_digest_check CHECK (original_sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT alarm_dispatch_closeout_receipts_operator_check CHECK (length(operator_id) BETWEEN 1 AND 128 AND operator_id = btrim(operator_id) AND operator_id !~ '[[:cntrl:]]'),
    CONSTRAINT alarm_dispatch_closeout_receipts_reason_check CHECK (length(reason) BETWEEN 1 AND 1024 AND reason = btrim(reason) AND reason !~ '[[:cntrl:]]'),
    CONSTRAINT alarm_dispatch_closeout_receipts_disposition_check CHECK (disposition = 'closed_without_replay'),
    CONSTRAINT alarm_dispatch_closeout_receipts_targets_check CHECK (cardinality(target_ids) BETWEEN 1 AND 100 AND jsonb_array_length(target_revisions) = cardinality(target_ids) AND jsonb_array_length(status_metadata) = cardinality(target_ids))
);

-- 원본 전체는 digest 계산에만 사용한다. payload, 본문과 오류 원문은 receipt에 복사하지 않는다.
-- 같은 SQL 함수를 preview와 잠금 후 기록에서 사용해 digest 해석 차이를 막는다.
CREATE OR REPLACE FUNCTION public.alarm_dispatch_closeout_snapshot(p_delivery_id bigint)
RETURNS TABLE (
    send_unit_id bigint, target_ids bigint[], target_revisions jsonb,
    status_metadata jsonb, original_sha256 text, member_count bigint
) LANGUAGE sql STABLE AS $function$
    WITH target AS (
        SELECT d.send_unit_id FROM public.alarm_dispatch_deliveries d WHERE d.id = p_delivery_id
    ), members AS (
        SELECT d.id, d.updated_at, d.status, d.attempt_count, d.last_error_code,
               d.quarantined_at, d.sent_at, d.cancelled_at,
               to_jsonb(d) AS delivery_facts, to_jsonb(e) AS event_facts,
               to_jsonb(u) AS unit_facts
        FROM public.alarm_dispatch_deliveries d
        JOIN target t ON d.send_unit_id = t.send_unit_id
        JOIN public.alarm_dispatch_events e ON e.id = d.event_id
        JOIN public.alarm_dispatch_send_units u ON u.id = d.send_unit_id
        ORDER BY d.id LIMIT 101
    ), aggregate AS (
        SELECT count(id) AS member_count,
               array_agg(id ORDER BY id) AS target_ids,
               jsonb_agg(jsonb_build_object('id', id::text, 'updatedAt', updated_at) ORDER BY id) AS target_revisions,
               jsonb_agg(jsonb_build_object(
                   'id', id::text, 'status', status, 'attemptCount', attempt_count,
                   'lastErrorCode', CASE
                       WHEN last_error_code ~ '^[A-Za-z0-9_.:-]{1,128}$' THEN last_error_code
                       WHEN last_error_code = '' THEN '' ELSE 'unclassified' END,
                   'updatedAt', updated_at,
                   'quarantinedAt', quarantined_at, 'sentAt', sent_at, 'cancelledAt', cancelled_at
               ) ORDER BY id) AS status_metadata,
               jsonb_agg(jsonb_build_object(
                   'delivery', delivery_facts,
                   'event', event_facts, 'sendUnit', unit_facts
               ) ORDER BY id) AS original_facts
        FROM members m
    )
    SELECT (SELECT t.send_unit_id FROM target t), a.target_ids, a.target_revisions,
           a.status_metadata, encode(sha256(convert_to(a.original_facts::text, 'UTF8')), 'hex'),
           a.member_count
    FROM aggregate a
    WHERE a.member_count > 0
$function$;

REVOKE ALL ON FUNCTION public.alarm_dispatch_closeout_snapshot(bigint) FROM PUBLIC;

-- 호출자는 SERIALIZABLE 트랜잭션을 소유한다. 오류·커밋 결과 불명은 재시도하지 않고 receipt_id를 조회한다.
CREATE OR REPLACE FUNCTION public.record_alarm_dispatch_closeout(
    p_receipt_id uuid, p_addressed_delivery_id bigint, p_expected_target_ids bigint[],
    p_expected_sha256 text, p_operator_id text, p_reason text
) RETURNS void LANGUAGE plpgsql AS $function$
DECLARE
    unit_id bigint;
    locked_count integer;
    snapshot record;
BEGIN
    IF current_setting('transaction_isolation') <> 'serializable' THEN
        RAISE EXCEPTION 'alarm dispatch closeout requires serializable transaction';
    END IF;
    IF p_receipt_id IS NULL OR p_addressed_delivery_id IS NULL OR p_addressed_delivery_id <= 0
       OR p_expected_target_ids IS NULL OR cardinality(p_expected_target_ids) NOT BETWEEN 1 AND 100
       OR p_expected_sha256 IS NULL OR p_expected_sha256 !~ '^[0-9a-f]{64}$'
       OR p_operator_id IS NULL OR length(btrim(p_operator_id)) NOT BETWEEN 1 AND 128
       OR p_operator_id <> btrim(p_operator_id) OR p_operator_id ~ '[[:cntrl:]]'
       OR p_reason IS NULL OR length(btrim(p_reason)) NOT BETWEEN 1 AND 1024
       OR p_reason <> btrim(p_reason) OR p_reason ~ '[[:cntrl:]]' THEN
        RAISE EXCEPTION 'invalid alarm dispatch closeout request';
    END IF;
    IF array_position(p_expected_target_ids, p_addressed_delivery_id) IS NULL THEN
        RAISE EXCEPTION 'addressed delivery missing from reviewed targets';
    END IF;

    -- 대상과 전체 send unit을 requeue와 같은 순서로 잠그고, 잠금 대기 뒤 재조회한다.
    SELECT d.send_unit_id INTO unit_id
    FROM public.alarm_dispatch_deliveries d WHERE d.id = p_addressed_delivery_id FOR UPDATE;
    IF NOT FOUND OR unit_id IS NULL THEN
        RAISE EXCEPTION 'addressed delivery or send unit missing';
    END IF;
    PERFORM d.id FROM public.alarm_dispatch_deliveries d
    WHERE d.send_unit_id = unit_id ORDER BY d.id LIMIT 101 FOR UPDATE OF d;
    GET DIAGNOSTICS locked_count = ROW_COUNT;
    SELECT s.send_unit_id, s.target_ids, s.target_revisions, s.status_metadata,
           s.original_sha256, s.member_count INTO snapshot
    FROM public.alarm_dispatch_closeout_snapshot(p_addressed_delivery_id) AS s;
    IF NOT FOUND OR snapshot.send_unit_id <> unit_id OR snapshot.member_count <> locked_count
       OR snapshot.member_count > 100
       OR snapshot.target_ids IS DISTINCT FROM p_expected_target_ids
       OR snapshot.original_sha256 IS DISTINCT FROM p_expected_sha256 THEN
        RAISE EXCEPTION 'reviewed closeout snapshot changed';
    END IF;
    IF EXISTS (
        SELECT 1 FROM public.alarm_dispatch_deliveries d
        WHERE d.id = ANY(snapshot.target_ids)
          AND (d.status <> 'quarantined' OR d.sent_at IS NOT NULL OR d.cancelled_at IS NOT NULL)
    ) THEN
        RAISE EXCEPTION 'closeout requires only quarantined, unsent targets';
    END IF;
    IF EXISTS (SELECT 1 FROM public.alarm_dispatch_closeout_receipts r WHERE r.send_unit_id = unit_id) THEN
        RAISE EXCEPTION 'send unit already has a closeout receipt';
    END IF;
    INSERT INTO public.alarm_dispatch_closeout_receipts (
        receipt_id, send_unit_id, addressed_delivery_id, target_ids,
        target_revisions, status_metadata, original_sha256, operator_id, reason
    ) VALUES (
        p_receipt_id, unit_id, p_addressed_delivery_id, snapshot.target_ids,
        snapshot.target_revisions, snapshot.status_metadata, snapshot.original_sha256,
        p_operator_id, p_reason
    );
END
$function$;

-- maintenance DB owner만 실행한다. 새 runtime role에는 실행 권한을 주지 않는다.
REVOKE ALL ON FUNCTION public.record_alarm_dispatch_closeout(uuid, bigint, bigint[], text, text, text) FROM PUBLIC;

CREATE OR REPLACE FUNCTION public.reject_alarm_dispatch_closeout_receipt_change()
RETURNS trigger LANGUAGE plpgsql AS $function$
BEGIN
    RAISE EXCEPTION 'alarm dispatch closeout receipts are append-only';
END
$function$;

DO $migration$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger
        WHERE tgrelid = 'public.alarm_dispatch_closeout_receipts'::regclass
          AND tgname = 'alarm_dispatch_closeout_receipts_immutable'
    ) THEN
        CREATE TRIGGER alarm_dispatch_closeout_receipts_immutable
            BEFORE UPDATE OR DELETE ON public.alarm_dispatch_closeout_receipts
            FOR EACH ROW EXECUTE FUNCTION public.reject_alarm_dispatch_closeout_receipt_change();
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger
        WHERE tgrelid = 'public.alarm_dispatch_closeout_receipts'::regclass
          AND tgname = 'alarm_dispatch_closeout_receipts_no_truncate'
    ) THEN
        CREATE TRIGGER alarm_dispatch_closeout_receipts_no_truncate
            BEFORE TRUNCATE ON public.alarm_dispatch_closeout_receipts
            FOR EACH STATEMENT EXECUTE FUNCTION public.reject_alarm_dispatch_closeout_receipt_change();
    END IF;
END
$migration$;
