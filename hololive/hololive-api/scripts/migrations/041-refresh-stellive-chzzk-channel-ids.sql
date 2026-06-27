-- 041-refresh-stellive-chzzk-channel-ids.sql
-- 2026-03-22 기준 치지직 공개 검색 결과에 맞춰 스텔라이브 active 멤버의 채널 ID를 갱신한다.

BEGIN;

UPDATE members
SET chzzk_channel_id = '45e71a76e949e16a34764deb962f9d9f'
WHERE channel_id = 'UClbYIn9LDbbFZ9w2shX3K0g'
  AND chzzk_channel_id IS DISTINCT FROM '45e71a76e949e16a34764deb962f9d9f';

UPDATE members
SET chzzk_channel_id = 'a6c4ddb09cdb160478996007bff35296'
WHERE channel_id = 'UCq-U-D8O6_6e4X6r-z9V0w'
  AND chzzk_channel_id IS DISTINCT FROM 'a6c4ddb09cdb160478996007bff35296';

UPDATE members
SET chzzk_channel_id = 'b044e3a3b9259246bc92e863e7d3f3b8'
WHERE channel_id = 'UC99CUC6yR6O_uXyS_3K7yKA'
  AND chzzk_channel_id IS DISTINCT FROM 'b044e3a3b9259246bc92e863e7d3f3b8';

UPDATE members
SET chzzk_channel_id = '4515b179f86b67b4981e16190817c580'
WHERE channel_id = 'UC9o9D7U5O8V0A-zO0v7UeLw'
  AND chzzk_channel_id IS DISTINCT FROM '4515b179f86b67b4981e16190817c580';

UPDATE members
SET chzzk_channel_id = '4325b1d5bbc321fad3042306646e2e50'
WHERE channel_id = 'UC9m5xP6u69zXpD7MscY-uYQ'
  AND chzzk_channel_id IS DISTINCT FROM '4325b1d5bbc321fad3042306646e2e50';

UPDATE members
SET chzzk_channel_id = '64d76089fba26b180d9c9e48a32600d9'
WHERE channel_id = 'UCYxLMfeX1CbMBll9MsGlzmw'
  AND chzzk_channel_id IS DISTINCT FROM '64d76089fba26b180d9c9e48a32600d9';

UPDATE members
SET chzzk_channel_id = '4d812b586ff63f8a2946e64fa860bbf5'
WHERE channel_id = 'UCcA21_PzN1EhNe7xS4MJGsQ'
  AND chzzk_channel_id IS DISTINCT FROM '4d812b586ff63f8a2946e64fa860bbf5';

COMMIT;
