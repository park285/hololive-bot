-- DEC-20260926-hololive-live-absence-evidence: youtubejs의 채널 /live 확인(channel_live_check)과
-- 영상 player 확인(video_live_check)을 별도 observation kind로 받고, 채널·영상별 최신 판정을
-- canonical 테이블에 보존한다. 두 kind는 absence 권한이 없으므로 live reducer 상태는 만들지 않는다.
--
-- kind 허용 목록은 180과 같은 멱등 교체로 넓힌다. 새 CHECK를 NOT VALID로 붙이고(순간 락),
-- 별도 문장에서 VALIDATE한 뒤(쓰기 허용 락), 한 문장 안에서 기존 CHECK를 지우고 이름을 넘긴다.
-- 파일 중간 실패 뒤 재실행해도 남은 단계만 이어서 수행한다.
DO $migration$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.observation_contract_generations'::regclass
          AND conname = 'chk_observation_contract_kind_vocab_next'
    ) AND NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.observation_contract_generations'::regclass
          AND conname = 'chk_observation_contract_kind_vocab'
          AND pg_get_constraintdef(oid) LIKE '%''channel_live_check''%'
          AND pg_get_constraintdef(oid) LIKE '%''video_live_check''%'
    ) THEN
        ALTER TABLE public.observation_contract_generations
            ADD CONSTRAINT chk_observation_contract_kind_vocab_next CHECK (
                observation_kind IN (
                    'community_page',
                    'video_list',
                    'shorts_list',
                    'live_snapshot',
                    'viewer_sample',
                    'channel_stats',
                    'channel_profile',
                    'channel_photo',
                    'schedule_snapshot',
                    'channel_live_check',
                    'video_live_check'
                )
            ) NOT VALID;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.youtube_collection_targets'::regclass
          AND conname = 'chk_youtube_collection_target_kind_vocab_next'
    ) AND NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.youtube_collection_targets'::regclass
          AND conname = 'chk_youtube_collection_target_kind_vocab'
          AND pg_get_constraintdef(oid) LIKE '%''channel_live_check''%'
          AND pg_get_constraintdef(oid) LIKE '%''video_live_check''%'
    ) THEN
        ALTER TABLE public.youtube_collection_targets
            ADD CONSTRAINT chk_youtube_collection_target_kind_vocab_next CHECK (
                observation_kind IN (
                    'community_page', 'video_list', 'shorts_list', 'live_snapshot',
                    'viewer_sample', 'channel_stats', 'channel_profile', 'channel_photo',
                    'schedule_snapshot', 'channel_live_check', 'video_live_check'
                )
            ) NOT VALID;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.source_observation_consumer_offsets'::regclass
          AND conname = 'chk_source_observation_consumer_offset_kind_vocab_next'
    ) AND NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.source_observation_consumer_offsets'::regclass
          AND conname = 'chk_source_observation_consumer_offset_kind_vocab'
          AND pg_get_constraintdef(oid) LIKE '%''channel_live_check''%'
          AND pg_get_constraintdef(oid) LIKE '%''video_live_check''%'
    ) THEN
        ALTER TABLE public.source_observation_consumer_offsets
            ADD CONSTRAINT chk_source_observation_consumer_offset_kind_vocab_next CHECK (
                observation_kind IN (
                    'community_page', 'video_list', 'shorts_list', 'live_snapshot',
                    'viewer_sample', 'channel_stats', 'channel_profile', 'channel_photo',
                    'schedule_snapshot', 'channel_live_check', 'video_live_check'
                )
            ) NOT VALID;
    END IF;
END
$migration$;

DO $migration$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.observation_contract_generations'::regclass
          AND conname = 'chk_observation_contract_kind_vocab_next'
          AND NOT convalidated
    ) THEN
        ALTER TABLE public.observation_contract_generations
            VALIDATE CONSTRAINT chk_observation_contract_kind_vocab_next;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.youtube_collection_targets'::regclass
          AND conname = 'chk_youtube_collection_target_kind_vocab_next'
          AND NOT convalidated
    ) THEN
        ALTER TABLE public.youtube_collection_targets
            VALIDATE CONSTRAINT chk_youtube_collection_target_kind_vocab_next;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.source_observation_consumer_offsets'::regclass
          AND conname = 'chk_source_observation_consumer_offset_kind_vocab_next'
          AND NOT convalidated
    ) THEN
        ALTER TABLE public.source_observation_consumer_offsets
            VALIDATE CONSTRAINT chk_source_observation_consumer_offset_kind_vocab_next;
    END IF;
END
$migration$;

DO $migration$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.observation_contract_generations'::regclass
          AND conname = 'chk_observation_contract_kind_vocab_next'
    ) THEN
        ALTER TABLE public.observation_contract_generations
            DROP CONSTRAINT IF EXISTS chk_observation_contract_kind_vocab;
        ALTER TABLE public.observation_contract_generations
            RENAME CONSTRAINT chk_observation_contract_kind_vocab_next
            TO chk_observation_contract_kind_vocab;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.youtube_collection_targets'::regclass
          AND conname = 'chk_youtube_collection_target_kind_vocab_next'
    ) THEN
        ALTER TABLE public.youtube_collection_targets
            DROP CONSTRAINT IF EXISTS chk_youtube_collection_target_kind_vocab;
        ALTER TABLE public.youtube_collection_targets
            RENAME CONSTRAINT chk_youtube_collection_target_kind_vocab_next
            TO chk_youtube_collection_target_kind_vocab;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.source_observation_consumer_offsets'::regclass
          AND conname = 'chk_source_observation_consumer_offset_kind_vocab_next'
    ) THEN
        ALTER TABLE public.source_observation_consumer_offsets
            DROP CONSTRAINT IF EXISTS chk_source_observation_consumer_offset_kind_vocab;
        ALTER TABLE public.source_observation_consumer_offsets
            RENAME CONSTRAINT chk_source_observation_consumer_offset_kind_vocab_next
            TO chk_source_observation_consumer_offset_kind_vocab;
    END IF;
END
$migration$;

BEGIN;

-- 이미 운영 중 세대가 올라간 행은 되돌리지 않는다. 새 kind의 첫 계약은 schema 1 / generation 1이다.
INSERT INTO observation_contract_generations (
    provider,
    observation_kind,
    current_schema_version,
    current_generation,
    updated_by
)
VALUES
    ('youtubejs', 'channel_live_check', 1, 1, 'migration-218'),
    ('youtubejs', 'video_live_check', 1, 1, 'migration-218')
ON CONFLICT (provider, observation_kind) DO NOTHING;

-- 채널별 최신 /live 판정. 음성은 CHANNEL_PAGE·UPCOMING_VIDEO뿐이며 LiveQuery coverage만 읽는다.
-- 원시 evidence가 retention으로 지워져도 hash와 네 시각은 남아 신선도 판정을 계속 증명한다.
CREATE TABLE IF NOT EXISTS youtube_channel_live_checks (
    channel_id VARCHAR(64) PRIMARY KEY,
    provider TEXT NOT NULL,
    outcome TEXT NOT NULL,
    selected_video_id VARCHAR(20),
    channel_identity_confirmed BOOLEAN NOT NULL,
    unknown_reason TEXT,
    observation_id BIGINT,
    evidence_sha256 TEXT NOT NULL,
    scheduled_for TIMESTAMPTZ NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- 한 관측의 subject는 채널 하나다. UNIQUE 인덱스가 evidence 삭제 시 SET NULL 탐색도 맡는다.
    CONSTRAINT uq_youtube_channel_live_checks_observation UNIQUE (observation_id),
    CONSTRAINT fk_youtube_channel_live_checks_observation
        FOREIGN KEY (observation_id)
        REFERENCES source_observations(id)
        ON DELETE SET NULL,
    CONSTRAINT chk_youtube_channel_live_checks_identity CHECK (
        length(channel_id) BETWEEN 1 AND 64
        AND (selected_video_id IS NULL OR length(selected_video_id) BETWEEN 1 AND 20)
    ),
    CONSTRAINT chk_youtube_channel_live_checks_provider CHECK (
        provider = 'youtubejs'
    ),
    CONSTRAINT chk_youtube_channel_live_checks_outcome_vocab CHECK (
        outcome IN ('LIVE_VIDEO', 'UPCOMING_VIDEO', 'CHANNEL_PAGE', 'UNKNOWN')
    ),
    CONSTRAINT chk_youtube_channel_live_checks_unknown_reason_vocab CHECK (
        unknown_reason IS NULL
        OR unknown_reason IN (
            'identity_missing',
            'identity_mismatch',
            'contradictory_fields',
            'structure_unrecognized',
            'not_waiting_state',
            'login_required_unclassified',
            'error_unclassified',
            'request_failed'
        )
    ),
    -- 알려진 결과는 요청 채널 identity가 확인된 경우에만 성립하고 UNKNOWN만 사유를 가진다.
    -- 채널 페이지는 영상을 고르지 않고, LIVE·예정 판정은 선택 영상이 있어야 한다.
    CONSTRAINT chk_youtube_channel_live_checks_outcome_shape CHECK (
        (outcome = 'UNKNOWN') = (unknown_reason IS NOT NULL)
        AND (outcome = 'UNKNOWN' OR channel_identity_confirmed)
        AND (outcome <> 'CHANNEL_PAGE' OR selected_video_id IS NULL)
        AND (outcome NOT IN ('LIVE_VIDEO', 'UPCOMING_VIDEO') OR selected_video_id IS NOT NULL)
        AND (
            unknown_reason IS NULL
            OR unknown_reason NOT IN ('identity_missing', 'identity_mismatch')
            OR NOT channel_identity_confirmed
        )
    ),
    CONSTRAINT chk_youtube_channel_live_checks_hash CHECK (
        evidence_sha256 ~ '^[0-9a-f]{64}$'
    ),
    -- 새 kind는 source_event_at을 받지 않으므로 EffectiveAt은 scheduled_for와 같다.
    CONSTRAINT chk_youtube_channel_live_checks_effective_clock CHECK (
        effective_at = scheduled_for
    )
);

-- 영상별 최신 가용성 판정. channel_id는 응답 값이 아니라 canonical session의 채널이다.
-- 가용성은 수명 상태가 아니며, 종료 반영은 기존 live reducer 경로만 소유한다.
CREATE TABLE IF NOT EXISTS youtube_video_availability (
    video_id VARCHAR(20) PRIMARY KEY,
    channel_id VARCHAR(64) NOT NULL,
    provider TEXT NOT NULL,
    identity_confirmed BOOLEAN NOT NULL,
    availability TEXT NOT NULL,
    method TEXT NOT NULL,
    unknown_reason TEXT,
    observation_id BIGINT,
    evidence_sha256 TEXT NOT NULL,
    scheduled_for TIMESTAMPTZ NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- 한 관측의 subject는 영상 하나다. UNIQUE 인덱스가 evidence 삭제 시 SET NULL 탐색도 맡는다.
    CONSTRAINT uq_youtube_video_availability_observation UNIQUE (observation_id),
    CONSTRAINT fk_youtube_video_availability_observation
        FOREIGN KEY (observation_id)
        REFERENCES source_observations(id)
        ON DELETE SET NULL,
    CONSTRAINT chk_youtube_video_availability_identity CHECK (
        length(video_id) BETWEEN 1 AND 20
        AND length(channel_id) BETWEEN 1 AND 64
    ),
    CONSTRAINT chk_youtube_video_availability_provider CHECK (
        provider = 'youtubejs'
    ),
    CONSTRAINT chk_youtube_video_availability_availability_vocab CHECK (
        availability IN ('PUBLIC', 'MEMBERS_ONLY', 'PUBLIC_UNAVAILABLE', 'UNKNOWN')
    ),
    CONSTRAINT chk_youtube_video_availability_method_vocab CHECK (
        method IN ('player_public', 'player_members_only', 'player_private', 'unknown')
    ),
    CONSTRAINT chk_youtube_video_availability_unknown_reason_vocab CHECK (
        unknown_reason IS NULL
        OR unknown_reason IN (
            'identity_missing',
            'identity_mismatch',
            'contradictory_fields',
            'structure_unrecognized',
            'not_waiting_state',
            'login_required_unclassified',
            'error_unclassified',
            'request_failed',
            'availability_unclassified'
        )
    ),
    -- 판정 방법은 가용성 값과 1:1이다. 알려진 가용성은 정확한 identity에서만 성립하고,
    -- availability_unclassified는 identity와 수명 사실이 확인된 채 가용성 필드만 없는 UNKNOWN이다.
    CONSTRAINT chk_youtube_video_availability_shape CHECK (
        (
            (availability = 'PUBLIC' AND method = 'player_public')
            OR (availability = 'MEMBERS_ONLY' AND method = 'player_members_only')
            OR (availability = 'PUBLIC_UNAVAILABLE' AND method = 'player_private')
            OR (availability = 'UNKNOWN' AND method = 'unknown')
        )
        AND (availability = 'UNKNOWN') = (unknown_reason IS NOT NULL)
        AND (availability = 'UNKNOWN' OR identity_confirmed)
        AND (unknown_reason IS DISTINCT FROM 'availability_unclassified' OR identity_confirmed)
        AND (
            unknown_reason IS NULL
            OR unknown_reason NOT IN ('identity_missing', 'identity_mismatch')
            OR NOT identity_confirmed
        )
    ),
    CONSTRAINT chk_youtube_video_availability_hash CHECK (
        evidence_sha256 ~ '^[0-9a-f]{64}$'
    ),
    -- 새 kind는 source_event_at을 받지 않으므로 EffectiveAt은 scheduled_for와 같다.
    CONSTRAINT chk_youtube_video_availability_effective_clock CHECK (
        effective_at = scheduled_for
    )
);

REVOKE ALL ON TABLE youtube_channel_live_checks FROM PUBLIC;
REVOKE ALL ON TABLE youtube_video_availability FROM PUBLIC;

-- consumer는 최신값을 upsert만 하며 행을 지우지 않는다. collector에는 canonical 권한을 주지 않는다.
DO $migration$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE ALL ON TABLE youtube_channel_live_checks, youtube_video_availability FROM hololive_runtime;
        GRANT SELECT, INSERT, UPDATE ON TABLE youtube_channel_live_checks TO hololive_runtime;
        GRANT SELECT, INSERT, UPDATE ON TABLE youtube_video_availability TO hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE ALL ON TABLE youtube_channel_live_checks, youtube_video_availability FROM hololive_scraper;
    END IF;
END
$migration$;

COMMIT;
