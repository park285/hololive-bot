CREATE TABLE bot_webhook_inbox (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id TEXT NOT NULL UNIQUE CHECK (length(message_id) BETWEEN 1 AND 512),
    room_id TEXT NOT NULL CHECK (length(room_id) BETWEEN 1 AND 256),
    ordering_key TEXT NOT NULL CHECK (length(ordering_key) BETWEEN 1 AND 512),
    payload TEXT NOT NULL CHECK (json_valid(payload)),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'retry', 'dead', 'succeeded')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    claim_token TEXT,
    lease_until INTEGER,
    available_at INTEGER NOT NULL,
    terminal_reason TEXT NOT NULL DEFAULT '' CHECK (length(terminal_reason) <= 512),
    terminal_at INTEGER,
    last_error TEXT NOT NULL DEFAULT '' CHECK (octet_length(last_error) <= 8192),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    CHECK (status <> 'processing' OR (claim_token IS NOT NULL AND lease_until IS NOT NULL)),
    CHECK (status <> 'dead' OR (terminal_at IS NOT NULL AND length(terminal_reason) > 0)),
    CHECK (status NOT IN ('dead', 'succeeded') OR payload = '{}')
) STRICT;

CREATE INDEX idx_bot_webhook_inbox_due ON bot_webhook_inbox (available_at, id)
    WHERE status IN ('pending', 'retry');
CREATE INDEX idx_bot_webhook_inbox_lease_expiry ON bot_webhook_inbox (lease_until, id)
    WHERE status = 'processing';
CREATE INDEX idx_bot_webhook_inbox_ordering_partition ON bot_webhook_inbox (ordering_key, id)
    WHERE status IN ('pending', 'processing', 'retry');
CREATE INDEX idx_bot_webhook_inbox_terminal_updated ON bot_webhook_inbox (updated_at, id)
    WHERE status IN ('dead', 'succeeded');

CREATE TABLE bot_webhook_heads (
    ordering_key TEXT NOT NULL PRIMARY KEY CHECK (length(ordering_key) BETWEEN 1 AND 512),
    message_id TEXT NOT NULL UNIQUE REFERENCES bot_webhook_inbox (message_id) ON DELETE CASCADE,
    updated_at INTEGER NOT NULL
) STRICT;

CREATE TABLE bot_command_executions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id TEXT NOT NULL UNIQUE CHECK (length(message_id) BETWEEN 1 AND 512),
    command_kind TEXT NOT NULL DEFAULT '' CHECK (length(command_kind) <= 128),
    status TEXT NOT NULL DEFAULT 'claimed' CHECK (status IN ('claimed', 'succeeded', 'failed', 'outcome_unknown')),
    claim_token TEXT NOT NULL CHECK (length(claim_token) BETWEEN 1 AND 256),
    claimed_at INTEGER NOT NULL,
    completed_at INTEGER,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    CHECK (status = 'claimed' OR completed_at IS NOT NULL)
) STRICT;

CREATE INDEX idx_bot_command_executions_status_claimed ON bot_command_executions (claimed_at, id)
    WHERE status = 'claimed';
CREATE INDEX idx_bot_command_executions_terminal_updated ON bot_command_executions (updated_at, id)
    WHERE status IN ('succeeded', 'failed', 'outcome_unknown');

CREATE TABLE bot_reply_outbox (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id TEXT NOT NULL CHECK (length(message_id) BETWEEN 1 AND 512),
    phase TEXT NOT NULL CHECK (length(phase) BETWEEN 1 AND 32),
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    room_id TEXT NOT NULL CHECK (length(room_id) BETWEEN 1 AND 256),
    payload TEXT CHECK (payload IS NULL OR json_valid(payload)),
    payload_hash TEXT NOT NULL CHECK (length(payload_hash) = 64 AND payload_hash NOT GLOB '*[^0-9a-f]*'),
    client_request_id TEXT NOT NULL UNIQUE CHECK (
        length(client_request_id) BETWEEN 8 AND 160 AND client_request_id NOT GLOB '*[^-A-Za-z0-9._:]*'
    ),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN (
        'pending', 'submitting', 'accepted', 'handoff_completed', 'retryable_pre_dispatch',
        'outcome_unknown', 'dead', 'permanent_conflict', 'manual_review', 'discarded'
    )),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    first_attempt_at INTEGER,
    iris_request_id TEXT NOT NULL DEFAULT '' CHECK (length(iris_request_id) <= 256),
    claim_token TEXT,
    lease_until INTEGER,
    last_error TEXT NOT NULL DEFAULT '' CHECK (octet_length(last_error) <= 8192),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    available_at INTEGER NOT NULL,
    operator_replay_grants INTEGER NOT NULL DEFAULT 0 CHECK (operator_replay_grants >= 0),
    accepted_at INTEGER,
    handoff_next_check_at INTEGER,
    row_version INTEGER NOT NULL DEFAULT 0,
    UNIQUE (message_id, phase, ordinal),
    CHECK (status <> 'submitting' OR (claim_token IS NOT NULL AND lease_until IS NOT NULL AND first_attempt_at IS NOT NULL)),
    CHECK (status <> 'accepted' OR (
        length(iris_request_id) > 0 AND claim_token IS NULL AND lease_until IS NULL
        AND accepted_at IS NOT NULL AND handoff_next_check_at IS NOT NULL
    )),
    CHECK (status = 'accepted' OR handoff_next_check_at IS NULL),
    CHECK (status IN ('handoff_completed', 'dead', 'permanent_conflict', 'discarded') OR payload IS NOT NULL)
) STRICT;

CREATE INDEX idx_bot_reply_outbox_due_available ON bot_reply_outbox (available_at, id)
    WHERE status IN ('pending', 'retryable_pre_dispatch', 'outcome_unknown');
CREATE INDEX idx_bot_reply_outbox_handoff_due ON bot_reply_outbox (handoff_next_check_at, id)
    WHERE status = 'accepted' AND claim_token IS NULL;
CREATE INDEX idx_bot_reply_outbox_room_active ON bot_reply_outbox (room_id, id)
    WHERE status IN ('pending', 'submitting', 'accepted', 'retryable_pre_dispatch', 'outcome_unknown');
CREATE INDEX idx_bot_reply_outbox_submitting_lease_expiry ON bot_reply_outbox (lease_until, id)
    WHERE status = 'submitting';
CREATE INDEX idx_bot_reply_outbox_manual_review_updated ON bot_reply_outbox (updated_at, id)
    WHERE status = 'manual_review';
CREATE INDEX idx_bot_reply_outbox_terminal_updated ON bot_reply_outbox (updated_at, id)
    WHERE status IN ('handoff_completed', 'dead', 'permanent_conflict', 'discarded');

CREATE TABLE bot_reply_outbox_replay_audit (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    outbox_id INTEGER NOT NULL REFERENCES bot_reply_outbox (id) ON DELETE CASCADE,
    grant_number INTEGER NOT NULL CHECK (grant_number > 0),
    event_type TEXT NOT NULL CHECK (event_type IN ('granted', 'replayed')),
    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 64 AND actor NOT GLOB '*[^-A-Za-z0-9._:@]*'),
    reason TEXT NOT NULL CHECK (
        octet_length(reason) BETWEEN 1 AND 256
        AND reason NOT GLOB ('*[' || char(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 127) || ']*')
    ),
    recorded_at INTEGER NOT NULL,
    UNIQUE (outbox_id, grant_number, event_type)
) STRICT;

CREATE INDEX idx_bot_reply_outbox_replay_audit_outbox_recorded ON bot_reply_outbox_replay_audit (outbox_id, recorded_at, id);

CREATE TABLE bot_reply_outbox_resolution_audit (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    outbox_id INTEGER NOT NULL UNIQUE REFERENCES bot_reply_outbox (id) ON DELETE CASCADE,
    decision TEXT NOT NULL CHECK (decision = 'discarded_without_replay'),
    observed_iris_state TEXT NOT NULL CHECK (observed_iris_state IN (
        'queued', 'preparing', 'prepared', 'sending', 'handoff_completed', 'failed', 'outcome_unknown', 'not_found'
    )),
    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 64 AND actor NOT GLOB '*[^-A-Za-z0-9._:@]*'),
    reason TEXT NOT NULL CHECK (
        octet_length(reason) BETWEEN 1 AND 256
        AND reason NOT GLOB ('*[' || char(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 127) || ']*')
    ),
    recorded_at INTEGER NOT NULL
) STRICT;

CREATE TRIGGER bot_reply_outbox_row_version
BEFORE UPDATE ON bot_reply_outbox
FOR EACH ROW WHEN NEW.row_version IS NOT OLD.row_version + 1
BEGIN
    SELECT RAISE(ABORT, 'bot_reply_outbox updates must advance row_version by one');
END;

CREATE TRIGGER bot_reply_outbox_discard_insert_rejected
BEFORE INSERT ON bot_reply_outbox
FOR EACH ROW WHEN NEW.status = 'discarded'
BEGIN
    SELECT RAISE(ABORT, 'discarded reply transition requires an audited manual-review decision');
END;

CREATE TRIGGER bot_reply_outbox_discard_audit_required
BEFORE UPDATE ON bot_reply_outbox
FOR EACH ROW WHEN NEW.status = 'discarded' AND OLD.status <> 'discarded' AND (
    OLD.status <> 'manual_review'
    OR NEW.payload IS NOT NULL
    OR NEW.claim_token IS NOT NULL
    OR NEW.lease_until IS NOT NULL
    OR NOT EXISTS (
        SELECT 1
        FROM bot_reply_outbox_resolution_audit AS audit
        WHERE audit.outbox_id = NEW.id
          AND audit.decision = 'discarded_without_replay'
    )
)
BEGIN
    SELECT RAISE(ABORT, 'discarded reply transition requires an audited manual-review decision');
END;

CREATE TRIGGER bot_reply_outbox_replay_claim_audit
BEFORE UPDATE OF status ON bot_reply_outbox
FOR EACH ROW WHEN NEW.status = 'submitting' AND OLD.status <> 'submitting' AND NEW.operator_replay_grants > 0
BEGIN
    SELECT RAISE(ABORT, 'manual replay grant audit is missing')
    WHERE NOT EXISTS (
        SELECT 1
        FROM bot_reply_outbox_replay_audit
        WHERE outbox_id = NEW.id
          AND grant_number = NEW.operator_replay_grants
          AND event_type = 'granted'
    );
    INSERT INTO bot_reply_outbox_replay_audit (outbox_id, grant_number, event_type, actor, reason, recorded_at)
    SELECT outbox_id, grant_number, 'replayed', actor, reason, NEW.updated_at
    FROM bot_reply_outbox_replay_audit
    WHERE outbox_id = NEW.id
      AND grant_number = NEW.operator_replay_grants
      AND event_type = 'granted'
    ON CONFLICT (outbox_id, grant_number, event_type) DO NOTHING;
END;

CREATE TRIGGER bot_reply_outbox_replay_audit_no_update
BEFORE UPDATE ON bot_reply_outbox_replay_audit
BEGIN
    SELECT RAISE(ABORT, 'bot_reply_outbox_replay_audit events are immutable');
END;

CREATE TRIGGER bot_reply_outbox_replay_audit_no_delete
BEFORE DELETE ON bot_reply_outbox_replay_audit
FOR EACH ROW WHEN EXISTS (SELECT 1 FROM bot_reply_outbox WHERE id = OLD.outbox_id)
BEGIN
    SELECT RAISE(ABORT, 'bot_reply_outbox_replay_audit events are immutable');
END;

CREATE TRIGGER bot_reply_outbox_resolution_audit_no_update
BEFORE UPDATE ON bot_reply_outbox_resolution_audit
BEGIN
    SELECT RAISE(ABORT, 'bot_reply_outbox_resolution_audit events are immutable');
END;

CREATE TRIGGER bot_reply_outbox_resolution_audit_no_delete
BEFORE DELETE ON bot_reply_outbox_resolution_audit
FOR EACH ROW WHEN EXISTS (SELECT 1 FROM bot_reply_outbox WHERE id = OLD.outbox_id)
BEGIN
    SELECT RAISE(ABORT, 'bot_reply_outbox_resolution_audit events are immutable');
END;

CREATE TABLE bot_webhook_nonces (
    digest BLOB NOT NULL PRIMARY KEY CHECK (length(digest) = 32),
    expires_at INTEGER NOT NULL
) STRICT, WITHOUT ROWID;

CREATE INDEX idx_bot_webhook_nonces_expiry ON bot_webhook_nonces (expires_at);
