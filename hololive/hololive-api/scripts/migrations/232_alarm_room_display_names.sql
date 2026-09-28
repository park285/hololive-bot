-- 관리자가 지정한 방 표시 이름은 방 단위 PG 행으로 보존한다. Kakao가 알림 등록 때 넘기는 alarms.room_name과 분리해
-- 알림 재등록·subscriber cache rebuild가 관리자 이름을 덮어쓰거나 지우지 않게 한다. 관리 목록은 이 이름을 우선하고,
-- 행이 없을 때만 alarms.room_name 대표값을 쓴다. 폭은 alarms.room_id(100)·room_name(255) 표준을 따른다.
CREATE TABLE IF NOT EXISTS alarm_room_display_names (
    room_id VARCHAR(100) PRIMARY KEY,
    display_name VARCHAR(255) NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_alarm_room_display_names_display_name_nonblank CHECK (btrim(display_name) <> '')
);

-- 관리 목록의 Kakao 방 이름 대표값은 "마지막으로 room_name이 바뀐 행"이다. created_at은 upsert로 이름만 바뀐
-- 기존 행을 반영하지 못하므로 alarm upsert가 room_name이 실제로 바뀔 때만 이 시각을 갱신한다.
-- now()는 STABLE(비volatile)이라 PG 11+ fast default로 카탈로그에만 기록되고 테이블 재작성은 없다. 기존 행은 모두
-- 이 migration 시각을 받으며, 동률은 목록 쿼리의 id DESC가 가른다.
ALTER TABLE alarms
    ADD COLUMN IF NOT EXISTS room_name_updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
