-- bot plane durable admission ledger 최초 도입(테이블만; 런타임 활성화는 후속 작업 소유).
-- state shape CHECK는 118 선례대로 claim_token/lease_until에 대해서는 단방향(status ⇒ 컬럼 존재)만
-- 강제한다 — 역방향까지 걸면 수동 회수가 lease 이력을 보존한 채 상태를 되돌리는 흐름이 막힌다.
-- 반면 terminal_reason은 사후 조사 입력이라 terminal_at과 함께 비어 있으면 안 된다.
-- room_id는 표준 식별자 폭(100)이 아니라 Iris webhook SDK의 Room 계약(256 rune)을 따른다.
-- ingress가 chat_id 부재 시 방 제목을 room 식별자로 승격시키므로 100자 가정이면 admission이 탈락한다.

CREATE TABLE IF NOT EXISTS bot_webhook_inbox (
    id BIGSERIAL PRIMARY KEY,
    message_id TEXT NOT NULL,
    room_id TEXT NOT NULL,
    ordering_key TEXT NOT NULL,
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    claim_token TEXT,
    lease_until TIMESTAMPTZ,
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    terminal_reason TEXT NOT NULL DEFAULT '',
    terminal_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_bot_webhook_inbox_status_vocab CHECK (
        status IN ('pending', 'processing', 'retry', 'dead', 'succeeded')
    ),
    CONSTRAINT chk_bot_webhook_inbox_message_id_size
        CHECK (length(message_id) > 0 AND length(message_id) <= 512),
    CONSTRAINT chk_bot_webhook_inbox_room_id_size
        CHECK (length(room_id) > 0 AND length(room_id) <= 256),
    CONSTRAINT chk_bot_webhook_inbox_ordering_key_size
        CHECK (length(ordering_key) > 0 AND length(ordering_key) <= 512),
    CONSTRAINT chk_bot_webhook_inbox_attempts CHECK (attempts >= 0),
    CONSTRAINT chk_bot_webhook_inbox_terminal_reason_size
        CHECK (length(terminal_reason) <= 512),
    CONSTRAINT chk_bot_webhook_inbox_last_error_size
        CHECK (octet_length(last_error) <= 8192),
    CONSTRAINT chk_bot_webhook_inbox_state_shape CHECK (
        (
            status <> 'processing'
            OR (claim_token IS NOT NULL AND lease_until IS NOT NULL)
        )
        AND (
            status <> 'dead'
            OR (terminal_at IS NOT NULL AND length(terminal_reason) > 0)
        )
    ),
    UNIQUE (message_id)
);

CREATE INDEX IF NOT EXISTS idx_bot_webhook_inbox_due
    ON bot_webhook_inbox (available_at ASC, id ASC)
    WHERE status IN ('pending', 'retry');

CREATE INDEX IF NOT EXISTS idx_bot_webhook_inbox_lease_expiry
    ON bot_webhook_inbox (lease_until ASC, id ASC)
    WHERE status = 'processing';

CREATE INDEX IF NOT EXISTS idx_bot_webhook_inbox_ordering_partition
    ON bot_webhook_inbox (ordering_key, id ASC)
    WHERE status IN ('pending', 'processing', 'retry');

CREATE TABLE IF NOT EXISTS bot_command_executions (
    id BIGSERIAL PRIMARY KEY,
    message_id TEXT NOT NULL,
    command_kind TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'claimed',
    claim_token TEXT NOT NULL,
    result_summary TEXT NOT NULL DEFAULT '',
    claimed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_bot_command_executions_status_vocab CHECK (
        status IN ('claimed', 'succeeded', 'failed')
    ),
    CONSTRAINT chk_bot_command_executions_message_id_size
        CHECK (length(message_id) > 0 AND length(message_id) <= 512),
    CONSTRAINT chk_bot_command_executions_command_kind_size
        CHECK (length(command_kind) <= 128),
    CONSTRAINT chk_bot_command_executions_claim_token_size
        CHECK (length(claim_token) > 0 AND length(claim_token) <= 256),
    CONSTRAINT chk_bot_command_executions_result_summary_size
        CHECK (octet_length(result_summary) <= 2048),
    CONSTRAINT chk_bot_command_executions_state_shape CHECK (
        status = 'claimed' OR completed_at IS NOT NULL
    ),
    UNIQUE (message_id)
);

CREATE INDEX IF NOT EXISTS idx_bot_command_executions_status_claimed
    ON bot_command_executions (claimed_at ASC, id ASC)
    WHERE status = 'claimed';

CREATE TABLE IF NOT EXISTS bot_reply_outbox (
    id BIGSERIAL PRIMARY KEY,
    message_id TEXT NOT NULL,
    phase TEXT NOT NULL,
    ordinal BIGINT NOT NULL,
    room_id TEXT NOT NULL,
    payload JSONB,
    payload_hash CHAR(64) NOT NULL,
    client_request_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    first_attempt_at TIMESTAMPTZ,
    iris_request_id TEXT NOT NULL DEFAULT '',
    claim_token TEXT,
    lease_until TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_bot_reply_outbox_status_vocab CHECK (
        status IN (
            'pending',
            'submitting',
            'accepted',
            'handoff_completed',
            'retryable_pre_dispatch',
            'outcome_unknown',
            'dead',
            'permanent_conflict',
            'manual_review'
        )
    ),
    CONSTRAINT chk_bot_reply_outbox_message_id_size
        CHECK (length(message_id) > 0 AND length(message_id) <= 512),
    CONSTRAINT chk_bot_reply_outbox_phase_size CHECK (length(phase) > 0 AND length(phase) <= 32),
    CONSTRAINT chk_bot_reply_outbox_ordinal CHECK (ordinal >= 0),
    CONSTRAINT chk_bot_reply_outbox_room_id_size
        CHECK (length(room_id) > 0 AND length(room_id) <= 256),
    CONSTRAINT chk_bot_reply_outbox_payload_hash CHECK (payload_hash ~ '^[0-9a-f]{64}$'),
    -- iris.WithClientRequestID 계약: 8..160 ASCII, [A-Za-z0-9._:-]만 허용.
    CONSTRAINT chk_bot_reply_outbox_client_request_id
        CHECK (client_request_id ~ '^[A-Za-z0-9._:-]{8,160}$'),
    CONSTRAINT chk_bot_reply_outbox_attempts CHECK (attempts >= 0),
    CONSTRAINT chk_bot_reply_outbox_iris_request_id_size CHECK (length(iris_request_id) <= 256),
    CONSTRAINT chk_bot_reply_outbox_last_error_size CHECK (octet_length(last_error) <= 8192),
    CONSTRAINT chk_bot_reply_outbox_state_shape CHECK (
        (
            status NOT IN ('submitting', 'accepted')
            OR (claim_token IS NOT NULL AND lease_until IS NOT NULL AND first_attempt_at IS NOT NULL)
        )
        AND (status <> 'accepted' OR length(iris_request_id) > 0)
        -- 재발송 가능한 동안에만 본문을 들고 있는다. 종단 뒤에도 남기면 Kakao 원문이
        -- retention 기간 내내 DB에 살아 로그에서 제거한 데이터 클래스가 되살아난다.
        AND (
            status IN ('handoff_completed', 'dead', 'permanent_conflict')
            OR payload IS NOT NULL
        )
    ),
    UNIQUE (message_id, phase, ordinal),
    UNIQUE (client_request_id)
);

CREATE INDEX IF NOT EXISTS idx_bot_reply_outbox_due
    ON bot_reply_outbox (created_at ASC, id ASC)
    WHERE status IN ('pending', 'retryable_pre_dispatch', 'outcome_unknown');

CREATE INDEX IF NOT EXISTS idx_bot_reply_outbox_lease_expiry
    ON bot_reply_outbox (lease_until ASC, id ASC)
    WHERE status IN ('submitting', 'accepted');

CREATE INDEX IF NOT EXISTS idx_bot_reply_outbox_message
    ON bot_reply_outbox (message_id, ordinal ASC);

COMMENT ON TABLE bot_webhook_inbox IS 'Bot plane durable webhook admission ledger (message_id keyed).';
COMMENT ON TABLE bot_command_executions IS 'Bot plane command execution claim ledger (one execution per inbound message).';
COMMENT ON TABLE bot_reply_outbox IS 'Bot plane reply outbox with immutable payload and fixed iris client request id.';
