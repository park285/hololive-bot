-- 245가 기존 행 분류를 위해 둔 열 기본값 'legacy_unknown'을 지운다. 이 값은 "수명 추적 도입 전 기록"이라는 뜻이라,
-- 출처를 빠뜨린 새 writer가 오류 없이 legacy 의미(시작 시각 비표시, 미추적 UPCOMING, 종료 판정 완화)를 얻었다.
-- production writer(repository_live_session_insert.sql, repository_live_premiere_classification_upsert.sql)는 이미
-- 출처를 명시하므로, 이제 출처 없는 INSERT는 NOT NULL 위반으로 실패한다. 기존 행과 CHECK 어휘, legacy_unknown
-- reader는 그대로 둔다. catalog만 바꾸며 행을 다시 쓰지 않고, 재적용은 아무것도 바꾸지 않는다.
BEGIN;
SET LOCAL lock_timeout = '3s';
ALTER TABLE public.youtube_live_sessions ALTER COLUMN lifecycle_origin DROP DEFAULT;
COMMIT;
