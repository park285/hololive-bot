package targetprojection

import (
	"errors"
	"testing"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

func TestLiveHeadViewerVideoIDsReturnsActiveVideosOnly(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)

	if _, err := pool.Exec(ctx, mustTestSQL("insert_live_head_fixture.sql")); err != nil {
		t.Fatal(err)
	}

	videos, err := dbx.InPgxTxWithResult(ctx, pool, func(tx dbx.Tx) ([]string, error) {
		return LiveHeadViewerVideoIDs(ctx, tx)
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(videos) != 2 || videos[0] != "vid-live" || videos[1] != "vid-soon" {
		t.Fatalf("videos = %#v", videos)
	}
}

func TestLiveHeadViewerVideoIDsRejectsOverflow(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)

	if _, err := pool.Exec(ctx, mustTestSQL("insert_live_head_overflow.sql"), MaxInputChannelCount+1); err != nil {
		t.Fatal(err)
	}

	_, err := dbx.InPgxTxWithResult(ctx, pool, func(tx dbx.Tx) ([]string, error) {
		return LiveHeadViewerVideoIDs(ctx, tx)
	})
	if !errors.Is(err, ErrInvalidProjection) {
		t.Fatalf("error = %v, want invalid projection", err)
	}
}
