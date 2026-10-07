\set ON_ERROR_STOP on
CREATE EXTENSION pg_jsonschema;
CREATE SCHEMA extension_benchmark;

-- 운영 aliases CHECK와 같은 정책입니다. 원소 타입·추가 키·SQL NULL 허용을 강화하지 않습니다.
CREATE FUNCTION extension_benchmark.aliases_builtin(value jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT value IS NULL OR (
        jsonb_typeof(value)='object' AND value ? 'ko' AND value ? 'ja'
        AND jsonb_typeof(value->'ko')='array' AND jsonb_typeof(value->'ja')='array'
    )
$$;
CREATE FUNCTION extension_benchmark.aliases_schema(value jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT value IS NULL OR public.jsonb_matches_schema(
        '{"type":"object","required":["ko","ja"],"properties":{"ko":{"type":"array"},"ja":{"type":"array"}}}'::json,
        value)
$$;

DO $cases$
DECLARE
    example record;
BEGIN
    FOR example IN SELECT cases.value, cases.expected FROM (VALUES
        (NULL::jsonb, true),
        ('{"ko":[],"ja":[]}'::jsonb, true),
        ('{"ko":["가나다"],"ja":["あいう"]}'::jsonb, true),
        ('{"ko":[1,null],"ja":[{}],"extra":true}'::jsonb, true),
        ('{}'::jsonb, false),
        ('{"ko":[]}'::jsonb, false),
        ('{"ja":[]}'::jsonb, false),
        ('{"ko":null,"ja":[]}'::jsonb, false),
        ('{"ko":{},"ja":[]}'::jsonb, false),
        ('{"ko":[],"ja":"x"}'::jsonb, false),
        ('[]'::jsonb, false),
        ('null'::jsonb, false),
        ('1'::jsonb, false),
        ('true'::jsonb, false)
    ) AS cases(value, expected) LOOP
        IF extension_benchmark.aliases_builtin(example.value) IS DISTINCT FROM example.expected
           OR extension_benchmark.aliases_schema(example.value) IS DISTINCT FROM example.expected THEN
            RAISE EXCEPTION 'aliases contract mismatch';
        END IF;
    END LOOP;
END
$cases$;

CREATE TABLE extension_benchmark.checked_aliases (
    value jsonb CHECK (extension_benchmark.aliases_schema(value))
);
INSERT INTO extension_benchmark.checked_aliases VALUES (NULL), ('{"ko":[],"ja":[]}');
DO $constraint$
BEGIN
    BEGIN
        INSERT INTO extension_benchmark.checked_aliases VALUES ('{"ko":[]}');
        RAISE EXCEPTION 'invalid aliases passed CHECK';
    EXCEPTION WHEN check_violation THEN
        NULL;
    END;
END
$constraint$;

-- coverage의 기존 AND 표현식은 키 누락에서 NULL이므로 CHECK를 통과합니다.
-- required를 추가하는 JSON Schema는 계약 강화이며 별도 데이터/앱 검토가 필요합니다.
SELECT (jsonb_typeof('{}'::jsonb)='object'
        AND jsonb_typeof('{}'::jsonb->'requested_channel_ids')='array') IS NOT FALSE AS legacy_missing_key_accepted,
       public.jsonb_matches_schema(
           '{"type":"object","required":["requested_channel_ids"],"properties":{"requested_channel_ids":{"type":"array"}}}',
           '{}'::jsonb) AS required_schema_missing_key_accepted;

CREATE TABLE extension_benchmark.alias_samples AS
SELECT jsonb_build_object('ko',jsonb_build_array('가나다 '||id),
                          'ja',jsonb_build_array('あいう '||id),'extra',id) AS value
FROM generate_series(1,20000) AS sample(id);
VACUUM (ANALYZE) extension_benchmark.alias_samples;
