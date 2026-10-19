-- 운영 view와 독립적인 매핑으로 원본 집계를 재계산한다. dbtest와 운영 진단이 함께 사용한다.
WITH mapping(kind, observation_kind, rpc_per_kind) AS (
    VALUES
        ('youtubejs_channel_live', 'live_snapshot', false),
        ('youtubejs_channel_live_check', 'channel_live_check', false),
        ('youtubejs_video_live', 'video_live_check', false),
        ('community_collect', 'community_page', false),
        ('youtubejs_content', 'video_list', true),
        ('youtubejs_content', 'shorts_list', true),
        ('youtubejs_channel_metadata', 'channel_profile', false),
        ('youtubejs_channel_metadata', 'channel_photo', false)
), expected AS (
    SELECT target.projection_generation, mapping.kind COLLATE "C" AS kind,
           target.subject_key COLLATE "C" AS subject_key,
           CASE WHEN mapping.rpc_per_kind THEN count(target.observation_kind) ELSE 1 END AS rpc_calls,
           min(target.poll_interval_ms) AS interval_ms, min(target.created_at) AS created_at,
           min(coalesce(target.not_before, '-infinity'::timestamptz)) AS eligible_at
    FROM public.youtube_collection_targets target
    JOIN public.youtube_collection_projection_generations header ON header.generation = target.projection_generation
    JOIN mapping USING (observation_kind)
    WHERE target.enabled AND header.status IN ('CURRENT', 'STAGING')
    GROUP BY target.projection_generation, mapping.kind COLLATE "C", target.subject_key COLLATE "C", mapping.rpc_per_kind
), drift AS (
    SELECT count(expected.projection_generation) FILTER (WHERE actual.projection_generation IS NULL) AS missing_rows,
           count(actual.projection_generation) FILTER (WHERE expected.projection_generation IS NULL) AS extra_rows,
           count(expected.projection_generation) FILTER (
               WHERE actual.projection_generation IS NOT NULL
                 AND ROW(expected.rpc_calls, expected.interval_ms, expected.created_at, expected.eligible_at)
                     IS DISTINCT FROM ROW(actual.rpc_calls, actual.interval_ms, actual.created_at, actual.eligible_at)
           ) AS changed_rows
    FROM expected
    FULL JOIN public.youtube_collection_runtime_bundles actual USING (projection_generation, kind, subject_key)
), mapping_differences AS (
    -- 중복도까지 비교해야 동일한 매핑 행이 추가돼도 정상으로 판정하지 않는다.
    (SELECT kind, observation_kind, rpc_per_kind FROM mapping
     EXCEPT ALL
     SELECT kind, observation_kind, rpc_per_kind FROM public.youtube_collection_runtime_kinds)
    UNION ALL
    (SELECT kind, observation_kind, rpc_per_kind FROM public.youtube_collection_runtime_kinds
     EXCEPT ALL
     SELECT kind, observation_kind, rpc_per_kind FROM mapping)
), mapping_drift AS (
    -- 값 변경의 양방향 차이는 한 observation kind로 세고, 잘못된 NULL key도 포함한다.
    SELECT count(difference) AS changed_rows
    FROM (SELECT DISTINCT observation_kind FROM mapping_differences) difference
)
SELECT drift.missing_rows, drift.extra_rows, drift.changed_rows, mapping_drift.changed_rows AS mapping_changed_rows
FROM drift CROSS JOIN mapping_drift;
