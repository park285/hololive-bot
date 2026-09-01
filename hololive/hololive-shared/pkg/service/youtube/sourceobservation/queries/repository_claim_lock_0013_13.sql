SELECT observation.id,
       observation.provider,
       observation.observation_kind,
       observation.subject_key,
       observation.observation_key,
       observation.schema_version,
       observation.contract_generation,
       observation.scheduled_for,
       observation.observed_at,
       observation.source_event_at,
       observation.received_at,
       observation.scope_sha256,
       observation.completeness,
       observation.continuity,
       observation.payload,
       observation.payload_sha256,
       observation.evidence_sha256,
       observation.collector_instance,
       observation.job_key,
       observation.collection_job_kind,
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
WHERE queue.observation_id = $1
  AND queue.status = 'PROCESSING'
  AND queue.lease_token = $2
  AND queue.lease_expires_at > NOW()
FOR UPDATE OF queue
