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

package acl

import (
	"context"
	stdErrors "errors"
	"fmt"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// aclStore는 ACL 영속 저장소 연산을 추상화한다.
//
// 직접 DB 호출을 메서드로 추출하여
// Service가 구현체(pgx)에 의존하지 않고 인터페이스에 의존하도록 한다.
type aclStore interface {
	// GetSetting은 key에 해당하는 설정 값을 반환한다. 없으면 found=false.
	GetSetting(ctx context.Context, key string) (value string, found bool, err error)
	// CreateSetting은 새 설정 행을 삽입한다.
	CreateSetting(ctx context.Context, key, value string) error
	// UpsertSetting은 key 기준으로 값을 삽입하거나 갱신한다.
	UpsertSetting(ctx context.Context, key, value string) error
	// ListRooms는 모든 ACL 방 행을 반환한다.
	ListRooms(ctx context.Context) ([]Room, error)
	// CreateRoom은 (roomID, listType) 방 행을 삽입한다.
	CreateRoom(ctx context.Context, roomID, listType string) error
	// DeleteRoom은 (roomID, listType) 방 행을 삭제한다.
	DeleteRoom(ctx context.Context, roomID, listType string) error
	// CountRooms는 roomID(+ 선택적 listType)에 해당하는 행 수를 센다.
	// listType이 빈 문자열이면 listType 조건 없이 roomID만으로 센다.
	CountRooms(ctx context.Context, roomID, listType string) (int64, error)
}

// pgxACLStore는 *pgxpool.Pool + pgxscan 기반 aclStore 구현체다.
type pgxACLStore struct {
	pool *pgxpool.Pool
}

func newPgxACLStore(pool *pgxpool.Pool) *pgxACLStore {
	return &pgxACLStore{pool: pool}
}

func (s *pgxACLStore) GetSetting(ctx context.Context, key string) (value string, ok bool, err error) {
	var stored *string

	scanErr := pgxscan.Get(ctx, s.pool, &stored, `SELECT value FROM acl_settings WHERE key = $1`, key)
	if scanErr != nil {
		if stdErrors.Is(scanErr, pgx.ErrNoRows) {
			return "", false, nil
		}

		return "", false, fmt.Errorf("get acl setting %q: %w", key, scanErr)
	}

	if stored == nil {
		return "", true, nil
	}

	return *stored, true, nil
}

func (s *pgxACLStore) CreateSetting(ctx context.Context, key, value string) error {
	if _, err := s.pool.Exec(ctx, `INSERT INTO acl_settings (key, value) VALUES ($1, $2)`, key, value); err != nil {
		return fmt.Errorf("create acl setting %q: %w", key, err)
	}

	return nil
}

func (s *pgxACLStore) UpsertSetting(ctx context.Context, key, value string) error {
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO acl_settings (key, value) VALUES ($1, $2)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`,
		key, value,
	); err != nil {
		return fmt.Errorf("upsert acl setting %q: %w", key, err)
	}

	return nil
}

func (s *pgxACLStore) ListRooms(ctx context.Context) ([]Room, error) {
	var rooms []Room

	if err := pgxscan.Select(ctx, s.pool, &rooms, `SELECT id, room_id, list_type FROM acl_rooms`); err != nil {
		return nil, fmt.Errorf("list acl rooms: %w", err)
	}

	return rooms, nil
}

func (s *pgxACLStore) CreateRoom(ctx context.Context, roomID, listType string) error {
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO acl_rooms (room_id, list_type) VALUES ($1, $2)`,
		roomID, listType,
	); err != nil {
		return fmt.Errorf("create acl room %q/%q: %w", roomID, listType, err)
	}

	return nil
}

func (s *pgxACLStore) DeleteRoom(ctx context.Context, roomID, listType string) error {
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM acl_rooms WHERE room_id = $1 AND list_type = $2`,
		roomID, listType,
	); err != nil {
		return fmt.Errorf("delete acl room %q/%q: %w", roomID, listType, err)
	}

	return nil
}

func (s *pgxACLStore) CountRooms(ctx context.Context, roomID, listType string) (int64, error) {
	var (
		count int64
		err   error
	)

	if listType == "" {
		err = s.pool.QueryRow(ctx, `SELECT count(*) FROM acl_rooms WHERE room_id = $1`, roomID).Scan(&count)
	} else {
		err = s.pool.QueryRow(ctx, `SELECT count(*) FROM acl_rooms WHERE room_id = $1 AND list_type = $2`, roomID, listType).Scan(&count)
	}

	if err != nil {
		return 0, fmt.Errorf("count acl rooms %q/%q: %w", roomID, listType, err)
	}

	return count, nil
}
