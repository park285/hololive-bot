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
	"sync"

	"github.com/jackc/pgx/v5/pgconn"
)

// pgUniqueViolation은 acl_settings.key / acl_rooms(room_id,list_type)의 UNIQUE 제약
// 위반을 실 PG와 동일하게(SQLSTATE 23505, *pgconn.PgError) 모사한다.
func pgUniqueViolation(constraint string) error {
	return &pgconn.PgError{
		Code:           "23505",
		Severity:       "ERROR",
		Message:        "duplicate key value violates unique constraint " + constraint,
		ConstraintName: constraint,
	}
}

// fakeACLStore는 aclStore 인메모리 구현체다.
//
// pgx에는 저장소 콜백 훅이 없으므로, 기존 fault-injection 테스트를
// 이 fake의 메서드별 hook으로 동등 재현한다. hook은 연산 직전에 호출되며
// 에러를 반환하면 해당 store 연산이 실패한다. hook이 nil이면 정상 동작한다.
type fakeACLStore struct {
	mu       sync.Mutex
	settings map[string]string
	rooms    map[roomKey]struct{}

	// hook: 연산 직전 호출. 에러 반환 시 연산 실패. afterCreateRoom/afterDeleteRoom은
	// 성공적 변경 직후 호출되어 mode 전환 등 부작용 재현에 쓰인다.
	getSettingHook    func(key string) error
	createSettingHook func(key, value string) error
	upsertHook        func(key, value string) error
	listRoomsHook     func() error
	createRoomHook    func(roomID, listType string) error
	afterCreateRoom   func(roomID, listType string)
	deleteRoomHook    func(roomID, listType string) error
	afterDeleteRoom   func(roomID, listType string)
	countRoomsHook    func(roomID, listType string) error
}

type roomKey struct {
	roomID   string
	listType string
}

func newFakeACLStore() *fakeACLStore {
	return &fakeACLStore{
		settings: make(map[string]string),
		rooms:    make(map[roomKey]struct{}),
	}
}

func (f *fakeACLStore) GetSetting(_ context.Context, key string) (value0 string, ok1 bool, err error) {
	if f.getSettingHook != nil {
		if err := f.getSettingHook(key); err != nil {
			return "", false, err
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	value, ok := f.settings[key]
	return value, ok, nil
}

func (f *fakeACLStore) CreateSetting(_ context.Context, key, value string) error {
	if f.createSettingHook != nil {
		if err := f.createSettingHook(key, value); err != nil {
			return err
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if _, exists := f.settings[key]; exists {
		return pgUniqueViolation("acl_settings_key_key")
	}

	f.settings[key] = value
	return nil
}

func (f *fakeACLStore) UpsertSetting(_ context.Context, key, value string) error {
	if f.upsertHook != nil {
		if err := f.upsertHook(key, value); err != nil {
			return err
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.settings[key] = value
	return nil
}

func (f *fakeACLStore) ListRooms(_ context.Context) ([]Room, error) {
	if f.listRoomsHook != nil {
		if err := f.listRoomsHook(); err != nil {
			return nil, err
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	rooms := make([]Room, 0, len(f.rooms))
	for k := range f.rooms {
		rooms = append(rooms, Room{RoomID: k.roomID, ListType: k.listType})
	}

	return rooms, nil
}

func (f *fakeACLStore) CreateRoom(_ context.Context, roomID, listType string) error {
	if f.createRoomHook != nil {
		if err := f.createRoomHook(roomID, listType); err != nil {
			return err
		}
	}

	f.mu.Lock()
	key := roomKey{roomID: roomID, listType: listType}
	if _, exists := f.rooms[key]; exists {
		f.mu.Unlock()
		return pgUniqueViolation("idx_room_list")
	}

	f.rooms[key] = struct{}{}
	f.mu.Unlock()

	if f.afterCreateRoom != nil {
		f.afterCreateRoom(roomID, listType)
	}

	return nil
}

func (f *fakeACLStore) DeleteRoom(_ context.Context, roomID, listType string) error {
	if f.deleteRoomHook != nil {
		if err := f.deleteRoomHook(roomID, listType); err != nil {
			return err
		}
	}

	f.mu.Lock()
	delete(f.rooms, roomKey{roomID: roomID, listType: listType})
	f.mu.Unlock()

	if f.afterDeleteRoom != nil {
		f.afterDeleteRoom(roomID, listType)
	}

	return nil
}

func (f *fakeACLStore) CountRooms(_ context.Context, roomID, listType string) (int64, error) {
	if f.countRoomsHook != nil {
		if err := f.countRoomsHook(roomID, listType); err != nil {
			return 0, err
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	var count int64
	for k := range f.rooms {
		if k.roomID != roomID {
			continue
		}

		if listType != "" && k.listType != listType {
			continue
		}

		count++
	}

	return count, nil
}

func (f *fakeACLStore) settingValue(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()

	v, ok := f.settings[key]
	if !ok {
		return ""
	}
	return v
}
