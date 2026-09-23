UPDATE x_space_login_attempts AS a
SET status = CASE
      WHEN s.revision <> a.submitted_revision THEN 'manual_override'
      WHEN s.candidate_state = 'rejected' THEN 'login_required'
      ELSE 'connected' END,
    error_code = CASE
      WHEN s.revision <> a.submitted_revision THEN 'manual_override'
      WHEN s.candidate_state = 'rejected' THEN 'candidate_rejected'
      ELSE '' END
FROM x_space_session AS s
WHERE s.id = 1 AND a.status IN ('submitted', 'connected')
  AND a.id = (SELECT max(id) FROM x_space_login_attempts)
  AND (s.revision <> a.submitted_revision
       OR (a.status = 'submitted' AND (s.candidate_state = 'rejected'
           OR (s.active_revision = a.submitted_revision AND s.state = 'connected'))))
