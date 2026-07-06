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

package membernews

import (
	"context"
	"fmt"
	"sort"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/internal/model"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func (r *Repository) IsSubscribed(ctx context.Context, roomID string) (bool, error) {
	if r.pool == nil {
		return false, fmt.Errorf("membernews repository pool is nil")
	}

	query := mustSQL("repository_query_0037_01.sql")
	var exists bool
	if err := r.pool.QueryRow(ctx, query, roomID).Scan(&exists); err != nil {
		return false, fmt.Errorf("is member news subscribed: %w", err)
	}
	return exists, nil
}

func (r *Repository) ListSubscribedRooms(ctx context.Context) ([]model.SubscribedRoom, error) {
	if r.pool == nil {
		return nil, fmt.Errorf("membernews repository pool is nil")
	}

	query := mustSQL("repository_query_0050_02.sql")
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list member news rooms: %w", err)
	}
	defer rows.Close()

	rooms := make([]model.SubscribedRoom, 0)
	for rows.Next() {
		var room model.SubscribedRoom
		if scanErr := rows.Scan(&room.ID, &room.RoomID, &room.RoomName, &room.CreatedAt); scanErr != nil {
			return nil, fmt.Errorf("scan member news room: %w", scanErr)
		}
		rooms = append(rooms, room)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterate member news rooms: %w", rowsErr)
	}
	return rooms, nil
}

func (r *Repository) GetRoomMembers(ctx context.Context, roomID string) ([]string, error) {
	if r.pool == nil {
		return nil, fmt.Errorf("membernews repository pool is nil")
	}

	query := mustSQL("repository_query_0080_03.sql")

	rows, err := r.pool.Query(ctx, query, roomID)
	if err != nil {
		return nil, fmt.Errorf("get room members: %w", err)
	}
	defer rows.Close()

	members := make([]string, 0)
	for rows.Next() {
		var memberName string
		if scanErr := rows.Scan(&memberName); scanErr != nil {
			return nil, fmt.Errorf("scan room member: %w", scanErr)
		}
		if memberName != "" {
			members = append(members, memberName)
		}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterate room members: %w", rowsErr)
	}

	sort.Strings(members)
	return members, nil
}

func (r *Repository) ListActiveMajorEvents(ctx context.Context) ([]model.Candidate, error) {
	if r.pool == nil {
		return nil, fmt.Errorf("membernews repository pool is nil")
	}

	query := mustSQL("repository_query_0129_04.sql")

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list active major events: %w", err)
	}
	defer rows.Close()

	result := make([]model.Candidate, 0)
	for rows.Next() {
		var (
			candidate model.Candidate
			eventType string
			members   []string
		)

		if scanErr := rows.Scan(
			&candidate.ID,
			&eventType,
			&candidate.Title,
			&candidate.Description,
			&members,
			&candidate.PubDate,
			&candidate.EventStartDate,
			&candidate.SourceURL,
		); scanErr != nil {
			return nil, fmt.Errorf("scan active major event: %w", scanErr)
		}

		candidate.Type = domainMajorEventType(eventType)
		candidate.Members = members
		result = append(result, candidate)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterate active major events: %w", rowsErr)
	}

	return result, nil
}

func domainMajorEventType(raw string) domain.MajorEventType {
	switch raw {
	case string(domain.MajorEventTypeNews):
		return domain.MajorEventTypeNews
	default:
		return domain.MajorEventTypeEvent
	}
}
