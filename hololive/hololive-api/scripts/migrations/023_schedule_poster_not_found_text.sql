-- !스케쥴 없음 안내의 016 기본 문구만 바꾸고 관리자가 고친 문구는 그대로 둔다.
UPDATE public.message_strings
SET value = '이번 주 일정표가 아직 없습니다.', updated_at = now()
WHERE namespace = 'misc'
  AND key = 'schedule_poster_not_found'
  AND value = '이번 주 번역된 일정 포스터가 아직 없습니다.';
