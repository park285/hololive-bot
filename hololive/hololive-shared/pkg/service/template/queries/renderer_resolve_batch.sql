SELECT DISTINCT ON (request.ordinality)
       request.ordinality, template.id, template.row_version, COALESCE(template.channel_id, '')
FROM unnest($1::text[], $2::text[], $3::text[]) WITH ORDINALITY AS request(template_key, channel_id, format_variant, ordinality)
JOIN notification_templates AS template
  ON template.template_key = request.template_key
 AND template.format_variant = request.format_variant
 AND (template.channel_id = NULLIF(request.channel_id, '') OR template.channel_id IS NULL)
ORDER BY request.ordinality, template.channel_id IS NULL
