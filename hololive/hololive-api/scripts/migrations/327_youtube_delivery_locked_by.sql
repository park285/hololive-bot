-- YouTube 전송 행의 claim 소유자 열. claim이 워커 인스턴스 ID를 기록하고, 발송 전이 fence가
-- (status, locked_by, row_version)을 호출자 claim과 비교한다. 같은 컨테이너 이름으로 재시작하면 소유자 이름이 이전
-- 프로세스와 같을 수 있으므로 claim 세대 구분은 row_version이 맡고 locked_by만으로 fence하지 않는다.
-- 기본값 없는 nullable 열 추가라 표를 다시 쓰지 않는다. 기존 행과 이 열을 모르는 이전 워커가 쓴 행은 NULL이며, 이전
-- 워커는 열 목록을 명시해 읽고 쓰므로 새 열을 무시한다. 워커 claim이 상시 쓰는 표라 잠금 대기를 3초로 끊는다.
BEGIN;
SET LOCAL lock_timeout = '3s';

ALTER TABLE public.youtube_notification_delivery
    ADD COLUMN IF NOT EXISTS locked_by text;

COMMIT;
