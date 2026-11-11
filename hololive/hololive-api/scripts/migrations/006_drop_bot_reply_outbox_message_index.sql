-- UNIQUE(message_id, phase, ordinal) 인덱스가 message_id 앞 열 조회를 이미 덮으므로 이 인덱스는 쓰기 비용만 더한다.
-- 단일 문장 CONCURRENTLY라 쓰기를 막지 않는다.
DROP INDEX CONCURRENTLY IF EXISTS public.idx_bot_reply_outbox_message;
