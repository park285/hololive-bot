SELECT observation.id,
       provider_value.name AS provider,
       kind.name AS observation_kind,
       observation.subject_key,
       encode(observation.observation_key, 'hex') AS observation_key,
       observation.schema_version,
       observation.contract_generation,
       observation.scheduled_for,
       observation.observed_at,
       observation.source_event_at,
       observation.received_at,
       encode(observation.scope_sha256, 'hex') AS scope_sha256,
       completeness_value.name AS completeness,
       continuity_value.name AS continuity,
       require_source_observation_payload(
           observation.id, kind.name, observation.schema_version,
           payload.observation_kind, payload.schema_version, payload.canonical_profile,
           payload.payload_sha256, payload.payload
       ) AS payload,
       observation.schedule_observed_at,
       encode(payload.payload_sha256, 'hex') AS payload_sha256,
       encode(observation.evidence_sha256, 'hex') AS evidence_sha256,
       provenance.collector_instance,
       provenance.job_key,
       provenance.collection_job_kind,
       observation.fence_epoch,
       observation.projection_generation,
       queue.attempt_count,
       queue.lease_owner,
       queue.lease_token,
       queue.lease_expires_at,
       EXISTS (
           SELECT 1
           FROM source_observation_replay_epoch AS epoch
           WHERE epoch.singleton
             AND observation.received_at < epoch.cutoff_received_at
       ) AS replay_epoch_rejected
FROM source_observation_queue AS queue
JOIN source_observations AS observation
  ON observation.id = queue.observation_id
JOIN source_observation_provenances AS provenance ON provenance.id = observation.provenance_id
JOIN source_observation_providers AS provider_value ON provider_value.id = observation.provider_id
JOIN source_observation_kinds AS kind ON kind.id = observation.observation_kind_id
JOIN source_observation_completeness_values AS completeness_value ON completeness_value.id = observation.completeness_id
JOIN source_observation_continuity_values AS continuity_value ON continuity_value.id = observation.continuity_id
LEFT JOIN source_observation_payloads AS payload ON payload.id = observation.payload_id
WHERE queue.observation_id = $1
  AND queue.status = 'PROCESSING'
  AND queue.lease_token = $2
  AND queue.lease_expires_at > NOW()
FOR UPDATE OF queue
