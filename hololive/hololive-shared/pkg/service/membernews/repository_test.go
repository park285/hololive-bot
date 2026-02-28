package membernews

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

type fakeSubscription struct {
	id        int
	roomID    string
	roomName  string
	createdAt time.Time
}

type fakeMemberNewsPool struct {
	nextID       int
	baseTime     time.Time
	subscription map[string]*fakeSubscription
}

func newFakeMemberNewsPool() *fakeMemberNewsPool {
	return &fakeMemberNewsPool{
		baseTime:     time.Date(2026, 2, 16, 9, 0, 0, 0, time.UTC),
		subscription: make(map[string]*fakeSubscription),
	}
}

func (f *fakeMemberNewsPool) Exec(_ context.Context, sql string, args ...any) error {
	query := strings.ToLower(strings.TrimSpace(sql))
	switch {
	case strings.Contains(query, "insert into member_news_subscriptions"):
		roomID, _ := args[0].(string)
		roomName, _ := args[1].(string)

		if existing, ok := f.subscription[roomID]; ok {
			if strings.TrimSpace(roomName) != "" {
				existing.roomName = roomName
			}
			return nil
		}

		f.nextID++
		f.subscription[roomID] = &fakeSubscription{
			id:        f.nextID,
			roomID:    roomID,
			roomName:  roomName,
			createdAt: f.baseTime.Add(time.Duration(f.nextID) * time.Minute),
		}
		return nil

	case strings.Contains(query, "delete from member_news_subscriptions"):
		roomID, _ := args[0].(string)
		delete(f.subscription, roomID)
		return nil
	}

	return fmt.Errorf("unsupported exec query: %s", query)
}

func (f *fakeMemberNewsPool) Query(_ context.Context, sql string, _ ...any) (rowsScanner, error) {
	query := strings.ToLower(strings.TrimSpace(sql))
	if !strings.Contains(query, "from member_news_subscriptions") {
		return nil, fmt.Errorf("unsupported query: %s", query)
	}
	if !strings.Contains(query, "order by created_at asc") {
		return nil, fmt.Errorf("missing created_at asc order by: %s", query)
	}

	items := make([]*fakeSubscription, 0, len(f.subscription))
	for _, sub := range f.subscription {
		items = append(items, sub)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].createdAt.Before(items[j].createdAt)
	})

	rows := make([][]any, 0, len(items))
	for _, item := range items {
		rows = append(rows, []any{
			item.id,
			item.roomID,
			item.roomName,
			item.createdAt,
		})
	}
	return &fakeRows{data: rows}, nil
}

func (f *fakeMemberNewsPool) QueryRow(_ context.Context, sql string, args ...any) rowScanner {
	query := strings.ToLower(strings.TrimSpace(sql))
	if !strings.Contains(query, "select exists(") {
		return fakeRow{err: fmt.Errorf("unsupported queryrow: %s", query)}
	}

	roomID, _ := args[0].(string)
	_, exists := f.subscription[roomID]
	return fakeRow{values: []any{exists}}
}

type fakeRow struct {
	values []any
	err    error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return fmt.Errorf("scan dest len mismatch: got %d want %d", len(dest), len(r.values))
	}
	for i := range dest {
		switch target := dest[i].(type) {
		case *bool:
			value, ok := r.values[i].(bool)
			if !ok {
				return fmt.Errorf("scan bool type mismatch at %d", i)
			}
			*target = value
		default:
			return fmt.Errorf("unsupported scan target type at %d", i)
		}
	}
	return nil
}

type fakeRows struct {
	data   [][]any
	index  int
	closed bool
	err    error
}

func (r *fakeRows) Close() {
	r.closed = true
}

func (r *fakeRows) Err() error {
	return r.err
}

func (r *fakeRows) Next() bool {
	if r.closed {
		return false
	}
	if r.index >= len(r.data) {
		r.closed = true
		return false
	}
	r.index++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.index == 0 || r.index > len(r.data) {
		return fmt.Errorf("scan called without valid current row")
	}

	row := r.data[r.index-1]
	if len(dest) != len(row) {
		return fmt.Errorf("scan dest len mismatch: got %d want %d", len(dest), len(row))
	}

	for i := range dest {
		switch target := dest[i].(type) {
		case *int:
			value, ok := row[i].(int)
			if !ok {
				return fmt.Errorf("scan int type mismatch at %d", i)
			}
			*target = value
		case *string:
			value, ok := row[i].(string)
			if !ok {
				return fmt.Errorf("scan string type mismatch at %d", i)
			}
			*target = value
		case *time.Time:
			value, ok := row[i].(time.Time)
			if !ok {
				return fmt.Errorf("scan time type mismatch at %d", i)
			}
			*target = value
		default:
			return fmt.Errorf("unsupported scan target type at %d", i)
		}
	}

	return nil
}

func TestRepository_SubscribeIdempotent(t *testing.T) {
	repo := &Repository{pool: newFakeMemberNewsPool()}

	ctx := context.Background()
	if err := repo.Subscribe(ctx, "room-a", "Alpha"); err != nil {
		t.Fatalf("first subscribe failed: %v", err)
	}
	if err := repo.Subscribe(ctx, "room-a", "Alpha Updated"); err != nil {
		t.Fatalf("second subscribe failed: %v", err)
	}

	subscribed, err := repo.IsSubscribed(ctx, "room-a")
	if err != nil {
		t.Fatalf("is subscribed failed: %v", err)
	}
	if !subscribed {
		t.Fatalf("expected room-a to remain subscribed")
	}

	rooms, err := repo.ListSubscribedRooms(ctx)
	if err != nil {
		t.Fatalf("list subscribed rooms failed: %v", err)
	}
	if len(rooms) != 1 {
		t.Fatalf("expected single subscribed room after idempotent upsert, got %d", len(rooms))
	}
	if rooms[0].RoomName != "Alpha Updated" {
		t.Fatalf("expected upsert to refresh room name, got %q", rooms[0].RoomName)
	}
}

func TestRepository_UnsubscribeThenIsSubscribedFalse(t *testing.T) {
	repo := &Repository{pool: newFakeMemberNewsPool()}
	ctx := context.Background()

	if err := repo.Subscribe(ctx, "room-a", "Alpha"); err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}

	if err := repo.Unsubscribe(ctx, "room-a"); err != nil {
		t.Fatalf("unsubscribe failed: %v", err)
	}

	subscribed, err := repo.IsSubscribed(ctx, "room-a")
	if err != nil {
		t.Fatalf("is subscribed failed: %v", err)
	}
	if subscribed {
		t.Fatalf("expected unsubscribed room-a to be false")
	}
}

func TestRepository_ListSubscribedRoomsCreatedAtAsc(t *testing.T) {
	repo := &Repository{pool: newFakeMemberNewsPool()}
	ctx := context.Background()

	if err := repo.Subscribe(ctx, "room-b", "Bravo"); err != nil {
		t.Fatalf("subscribe room-b failed: %v", err)
	}
	if err := repo.Subscribe(ctx, "room-a", "Alpha"); err != nil {
		t.Fatalf("subscribe room-a failed: %v", err)
	}

	rooms, err := repo.ListSubscribedRooms(ctx)
	if err != nil {
		t.Fatalf("list subscribed rooms failed: %v", err)
	}
	if len(rooms) != 2 {
		t.Fatalf("expected 2 subscribed rooms, got %d", len(rooms))
	}

	if rooms[0].RoomID != "room-b" || rooms[1].RoomID != "room-a" {
		t.Fatalf("expected created_at asc order [room-b room-a], got [%s %s]", rooms[0].RoomID, rooms[1].RoomID)
	}
}

