(track.kind, track.content_id) IN (
    SELECT input.kind, input.content_id
    FROM unnest($2::text[], $3::text[]) AS input(kind, content_id)
)
