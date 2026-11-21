SELECT EXISTS (
           SELECT 1
           FROM youtube_collection_projection_generations AS projection
           WHERE projection.status = 'CURRENT'
             AND projection.valid_until > statement_timestamp()
       ) AS projection_current,
       COALESCE(
           job.membership_kinds = $6::text[]
           AND job.membership_exact_subject = $7::boolean
           AND youtube_collection_membership_valid(
               job.membership_kinds,
               job.membership_exact_subject,
               job.subject_key,
               job.membership_target_count,
               job.projection_generation
           ),
           FALSE
       ) AS membership_valid
FROM youtube_collection_job_leases AS job
WHERE job.job_key = $1
  AND job.owner_instance = $2
  AND job.fence_epoch = $3
  AND job.projection_generation = $4
  AND job.scheduled_for = $5
  AND job.slot_state = 'ACTIVE'
  AND job.lease_expires_at > clock_timestamp()
