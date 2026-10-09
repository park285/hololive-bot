-- 해소 불가 영상 종료 검증용 채널 /live 음성. identity를 확인한 채널 페이지·예정 영상 결과만 음성이며,
-- 마지막 LIVE positive 이후이고 영상 확인 예정 시각 기준 5분 안의 결과만 신선하다(채널 확인 freshness 상한과 같다).
SELECT effective_at
FROM youtube_channel_live_checks
WHERE channel_id = $1
  AND channel_identity_confirmed
  AND outcome IN ('UPCOMING_VIDEO', 'CHANNEL_PAGE')
  AND effective_at > $2
  AND effective_at BETWEEN $3::timestamptz - INTERVAL '5 minutes' AND $3::timestamptz + INTERVAL '5 minutes'
