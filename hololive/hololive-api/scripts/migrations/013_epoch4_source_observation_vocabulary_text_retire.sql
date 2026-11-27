BEGIN;
SET LOCAL lock_timeout = '3s';

LOCK TABLE public.source_observations IN ACCESS EXCLUSIVE MODE;

DROP TRIGGER derive_source_observation_vocabulary ON public.source_observations;
DROP FUNCTION public.derive_source_observation_vocabulary();
DROP INDEX public.idx_source_observations_kind_received_id;

ALTER TABLE public.source_observations
    DROP CONSTRAINT fk_source_observation_contract,
    DROP CONSTRAINT fk_source_observation_provider_name,
    DROP CONSTRAINT fk_source_observation_kind_name,
    DROP CONSTRAINT fk_source_observation_completeness_name,
    DROP CONSTRAINT fk_source_observation_continuity_name,
    DROP CONSTRAINT chk_source_observation_completeness_vocab,
    DROP CONSTRAINT chk_source_observation_continuity_vocab,
    DROP COLUMN provider,
    DROP COLUMN observation_kind,
    DROP COLUMN completeness,
    DROP COLUMN continuity;

ALTER TABLE public.source_observations
    ALTER COLUMN provider_id SET DATA TYPE smallint USING (provider_id + 0),
    ADD CONSTRAINT fk_source_observation_completeness FOREIGN KEY (completeness_id)
        REFERENCES public.source_observation_completeness_values (id) ON DELETE RESTRICT,
    ADD CONSTRAINT fk_source_observation_continuity FOREIGN KEY (continuity_id)
        REFERENCES public.source_observation_continuity_values (id) ON DELETE RESTRICT;

COMMIT;
