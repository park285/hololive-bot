INSERT INTO event_grade_chapters(region,event_id,chapter_id,event_type,starts_at,ends_at,event_ends_at,reveals_at,observed_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (region,event_id,chapter_id) DO UPDATE SET
    event_type = EXCLUDED.event_type, starts_at = EXCLUDED.starts_at, ends_at = EXCLUDED.ends_at,
    event_ends_at = EXCLUDED.event_ends_at, reveals_at = EXCLUDED.reveals_at, observed_at = EXCLUDED.observed_at,
    binding_conflicted = false
RETURNING OLD.region IS NOT NULL AND greatest(OLD.event_ends_at,OLD.reveals_at) <> greatest(NEW.event_ends_at,NEW.reveals_at)
