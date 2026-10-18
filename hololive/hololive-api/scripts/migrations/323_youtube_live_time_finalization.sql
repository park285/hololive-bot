-- 종료된 방송의 시작·종료 시각을 YouTube 아카이브 시각(liveBroadcastDetails)으로 확정하기 위한 출처 열과 진행 상태 표.
-- times_source는 세션 시각의 출처다. 기존 행과 새 행은 observed로 시작하고, 영상 확인 consumer의 확정 UPDATE만
-- provider로 바꾼다. youtube_live_time_finalizations는 확정 재시도 계약의 기록이다.
-- 상수 기본값 열 추가는 표를 다시 쓰지 않지만 ACCESS EXCLUSIVE가 필요하다. live upsert가 초당 수 회라 잠금 대기를
-- 3초로 끊는다(291 선례). status_observed_at은 활성 세션의 반복 관측마다 바뀌므로 세션 표에 확정 대상 색인을
-- 두지 않는다(HOT 갱신 유지). reader는 약 1만 행의 세션 표를 순차로 읽는다.
BEGIN;
SET LOCAL lock_timeout = '3s';

ALTER TABLE public.youtube_live_sessions
    ADD COLUMN IF NOT EXISTS times_source text NOT NULL DEFAULT 'observed';

DO $constraint$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_catalog.pg_constraint
        WHERE conrelid = 'public.youtube_live_sessions'::regclass
          AND conname = 'chk_youtube_live_sessions_times_source_vocab'
    ) THEN
        ALTER TABLE public.youtube_live_sessions
            ADD CONSTRAINT chk_youtube_live_sessions_times_source_vocab
            CHECK (times_source IN ('observed', 'provider')) NOT VALID;
    END IF;
END
$constraint$;

-- queue: new는 종료를 관측한 신규 세션, backlog는 운영 투입으로 들어온 기존 기록이다.
-- outcome: pending만 대기열 대상이다. finalized·exhausted·conflict·members_only·reverted는 최종 상태다.
-- attempts는 응답 수준 UNKNOWN 시도 수(최대 4), deferrals는 request_failed 연기 수(최대 12)다.
-- retry_base_at은 재시도 간격(3분·30분·3시간·24시간)의 기준 시각이다. 신규는 종료 관측 시각, backlog는 첫 시도 시각이다.
-- next_attempt_at이 없는 pending은 아직 시도하지 않은 backlog뿐이며, reader가 묶음 안 순서로 시각을 정한다.
-- observation_id는 마지막으로 반영한 영상 확인이다. 원본 보존 삭제를 막지 않도록 ON DELETE SET NULL로 둔다(218 선례).
-- observed_started_at·observed_ended_at은 확정 전 세션 시각 사본이며 감사와 되돌리기에 쓴다.
CREATE TABLE IF NOT EXISTS public.youtube_live_time_finalizations (
    video_id varchar(20) PRIMARY KEY
        REFERENCES public.youtube_live_sessions (video_id) ON DELETE CASCADE,
    queue text NOT NULL,
    outcome text NOT NULL,
    reason text,
    attempts smallint NOT NULL DEFAULT 0,
    deferrals smallint NOT NULL DEFAULT 0,
    retry_base_at timestamptz,
    next_attempt_at timestamptz,
    last_attempt_at timestamptz,
    observation_id bigint
        REFERENCES public.source_observations (id) ON DELETE SET NULL,
    observed_started_at timestamptz,
    observed_ended_at timestamptz,
    enqueued_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_youtube_live_time_finalizations_observation UNIQUE (observation_id),
    CONSTRAINT chk_youtube_live_time_finalizations_queue_vocab
        CHECK (queue IN ('new', 'backlog')),
    CONSTRAINT chk_youtube_live_time_finalizations_outcome_vocab
        CHECK (outcome IN ('pending', 'finalized', 'exhausted', 'conflict', 'members_only', 'reverted')),
    CONSTRAINT chk_youtube_live_time_finalizations_counts
        CHECK (attempts BETWEEN 0 AND 4 AND deferrals BETWEEN 0 AND 12),
    CONSTRAINT chk_youtube_live_time_finalizations_reason
        CHECK (reason IS NULL OR length(reason) BETWEEN 1 AND 64),
    CONSTRAINT chk_youtube_live_time_finalizations_state CHECK (
        CASE outcome
            WHEN 'pending' THEN completed_at IS NULL
                AND (next_attempt_at IS NOT NULL OR (queue = 'backlog' AND attempts = 0 AND deferrals = 0))
            WHEN 'finalized' THEN completed_at IS NOT NULL AND next_attempt_at IS NULL AND reason IS NULL
            WHEN 'members_only' THEN completed_at IS NOT NULL AND next_attempt_at IS NULL
            WHEN 'reverted' THEN completed_at IS NOT NULL AND next_attempt_at IS NULL
            ELSE completed_at IS NOT NULL AND next_attempt_at IS NULL AND reason IS NOT NULL
        END
    )
);

REVOKE ALL ON TABLE public.youtube_live_time_finalizations FROM PUBLIC;

-- 확정 진행은 YouTube plane consumer만 쓴다. 행 삭제는 세션 삭제의 CASCADE뿐이다. collector에는 권한을 주지 않는다.
DO $roles$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE ALL ON TABLE public.youtube_live_time_finalizations FROM hololive_runtime;
        GRANT SELECT, INSERT, UPDATE ON TABLE public.youtube_live_time_finalizations TO hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE ALL ON TABLE public.youtube_live_time_finalizations FROM hololive_scraper;
    END IF;
END
$roles$;

COMMIT;

-- 블록 밖 autocommit 문장이라 검증 스캔은 쓰기를 막지 않는 잠금으로 진행한다. 이미 검증된 제약에는 no-op이다.
ALTER TABLE public.youtube_live_sessions
    VALIDATE CONSTRAINT chk_youtube_live_sessions_times_source_vocab;
