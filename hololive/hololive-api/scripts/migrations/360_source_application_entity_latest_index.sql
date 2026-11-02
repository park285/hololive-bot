-- 361의 retention 보호가 "같은 세션에 더 새 application이 있는가"를 행마다 묻는다. 기존 유일 인덱스는
-- observation_id가 선두라 entity 기준 탐색에 쓸 수 없으므로 entity 선두 인덱스를 둔다.
-- 2026-11-02 운영: LIVE/UPCOMING 세션 163개가 application 137만 행을 보호해 7일 지난 live_snapshot 80만 행이
-- 남아 있었다(표본 3,609건 중 99.3%가 이 보호 사유).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_source_application_entity_latest
    ON public.source_observation_applications (entity_kind_id, entity_key, observation_id);
