-- 멤버($1)의 JST 날짜($2)에 !스케쥴이 게시하는 확정본 하나를 고른다. 재추출 중이거나 재추출이 실패해도 이전 묶음이 있으면 고른다.
-- 오늘을 덮는지는 주 범위를 행 묶음의 하루 밀기 값(rows_day_shift)만큼 민 범위로 본다. JST로 바꾼 행이 주 범위보다 하루 늦거나 이른 포스터를 그 날짜에 보여 주기 위해서다.
-- 오늘을 덮는 확정본이 없을 때만 민 시작일이 7일 안에 오는 확정본 가운데 가장 가까운 주를 고른다. 주가 끝나기 전에 다음 주 포스터가 올라오기 때문이다.
-- 정렬은 밀지 않은 주 시작일로 해야 같은 주의 늦게 올린 수정본이 하루 밀기 값이 다른 이전 확정본보다 앞선다.
SELECT id
FROM schedule_posters
WHERE channel_id = $1 AND review_status = 'confirmed' AND rows_extracted_at IS NOT NULL
  AND week_start + rows_day_shift <= $2::date + 7 AND week_end + rows_day_shift >= $2::date
ORDER BY week_start + rows_day_shift > $2::date,
         CASE WHEN week_start + rows_day_shift <= $2::date THEN week_start END DESC,
         week_start, posted_at DESC, id DESC
LIMIT 1
