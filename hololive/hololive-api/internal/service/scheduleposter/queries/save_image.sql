-- revision과 updated_at은 건드리지 않는다. 관리자의 revision 조건과 진행 중인 검토·행 추출 기록을 깨지 않기 위해서다.
-- 거절·만료는 되돌릴 수 없어 그 상태로 바꾸는 문장이 그림을 비우므로, 그 뒤에 끝난 원본 받기가 다시 채우지 않게 상태를 본다.
UPDATE schedule_posters
SET image_data = $2, image_mime = $3
WHERE id = $1 AND image_data IS NULL AND review_status IN ('pending_review', 'confirmed')
