-- acl_rooms.room_id를 정규 signed i64 chatID로 닫는다. ACL 판정은 chatID로 하므로 옛 방 이름 같은 비정규 행은
-- whitelist에서도 blacklist에서도 일치하지 않고, 운영자에게 등록된 방처럼 보이기만 했다. 새 등록은 이미 chatID만 받는다.
-- 이 migration은 그 비정규 행을 지운다. 지울 대상은 적용 전 운영 조회
-- `SELECT list_type, room_id FROM acl_rooms WHERE room_id !~ '^(0|-?[1-9][0-9]{0,18})$'`로 확인하고 승인받는다.
-- CHECK는 Go validateRoomChatID(부호·앞자리 0·i64 범위)와 같은 규칙이다. `-0`은 받지 않는다.
-- 표가 작아도 VALIDATE는 CONVENTIONS대로 ADD와 다른 트랜잭션에서 한다. 재적용은 아무것도 바꾸지 않는다.
BEGIN;
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';
-- DELETE와 ADD CONSTRAINT 사이에 다른 writer가 비정규 행을 넣지 못하게 처음부터 쓰기를 막는다.
LOCK TABLE public.acl_rooms IN SHARE ROW EXCLUSIVE MODE;
-- SQL은 AND/OR 평가 순서를 보장하지 않으므로 형식이 맞는 값만 numeric으로 바꾸도록 CASE로 나눈다.
DELETE FROM public.acl_rooms
WHERE CASE WHEN room_id ~ '^(0|-?[1-9][0-9]{0,18})$'
           THEN room_id::numeric NOT BETWEEN -9223372036854775808 AND 9223372036854775807
           ELSE true END;
DO $migration$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_catalog.pg_constraint
        WHERE conrelid = 'public.acl_rooms'::regclass AND conname = 'chk_acl_rooms_room_id_chat_id'
    ) THEN
        ALTER TABLE public.acl_rooms ADD CONSTRAINT chk_acl_rooms_room_id_chat_id CHECK (
            CASE WHEN room_id ~ '^(0|-?[1-9][0-9]{0,18})$'
                 THEN room_id::numeric BETWEEN -9223372036854775808 AND 9223372036854775807
                 ELSE false END
        ) NOT VALID;
    END IF;
END
$migration$;
COMMIT;
ALTER TABLE public.acl_rooms VALIDATE CONSTRAINT chk_acl_rooms_room_id_chat_id;
