WITH epoch3(filename, checksum) AS (VALUES
    ('001_schema_epoch3_baseline.sql','788a04b9eba0b65f247258f3319ed0a672ada38cbbaf8ac0ac99554281e8e286'),
    ('002_drop_x_space_login_attempts.sql','8c92cd133a076a901733322fa82fa46bc47dbf16019af50348516e30aaaf224a'),
    ('003_source_retention_protected_set.sql','ed941bd0d81d74f3a6eaabd52b1c8607da7f0e032dc411fdf9f3cec9ce81b810'),
    ('004_source_application_natural_key.sql','53c8206fffd0e4742538b79d34a76b2d8f0452415b84c32ac3099db23216307e'),
    ('005_youtube_collection_candidate_next_due.sql','db2b40fc17db9a1779545de7d147e06c9548e5849716b1c3fc49c6e0b4baf037'),
    ('006_drop_bot_reply_outbox_message_index.sql','d4b690eeefd9203ed0c47b24dc186092a34783569bb592dc0d741207f0b31cae'),
    ('007_narrow_send_route_and_alarm_types_checks.sql','51f7dccf3b09f69b29a6f5cd6a26d1f67b4262a4c1f0be36815ff8372aae9e5a'),
    ('008_youtubejs_live_contract_generation.sql','30acd1d456ee401d71eba0a93517404034a7635b6d80e8e3ea990142b74854fa'),
    ('009_drop_llm_feature_storage.sql','83df2a06cb12cd4bb46807284eba5bf9fdf1fe1cd069b79a2db8700b21d83eec'),
    ('010_member_schedule_posters.sql','eb913e470437164f4905c1794f02642a78f38102712b1da9a6791bbbcd79a39e'),
    ('011_help_schedule_poster.sql','a8249113fa95e3e22d59ba186eb25aec7ed7920eb64d950dbe18badbdce08f3a'),
    ('012_schedule_poster_keep_translation.sql','b374445ecde1d2e78566ba05a5b71ab11ee2440b3a171231f8761d3b86f30747'),
    ('013_schedule_poster_source.sql','37afd5787f566749be2262ebf34d358cffd2f0a9f8492b92b10699e4163edc77'),
    ('014_schedule_poster_thumbnail.sql','d9047b2eb8c80caf8f53e552308cb3590f55303cec8e568d321c3595a61085f9'),
    ('015_schedule_poster_translation_inputs.sql','05292f941572e236be4f82ce1d95adff20a2fa947ee9561adf830db993600885'),
    ('016_schedule_poster_message_strings.sql','de794461fb964aaacdba61627d78d48f356b374bbede4a9d1fd3a8094d243155'),
    ('017_schedule_poster_review_request.sql','c1863b05f38263c68c29e0f48b6fc4425dde201c61eed6e320af607acf36a3a7'),
    ('018_schedule_poster_review_request_reason.sql','6ce8d45114f623d84daf25150eb1c8e71be5116a2ac5b941327c48b9afe86b11'),
    ('019_schedule_poster_rows.sql','fb217ad81204a967a4fb0f75e5d09ee1af68bef2893d765f184faabce14bef86'),
    ('020_schedule_poster_changes.sql','1916fea51e371f4e21032239bd7a7b32bdb2007d47004dcb1ab26b52af837692'),
    ('021_youtube_broadcast_type_decisions.sql','50fbc55f4eb1aad9f1e833a77bdf04ff81bb2a4d3266034bbd64fe94ca8136b1'),
    ('022_drop_birthday_stream_template.sql','7ccd0d3d53a4a545e2eb6b39304a0412b6cecb11a8d5b7ede02abae495f2f740'),
    ('023_schedule_poster_not_found_text.sql','b636c62e118003d501355e4e3f9d77fb3668814fc2ca5c203041545a28302166'),
    ('024_schedule_poster_image.sql','99ed41f85520f5d6a8bd347d14e646b03e394c5edf58d32134eb4b17adb11a02'),
    ('025_bot_command_error_message_strings.sql','dfe0993c034e696015773448d04feb66845408ba0f9dcde1f419ca1e3f826159'),
    ('026_schedule_poster_polish_memo.sql','c504232b68088d11c8c3a476e2361e8a689aacc1b35ac030cd788bcc1cea817e'),
    ('027_schedule_poster_post_channels.sql','0655fb76dcc065b17e8c2fc65ec9080ef98b9d428ed4cb162d9ad8b66d92854e'),
    ('028_collector_database_identity_fence.sql','97d9a25b04a27ccafee3b5a3fc5a35ae984ae4eab6311363d31b9ec469896d01'),
    ('029_alarm_worker_database_role.sql','9883c4840f456d5acd393337f492dca415f6f57b75b8b3e00daab69935b016c6'),
    ('030_collector_identity_owner_token.sql','6166d04495fd2554c91d72b972ed645d7d693c580c18bcbba245976b127a7113'),
    ('031_retire_holodex_channel_profile.sql','e86e5e2dd144aeeea3b2be5f64aff2cb7963efd79ac0771cc337d01a66d073c4'),
    ('032_schedule_poster_template.sql','5d0f4199d01427c4bd4ed19a48e39e7e9f6c28f1b8b95bd8ac668154cd380632')
), active AS (
    SELECT split_part(line,'  ',2) AS filename, split_part(line,'  ',1) AS checksum, ordinal
    FROM unnest(string_to_array($1::text, E'\n')) WITH ORDINALITY AS f(line,ordinal)
), receipts AS (
    SELECT m.filename, c.checksum_sha256::text AS checksum,
           m.filename IS NOT NULL AND m.applied_at IS NOT NULL AND c.filename IS NOT NULL AND c.recorded_at IS NOT NULL AS paired
    FROM public.schema_migrations m FULL JOIN public.schema_migration_checksums c USING (filename)
), state AS (
    SELECT count(*) AS total, count(DISTINCT r.filename) AS names, bool_and(r.paired) AS complete,
           count(e.filename) AS legacy, count(*) FILTER (WHERE a.ordinal = 1) AS baseline,
           count(*) FILTER (WHERE a.ordinal > 1) AS suffix, coalesce(max(a.ordinal),1) AS last
    FROM receipts r LEFT JOIN epoch3 e USING (filename, checksum)
    LEFT JOIN active a ON a.filename = r.filename AND a.checksum = r.checksum
)
SELECT CASE WHEN complete AND names = total AND last = suffix+1 AND legacy = 32 AND total = 32+suffix THEN 3
            WHEN complete AND names = total AND last = suffix+1 AND baseline = 1 AND total = 1+suffix THEN 4 ELSE 0 END AS epoch,
       suffix::integer AS applied, (SELECT count(*) FROM active) - 1 - suffix AS pending
FROM state
WHERE (SELECT count(*) FROM pg_catalog.pg_class WHERE oid IN ('public.schema_migrations'::regclass, 'public.schema_migration_checksums'::regclass) AND relkind = 'r') = 2
  AND NOT EXISTS (SELECT FROM pg_catalog.pg_index WHERE NOT (indisvalid AND indisready AND indislive));
