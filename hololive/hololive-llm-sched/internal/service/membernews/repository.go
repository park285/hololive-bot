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
	"log/slog"
	"sort"

	"github.com/kapu/hololive-llm-sched/internal/service/membernews/internal/model"
	"github.com/kapu/hololive-llm-sched/internal/service/subscription"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

var _ subscription.SubscriptionRepository[model.SubscribedRoom] = (*Repository)(nil)

const (
	memberNewsRoomsKey     = "membernews:rooms"
	memberNewsRoomNamesKey = "membernews:room_names"
)

type rowScanner interface {
	Scan(dest ...any) error
}

type rowsScanner interface {
	Close()
	Err() error
	Next() bool
	Scan(dest ...any) error
}

type memberNewsQuerier interface {
	Exec(ctx context.Context, sql string, arguments ...any) error
	Query(ctx context.Context, sql string, args ...any) (rowsScanner, error)
	QueryRow(ctx context.Context, sql string, args ...any) rowScanner
}

// Repository: member news 구독/후보 조회 저장소.
type Repository struct {
	pool  memberNewsQuerier
	cache cache.Client
	log   *slog.Logger
}

// NewRepository: 저장소 생성.
func NewRepository(postgres database.Client, cacheSvc cache.Client, logger *slog.Logger) *Repository {
	if logger == nil {
		logger = slog.Default()
	}
	var pool memberNewsQuerier
	if postgres != nil {
		pool = newPGXMemberNewsQuerier(postgres.GetPool())
	}
	return &Repository{
		pool:  pool,
		cache: cacheSvc,
		log:   logger,
	}
}

// Subscribe: 뉴스 알림 구독 등록/갱신.
func (r *Repository) Subscribe(ctx context.Context, roomID, roomName string) error {
	if r.pool == nil {
		return fmt.Errorf("membernews repository pool is nil")
	}

	query := `
		INSERT INTO member_news_subscriptions (room_id, room_name)
		VALUES ($1, $2)
		ON CONFLICT (room_id) DO UPDATE
		SET room_name = COALESCE(EXCLUDED.room_name, member_news_subscriptions.room_name),
		    updated_at = NOW()
	`

	if err := r.pool.Exec(ctx, query, roomID, roomName); err != nil {
		return fmt.Errorf("subscribe member news: %w", err)
	}

	r.writeThroughSubscribe(ctx, roomID, roomName)
	return nil
}

// Unsubscribe: 뉴스 알림 구독 해제.
func (r *Repository) Unsubscribe(ctx context.Context, roomID string) error {
	if r.pool == nil {
		return fmt.Errorf("membernews repository pool is nil")
	}

	query := `DELETE FROM member_news_subscriptions WHERE room_id = $1`
	if err := r.pool.Exec(ctx, query, roomID); err != nil {
		return fmt.Errorf("unsubscribe member news: %w", err)
	}

	r.writeThroughUnsubscribe(ctx, roomID)
	return nil
}

// IsSubscribed: 구독 여부 조회.
func (r *Repository) IsSubscribed(ctx context.Context, roomID string) (bool, error) {
	if r.pool == nil {
		return false, fmt.Errorf("membernews repository pool is nil")
	}

	query := `SELECT EXISTS(SELECT 1 FROM member_news_subscriptions WHERE room_id = $1)`
	var exists bool
	if err := r.pool.QueryRow(ctx, query, roomID).Scan(&exists); err != nil {
		return false, fmt.Errorf("is member news subscribed: %w", err)
	}
	return exists, nil
}

// ListSubscribedRooms: 구독 방 목록 조회(created_at 오름차순).
func (r *Repository) ListSubscribedRooms(ctx context.Context) ([]model.SubscribedRoom, error) {
	if r.pool == nil {
		return nil, fmt.Errorf("membernews repository pool is nil")
	}

	query := `
		SELECT id, room_id, COALESCE(room_name, ''), created_at
		FROM member_news_subscriptions
		ORDER BY created_at ASC
	`
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

// WarmupCacheFromDB: 부팅 시 DB 기준으로 Valkey set/hash를 재적재합니다.
func (r *Repository) WarmupCacheFromDB(ctx context.Context) error {
	rooms, err := r.ListSubscribedRooms(ctx)
	if err != nil {
		return fmt.Errorf("list subscribed rooms for warmup: %w", err)
	}

	if r.cache == nil {
		return nil
	}

	if err := r.cache.Del(ctx, memberNewsRoomsKey); err != nil {
		r.log.Warn("MemberNews warmup: failed to clear rooms set",
			slog.String("key", memberNewsRoomsKey),
			slog.String("error", err.Error()),
		)
	}
	if err := r.cache.Del(ctx, memberNewsRoomNamesKey); err != nil {
		r.log.Warn("MemberNews warmup: failed to clear room names hash",
			slog.String("key", memberNewsRoomNamesKey),
			slog.String("error", err.Error()),
		)
	}

	if len(rooms) == 0 {
		return nil
	}

	roomIDs := make([]string, 0, len(rooms))
	nameFields := make(map[string]any, len(rooms))
	for _, room := range rooms {
		roomIDs = append(roomIDs, room.RoomID)
		nameFields[room.RoomID] = room.RoomName
	}

	if _, err := r.cache.SAdd(ctx, memberNewsRoomsKey, roomIDs); err != nil {
		r.log.Warn("MemberNews warmup: failed to load rooms set",
			slog.Int("count", len(roomIDs)),
			slog.String("error", err.Error()),
		)
	}

	if err := r.cache.HMSet(ctx, memberNewsRoomNamesKey, nameFields); err != nil {
		r.log.Warn("MemberNews warmup: failed to load room names hash",
			slog.Int("count", len(nameFields)),
			slog.String("error", err.Error()),
		)
	}

	return nil
}

// GetRoomMembers: alarms 기반 room 구독 멤버 목록 조회.
func (r *Repository) GetRoomMembers(ctx context.Context, roomID string) ([]string, error) {
	if r.pool == nil {
		return nil, fmt.Errorf("membernews repository pool is nil")
	}

	query := `
		SELECT DISTINCT
			COALESCE(
				NULLIF(a.member_name, ''),
				NULLIF(m.korean_name, ''),
				NULLIF(m.english_name, ''),
				NULLIF(m.japanese_name, '')
			) AS member_name
		FROM alarms a
		LEFT JOIN members m ON m.channel_id = a.channel_id
		WHERE a.room_id = $1
		  AND COALESCE(
				NULLIF(a.member_name, ''),
				NULLIF(m.korean_name, ''),
				NULLIF(m.english_name, ''),
				NULLIF(m.japanese_name, '')
			) IS NOT NULL
		ORDER BY member_name ASC
	`

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

// ListActiveMajorEvents: major_events(active)에서 뉴스/행사 후보를 모두 읽습니다.
func (r *Repository) ListActiveMajorEvents(ctx context.Context) ([]model.Candidate, error) {
	if r.pool == nil {
		return nil, fmt.Errorf("membernews repository pool is nil")
	}

	query := `
		SELECT
			id,
			type,
			COALESCE(title, ''),
			COALESCE(description, ''),
			COALESCE(members, '{}'::text[]),
			pub_date,
			event_start_date,
			COALESCE(link, '')
		FROM major_events
		WHERE status = 'active'
		  AND type IN ('news', 'event')
		  AND COALESCE(link_status, 'unchecked') NOT IN ('failed', 'blocked')
	`

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

func (r *Repository) writeThroughSubscribe(ctx context.Context, roomID, roomName string) {
	if r.cache == nil {
		return
	}

	if _, err := r.cache.SAdd(ctx, memberNewsRoomsKey, []string{roomID}); err != nil {
		r.log.Warn("MemberNews subscribe write-through SADD failed",
			slog.String("room_id", roomID),
			slog.String("error", err.Error()),
		)
	}

	if err := r.cache.HSet(ctx, memberNewsRoomNamesKey, roomID, roomName); err != nil {
		r.log.Warn("MemberNews subscribe write-through HSET failed",
			slog.String("room_id", roomID),
			slog.String("error", err.Error()),
		)
	}
}

func (r *Repository) writeThroughUnsubscribe(ctx context.Context, roomID string) {
	if r.cache == nil {
		return
	}

	if _, err := r.cache.SRem(ctx, memberNewsRoomsKey, []string{roomID}); err != nil {
		r.log.Warn("MemberNews unsubscribe write-through SREM failed",
			slog.String("room_id", roomID),
			slog.String("error", err.Error()),
		)
	}

	if err := r.cache.HDel(ctx, memberNewsRoomNamesKey, roomID); err != nil {
		r.log.Warn("MemberNews unsubscribe write-through HDEL failed",
			slog.String("room_id", roomID),
			slog.String("error", err.Error()),
		)
	}
}

func domainMajorEventType(raw string) domain.MajorEventType {
	switch raw {
	case string(domain.MajorEventTypeNews):
		return domain.MajorEventTypeNews
	default:
		return domain.MajorEventTypeEvent
	}
}
