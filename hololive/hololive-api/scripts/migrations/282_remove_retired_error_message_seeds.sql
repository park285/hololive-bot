-- error/external_api_call_failed·cache_connection_failed는 v9.0.0부터 Go 상수·기동 검증·렌더 경로 어디에서도 읽지 않는다.
-- v8.0.0 이미지는 bot plane 기동 때 두 key를 검증하므로 v9.0.0이 운영에 배포된 뒤에 지운다. 이 migration 뒤
-- v8.0.0으로 되돌리려면 001 baseline의 두 시드 행을 다시 넣어야 한다. 재적용해도 지울 행이 없다.
BEGIN;
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';
DELETE FROM public.message_strings
WHERE namespace = 'error' AND key IN ('external_api_call_failed', 'cache_connection_failed');
COMMIT;
