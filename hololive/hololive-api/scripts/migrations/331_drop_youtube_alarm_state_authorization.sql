-- youtube_community_shorts_alarm_states의 발송 전 선점 열 authorized_at과 ENQUEUED 상태를 지운다. 현재 API·워커는
-- 이 열을 읽거나 쓰지 않고 delivery_status를 alarm_sent_at만으로 정한다. 그래서 남은 ENQUEUED 행은 미발송이면
-- DETECTED, 발송 시각이 있으면 SENT로 맞춘 뒤 어휘 CHECK에서 ENQUEUED를 뺀다.
-- 열에 걸린 부분 색인 idx_ycsas_authorized_at은 DROP COLUMN이 함께 지운다. CHECK 교체가 어차피 ACCESS EXCLUSIVE를
-- 잡으므로 색인을 별도 비차단 문장으로 먼저 지워도 잠금이 줄지 않는다. 검사와 변경이 같은 잠금을 쓰게 처음부터
-- ACCESS EXCLUSIVE를 잡고(256 선례), API upsert와 워커 발송 표시가 상시 쓰는 표라 잠금 대기를 3초로 끊는다.
-- 재실행하면 UPDATE는 0행이고, CHECK는 같은 정의로 다시 만들어 검증하며, 열 삭제는 IF EXISTS로 건너뛴다.
BEGIN;
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';

LOCK TABLE public.youtube_community_shorts_alarm_states IN ACCESS EXCLUSIVE MODE;

UPDATE public.youtube_community_shorts_alarm_states
SET delivery_status = CASE WHEN alarm_sent_at IS NULL THEN 'DETECTED' ELSE 'SENT' END,
    updated_at = clock_timestamp()
WHERE delivery_status = 'ENQUEUED';

ALTER TABLE public.youtube_community_shorts_alarm_states
    DROP CONSTRAINT IF EXISTS chk_youtube_community_shorts_alarm_states_delivery_status_vocab;

ALTER TABLE public.youtube_community_shorts_alarm_states
    ADD CONSTRAINT chk_youtube_community_shorts_alarm_states_delivery_status_vocab
    CHECK (delivery_status IN ('DETECTED', 'SENT')) NOT VALID;

ALTER TABLE public.youtube_community_shorts_alarm_states DROP COLUMN IF EXISTS authorized_at;

COMMIT;

ALTER TABLE public.youtube_community_shorts_alarm_states
    VALIDATE CONSTRAINT chk_youtube_community_shorts_alarm_states_delivery_status_vocab;
