SELECT scheduled_for,
       observation_id,
       encode(evidence_sha256, 'hex') AS evidence_sha256,
       effective_at,
       received_at,
       encode(scope_sha256, 'hex') AS scope_sha256,
       coverage
FROM youtube_content_absence_slots
WHERE channel_id = $1
  AND observation_kind = $2
  -- reducer는 현재 slot(재처리 판정, $3=ScheduledFor)과 이번 관측보다 늦은 slot(늦은 positive 재적용,
  -- $4=EffectiveAt)만 읽는다. 적용 순서가 First/Second absence를 정하므로 scheduled_for 순으로 고정한다.
  AND (scheduled_for = $3 OR effective_at > $4)
ORDER BY scheduled_for
FOR UPDATE
