WITH input AS MATERIALIZED (
    SELECT provider,
           observation_kind,
           schema_version,
           contract_generation
    FROM jsonb_to_recordset($1::jsonb) AS value(
        provider text,
        observation_kind text,
        schema_version smallint,
        contract_generation bigint
    )
), requested AS MATERIALIZED (
    SELECT DISTINCT provider,
                    observation_kind,
                    schema_version,
                    contract_generation
    FROM input
), locked AS MATERIALIZED (
    SELECT requested.provider,
           requested.observation_kind,
           requested.schema_version,
           requested.contract_generation,
           current.current_schema_version,
           current.current_generation
    FROM requested
    LEFT JOIN LATERAL lock_observation_contract(
        requested.provider,
        requested.observation_kind
    ) AS current ON TRUE
)
SELECT COALESCE(
    bool_and(
        current_schema_version = schema_version
        AND current_generation = contract_generation
    ),
    FALSE
)
FROM locked
