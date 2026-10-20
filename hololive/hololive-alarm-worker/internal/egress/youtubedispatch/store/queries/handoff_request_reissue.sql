-- Iris failed 재발급의 고정 요청 쪽 기록이다. 본문·경로·멤버는 그대로 두고 세대만 올리며 handoff 확인 상태를 비운다.
-- 다음 claim은 같은 본문을 새 세대 ID(base:rN)로 보낸다. 세대 상한은 send_request_advance.sql과 같다. 호출자는 같은
-- 트랜잭션에서 요청을 잠그고 claim 토큰·세대($2)를 확인했다.
UPDATE youtube_notification_send_request
SET generation = generation + 1,
    handoff_request_id = NULL,
    handoff_next_check_at = NULL,
    handoff_checked_at = NULL
WHERE base_id = $1
  AND generation = $2::INT
  AND generation < 2
