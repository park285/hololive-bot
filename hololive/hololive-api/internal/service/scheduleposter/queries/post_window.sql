-- 글 d가 확정본 p의 글인 범위: 포스터 게시 뒤, 주 첫날 00:00 JST부터 주 마지막 날 다음 날 00:00 JST 전까지다.
-- 대응 작업은 !스케쥴과 같은 확정본(오늘을 덮는 주, 없으면 7일 안에 시작하는 주)만 고르므로 주가 끝난 뒤 글은 이 포스터에 대응할 수 없고,
-- 목록·철회·되살리기도 그 글을 다루지 않는다. 다가오는 주 포스터를 게시하는 날에 올린 주 첫날 전 글은 어느 포스터에도 대응하지 않는다.
d.posted_at > p.posted_at AND d.posted_at >= (p.week_start::timestamp AT TIME ZONE 'Asia/Tokyo')
    AND d.posted_at < ((p.week_end + 1)::timestamp AT TIME ZONE 'Asia/Tokyo')
