-- alarm-worker는 v10.1.0부터 이 표에 쓰지 않고, 읽는 런타임 조회는 처음부터 없었다(채널별 마지막 평가 시각만 담음).
-- 후보 복구에 필요한 정보는 alarm_upcoming_candidates.checked_at이 가진다.
-- 이 표에 쓰는 이전 alarm-worker image가 아직 실행 중이면 upcoming 후보 저장이 실패하므로, 새 worker로 바꾼 뒤 적용한다.
DROP TABLE IF EXISTS public.alarm_upcoming_checkpoints;
