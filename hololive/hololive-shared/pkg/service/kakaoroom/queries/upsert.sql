-- 명령마다 같은 방 사실을 다시 관측하므로, 저장값이 이미 같으면 INSERT 후보를 만들지 않는다.
-- ON CONFLICT DO UPDATE는 WHERE가 거짓이어도 충돌 행을 잠그기 때문에 NOT EXISTS로 먼저 걸러야
-- no-op 관측이 쓰기 트랜잭션(XID·행 잠금 WAL)을 남기지 않는다. VALUES의 명시 캐스트는 같은
-- 파라미터를 INSERT 열과 비교식에 함께 쓸 때 서버 타입 추론이 어긋나지 않게 한다.
WITH incoming (room_id, room_type, room_link_id) AS (
    VALUES ($1::text, $2::text, $3::text)
)
INSERT INTO kakao_rooms (room_id, room_type, room_link_id)
SELECT incoming.room_id, incoming.room_type, incoming.room_link_id
FROM incoming
WHERE NOT EXISTS (
    SELECT 1
    FROM kakao_rooms AS stored
    WHERE stored.room_id = incoming.room_id
      AND stored.room_type = incoming.room_type
      AND stored.room_link_id = incoming.room_link_id
)
ON CONFLICT (room_id) DO UPDATE
SET room_type = EXCLUDED.room_type,
    room_link_id = EXCLUDED.room_link_id,
    updated_at = now()
WHERE (kakao_rooms.room_type, kakao_rooms.room_link_id)
    IS DISTINCT FROM (EXCLUDED.room_type, EXCLUDED.room_link_id)
