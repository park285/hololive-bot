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

// 같은 방 사실을 다시 관측하면 행을 잠그지도 새 버전을 만들지도 않고, 값이 바뀔 때만 갱신한다.
// 테스트 풀은 운영과 같은 cache_statement 모드라 명시 캐스트 SQL의 prepare 경로도 함께 검증한다.
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
