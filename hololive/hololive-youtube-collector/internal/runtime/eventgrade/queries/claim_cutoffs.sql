SELECT job_key, event_id, chapter_id, final_after, binding_observed_at, fence_epoch, claimed_at
FROM public.event_grade_claim_cutoffs($1, $2, $3, $4)
