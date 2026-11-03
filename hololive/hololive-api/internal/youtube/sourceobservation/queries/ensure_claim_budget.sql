WITH extended AS (
    -- 남은 lease가 예산($3 ms)보다 짧을 때만 연장한다. 충분하면 행을 쓰지 않고 아래 가지가 현재 만료 시각을 돌려준다.
    -- 두 가지는 같은 statement snapshot의 lease_expires_at을 배타적 조건(< / >=)으로 나누므로 최대 1행이다.
    -- 0행이면 claim을 잃은 것(ErrClaimLost)이고, 최종 판정은 finalize의 claim_lock(FOR UPDATE)이 다시 한다.
    UPDATE source_observation_queue
    SET lease_expires_at = NOW() + ($3::bigint * INTERVAL '1 millisecond'),
        updated_at = NOW()
    WHERE observation_id = $1
      AND status = 'PROCESSING'
      AND lease_token = $2
      AND lease_expires_at > NOW()
      AND lease_expires_at < NOW() + ($3::bigint * INTERVAL '1 millisecond')
    RETURNING lease_expires_at
)
SELECT lease_expires_at
FROM extended
UNION ALL
SELECT lease_expires_at
FROM source_observation_queue
WHERE observation_id = $1
  AND status = 'PROCESSING'
  AND lease_token = $2
  AND lease_expires_at >= NOW() + ($3::bigint * INTERVAL '1 millisecond')
