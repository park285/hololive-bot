// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package majorevent

import (
	"context"
	"fmt"
)

func (r *Repository) CreateTable(ctx context.Context) error {
	if err := r.requirePool("create major_event_subscriptions table"); err != nil {
		return err
	}

	query := `
		CREATE TABLE IF NOT EXISTS major_event_subscriptions (
			id SERIAL PRIMARY KEY,
			room_id VARCHAR(255) UNIQUE NOT NULL,
			room_name VARCHAR(255),
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		)
	`

	_, err := r.pool.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("create major_event_subscriptions table: %w", err)
	}
	return nil
}

func (r *Repository) CreateEventsTable(ctx context.Context) error {
	if err := r.requirePool("create major_events table"); err != nil {
		return err
	}

	createTableQuery := `
		CREATE TABLE IF NOT EXISTS major_events (
			id SERIAL PRIMARY KEY,
			external_id VARCHAR(500) UNIQUE NOT NULL,
			type VARCHAR(20) DEFAULT 'event',
			title VARCHAR(500) NOT NULL,
			link VARCHAR(1000) NOT NULL,
			description TEXT,
			members TEXT[],
			pub_date TIMESTAMPTZ,
			event_start_date DATE,
			event_end_date DATE,
			status VARCHAR(50) DEFAULT 'active',
			link_status VARCHAR(20) DEFAULT 'unchecked',
			link_checked_at TIMESTAMPTZ,
			notified_at TIMESTAMPTZ,
			notified_week VARCHAR(10),
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);
	`

	_, err := r.pool.Exec(ctx, createTableQuery)
	if err != nil {
		return fmt.Errorf("create major_events table: %w", err)
	}

	if err := r.applyEventSchemaMigrations(ctx); err != nil {
		return fmt.Errorf("apply event schema migrations: %w", err)
	}

	return nil
}

func (r *Repository) migrateTypeColumn(ctx context.Context) error {
	addColumnQuery := `
		DO $$
		BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'major_events' AND column_name = 'type'
			) THEN
				ALTER TABLE major_events ADD COLUMN type VARCHAR(20) DEFAULT 'event';
			END IF;
		END $$;
	`
	if _, err := r.pool.Exec(ctx, addColumnQuery); err != nil {
		return fmt.Errorf("add type column: %w", err)
	}

	// Backfill NULL values
	backfillQuery := `UPDATE major_events SET type = 'event' WHERE type IS NULL`
	if _, err := r.pool.Exec(ctx, backfillQuery); err != nil {
		return fmt.Errorf("backfill type column: %w", err)
	}

	// Set NOT NULL constraint
	setNotNullQuery := `
		DO $$
		BEGIN
			IF EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'major_events' AND column_name = 'type' AND is_nullable = 'YES'
			) THEN
				ALTER TABLE major_events ALTER COLUMN type SET NOT NULL;
			END IF;
		END $$;
	`
	if _, err := r.pool.Exec(ctx, setNotNullQuery); err != nil {
		return fmt.Errorf("set type not null: %w", err)
	}

	return nil
}

func (r *Repository) migrateNotifiedMonthColumn(ctx context.Context) error {
	query := `
		DO $$
		BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'major_events' AND column_name = 'notified_month'
			) THEN
				ALTER TABLE major_events ADD COLUMN notified_month VARCHAR(10);
			END IF;
		END $$;
	`
	if _, err := r.pool.Exec(ctx, query); err != nil {
		return fmt.Errorf("add notified_month column: %w", err)
	}
	return nil
}

func (r *Repository) migrateLinkCheckColumns(ctx context.Context) error {
	addColumnsQuery := `
		DO $$
		BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'major_events' AND column_name = 'link_status'
			) THEN
				ALTER TABLE major_events ADD COLUMN link_status VARCHAR(20) DEFAULT 'unchecked';
			END IF;

			IF NOT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'major_events' AND column_name = 'link_checked_at'
			) THEN
				ALTER TABLE major_events ADD COLUMN link_checked_at TIMESTAMPTZ;
			END IF;
		END $$;
	`
	if _, err := r.pool.Exec(ctx, addColumnsQuery); err != nil {
		return fmt.Errorf("add link check columns: %w", err)
	}

	if _, err := r.pool.Exec(ctx, `UPDATE major_events SET link_status = 'unchecked' WHERE link_status IS NULL`); err != nil {
		return fmt.Errorf("backfill link_status: %w", err)
	}

	setDefaultAndNotNullQuery := `
		ALTER TABLE major_events
			ALTER COLUMN link_status SET DEFAULT 'unchecked',
			ALTER COLUMN link_status SET NOT NULL;
	`
	if _, err := r.pool.Exec(ctx, setDefaultAndNotNullQuery); err != nil {
		return fmt.Errorf("set link_status constraints: %w", err)
	}

	return nil
}

func (r *Repository) createIndexes(ctx context.Context) error {
	query := `
		CREATE INDEX IF NOT EXISTS idx_major_events_start_date ON major_events(event_start_date);
		CREATE INDEX IF NOT EXISTS idx_major_events_status ON major_events(status);
		CREATE INDEX IF NOT EXISTS idx_major_events_notified ON major_events(notified_week);
		CREATE INDEX IF NOT EXISTS idx_major_events_type ON major_events(type);
		CREATE INDEX IF NOT EXISTS idx_major_events_notified_month ON major_events(notified_month);
		CREATE INDEX IF NOT EXISTS idx_major_events_link_status ON major_events(link_status);
		CREATE INDEX IF NOT EXISTS idx_major_events_link_checked_at ON major_events(link_checked_at);
	`
	_, err := r.pool.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("create indexes: %w", err)
	}
	return nil
}
