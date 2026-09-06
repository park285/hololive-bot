ALTER TABLE public.alarms
    ADD COLUMN IF NOT EXISTS host_id TEXT NOT NULL DEFAULT '';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'public.alarms'::regclass
          AND conname = 'chk_alarms_host_id_vocab'
    ) THEN
        ALTER TABLE public.alarms
            ADD CONSTRAINT chk_alarms_host_id_vocab CHECK (
                host_id = '' OR (
                    channel_id = 'UC3OH5FKQ3qtl4uRme_vZTgA'
                    AND host_id IN ('kiyosumi-lyra', 'reimei-mira', 'yoinagi-neon')
                )
            ) NOT VALID;
    END IF;
END
$$;

ALTER TABLE public.alarms VALIDATE CONSTRAINT chk_alarms_host_id_vocab;
