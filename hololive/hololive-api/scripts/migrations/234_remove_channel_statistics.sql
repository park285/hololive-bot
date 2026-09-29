-- 채널 통계는 비활성 상태로 보존하지 않습니다. 통계 publisher를 중지하고 consumer의
-- 진행 중 작업을 배수한 뒤 적용합니다. profile/photo와 공유하는 metadata lease는 보존합니다.
-- 기존 189처럼 CALL의 각 배치를 commit하여 중단 뒤 재실행할 수 있으며, 임시 절차는 제거합니다.
CREATE OR REPLACE PROCEDURE public.remove_channel_statistics_v234()
LANGUAGE plpgsql
AS $procedure$
DECLARE
    target_table text;
    affected_rows integer;
BEGIN
    PERFORM set_config('lock_timeout', '3s', true);
    IF EXISTS (
        SELECT 1 FROM public.youtube_collection_job_leases
        WHERE collection_job_kind IN ('youtubejs_channel_metadata', 'holodex_metadata')
          AND slot_state = 'ACTIVE'
    ) OR EXISTS (
        SELECT 1
        FROM public.source_observation_queue AS queue
        JOIN public.source_observations AS observation ON observation.id = queue.observation_id
        WHERE observation.observation_kind = 'channel_stats' AND queue.status = 'PROCESSING'
    ) THEN
        RAISE EXCEPTION 'channel statistics removal requires drained metadata publishers and statistics consumers';
    END IF;

	IF EXISTS (
		SELECT 1 FROM public.youtube_notification_outbox
		WHERE kind = 'MILESTONE' AND locked_at IS NOT NULL
	) OR EXISTS (
		SELECT 1 FROM public.youtube_notification_delivery AS delivery
		JOIN public.youtube_notification_outbox AS outbox ON outbox.id = delivery.outbox_id
		WHERE outbox.kind = 'MILESTONE' AND delivery.status IN ('SENDING', 'QUARANTINED')
	) OR EXISTS (
		SELECT 1 FROM public.youtube_notification_delivery_ledger
		WHERE kind = 'MILESTONE' AND status = 'QUARANTINED'
	) THEN
		RAISE EXCEPTION 'channel statistics removal requires review of unresolved milestone deliveries';
	END IF;

    -- 2026-09-29 운영 조회에서 두 경로 모두 0건입니다. 별도 영구 closeout 영수증을
    -- 가진 dispatch 이력이 나타나면 일반 원장 삭제로 범위를 넓히지 않고 검토를 요구합니다.
    IF EXISTS (SELECT 1 FROM public.alarm_dispatch_events
               WHERE payload #>> '{youtube_outbox,kind}' = 'MILESTONE')
       OR EXISTS (SELECT 1 FROM public.alarm_dispatch_event_collisions
                  WHERE payload #>> '{youtube_outbox,kind}' = 'MILESTONE') THEN
        RAISE EXCEPTION 'channel statistics removal requires review of durable milestone dispatch history';
    END IF;

    -- 구 current hash를 수정된 target 집합에 재사용하지 않습니다. 새 API refresh가 남길
    -- profile/photo 등으로 새 generation을 활성화하기 전에는 오래된 projection을 사용할 수 없습니다.
    UPDATE public.youtube_collection_projection_generations AS generation
    SET valid_until = LEAST(generation.valid_until, statement_timestamp())
    WHERE generation.status = 'CURRENT'
      AND EXISTS (
          SELECT 1 FROM public.youtube_collection_targets AS target
          WHERE target.projection_generation = generation.generation
            AND target.observation_kind = 'channel_stats'
      );
    COMMIT;

    -- 통계 외의 공유 테이블 행은 손대지 않으며 큰 FK cascade보다 자식을 먼저 지웁니다.
    FOREACH target_table IN ARRAY ARRAY[
        'youtube_collection_target_reasons',
        'youtube_collection_targets',
        'source_observation_applications',
        'source_observation_replay_requests',
        'source_observation_collisions',
        'source_reconciliation_conflicts',
        'source_collection_checkpoints',
        'source_observation_consumer_offsets'
    ] LOOP
        LOOP
            PERFORM set_config('lock_timeout', '3s', true);
            EXECUTE format(
                'WITH picked AS (SELECT ctid FROM public.%1$I '
                'WHERE observation_kind = $1 LIMIT 1000 FOR UPDATE) '
                'DELETE FROM public.%1$I AS target USING picked WHERE target.ctid = picked.ctid',
                target_table
            ) USING 'channel_stats';
            GET DIAGNOSTICS affected_rows = ROW_COUNT;
            COMMIT;
            EXIT WHEN affected_rows = 0;
        END LOOP;
    END LOOP;

    LOOP
        PERFORM set_config('lock_timeout', '3s', true);
        -- 각 observation에는 queue가 최대 한 행이며 통계 application/replay는 위에서 제거했습니다.
        WITH picked AS (
            SELECT id FROM public.source_observations
            WHERE observation_kind = 'channel_stats'
            ORDER BY id LIMIT 1000 FOR UPDATE
        )
        DELETE FROM public.source_observations AS observation
        USING picked WHERE observation.id = picked.id;
        GET DIAGNOSTICS affected_rows = ROW_COUNT;
        COMMIT;
        EXIT WHEN affected_rows = 0;
    END LOOP;

    -- 전용 통계 알림의 자식부터 제한된 배치로 제거하고 다른 알림 원장은 보존합니다.
    FOREACH target_table IN ARRAY ARRAY[
        'youtube_notification_delivery_telemetry', 'youtube_notification_delivery'
    ] LOOP
        LOOP
            PERFORM set_config('lock_timeout', '3s', true);
            EXECUTE format(
                'WITH picked AS (SELECT child.ctid FROM public.%1$I AS child '
                'JOIN public.youtube_notification_outbox AS outbox ON outbox.id = child.outbox_id '
                'WHERE outbox.kind = $1 LIMIT 1000 FOR UPDATE OF child) '
                'DELETE FROM public.%1$I AS target USING picked WHERE target.ctid = picked.ctid',
                target_table
            ) USING 'MILESTONE';
            GET DIAGNOSTICS affected_rows = ROW_COUNT;
            COMMIT;
            EXIT WHEN affected_rows = 0;
        END LOOP;
    END LOOP;
    FOREACH target_table IN ARRAY ARRAY[
        'youtube_notification_outbox', 'youtube_notification_delivery_ledger',
        'youtube_community_shorts_alarm_states', 'youtube_community_shorts_source_posts',
        'youtube_content_alarm_tracking'
    ] LOOP
        LOOP
            PERFORM set_config('lock_timeout', '3s', true);
            EXECUTE format(
                'WITH picked AS (SELECT ctid FROM public.%1$I '
                'WHERE kind = $1 LIMIT 1000 FOR UPDATE) '
                'DELETE FROM public.%1$I AS target USING picked WHERE target.ctid = picked.ctid',
                target_table
            ) USING 'MILESTONE';
            GET DIAGNOSTICS affected_rows = ROW_COUNT;
            COMMIT;
            EXIT WHEN affected_rows = 0;
        END LOOP;
    END LOOP;

    PERFORM set_config('lock_timeout', '3s', true);
    DELETE FROM public.observation_contract_generations WHERE observation_kind = 'channel_stats';
    DELETE FROM public.notification_templates WHERE template_key IN (
        'CMD_STATS_COUNT', 'CMD_STATS_GAINERS', 'CMD_MILESTONE_ACHIEVED',
        'CMD_MILESTONE_APPROACHING', 'OUTBOX_MILESTONE'
    );
    DELETE FROM public.message_strings WHERE namespace = 'error' AND key IN (
        'unknown_stats_period', 'stats_query_failed', 'no_stats_data',
        'subscriber_need_member_name', 'subscriber_query_failed', 'no_subscriber_data'
    );
    UPDATE public.notification_templates
    SET body = replace(body, E'{{mdescape .Prefix}}구독자 [멤버명] - 구독자 수\n', ''), updated_at = now()
    WHERE template_key = 'CMD_HELP'
      AND strpos(body, E'{{mdescape .Prefix}}구독자 [멤버명] - 구독자 수\n') > 0;
    COMMIT;
END
$procedure$;
REVOKE ALL ON PROCEDURE public.remove_channel_statistics_v234() FROM PUBLIC;
CALL public.remove_channel_statistics_v234();
DROP PROCEDURE public.remove_channel_statistics_v234();

BEGIN;
SET LOCAL lock_timeout = '3s';

DROP TABLE IF EXISTS public.youtube_channel_stats_evidence;
DROP TABLE IF EXISTS public.youtube_channel_stats_heads;
DROP TABLE IF EXISTS public.youtube_channel_stats_snapshots;
DROP TABLE IF EXISTS public.youtube_stats_changes;
DROP TABLE IF EXISTS public.youtube_stats_history;
DROP TABLE IF EXISTS public.youtube_milestones;
DROP TABLE IF EXISTS public.youtube_milestone_approaching;

-- 남긴 kind의 실제 어휘만 표현합니다. 별도 retired-name 가드나 호환 codec은 두지 않습니다.
ALTER TABLE public.observation_contract_generations
    DROP CONSTRAINT IF EXISTS chk_observation_contract_kind_vocab,
    ADD CONSTRAINT chk_observation_contract_kind_vocab CHECK (observation_kind IN (
        'community_page', 'video_list', 'shorts_list', 'live_snapshot', 'viewer_sample',
        'channel_profile', 'channel_photo', 'schedule_snapshot', 'channel_live_check', 'video_live_check'
    )) NOT VALID;
ALTER TABLE public.youtube_collection_targets
    DROP CONSTRAINT IF EXISTS chk_youtube_collection_target_kind_vocab,
    ADD CONSTRAINT chk_youtube_collection_target_kind_vocab CHECK (observation_kind IN (
        'community_page', 'video_list', 'shorts_list', 'live_snapshot', 'viewer_sample',
        'channel_profile', 'channel_photo', 'schedule_snapshot', 'channel_live_check', 'video_live_check'
    )) NOT VALID;
ALTER TABLE public.source_observation_consumer_offsets
    DROP CONSTRAINT IF EXISTS chk_source_observation_consumer_offset_kind_vocab,
    ADD CONSTRAINT chk_source_observation_consumer_offset_kind_vocab CHECK (observation_kind IN (
        'community_page', 'video_list', 'shorts_list', 'live_snapshot', 'viewer_sample',
        'channel_profile', 'channel_photo', 'schedule_snapshot', 'channel_live_check', 'video_live_check'
    )) NOT VALID;
ALTER TABLE public.youtube_notification_outbox
    DROP CONSTRAINT IF EXISTS chk_youtube_notification_outbox_kind_vocab,
    ADD CONSTRAINT chk_youtube_notification_outbox_kind_vocab
        CHECK (kind IN ('NEW_VIDEO', 'NEW_SHORT', 'LIVE_STREAM', 'COMMUNITY_POST')) NOT VALID;
ALTER TABLE public.youtube_notification_delivery_ledger
    DROP CONSTRAINT IF EXISTS chk_youtube_notification_delivery_ledger_kind_vocab,
    ADD CONSTRAINT chk_youtube_notification_delivery_ledger_kind_vocab
        CHECK (kind IN ('NEW_VIDEO', 'NEW_SHORT', 'LIVE_STREAM', 'COMMUNITY_POST')) NOT VALID;
ALTER TABLE public.youtube_community_shorts_alarm_states
    DROP CONSTRAINT IF EXISTS chk_youtube_community_shorts_alarm_states_kind_vocab,
    ADD CONSTRAINT chk_youtube_community_shorts_alarm_states_kind_vocab
        CHECK (kind IN ('NEW_VIDEO', 'NEW_SHORT', 'LIVE_STREAM', 'COMMUNITY_POST')) NOT VALID;
ALTER TABLE public.youtube_community_shorts_source_posts
    DROP CONSTRAINT IF EXISTS chk_youtube_community_shorts_source_posts_kind_vocab,
    ADD CONSTRAINT chk_youtube_community_shorts_source_posts_kind_vocab
        CHECK (kind IN ('NEW_VIDEO', 'NEW_SHORT', 'LIVE_STREAM', 'COMMUNITY_POST')) NOT VALID;
ALTER TABLE public.youtube_content_alarm_tracking
    DROP CONSTRAINT IF EXISTS chk_youtube_content_alarm_tracking_kind_vocab,
    ADD CONSTRAINT chk_youtube_content_alarm_tracking_kind_vocab
        CHECK (kind IN ('NEW_VIDEO', 'NEW_SHORT', 'LIVE_STREAM', 'COMMUNITY_POST')) NOT VALID;
COMMIT;

ALTER TABLE public.observation_contract_generations VALIDATE CONSTRAINT chk_observation_contract_kind_vocab;
ALTER TABLE public.youtube_collection_targets VALIDATE CONSTRAINT chk_youtube_collection_target_kind_vocab;
ALTER TABLE public.source_observation_consumer_offsets VALIDATE CONSTRAINT chk_source_observation_consumer_offset_kind_vocab;
ALTER TABLE public.youtube_notification_outbox VALIDATE CONSTRAINT chk_youtube_notification_outbox_kind_vocab;
ALTER TABLE public.youtube_notification_delivery_ledger VALIDATE CONSTRAINT chk_youtube_notification_delivery_ledger_kind_vocab;
ALTER TABLE public.youtube_community_shorts_alarm_states VALIDATE CONSTRAINT chk_youtube_community_shorts_alarm_states_kind_vocab;
ALTER TABLE public.youtube_community_shorts_source_posts VALIDATE CONSTRAINT chk_youtube_community_shorts_source_posts_kind_vocab;
ALTER TABLE public.youtube_content_alarm_tracking VALIDATE CONSTRAINT chk_youtube_content_alarm_tracking_kind_vocab;
