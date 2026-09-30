-- 선정된 방별 후보와 평가 checkpoint를 동일 commit으로 보존한다.
CREATE TABLE IF NOT EXISTS alarm_upcoming_candidates (
    dedupe_key TEXT PRIMARY KEY,
    event_key TEXT NOT NULL,
    payload_hash TEXT NOT NULL,
    channel_id VARCHAR(64) NOT NULL,
    stream_id TEXT NOT NULL,
    room_id VARCHAR(100) NOT NULL,
    scheduled_at TIMESTAMPTZ NOT NULL,
    notification JSONB NOT NULL,
    selected_at TIMESTAMPTZ NOT NULL,
    checked_at TIMESTAMPTZ NOT NULL,
    terminal_at TIMESTAMPTZ,
    outcome TEXT NOT NULL DEFAULT 'pending',
    CONSTRAINT chk_alarm_upcoming_candidates_outcome_vocab CHECK (
        outcome IN ('pending', 'accepted', 'rejected_collision', 'rejected_terminal',
                    'expired', 'schedule_changed', 'stream_ended', 'subscription_removed')
    ),
    CONSTRAINT chk_alarm_upcoming_candidates_terminal CHECK ((outcome = 'pending') = (terminal_at IS NULL))
);
CREATE TABLE IF NOT EXISTS alarm_upcoming_checkpoints (
    channel_id VARCHAR(64) PRIMARY KEY,
    evaluated_at TIMESTAMPTZ NOT NULL
);
