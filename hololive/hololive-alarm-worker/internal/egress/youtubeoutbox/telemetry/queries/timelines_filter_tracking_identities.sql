-- timeline 조회 필터: $2/$3은 같은 길이의 (kind, content_id) 쌍 배열이다.
-- 쌍 단위로 비교해 kind×content_id 교차 조합을 고르지 않는다.
(track.kind, track.content_id) IN (
    SELECT input.kind, input.content_id
    FROM unnest($2::text[], $3::text[]) AS input(kind, content_id)
)
