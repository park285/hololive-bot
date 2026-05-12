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
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/domain"
)

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

func (r *Repository) queryEvents(ctx context.Context, action string, query string, args ...any) ([]*domain.MajorEvent, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", action, err)
	}
	defer rows.Close()

	return r.scanEvents(rows)
}

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

	eventType, linkStatus := normalizeEventForUpsert(event)

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

func (r *Repository) GetEventsByDateRange(ctx context.Context, startDate, endDate time.Time, weekKey string) ([]*domain.MajorEvent, error) {
	query := majorEventSelectColumns + `
		WHERE status = $1
		  AND type IN ($2, $3)
		  AND COALESCE(event_start_date, (pub_date AT TIME ZONE 'UTC')::date) <= $5
		  AND COALESCE(event_end_date, event_start_date, (pub_date AT TIME ZONE 'UTC')::date) >= $4
		  AND (notified_week IS NULL OR notified_week != $6)
		ORDER BY event_start_date ASC
	`

	return r.queryEvents(
		ctx,
		"get events by date range",
		query,
		domain.MajorEventStatusActive,
		domain.MajorEventTypeEvent,
		domain.MajorEventTypeNews,
		startDate,
		endDate,
		weekKey,
	)
}

func (r *Repository) GetEventsByMonth(ctx context.Context, year, month int, monthKey string) ([]*domain.MajorEvent, error) {
	monthStart := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, -1)

	query := majorEventSelectColumns + `
		WHERE status = $1
		  AND type IN ($2, $3)
		  AND COALESCE(event_start_date, (pub_date AT TIME ZONE 'UTC')::date) <= $4
		  AND COALESCE(event_end_date, event_start_date, (pub_date AT TIME ZONE 'UTC')::date) >= $5
		  AND (notified_month IS NULL OR notified_month != $6)
		ORDER BY event_start_date ASC
	`

	return r.queryEvents(
		ctx,
		"get events by month",
		query,
		domain.MajorEventStatusActive,
		domain.MajorEventTypeEvent,
		domain.MajorEventTypeNews,
		monthEnd,
		monthStart,
		monthKey,
	)
}

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

func (r *Repository) GetAllActiveEvents(ctx context.Context) ([]*domain.MajorEvent, error) {
	query := majorEventSelectColumns + `
		WHERE status = $1
		ORDER BY event_start_date ASC
	`
	return r.queryEvents(ctx, "get all active events", query, domain.MajorEventStatusActive)
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
