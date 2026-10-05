-- 2026-10-05 1단계 배포부터 alarms.member_name을 읽거나 쓰는 코드가 없다. 멤버 이름은 members 정본에서만 읽는다.
-- 이 열에만 걸린 idx_alarms_channel_member_latest도 함께 지워진다. alarm-worker가 alarms를 자주 읽으므로 잠금 대기를
-- 짧게 끊는다. 실패하면 ledger에 남지 않아 다음 실행에서 파일 전체를 다시 적용한다.
BEGIN;
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';
ALTER TABLE public.alarms DROP COLUMN IF EXISTS member_name;
-- misc/alarm_unknown_member는 1단계 alarm-worker부터 읽지 않고 기동 검사에서도 빠졌다.
DELETE FROM public.message_strings WHERE namespace = 'misc' AND key = 'alarm_unknown_member';
COMMIT;
