package acl

import (
	"context"
	stdErrors "errors"
	"fmt"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type aclSettingStore interface {
	GetSetting(ctx context.Context, key string) (value string, found bool, err error)
	CreateSetting(ctx context.Context, key, value string) error
	UpsertSetting(ctx context.Context, key, value string) error
}

type aclRoomStore interface {
	ListRooms(ctx context.Context) ([]Room, error)
	CreateRoom(ctx context.Context, roomID, listType string) (created bool, err error)
	DeleteRoom(ctx context.Context, roomID, listType string) (deleted bool, err error)
}

type aclStore interface {
	aclSettingStore
	aclRoomStore
}

type pgxACLStore struct {
	pool *pgxpool.Pool
}

func newPgxACLStore(pool *pgxpool.Pool) *pgxACLStore {
	return &pgxACLStore{pool: pool}
}

func (s *pgxACLStore) GetSetting(ctx context.Context, key string) (value string, ok bool, err error) {
	var stored *string

	scanErr := pgxscan.Get(ctx, s.pool, &stored, getSettingSQL, key)
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
	if _, err := s.pool.Exec(ctx, createSettingSQL, key, value); err != nil {
		return fmt.Errorf("create acl setting %q: %w", key, err)
	}

	return nil
}

func (s *pgxACLStore) UpsertSetting(ctx context.Context, key, value string) error {
	if _, err := s.pool.Exec(ctx,
		upsertSettingSQL,
		key, value,
	); err != nil {
		return fmt.Errorf("upsert acl setting %q: %w", key, err)
	}

	return nil
}

func (s *pgxACLStore) ListRooms(ctx context.Context) ([]Room, error) {
	var rooms []Room

	if err := pgxscan.Select(ctx, s.pool, &rooms, listRoomsSQL); err != nil {
		return nil, fmt.Errorf("list acl rooms: %w", err)
	}

	return rooms, nil
}

func (s *pgxACLStore) CreateRoom(ctx context.Context, roomID, listType string) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		createRoomSQL,
		roomID, listType,
	)
	if err != nil {
		return false, fmt.Errorf("create acl room %q/%q: %w", roomID, listType, err)
	}

	return tag.RowsAffected() == 1, nil
}

func (s *pgxACLStore) DeleteRoom(ctx context.Context, roomID, listType string) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		deleteRoomSQL,
		roomID, listType,
	)
	if err != nil {
		return false, fmt.Errorf("delete acl room %q/%q: %w", roomID, listType, err)
	}

	return tag.RowsAffected() > 0, nil
}
