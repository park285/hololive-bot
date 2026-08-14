package targetprojection

import (
	"context"
	"testing"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

func TestLiveHeadViewerVideoIDsReturnsActiveVideosOnly(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_reconciliation_heads (video_id, status)
		VALUES ('vid-ended', 'ENDED'), ('vid-live', 'LIVE'), ('vid-soon', 'UPCOMING')
	`); err != nil {
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
