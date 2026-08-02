-- NULL = 미판정(watch probe 미실행/실패). 판정 후 불변.
ALTER TABLE youtube_live_sessions ADD COLUMN IF NOT EXISTS is_premiere boolean;

COMMENT ON COLUMN youtube_live_sessions.is_premiere IS 'NULL = undecided (watch probe not run or failed). First non-NULL decision wins and is immutable; enforced by the live poller upsert COALESCE, not by schema.';
