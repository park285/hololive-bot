package kakaoroom

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
)

type kakaoRoomRowVersion struct {
	xmin string
	xmax string
	ctid string
}

func readKakaoRoomRowVersion(t *testing.T, pool *pgxpool.Pool, roomID string) kakaoRoomRowVersion {
	t.Helper()

	var version kakaoRoomRowVersion

	if err := pool.QueryRow(t.Context(), `
		SELECT xmin::text, xmax::text, ctid::text
		FROM kakao_rooms
		WHERE room_id = $1
	`, roomID).Scan(&version.xmin, &version.xmax, &version.ctid); err != nil {
		t.Fatalf("read kakao room row version %s: %v", roomID, err)
	}

	return version
}

func TestStoreUpsertLeavesUnchangedRoomUntouched(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := dbtest.NewPool(t)
	s := &store{pool: pool}
	open := Facts{RoomID: "room-upsert", RoomType: "OM", RoomLinkID: "77"}

	if err := s.upsert(ctx, open); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	first := readKakaoRoomRowVersion(t, pool, open.RoomID)

	if err := s.upsert(ctx, open); err != nil {
		t.Fatalf("unchanged upsert: %v", err)
	}

	if got := readKakaoRoomRowVersion(t, pool, open.RoomID); got != first {
		t.Fatalf("unchanged upsert touched the row: before=%+v after=%+v", first, got)
	}

	regular := Facts{RoomID: open.RoomID, RoomType: "MultiChat", RoomLinkID: ""}
	if err := s.upsert(ctx, regular); err != nil {
		t.Fatalf("changed upsert: %v", err)
	}

	if got := readKakaoRoomRowVersion(t, pool, open.RoomID); got.xmin == first.xmin || got.ctid == first.ctid {
		t.Fatalf("changed upsert must write a new row version: before=%+v after=%+v", first, got)
	}

	stored, ok, err := s.get(ctx, open.RoomID)
	if err != nil || !ok {
		t.Fatalf("get after changed upsert: ok=%v err=%v", ok, err)
	}

	if stored != regular {
		t.Fatalf("stored facts = %+v, want %+v", stored, regular)
	}
}

func TestDeliveryPausesResumeOnlyPausedRooms(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := dbtest.NewPool(t)

	if _, err := pool.Exec(ctx, `
		INSERT INTO kakao_room_delivery_pauses (room_id, reason, paused_at)
		VALUES ('room-paused-a', 'room_not_joined', '2026-11-25T01:00:00Z'),
		       ('room-paused-b', 'room_not_joined', '2026-11-25T02:00:00Z')
	`); err != nil {
		t.Fatalf("seed pauses: %v", err)
	}

	pauses := NewDeliveryPauses(pool)

	listed, err := pauses.List(ctx)
	if err != nil {
		t.Fatalf("list pauses: %v", err)
	}

	if len(listed) != 2 || listed[0].RoomID != "room-paused-a" || listed[1].RoomID != "room-paused-b" || listed[0].Reason != "room_not_joined" {
		t.Fatalf("listed pauses = %+v", listed)
	}

	resumed, err := pauses.Resume(ctx, "room-paused-a")
	if err != nil || !resumed {
		t.Fatalf("resume paused room: resumed=%v err=%v", resumed, err)
	}

	resumed, err = pauses.Resume(ctx, "room-paused-a")
	if err != nil || resumed {
		t.Fatalf("resume already resumed room: resumed=%v err=%v", resumed, err)
	}

	catalog := New(pool, nil, nil)
	catalog.Observe(ctx, "room-paused-b", "", "")

	listed, err = pauses.List(ctx)
	if err != nil || len(listed) != 0 {
		t.Fatalf("pauses after observed message = %+v err=%v", listed, err)
	}
}
