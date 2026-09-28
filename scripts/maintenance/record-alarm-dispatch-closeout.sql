-- 격리 send unit의 재발송 없는 종료 결정만 기록한다. delivery/event/send unit은 수정하지 않는다.
-- preview의 targetIds(쉼표 구분)와 originalSha256을 정확히 전달한다.
-- 결과 불명 시 receipt_id로 조회한다. 자동 재실행하지 않는다.
\set ON_ERROR_STOP on
\if :{?receipt_id}
\else
  \echo 'receipt_id is required'
  \quit 3
\endif
\if :{?addressed_delivery_id}
\else
  \echo 'addressed_delivery_id is required'
  \quit 3
\endif
\if :{?expected_target_ids}
\else
  \echo 'expected_target_ids is required'
  \quit 3
\endif
\if :{?expected_sha256}
\else
  \echo 'expected_sha256 is required'
  \quit 3
\endif
\if :{?operator_id}
\else
  \echo 'operator_id is required'
  \quit 3
\endif
\if :{?reason}
\else
  \echo 'reason is required'
  \quit 3
\endif
BEGIN ISOLATION LEVEL SERIALIZABLE;
SET LOCAL statement_timeout = '10s';
SET LOCAL lock_timeout = '3s';
SET LOCAL TIME ZONE 'UTC';
DO $operation$
BEGIN
    IF current_database() <> 'hololive' THEN
        RAISE EXCEPTION 'wrong database for alarm dispatch closeout';
    END IF;
END
$operation$;
SELECT public.record_alarm_dispatch_closeout(
    :'receipt_id'::uuid,
    :'addressed_delivery_id'::bigint,
    string_to_array(:'expected_target_ids', ',')::bigint[],
    :'expected_sha256', :'operator_id', :'reason'
);
COMMIT;
SELECT receipt_id, send_unit_id, addressed_delivery_id, target_ids,
       target_revisions, status_metadata, original_sha256,
       operator_id, reason, disposition, recorded_at
FROM public.alarm_dispatch_closeout_receipts
WHERE receipt_id = :'receipt_id'::uuid;
