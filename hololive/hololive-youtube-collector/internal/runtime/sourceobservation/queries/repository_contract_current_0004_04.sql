SELECT current_schema_version,
       current_generation
FROM lock_observation_contract($1, $2)
