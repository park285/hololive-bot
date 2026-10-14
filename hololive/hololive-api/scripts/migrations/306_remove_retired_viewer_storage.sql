-- 퇴역 생산자/소비자를 중지한 뒤 적용합니다. 삭제 승인은 아래 다섯 제품 테이블뿐입니다.
-- 공유 원장에 예상 밖 viewer 이력이 있으면 queue/replay/lease 보호를 우회하지 않고 전체를 롤백합니다.
BEGIN;
SET LOCAL lock_timeout = '3s';

LOCK TABLE public.observation_contract_generations,
    public.youtube_collection_targets,
    public.youtube_collection_target_reasons,
    public.youtube_collection_job_leases,
    public.source_observations,
    public.source_observation_payloads,
    public.source_collection_checkpoints,
    public.source_observation_applications,
    public.source_observation_collisions,
    public.source_observation_consumer_offsets,
    public.source_observation_replay_requests,
    public.source_observation_subject_heads,
    public.source_reconciliation_conflicts
IN SHARE ROW EXCLUSIVE MODE;

DO $migration$
DECLARE
    target_table text;
    has_history boolean;
BEGIN
    FOREACH target_table IN ARRAY ARRAY[
        'source_observations',
        'source_observation_payloads',
        'source_observation_collisions',
        'source_observation_replay_requests',
        'source_observation_subject_heads',
        'source_reconciliation_conflicts',
        'youtube_collection_targets',
        'youtube_collection_target_reasons'
    ] LOOP
        EXECUTE format('SELECT EXISTS (SELECT 1 FROM public.%I WHERE observation_kind = $1)', target_table)
            INTO has_history USING 'viewer_sample';
        IF has_history THEN
            RAISE EXCEPTION 'viewer removal requires review of retained history in public.%', target_table;
        END IF;
    END LOOP;

    IF EXISTS (
        SELECT 1 FROM public.youtube_collection_job_leases
        WHERE (collection_job_kind = 'youtubejs_viewer' AND slot_state = 'ACTIVE')
           OR (collection_job_kind <> 'youtubejs_viewer' AND 'viewer_sample' = ANY (membership_kinds))
    ) THEN
        RAISE EXCEPTION 'viewer removal requires review of retained collection leases';
    END IF;
END
$migration$;

-- 퇴역 kind의 재개 위치만 제거합니다. 관측/재처리와 활성 lease가 없다는 위 검사를 통과한 뒤 실행합니다.
DELETE FROM public.source_collection_checkpoints WHERE observation_kind = 'viewer_sample';
DELETE FROM public.source_observation_consumer_offsets WHERE observation_kind = 'viewer_sample';
DELETE FROM public.youtube_collection_job_leases
WHERE collection_job_kind = 'youtubejs_viewer' AND slot_state <> 'ACTIVE';

-- 등록행은 실행 계약이지 관측 이력이 아닙니다. RESTRICT FK가 알 수 없는 참조의 삭제를 막습니다.
DELETE FROM public.observation_contract_generations WHERE observation_kind = 'viewer_sample';

DROP TABLE IF EXISTS public.youtube_live_viewer_sample_evidence;
DROP TABLE IF EXISTS public.youtube_live_viewer_sample_heads;
DROP TABLE IF EXISTS public.youtube_live_viewer_samples;
DROP TABLE IF EXISTS public.youtube_stream_stats;
DROP TABLE IF EXISTS public.youtube_channel_latest_stats;

ALTER TABLE public.observation_contract_generations
    DROP CONSTRAINT IF EXISTS chk_observation_contract_kind_vocab,
    ADD CONSTRAINT chk_observation_contract_kind_vocab CHECK (observation_kind IN (
        'community_page', 'video_list', 'shorts_list', 'live_snapshot',
        'channel_profile', 'channel_photo', 'schedule_snapshot', 'channel_live_check', 'video_live_check'
    )) NOT VALID;
ALTER TABLE public.youtube_collection_targets
    DROP CONSTRAINT IF EXISTS chk_youtube_collection_target_kind_vocab,
    ADD CONSTRAINT chk_youtube_collection_target_kind_vocab CHECK (observation_kind IN (
        'community_page', 'video_list', 'shorts_list', 'live_snapshot',
        'channel_profile', 'channel_photo', 'schedule_snapshot', 'channel_live_check', 'video_live_check'
    )) NOT VALID;
ALTER TABLE public.source_observation_consumer_offsets
    DROP CONSTRAINT IF EXISTS chk_source_observation_consumer_offset_kind_vocab,
    ADD CONSTRAINT chk_source_observation_consumer_offset_kind_vocab CHECK (observation_kind IN (
        'community_page', 'video_list', 'shorts_list', 'live_snapshot',
        'channel_profile', 'channel_photo', 'schedule_snapshot', 'channel_live_check', 'video_live_check'
    )) NOT VALID;
COMMIT;

ALTER TABLE public.observation_contract_generations VALIDATE CONSTRAINT chk_observation_contract_kind_vocab;
ALTER TABLE public.youtube_collection_targets VALIDATE CONSTRAINT chk_youtube_collection_target_kind_vocab;
ALTER TABLE public.source_observation_consumer_offsets VALIDATE CONSTRAINT chk_source_observation_consumer_offset_kind_vocab;
