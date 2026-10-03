SELECT locked.provider,
       locked.observation_kind,
       locked.subject_key,
       locked.observation_key,
       locked.schema_version,
       locked.contract_generation,
       locked.evidence_sha256,
       require_source_observation_payload(
           observation.id, observation.observation_kind, observation.schema_version,
           payload.observation_kind, payload.schema_version, payload.canonical_profile,
           payload.payload_sha256, payload.payload
       ) AS payload,
       encode(payload.payload_sha256, 'hex') AS payload_sha256,
       EXISTS (
           SELECT 1
           FROM source_observation_replay_epoch AS epoch
           WHERE epoch.singleton
             AND observation.received_at < epoch.cutoff_received_at
       ) AS replay_epoch_rejected
FROM lock_source_observation($1) AS locked
JOIN source_observations AS observation
  ON observation.id = $1
LEFT JOIN source_observation_payloads AS payload ON payload.id = observation.payload_id
