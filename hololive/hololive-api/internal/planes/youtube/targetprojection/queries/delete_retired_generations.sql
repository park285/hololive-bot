SELECT deleted_reasons, deleted_targets, deleted_generations
FROM public.delete_retired_youtube_projection_batch($1, $2)
