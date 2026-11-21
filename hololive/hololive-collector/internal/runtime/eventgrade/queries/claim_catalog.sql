SELECT job_key, fence_epoch, claimed_at FROM public.event_grade_claim_catalog($1, $2, $3)
