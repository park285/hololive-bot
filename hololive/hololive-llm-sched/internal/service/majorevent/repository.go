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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

type Repository struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

const majorEventSelectColumns = `
		SELECT id, external_id, type, title, link, COALESCE(description, '') as description, COALESCE(members, '{}') as members, pub_date,
			   event_start_date, event_end_date, status, COALESCE(link_status, 'unchecked') as link_status, link_checked_at, notified_at, COALESCE(notified_week, '') as notified_week,
			   COALESCE(notified_month, '') as notified_month, created_at, updated_at
		FROM major_events
	`

func (r *Repository) requirePool(action string) error {
	if r == nil || r.pool == nil {
		return fmt.Errorf("%s: postgres pool not configured", action)
	}
	return nil
}

func NewRepository(postgres database.Client, logger *slog.Logger) *Repository {
	return &Repository{
		pool:   postgres.GetPool(),
		logger: logger,
	}
}

func normalizeEventForUpsert(event *domain.MajorEvent) (domain.MajorEventType, domain.MajorEventLinkStatus) {
	eventType := event.Type
	if eventType == "" {
		eventType = domain.MajorEventTypeEvent
	}

	linkStatus := event.LinkStatus
	if linkStatus == "" {
		linkStatus = domain.MajorEventLinkStatusUnchecked
	}

	return eventType, linkStatus
}

func (r *Repository) Subscribe(ctx context.Context, roomID, roomName string) error {
	if err := r.requirePool("subscribe major event"); err != nil {
		return err
	}

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

func (r *Repository) Unsubscribe(ctx context.Context, roomID string) error {
	if err := r.requirePool("unsubscribe major event"); err != nil {
		return err
	}

	query := `DELETE FROM major_event_subscriptions WHERE room_id = $1`
	_, err := r.pool.Exec(ctx, query, roomID)
	if err != nil {
		return fmt.Errorf("unsubscribe major event: %w", err)
	}
	return nil
}

func (r *Repository) IsSubscribed(ctx context.Context, roomID string) (bool, error) {
	if err := r.requirePool("check subscription"); err != nil {
		return false, err
	}

	query := `SELECT EXISTS(SELECT 1 FROM major_event_subscriptions WHERE room_id = $1)`

	var exists bool
	err := r.pool.QueryRow(ctx, query, roomID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check subscription: %w", err)
	}
	return exists, nil
}

func (r *Repository) GetSubscribedRooms(ctx context.Context) ([]*domain.EventRoomSubscription, error) {
	if err := r.requirePool("get subscribed rooms"); err != nil {
		return nil, err
	}

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

func (r *Repository) applyEventSchemaMigrations(ctx context.Context) error {
	migrations := []struct {
		name string
		run  func(context.Context) error
	}{
		{name: "migrate type column", run: r.migrateTypeColumn},
		{name: "migrate notified_month column", run: r.migrateNotifiedMonthColumn},
		{name: "migrate link check columns", run: r.migrateLinkCheckColumns},
		{name: "create indexes", run: r.createIndexes},
	}

	for _, migration := range migrations {
		if err := migration.run(ctx); err != nil {
			return fmt.Errorf("%s: %w", migration.name, err)
		}
	}

	return nil
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
