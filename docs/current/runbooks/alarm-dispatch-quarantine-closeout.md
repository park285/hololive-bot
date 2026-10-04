# 격리 발송 종료 영수증

`closed_without_replay`는 검토한 격리 send unit을 **재발송하지 않기로 한 결정**을 기록합니다. provider가 실제로 보냈는지 판정하지 않습니다. 기존 delivery/event/send unit의 상태·시각·오류·시도 횟수·재시도 일정과 정상 retention은 그대로 유지합니다. 원본 행 전체로 계산한 SHA-256 digest, 정확한 대상 ID와 `updated_at` revision, 상태 메타데이터, 운영자·사유·시각을 별도 append-only receipt에 보존합니다. payload, 메시지 본문과 오류 원문은 receipt에 복사하지 않습니다.

이 경로는 관리자 HTTP/UI가 아니라 maintenance DB owner의 검토 절차입니다. migration 228/229 적용 전 227의 완료 조건과 ledger state singleton 원본·복구 SQL을 별도 보존합니다. 229는 적용 기록과 현재 완료 singleton을 잠금 아래 다시 검사한 뒤 상태 표식만 폐기하며 logical delivery ledger는 유지합니다. 기존 table이 229 적용 기록 없이 이미 없다면 결과를 추정하지 않고 중단합니다.

실제 maintenance 연결이 migration owner 또는 동등한 권한인지 먼저 read-only로 확인합니다. 기록 함수의 `PUBLIC` 실행 권한은 제거돼 있으며 새 runtime role에 권한을 추가하지 않습니다. 아래 결과가 모두 참이 아니면 기록을 시작하지 않습니다.

```sql
SELECT current_user,
       has_function_privilege(current_user,
           'public.record_alarm_dispatch_closeout(uuid,bigint,bigint[],text,text,text)', 'EXECUTE') AS can_record,
       has_table_privilege(current_user, 'public.alarm_dispatch_closeout_receipts', 'INSERT') AS can_insert;
```

1. read-only `preview-alarm-dispatch-closeout.sql`을 `addressed_delivery_id`와 실행하고 결과를 비공개 검토 기록에 보존합니다. `memberCount`가 1~100이고 `targetIds` 전체가 정확히 검토한 격리 항목인지 확인합니다. 같은 send unit의 일부만 선택하지 않습니다.
2. 새 UUID `receipt_id`를 한 번 정하고, `expected_target_ids`에 preview의 ID를 **오름차순 쉼표 구분**으로, `expected_sha256`에 digest를 그대로 전달해 `record-alarm-dispatch-closeout.sql`을 한 번 실행합니다.
3. 성공하면 같은 `receipt_id`와 preview의 `sendUnitId`로 `lookup-alarm-dispatch-closeout.sql`을 조회합니다. 연결 단절·timeout·직렬화 충돌·중복 제약 오류 뒤에도 먼저 이 조회를 실행합니다. 영수증이 없거나 다른 receipt가 그 send unit을 소유하면 원인과 현재 원장을 검토합니다. 결과 불명을 근거로 자동 재실행하지 않습니다.

중앙 런타임 호스트에는 `psql`이 없으므로 기존 maintenance executor의 읽기 전용 SQL 마운트만 이 경로로 바꿉니다. 아래의 대문자 값은 보존한 preview·승인된 운영자 메타데이터에서 채웁니다.

```bash
sudo -n env MIGRATIONS_DIR=/opt/hololive-bot/compose/current/scripts/maintenance \
  ./scripts/runtime/db-maintenance-exec.sh psql -w -X -v ON_ERROR_STOP=1 \
  -v addressed_delivery_id=2042 -f /migrations/preview-alarm-dispatch-closeout.sql

sudo -n env MIGRATIONS_DIR=/opt/hololive-bot/compose/current/scripts/maintenance \
  ./scripts/runtime/db-maintenance-exec.sh psql -w -X -v ON_ERROR_STOP=1 \
  -v receipt_id=UUID -v addressed_delivery_id=2042 \
  -v expected_target_ids=2042,2043 -v expected_sha256=SHA256 \
  -v operator_id=OPERATOR -v reason='REVIEW_REASON' \
  -f /migrations/record-alarm-dispatch-closeout.sql

sudo -n env MIGRATIONS_DIR=/opt/hololive-bot/compose/current/scripts/maintenance \
  ./scripts/runtime/db-maintenance-exec.sh psql -w -X -v ON_ERROR_STOP=1 \
  -v receipt_id=UUID -v send_unit_id=SEND_UNIT_ID \
  -f /migrations/lookup-alarm-dispatch-closeout.sql
```

기록 함수는 SERIALIZABLE 트랜잭션에서 대상과 send unit 전체를 ID 순서로 잠그고 현재 ID 집합·digest·격리 상태를 재검사합니다. 대상 행이 전부 `quarantined`이고 `sent_at`과 `cancelled_at`이 비어 있을 때만 INSERT합니다. `receipt_id`와 `send_unit_id`는 각각 유일하며 UPDATE/DELETE는 트리거가 거부합니다. 기존 재처리 API와 worker는 이 receipt를 상태 전환이나 전송 성공으로 해석하지 않습니다.

worker의 `alarm_dispatch_pg_unreviewed_quarantined_rows`는 receipt의 `target_ids`·`status_metadata`가 행의 현재 revision·상태와 정확히 일치하는 격리 행만 검토 완료로 보고 뺍니다. `alarm_dispatch_pg_quarantined_rows`는 보존 총량이라 receipt 뒤에도 retention 삭제 전까지 그대로입니다. 이후 재처리·재격리로 revision이 바뀐 행은 같은 receipt가 있어도 다시 검토 대상입니다.
