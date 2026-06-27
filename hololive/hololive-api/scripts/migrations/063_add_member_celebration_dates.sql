ALTER TABLE members ADD COLUMN IF NOT EXISTS birthday DATE;
ALTER TABLE members ADD COLUMN IF NOT EXISTS debut_date DATE;

CREATE INDEX IF NOT EXISTS idx_members_birthday_month_day
    ON members (EXTRACT(MONTH FROM birthday), EXTRACT(DAY FROM birthday))
    WHERE birthday IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_members_debut_date_month_day
    ON members (EXTRACT(MONTH FROM debut_date), EXTRACT(DAY FROM debut_date))
    WHERE debut_date IS NOT NULL;

ALTER TYPE alarm_type ADD VALUE IF NOT EXISTS 'BIRTHDAY';
ALTER TYPE alarm_type ADD VALUE IF NOT EXISTS 'ANNIVERSARY';
