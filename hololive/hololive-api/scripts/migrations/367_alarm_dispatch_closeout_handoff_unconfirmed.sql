-- handoff 결과 불명(handoff_unconfirmed)으로 격리된 행은 Iris에 넘긴 시각(sent_at)이 남아 있어
-- 228의 기록 함수가 거부했고, 검토할 경로가 없어 보존 기간(180일) 내내 미검토 격리로 남았다.
-- closeout은 재발송하지 않기로 한 결정만 기록하므로 전달 여부와 무관하게 격리·미취소 행이면 받는다.
-- 함수 본문 외의 잠금 순서·snapshot 재검사·중복 거부·append-only 계약은 228과 같다.
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
          AND (d.status <> 'quarantined' OR d.cancelled_at IS NOT NULL)
    ) THEN
        RAISE EXCEPTION 'closeout requires only quarantined, uncancelled targets';
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
