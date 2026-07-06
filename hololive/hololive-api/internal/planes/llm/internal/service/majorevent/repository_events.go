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

	query := mustSQL("repository_events_0038_01.sql")

	rows, err := r.pool.Query(ctx, query, eventType, limit)
	if err != nil {
		return nil, nil, fmt.Errorf("get recent external IDs: %w", err)
	}
	defer rows.Close()

	return scanRecentExternalIDs(rows, limit)
}

func scanRecentExternalIDs(rows pgx.Rows, limit int) ([]string, *time.Time, error) {
	externalIDs := make([]string, 0, limit)
	var latestPubDate *time.Time

	for rows.Next() {
		externalID, pubDate, err := scanRecentExternalIDRow(rows)
		if err != nil {
			return nil, nil, err
		}
		if externalID != "" {
			externalIDs = append(externalIDs, externalID)
		}
		latestPubDate = firstRecentPubDate(latestPubDate, pubDate)
	}

	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate recent external IDs: %w", err)
	}

	return externalIDs, latestPubDate, nil
}

func scanRecentExternalIDRow(rows pgx.Rows) (string, *time.Time, error) {
	var externalID string
	var pubDate *time.Time
	if err := rows.Scan(&externalID, &pubDate); err != nil {
		return "", nil, fmt.Errorf("scan recent external ID: %w", err)
	}
	return externalID, pubDate, nil
}

func firstRecentPubDate(current, candidate *time.Time) *time.Time {
	if current != nil || candidate == nil {
		return current
	}
	normalized := candidate.UTC()
	return &normalized
}

func (r *Repository) queryEvents(ctx context.Context, action, query string, args ...any) ([]*domain.MajorEvent, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", action, err)
	}
	defer rows.Close()

	return r.scanEvents(rows)
}

func (r *Repository) UpsertEvent(ctx context.Context, event *domain.MajorEvent) error {
	query := mustSQL("repository_events_0105_02.sql")

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

	query := mustSQL("repository_events_0209_03.sql")

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

	query := mustSQL("repository_events_0228_04.sql")

	_, err := r.pool.Exec(ctx, query, weekKey, eventIDs)
	if err != nil {
		return fmt.Errorf("mark events as notified: %w", err)
	}
	return nil
}

func (r *Repository) UpdateExpiredEvents(ctx context.Context) (int64, error) {
	query := mustSQL("repository_events_0244_05.sql")

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
