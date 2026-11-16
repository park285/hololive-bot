-- 관리자 검토로 넘기는 UPDATE(request_review, hand_over_unreviewed)가 자동 판정의 확인과 변경 사이에 끼지 않도록 후보 행을 잠근다.
SELECT review_requested_at IS NULL FROM schedule_posters WHERE id = $1 FOR UPDATE
