-- API refresh와 공유하는 projection guard를 먼저 share 잠금하고, 잠금 이후 snapshot의 CURRENT를 읽는다.
SELECT generation
FROM lock_current_youtube_collection_projection()
