-- commit 응답이 불명확하면 이 조회로 영수증 존재 여부를 먼저 확인한다.
\set ON_ERROR_STOP on
\if :{?receipt_id}
\else
  \echo 'receipt_id is required'
  \quit 3
\endif
\if :{?send_unit_id}
\else
  \echo 'send_unit_id is required'
  \quit 3
\endif
BEGIN READ ONLY;
SELECT receipt_id, send_unit_id, addressed_delivery_id, target_ids,
       target_revisions, status_metadata, original_sha256,
       operator_id, reason, disposition, recorded_at
FROM public.alarm_dispatch_closeout_receipts
WHERE receipt_id = :'receipt_id'::uuid OR send_unit_id = :'send_unit_id'::bigint
ORDER BY recorded_at, receipt_id;
COMMIT;
