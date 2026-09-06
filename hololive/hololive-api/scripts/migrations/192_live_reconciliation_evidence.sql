-- HD01: canonical 상태가 없는 종료와 재처리에 필요한 absence 사실도 consume commit에 보존한다.
-- raw observation retention은 이 domain 상태의 수명이 아니다. 현재 과거 effective_at 하한이
-- 없으므로 slot을 임의 TTL로 지우지 않고, pending은 reducer의 종결/무효화 전이에서 정리한다.
BEGIN;

CREATE TABLE IF NOT EXISTS youtube_live_pending_ends (
    video_id TEXT PRIMARY KEY,
    channel_id VARCHAR(64) NOT NULL,
    kind TEXT NOT NULL,
    observation_id BIGINT NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    scheduled_for TIMESTAMPTZ NOT NULL,
    ended_at TIMESTAMPTZ,
    negative_eligible BOOLEAN NOT NULL,
    scope_covers BOOLEAN NOT NULL,
    CONSTRAINT uq_youtube_live_pending_end_observation UNIQUE (video_id, observation_id),
    CONSTRAINT chk_youtube_live_pending_ends_kind_vocab
        CHECK (kind IN ('EXPLICIT_END', 'EXPLICIT_CANCEL', 'SCOPED_ABSENCE')),
    CONSTRAINT chk_youtube_live_pending_ends_video_id CHECK (length(video_id) BETWEEN 1 AND 128)
);

CREATE TABLE IF NOT EXISTS youtube_live_absence_slots (
    observation_id BIGINT PRIMARY KEY,
    scheduled_for TIMESTAMPTZ NOT NULL,
    evidence_sha256 TEXT NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    scope_sha256 TEXT NOT NULL,
    coverage JSONB NOT NULL,
    CONSTRAINT chk_youtube_live_absence_slots_coverage CHECK (
        jsonb_typeof(coverage) = 'object'
        AND jsonb_typeof(coverage -> 'requested_channel_ids') = 'array'
    )
);

ALTER TABLE youtube_live_reconciliation_heads
    ADD COLUMN IF NOT EXISTS first_absence_scheduled_for TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS second_absence_scheduled_for TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_absence_observation_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS ignored_absence_scheduled_for TIMESTAMPTZ[] NOT NULL DEFAULT '{}';

-- 기존 candidate의 원본은 기존 FK로 보호되어 있다. 저장되지 않았던 다른 과거 상태는
-- 추정하지 않으며, 아직 consume되지 않은 raw 관측도 미리 적용하지 않는다.
INSERT INTO youtube_live_pending_ends (
    video_id, channel_id, kind, observation_id, effective_at, received_at,
    scheduled_for, ended_at, negative_eligible, scope_covers
)
SELECT head.video_id, COALESCE(session.channel_id, fact.item ->> 'channel_id', ''),
       head.end_candidate_kind, observation.id, head.last_end_evidence_at,
       observation.received_at, observation.scheduled_for,
       (fact.item ->> 'ended_at')::TIMESTAMPTZ, true, true
FROM youtube_live_reconciliation_heads AS head
JOIN source_observations AS observation ON observation.id = head.end_candidate_observation_id
LEFT JOIN youtube_live_sessions AS session ON session.video_id = head.video_id
LEFT JOIN LATERAL (
    SELECT item FROM jsonb_array_elements(observation.payload -> 'sessions') AS item
    WHERE item ->> 'video_id' = head.video_id
) AS fact ON true
ON CONFLICT (video_id) DO NOTHING;

-- candidate의 완전한 사실은 domain projection이 소유한다. raw가 삭제된 뒤 늦은 positive가
-- 도착해도 같은 observation identity를 candidate로 사용할 수 있어야 한다.
ALTER TABLE youtube_live_reconciliation_heads
    DROP CONSTRAINT IF EXISTS youtube_live_reconciliation_h_end_candidate_observation_id_fkey;
DO $migration$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'youtube_live_reconciliation_heads'::regclass
          AND conname = 'fk_youtube_live_head_pending_end'
    ) THEN
        ALTER TABLE youtube_live_reconciliation_heads
            ADD CONSTRAINT fk_youtube_live_head_pending_end
            FOREIGN KEY (video_id, end_candidate_observation_id)
            REFERENCES youtube_live_pending_ends(video_id, observation_id)
            DEFERRABLE INITIALLY DEFERRED;
    END IF;

    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE ALL ON TABLE youtube_live_pending_ends, youtube_live_absence_slots FROM hololive_runtime;
        GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE youtube_live_pending_ends TO hololive_runtime;
        GRANT SELECT, INSERT ON TABLE youtube_live_absence_slots TO hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE ALL ON TABLE youtube_live_pending_ends, youtube_live_absence_slots FROM hololive_scraper;
    END IF;
END
$migration$;

COMMIT;
