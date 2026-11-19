UPDATE schedule_posters
SET image_data = $2, image_mime = $3
WHERE id = $1 AND image_data IS NULL AND review_status IN ('pending_review', 'confirmed')
