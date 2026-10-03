package sourceobservation

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/content"
	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	publishkit "github.com/kapu/hololive-youtube-collector/testkit/sourceobservation"
)

func TestBoundedVideoTitlePreservesUTF8(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		title string
		want  string
	}{
		{name: "short", title: "한글과 日本語 🎵", want: "한글과 日本語 🎵"},
		{name: "ASCII boundary", title: strings.Repeat("x", 501), want: strings.Repeat("x", 500)},
		{name: "three byte boundary", title: strings.Repeat("가", 167), want: strings.Repeat("가", 166)},
		{name: "four byte boundary", title: "a" + strings.Repeat("🎵", 125), want: "a" + strings.Repeat("🎵", 124)},
		{name: "exact boundary", title: strings.Repeat("🎵", 125), want: strings.Repeat("🎵", 125)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := boundedVideoTitle(tt.title)
			if got != tt.want || !utf8.ValidString(got) {
				t.Fatalf("bounded title = %q, want valid UTF-8 %q", got, tt.want)
			}
		})
	}
}

func TestContentConsumerPersistsUTF8TitleAndNotification(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedContentWatermark(t, pool)

	payload, err := contract.MarshalPayloadV1(contract.VideoListV1{
		ChannelID: testChannelID,
		Videos: []contract.VideoListItemV1{{
			VideoID: testVideoID, ChannelID: testChannelID, Title: strings.Repeat("가", 167),
		}},
		Coverage: contract.ChannelListCoverageV1{ChannelID: testChannelID, MaxResults: 10, Exhausted: true},
	})
	if err != nil {
		t.Fatalf("marshal valid video payload: %v", err)
	}

	repo := NewRepository(pool)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, testChannelID, "youtubejs_content")
	envelope := prepareContentListEnvelope(t, &proof, contract.KindVideoList, contract.CompletenessComplete, payload)

	if _, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(envelope)); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if err := NewConsumerWithAbsenceGrace(repo, 0).Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("consume: %v", err)
	}

	var title, notificationTitle string

	if err := pool.QueryRow(ctx, `
		SELECT v.title, n.payload->>'title'
		FROM youtube_videos v JOIN youtube_notification_outbox n ON n.content_id = v.video_id
		WHERE v.video_id = $1
	`, testVideoID).Scan(&title, &notificationTitle); err != nil {
		t.Fatalf("read persisted title and notification: %v", err)
	}

	want := strings.Repeat("가", 166)
	if title != want || notificationTitle != want {
		t.Fatalf("persisted titles differ: video=%q notification=%q want=%q", title, notificationTitle, want)
	}
}

func TestContentFieldUpdatesRollBackAcrossBatchBoundary(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)

	// 두 번째 전송 묶음의 오류도 앞서 실행된 첫 묶음 전체와 함께 롤백해야 한다.
	const count = 129

	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_videos (video_id, channel_id, title, first_seen_at, last_seen_at)
		SELECT 'field-update-' || i, $1, 'original', NOW(), NOW() FROM generate_series(0, $2::int - 1) i
	`, testChannelID, count); err != nil {
		t.Fatalf("seed field update videos: %v", err)
	}

	updates := make([]content.Entity, count)
	for i := range updates {
		updates[i] = content.Entity{VideoID: fmt.Sprintf("field-update-%d", i), Title: "updated"}
	}

	updates[count-1].Title = "invalid\x00title"

	err := dbx.InPgxTx(ctx, pool, func(tx dbx.Tx) error {
		return persistContentFieldUpdates(ctx, tx, updates, time.Now().UTC())
	})
	if err == nil {
		t.Fatal("expected PostgreSQL to reject the final title")
	}

	var unchanged int

	if err := pool.QueryRow(ctx, `SELECT count(*) FROM youtube_videos WHERE title = 'original'`).Scan(&unchanged); err != nil {
		t.Fatalf("read rolled back videos: %v", err)
	}

	if unchanged != count {
		t.Fatalf("unchanged rows = %d, want %d", unchanged, count)
	}
}
