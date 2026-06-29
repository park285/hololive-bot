-- settlement v2: 18일 앵커 기반 회차 모델
-- 기존 038 스키마는 legacy로 유지

CREATE TABLE IF NOT EXISTS settlement_room_configs (
    room_id VARCHAR(64) PRIMARY KEY,
    billing_anchor_day INT NOT NULL DEFAULT 18 CHECK (billing_anchor_day BETWEEN 1 AND 28),
    billing_tz TEXT NOT NULL DEFAULT 'Asia/Seoul',
    total_amount INT NOT NULL DEFAULT 144000,
    per_person INT NOT NULL DEFAULT 36000,
    require_explicit_for_multiple BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO settlement_room_configs (room_id) VALUES
    ('10000000000000001'),
    ('200000000000002'),
    ('10000000000000003')
ON CONFLICT (room_id) DO NOTHING;

CREATE TABLE IF NOT EXISTS settlement_member_terms (
    id SERIAL PRIMARY KEY,
    room_id VARCHAR(64) NOT NULL,
    member_id INT NOT NULL REFERENCES settlement_members(id) ON DELETE CASCADE,
    effective_from_at TIMESTAMPTZ NOT NULL,
    effective_to_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (effective_to_at IS NULL OR effective_to_at > effective_from_at),
    UNIQUE (room_id, member_id, effective_from_at)
);

CREATE INDEX IF NOT EXISTS idx_settlement_member_terms_active
    ON settlement_member_terms (room_id, effective_from_at, effective_to_at);

-- 기존 멤버를 open-ended membership으로 백필
INSERT INTO settlement_member_terms (room_id, member_id, effective_from_at)
SELECT sm.room_id, sm.id, sm.registered_at
FROM settlement_members sm
WHERE NOT EXISTS (
    SELECT 1
    FROM settlement_member_terms smt
    WHERE smt.room_id = sm.room_id
      AND smt.member_id = sm.id
)
ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS settlement_cycles_v2 (
    id SERIAL PRIMARY KEY,
    room_id VARCHAR(64) NOT NULL,
    cycle_key DATE NOT NULL,
    period_start_at TIMESTAMPTZ NOT NULL,
    period_end_at TIMESTAMPTZ NOT NULL,
    total_amount INT NOT NULL,
    per_person INT NOT NULL,
    billing_anchor_day INT NOT NULL,
    member_count_snapshot INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (room_id, cycle_key),
    UNIQUE (room_id, period_start_at),
    CHECK (period_end_at > period_start_at)
);

CREATE INDEX IF NOT EXISTS idx_settlement_cycles_v2_room_start
    ON settlement_cycles_v2 (room_id, period_start_at DESC);

CREATE TABLE IF NOT EXISTS settlement_payments_v2 (
    id SERIAL PRIMARY KEY,
    cycle_id INT NOT NULL REFERENCES settlement_cycles_v2(id) ON DELETE CASCADE,
    member_id INT NOT NULL REFERENCES settlement_members(id),
    member_name_snapshot VARCHAR(32) NOT NULL,
    paid_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (cycle_id, member_id)
);

CREATE INDEX IF NOT EXISTS idx_settlement_payments_v2_unpaid
    ON settlement_payments_v2 (cycle_id)
    WHERE paid_at IS NULL;

CREATE TABLE IF NOT EXISTS settlement_payment_events_v2 (
    id SERIAL PRIMARY KEY,
    cycle_id INT NOT NULL REFERENCES settlement_cycles_v2(id) ON DELETE CASCADE,
    member_id INT NOT NULL REFERENCES settlement_members(id),
    source_type VARCHAR(32) NOT NULL,
    source_event_id VARCHAR(128) NOT NULL,
    paid_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (source_type, source_event_id)
);

CREATE INDEX IF NOT EXISTS idx_settlement_payment_events_v2_cycle_member
    ON settlement_payment_events_v2 (cycle_id, member_id);
