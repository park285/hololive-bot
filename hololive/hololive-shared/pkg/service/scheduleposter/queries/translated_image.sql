SELECT translated_mime, translated_image
FROM schedule_posters
WHERE id = $1 AND translation_status = 'succeeded'
