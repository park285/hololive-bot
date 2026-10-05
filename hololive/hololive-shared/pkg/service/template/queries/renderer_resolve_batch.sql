SELECT DISTINCT ON (request.ordinality)
       request.ordinality, template.id, template.row_version, COALESCE(template.channel_id, '')
FROM unnest($1::text[], $2::text[]) WITH ORDINALITY AS request(template_key, channel_id, ordinality)
JOIN notification_templates AS template
  ON template.template_key = request.template_key
 AND (template.channel_id = NULLIF(request.channel_id, '') OR template.channel_id IS NULL)
ORDER BY request.ordinality, template.channel_id IS NULL
