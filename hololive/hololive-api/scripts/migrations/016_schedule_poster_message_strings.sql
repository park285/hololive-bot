-- !스케쥴 응답 문구를 다른 명령처럼 message_strings에서 읽는다. bot plane은 기동 때 세 key가 있는지 검증한다.
INSERT INTO public.message_strings (namespace, key, value)
VALUES
    ('error', 'schedule_poster_need_member_name', E'❌ 멤버 이름을 입력해주세요.\n예) !스케쥴 소피아'),
    ('error', 'schedule_poster_query_failed', '❌ 일정 포스터 조회 중 오류가 발생했습니다.'),
    ('misc', 'schedule_poster_not_found', '이번 주 번역된 일정 포스터가 아직 없습니다.')
ON CONFLICT (namespace, key) DO NOTHING;
