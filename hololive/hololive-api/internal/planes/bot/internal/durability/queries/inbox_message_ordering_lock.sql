-- 잠금 키는 inbox_ordering_key_lock.sql·inbox_ordering_keys_lock.sql과 같아야 같은 ordering key의 admit·정산·회수가 직렬화된다.
-- message가 없으면 행이 없고 잠금도 잡지 않는다.
SELECT pg_advisory_xact_lock(hashtextextended(ordering_key, 0))
FROM bot_webhook_inbox
WHERE message_id = $1
