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
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

// Repository: 대형 행사 구독 데이터의 영속 저장소 (PostgreSQL)
type Repository struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

// NewRepository: 새로운 Repository를 생성합니다.
func NewRepository(postgres database.Client, logger *slog.Logger) *Repository {
	return &Repository{
		pool:   postgres.GetPool(),
		logger: logger,
	}
}

// Subscribe: 방의 대형 행사 알림을 구독합니다. 이미 구독 중이면 무시합니다.
func (r *Repository) Subscribe(ctx context.Context, roomID, roomName string) error {
	query := `
		INSERT INTO major_event_subscriptions (room_id, room_name)
		VALUES ($1, $2)
		ON CONFLICT (room_id) DO UPDATE
		SET room_name = COALESCE(EXCLUDED.room_name, major_event_subscriptions.room_name)
	`

	_, err := r.pool.Exec(ctx, query, roomID, roomName)
	if err != nil {
		return fmt.Errorf("subscribe major event: %w", err)
	}
	return nil
}

// Unsubscribe: 방의 대형 행사 알림 구독을 해제합니다.
func (r *Repository) Unsubscribe(ctx context.Context, roomID string) error {
	query := `DELETE FROM major_event_subscriptions WHERE room_id = $1`
	_, err := r.pool.Exec(ctx, query, roomID)
	if err != nil {
		return fmt.Errorf("unsubscribe major event: %w", err)
	}
	return nil
}

// IsSubscribed: 방이 대형 행사 알림을 구독 중인지 확인합니다.
func (r *Repository) IsSubscribed(ctx context.Context, roomID string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM major_event_subscriptions WHERE room_id = $1)`

	var exists bool
	err := r.pool.QueryRow(ctx, query, roomID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check subscription: %w", err)
	}
	return exists, nil
}

// GetSubscribedRooms: 구독 중인 모든 방 목록을 조회합니다.
func (r *Repository) GetSubscribedRooms(ctx context.Context) ([]*domain.EventRoomSubscription, error) {
	query := `
		SELECT id, room_id, COALESCE(room_name, '') as room_name, created_at
		FROM major_event_subscriptions
		ORDER BY created_at ASC
	`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get subscribed rooms: %w", err)
	}
	defer rows.Close()

	return r.scanSubscriptions(rows)
}

func (r *Repository) scanSubscriptions(rows pgx.Rows) ([]*domain.EventRoomSubscription, error) {
	var subscriptions []*domain.EventRoomSubscription

	for rows.Next() {
		var sub domain.EventRoomSubscription
		err := rows.Scan(&sub.ID, &sub.RoomID, &sub.RoomName, &sub.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		subscriptions = append(subscriptions, &sub)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}

	return subscriptions, nil
}

// CreateTable: 테이블이 없으면 생성합니다. (마이그레이션용)
func (r *Repository) CreateTable(ctx context.Context) error {
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

// CreateEventsTable: major_events 테이블을 생성합니다. (마이그레이션용)
func (r *Repository) CreateEventsTable(ctx context.Context) error {
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

	if err := r.migrateTypeColumn(ctx); err != nil {
		return fmt.Errorf("migrate type column: %w", err)
	}

	if err := r.migrateNotifiedMonthColumn(ctx); err != nil {
		return fmt.Errorf("migrate notified_month column: %w", err)
	}

	if err := r.migrateLinkCheckColumns(ctx); err != nil {
		return fmt.Errorf("migrate link check columns: %w", err)
	}

	if err := r.createIndexes(ctx); err != nil {
		return fmt.Errorf("create indexes: %w", err)
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

// GetRecentExternalIDs: 최근 저장된 이벤트 external_id 목록과 최신 pub_date를 조회합니다.
func (r *Repository) GetRecentExternalIDs(ctx context.Context, eventType domain.MajorEventType, limit int) ([]string, *time.Time, error) {
	if limit <= 0 {
		limit = 1
	}

	query := `
		SELECT external_id, pub_date
		FROM major_events
		WHERE type = $1
		ORDER BY pub_date DESC NULLS LAST, updated_at DESC
		LIMIT $2
	`

	rows, err := r.pool.Query(ctx, query, eventType, limit)
	if err != nil {
		return nil, nil, fmt.Errorf("get recent external IDs: %w", err)
	}
	defer rows.Close()

	externalIDs := make([]string, 0, limit)
	var latestPubDate *time.Time

	for rows.Next() {
		var externalID string
		var pubDate *time.Time
		if scanErr := rows.Scan(&externalID, &pubDate); scanErr != nil {
			return nil, nil, fmt.Errorf("scan recent external ID: %w", scanErr)
		}
		if externalID != "" {
			externalIDs = append(externalIDs, externalID)
		}
		if latestPubDate == nil && pubDate != nil {
			normalized := pubDate.UTC()
			latestPubDate = &normalized
		}
	}

	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate recent external IDs: %w", err)
	}

	return externalIDs, latestPubDate, nil
}

// UpsertEvent: 이벤트를 삽입하거나 업데이트합니다. (external_id 기준)
func (r *Repository) UpsertEvent(ctx context.Context, event *domain.MajorEvent) error {
	query := `
		INSERT INTO major_events (external_id, type, title, link, description, members, pub_date, event_start_date, event_end_date, status, link_status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	ON CONFLICT (external_id) DO UPDATE
	SET title = EXCLUDED.title,
		link = EXCLUDED.link,
		description = EXCLUDED.description,
		members = EXCLUDED.members,
		pub_date = EXCLUDED.pub_date,
		event_start_date = EXCLUDED.event_start_date,
		event_end_date = EXCLUDED.event_end_date,
		type = EXCLUDED.type,
		status = CASE
			WHEN major_events.status = 'canceled' THEN major_events.status
			WHEN major_events.status = 'ended' AND EXCLUDED.event_start_date >= CURRENT_DATE THEN 'active'
			ELSE major_events.status
		END,
		link_status = CASE
			WHEN major_events.link IS DISTINCT FROM EXCLUDED.link THEN 'unchecked'
			ELSE major_events.link_status
		END,
		link_checked_at = CASE
			WHEN major_events.link IS DISTINCT FROM EXCLUDED.link THEN NULL
			ELSE major_events.link_checked_at
		END,
		updated_at = NOW()
	RETURNING id
	`

	eventType := event.Type
	if eventType == "" {
		eventType = domain.MajorEventTypeEvent
	}
	linkStatus := event.LinkStatus
	if linkStatus == "" {
		linkStatus = domain.MajorEventLinkStatusUnchecked
	}

	err := r.pool.QueryRow(ctx, query,
		event.ExternalID,
		eventType,
		event.Title,
		event.Link,
		event.Description,
		event.Members,
		event.PubDate,
		event.EventStartDate,
		event.EventEndDate,
		event.Status,
		linkStatus,
	).Scan(&event.ID)
	if err != nil {
		return fmt.Errorf("upsert event: %w", err)
	}
	return nil
}

// GetEventsByDateRange: 날짜 범위 내의 활성 행사를 조회합니다. (event + news, 주간 중복 방지)
func (r *Repository) GetEventsByDateRange(ctx context.Context, startDate, endDate time.Time, weekKey string) ([]*domain.MajorEvent, error) {
	query := `
		SELECT id, external_id, type, title, link, COALESCE(description, '') as description, COALESCE(members, '{}') as members, pub_date,
			   event_start_date, event_end_date, status, COALESCE(link_status, 'unchecked') as link_status, link_checked_at, notified_at, COALESCE(notified_week, '') as notified_week,
			   COALESCE(notified_month, '') as notified_month, created_at, updated_at
		FROM major_events
		WHERE status = $1
		  AND type IN ($2, $3)
		  AND COALESCE(event_start_date, (pub_date AT TIME ZONE 'UTC')::date) <= $5
		  AND COALESCE(event_end_date, event_start_date, (pub_date AT TIME ZONE 'UTC')::date) >= $4
		  AND (notified_week IS NULL OR notified_week != $6)
		ORDER BY event_start_date ASC
	`

	rows, err := r.pool.Query(
		ctx,
		query,
		domain.MajorEventStatusActive,
		domain.MajorEventTypeEvent,
		domain.MajorEventTypeNews,
		startDate,
		endDate,
		weekKey,
	)
	if err != nil {
		return nil, fmt.Errorf("get events by date range: %w", err)
	}
	defer rows.Close()

	return r.scanEvents(rows)
}

// GetEventsByMonth: 특정 월에 걸친 활성 행사를 조회합니다. (event + news, 월간 중복 방지)
func (r *Repository) GetEventsByMonth(ctx context.Context, year, month int, monthKey string) ([]*domain.MajorEvent, error) {
	monthStart := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, -1)

	query := `
		SELECT id, external_id, type, title, link, COALESCE(description, '') as description, COALESCE(members, '{}') as members, pub_date,
			   event_start_date, event_end_date, status, COALESCE(link_status, 'unchecked') as link_status, link_checked_at, notified_at, COALESCE(notified_week, '') as notified_week,
			   COALESCE(notified_month, '') as notified_month, created_at, updated_at
		FROM major_events
		WHERE status = $1
		  AND type IN ($2, $3)
		  AND COALESCE(event_start_date, (pub_date AT TIME ZONE 'UTC')::date) <= $4
		  AND COALESCE(event_end_date, event_start_date, (pub_date AT TIME ZONE 'UTC')::date) >= $5
		  AND (notified_month IS NULL OR notified_month != $6)
		ORDER BY event_start_date ASC
	`

	rows, err := r.pool.Query(
		ctx,
		query,
		domain.MajorEventStatusActive,
		domain.MajorEventTypeEvent,
		domain.MajorEventTypeNews,
		monthEnd,
		monthStart,
		monthKey,
	)
	if err != nil {
		return nil, fmt.Errorf("get events by month: %w", err)
	}
	defer rows.Close()

	return r.scanEvents(rows)
}

// MarkEventsAsMonthlyNotified: 이벤트들을 월간 알림 발송 완료로 표시합니다.
func (r *Repository) MarkEventsAsMonthlyNotified(ctx context.Context, eventIDs []int, monthKey string) error {
	if len(eventIDs) == 0 {
		return nil
	}

	query := `
		UPDATE major_events
		SET notified_month = $1,
			updated_at = NOW()
		WHERE id = ANY($2)
	`

	_, err := r.pool.Exec(ctx, query, monthKey, eventIDs)
	if err != nil {
		return fmt.Errorf("mark events as monthly notified: %w", err)
	}
	return nil
}

// MarkEventsAsNotified: 이벤트들을 알림 발송 완료로 표시합니다.
func (r *Repository) MarkEventsAsNotified(ctx context.Context, eventIDs []int, weekKey string) error {
	if len(eventIDs) == 0 {
		return nil
	}

	query := `
		UPDATE major_events
		SET notified_at = NOW(),
			notified_week = $1,
			updated_at = NOW()
		WHERE id = ANY($2)
	`

	_, err := r.pool.Exec(ctx, query, weekKey, eventIDs)
	if err != nil {
		return fmt.Errorf("mark events as notified: %w", err)
	}
	return nil
}

// UpdateExpiredEvents: 종료된 이벤트의 상태를 업데이트합니다.
func (r *Repository) UpdateExpiredEvents(ctx context.Context) (int64, error) {
	query := `
		UPDATE major_events
		SET status = $1,
			updated_at = NOW()
		WHERE status = $2
		  AND (
			event_end_date < CURRENT_DATE
			OR (event_end_date IS NULL AND event_start_date < CURRENT_DATE)
		  )
	`

	result, err := r.pool.Exec(ctx, query, domain.MajorEventStatusEnded, domain.MajorEventStatusActive)
	if err != nil {
		return 0, fmt.Errorf("update expired events: %w", err)
	}
	return result.RowsAffected(), nil
}

// GetAllActiveEvents: 모든 활성 이벤트를 조회합니다.
func (r *Repository) GetAllActiveEvents(ctx context.Context) ([]*domain.MajorEvent, error) {
	query := `
		SELECT id, external_id, type, title, link, COALESCE(description, '') as description, COALESCE(members, '{}') as members, pub_date,
			   event_start_date, event_end_date, status, COALESCE(link_status, 'unchecked') as link_status, link_checked_at, notified_at, COALESCE(notified_week, '') as notified_week,
			   COALESCE(notified_month, '') as notified_month, created_at, updated_at
		FROM major_events
		WHERE status = $1
		ORDER BY event_start_date ASC
	`

	rows, err := r.pool.Query(ctx, query, domain.MajorEventStatusActive)
	if err != nil {
		return nil, fmt.Errorf("get all active events: %w", err)
	}
	defer rows.Close()

	return r.scanEvents(rows)
}

func (r *Repository) scanEvents(rows pgx.Rows) ([]*domain.MajorEvent, error) {
	var events []*domain.MajorEvent

	for rows.Next() {
		var event domain.MajorEvent
		err := rows.Scan(
			&event.ID,
			&event.ExternalID,
			&event.Type,
			&event.Title,
			&event.Link,
			&event.Description,
			&event.Members,
			&event.PubDate,
			&event.EventStartDate,
			&event.EventEndDate,
			&event.Status,
			&event.LinkStatus,
			&event.LinkCheckedAt,
			&event.NotifiedAt,
			&event.NotifiedWeek,
			&event.NotifiedMonth,
			&event.CreatedAt,
			&event.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		events = append(events, &event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}

	return events, nil
}
