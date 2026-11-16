-- bot plane 명령 한도 초과와 방송 이력·방송 시간·썸네일 안내 문구다. bot plane은 기동 때 이 key들이 있는지 검증한다.
INSERT INTO public.message_strings (namespace, key, value)
VALUES
    ('error', 'command_rate_limited', '방송 조회 요청이 너무 많습니다. 잠시 후 다시 시도해 주세요.'),
    ('error', 'broadcast_history_query_failed', '방송 이력 조회 중 오류가 발생했습니다.'),
    ('error', 'broadcast_time_query_failed', '방송 시간 조회 중 오류가 발생했습니다.'),
    ('error', 'broadcast_type_unknown', '알 수 없는 방송 타입입니다. 사용 가능: 게임, 잡담, 노래, ASMR, 멤버십, 이벤트, 경마, 동시시청, 뉴스, 기타, 미분류'),
    ('error', 'broadcast_thumbnail_usage', '사용법: 방송이력 썸네일 <video_id>'),
    ('error', 'broadcast_thumbnail_not_found', '종료된 방송 이력에서 해당 video_id를 찾지 못했습니다.'),
    ('error', 'broadcast_thumbnail_download_failed', '고화질 썸네일을 다운로드하지 못했습니다.')
ON CONFLICT (namespace, key) DO NOTHING;
-- error/async_command_backpressure를 읽는 코드가 없다.
DELETE FROM public.message_strings
WHERE namespace = 'error'
  AND key = 'async_command_backpressure';
