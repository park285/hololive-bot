-- 행 잠금을 걸지 않는다. epoch의 불변성은 singleton PRIMARY KEY와 CHECK, 그리고 migration 191이
-- runtime role에서 UPDATE·DELETE·TRUNCATE를 REVOKE한 ACL이 소유한다. FOR SHARE는 그 위에 아무것도
-- 더하지 못하면서 대상 컬럼의 UPDATE 권한을 요구해 runtime role에서 42501로 실패한다.
SELECT cutoff_received_at
FROM source_observation_replay_epoch
WHERE singleton
