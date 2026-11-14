-- 그 사이 거절·주 범위 수정·재추출 예약으로 rows_revision이 올랐으면 다른 입력으로 뽑은 결과라 기록하지 않는다.
-- revision과 updated_at은 건드리지 않는다. 진행 중인 이미지 번역의 결과 기록과 Console의 revision 조건을 깨지 않기 위해서다.
UPDATE schedule_posters
SET rows_status = 'succeeded', rows_error = '', rows_extracted_at = now()
WHERE id = $1 AND rows_revision = $2 AND review_status = 'confirmed' AND rows_status = 'pending'
