-- 관리자에서 제출한 후보 인증은 worker 검증 성공 뒤에만 활성 인증을 교체한다.
CREATE TABLE IF NOT EXISTS x_space_session (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    revision BIGINT NOT NULL DEFAULT 0,
    active_revision BIGINT NOT NULL DEFAULT 0,
    active_ciphertext BYTEA,
    candidate_ciphertext BYTEA,
    state TEXT NOT NULL DEFAULT 'unconfigured',
    candidate_state TEXT NOT NULL DEFAULT 'idle',
    last_error TEXT NOT NULL DEFAULT '',
    candidate_error TEXT NOT NULL DEFAULT '',
    last_checked_at TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    next_check_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_x_space_session_state_vocab CHECK (state IN ('unconfigured','connected','auth_required','rate_limited','error')),
    CONSTRAINT chk_x_space_session_candidate_state_vocab CHECK (candidate_state IN ('idle','pending','accepted','rejected'))
);

-- 최초 관측 스냅샷은 제목/표시명 변경에도 발송 이벤트의 payload를 고정한다.
CREATE TABLE IF NOT EXISTS x_space_starts (
    space_id VARCHAR(64) PRIMARY KEY,
    payload JSONB NOT NULL,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
