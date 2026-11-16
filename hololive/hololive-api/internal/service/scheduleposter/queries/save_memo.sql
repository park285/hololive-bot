UPDATE schedule_poster_translation_memo
SET body = $2, revision = revision + 1, updated_at = now()
WHERE id AND revision = $1
RETURNING revision
