-- 중단된 인덱스 생성으로 남은 invalid 인덱스는 기존 유일성을 대체할 수 없다.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_index
        WHERE indexrelid = 'public.idx_alarms_room_channel_host'::regclass
          AND indisvalid AND indisunique
    ) THEN
        RAISE EXCEPTION 'idx_alarms_room_channel_host must be valid before replacing channel uniqueness';
    END IF;
END
$$;

ALTER TABLE public.alarms DROP CONSTRAINT IF EXISTS alarms_room_channel_unique;
