-- 로그인은 외부 세션 생성 부작용이 있어, 프로세스 종료 후에도 시도와 결과 불명을 보존합니다.
CREATE TABLE IF NOT EXISTS x_space_login_attempts (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    configuration_revision BIGINT NOT NULL CHECK (configuration_revision > 0),
    session_revision BIGINT NOT NULL CHECK (session_revision >= 0),
    submitted_revision BIGINT,
    status TEXT NOT NULL,
    error_code TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    CONSTRAINT chk_x_space_login_attempts_status_vocab CHECK (
        status IN ('running', 'submitted', 'connected', 'login_required', 'outcome_unknown', 'manual_override')
    ),
    CONSTRAINT chk_x_space_login_attempts_error_code_vocab CHECK (
        error_code IN ('', 'additional_authentication', 'login_rejected', 'browser_failed',
                       'outcome_unknown', 'interrupted', 'candidate_rejected', 'manual_override')
    ),
    CONSTRAINT uq_x_space_login_attempts_generation UNIQUE (configuration_revision, session_revision)
);
