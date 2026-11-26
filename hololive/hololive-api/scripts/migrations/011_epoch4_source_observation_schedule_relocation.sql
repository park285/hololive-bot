BEGIN;
SET LOCAL lock_timeout = '3s';

ALTER TABLE public.source_observations
    ADD COLUMN schedule_observed_at jsonb,
    ADD CONSTRAINT chk_source_observation_schedule_observed_at
        CHECK (schedule_observed_at IS NULL OR (jsonb_typeof(schedule_observed_at) = 'object'
            AND schedule_observed_at <> '{}'::jsonb AND octet_length(schedule_observed_at::text) <= 4096)),
    DROP COLUMN created_at;

COMMIT;
