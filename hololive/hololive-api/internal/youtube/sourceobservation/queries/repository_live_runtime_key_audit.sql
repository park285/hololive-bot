-- 이 트랜잭션에서 문장 트리거가 refresh한 수명 모델 key 수와, 그 key와 writer가 모은 key($1)의 차집합 크기를 센다.
-- missing은 트리거만 갱신한 key, extra는 writer만 모은 key다.
WITH trigger_keys AS (
    SELECT refreshed.video_id
    FROM unnest(NULLIF(current_setting('hololive.live_runtime_trigger_keys', true), '')::text[]) AS refreshed(video_id)
), writer_keys AS (
    SELECT collected.video_id
    FROM unnest($1::text[]) AS collected(video_id)
)
SELECT
    (SELECT count(DISTINCT trigger_keys.video_id) FROM trigger_keys) AS trigger_keys,
    (SELECT count(missing.video_id)
     FROM (SELECT video_id FROM trigger_keys EXCEPT SELECT video_id FROM writer_keys) AS missing) AS missing,
    (SELECT count(extra.video_id)
     FROM (SELECT video_id FROM writer_keys EXCEPT SELECT video_id FROM trigger_keys) AS extra) AS extra
