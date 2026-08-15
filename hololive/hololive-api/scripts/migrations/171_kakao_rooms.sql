CREATE TABLE IF NOT EXISTS kakao_rooms (
    room_id VARCHAR(100) PRIMARY KEY,
    room_type VARCHAR(64) NOT NULL DEFAULT '',
    room_link_id VARCHAR(128) NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT kakao_rooms_room_id_len CHECK (length(room_id) > 0 AND length(room_id) <= 100),
    CONSTRAINT kakao_rooms_room_type_len CHECK (length(room_type) <= 64),
    CONSTRAINT kakao_rooms_room_link_id_len CHECK (length(room_link_id) <= 128)
);
