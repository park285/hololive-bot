-- 정산 봇: 월별 구독료 정산 관리 테이블 (방별 분리)

-- 멤버 등록 (방 + kakao user_id <-> 정산 이름)
CREATE TABLE IF NOT EXISTS settlement_members (
    id SERIAL PRIMARY KEY,
    room_id VARCHAR(64) NOT NULL,
    kakao_user_id VARCHAR(64) NOT NULL,
    member_name VARCHAR(32) NOT NULL,
    registered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (room_id, kakao_user_id),
    UNIQUE (room_id, member_name)
);

-- 월별 정산 주기 (방별)
CREATE TABLE IF NOT EXISTS settlement_cycles (
    id SERIAL PRIMARY KEY,
    room_id VARCHAR(64) NOT NULL,
    year INT NOT NULL,
    month INT NOT NULL,
    total_amount INT NOT NULL DEFAULT 144000,
    per_person INT NOT NULL DEFAULT 36000,
    due_day INT NOT NULL DEFAULT 18,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (room_id, year, month)
);

-- 납부 상태
CREATE TABLE IF NOT EXISTS settlement_payments (
    id SERIAL PRIMARY KEY,
    cycle_id INT NOT NULL REFERENCES settlement_cycles(id),
    member_id INT NOT NULL REFERENCES settlement_members(id),
    paid_at TIMESTAMPTZ,
    UNIQUE (cycle_id, member_id)
);

CREATE INDEX IF NOT EXISTS idx_sp_unpaid ON settlement_payments (cycle_id) WHERE paid_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_sm_room ON settlement_members (room_id);
CREATE INDEX IF NOT EXISTS idx_sc_room ON settlement_cycles (room_id);
