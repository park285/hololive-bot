package majorevent

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

// bot은 방 이름을 모르므로(Iris webhook에 방 제목 없음) 빈 이름으로 구독한다. 모르는 이름은 NULL로 저장하고, 재구독이
// 저장된 이름을 지우거나 방 ID로 덮어쓰지 않아야 한다.
func TestRepositoryPostgres_SubscribeKeepsKnownRoomNameWhenNameUnknown(t *testing.T) {
	pool := dbtest.NewPool(t)
	repository := NewRepository(&databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}, nil)
	ctx := t.Context()

	const (
		namedRoom   = "me-sub-named"
		unknownRoom = "me-sub-unknown"
	)

	subscribe := func(roomID, roomName string) {
		t.Helper()

		if err := repository.Subscribe(ctx, roomID, roomName); err != nil {
			t.Fatalf("Subscribe(%q, %q): %v", roomID, roomName, err)
		}
	}

	storedName := func(roomID string) (name string, isNull bool) {
		t.Helper()

		if err := pool.QueryRow(ctx,
			`SELECT COALESCE(room_name, ''), room_name IS NULL FROM major_event_subscriptions WHERE room_id = $1`, roomID,
		).Scan(&name, &isNull); err != nil {
			t.Fatalf("read room_name for %q: %v", roomID, err)
		}

		return name, isNull
	}

	subscribe(namedRoom, "홀로 방")
	subscribe(unknownRoom, "")

	if name, isNull := storedName(unknownRoom); !isNull {
		t.Fatalf("unknown room name stored as %q, want NULL", name)
	}

	subscribe(namedRoom, "")
	subscribe(namedRoom, "   ")

	if name, _ := storedName(namedRoom); name != "홀로 방" {
		t.Fatalf("re-subscribe with unknown name left room_name = %q, want 홀로 방", name)
	}

	subscribe(unknownRoom, "새 방")

	rooms, err := repository.GetSubscribedRooms(ctx)
	if err != nil {
		t.Fatalf("GetSubscribedRooms: %v", err)
	}

	got := map[string]string{}

	for _, room := range rooms {
		got[room.RoomID] = room.RoomName
	}

	if got[namedRoom] != "홀로 방" || got[unknownRoom] != "새 방" {
		t.Fatalf("GetSubscribedRooms names = %v, want %s=홀로 방 and %s=새 방", got, namedRoom, unknownRoom)
	}
}
