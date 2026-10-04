-- API refresh와 공유하는 projection guard를 share 잠금한 뒤 새 snapshot의 CURRENT generation을 반환한다.
-- 같은 트랜잭션의 이후 문장은 전환 중간 상태를 보지 않으며, 이 잠금은 항상 lease 행 잠금보다 먼저 얻는다.
SELECT generation
FROM lock_current_youtube_collection_projection()
