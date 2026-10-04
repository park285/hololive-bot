package community

import (
	"strings"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

const (
	testChannelID       = "UC_TEST"
	testPostID          = "post-1"
	testCanonicalPostID = "community:post-1"
)

func TestWindowArtifactsKeepCanonicalIDAndNotificationPayload(t *testing.T) {
	publishedAt := time.Date(2026, time.April, 10, 1, 11, 12, 0, time.UTC)
	detectedAt := time.Date(2026, time.August, 13, 8, 0, 0, 0, time.UTC)
	payload := contract.CommunityPayloadV1{
		ChannelID: testChannelID,
		Posts: []contract.CommunityPostV1{{
			PostID:       testPostID,
			ChannelID:    testChannelID,
			AuthorName:   "Author",
			ContentText:  "hello world",
			PublishedAt:  &publishedAt,
			LikeCount:    1,
			CommentCount: 2,
		}},
	}

	batch := NormalizeWindow(&payload).Artifacts(true, nil, detectedAt)

	if len(batch.Posts) != 1 || batch.Posts[0].PostID != testCanonicalPostID {
		t.Fatalf("posts = %#v, want canonical community:post-1", batch.Posts)
	}

	if batch.Watermark == nil || batch.Watermark.LastContentID != testCanonicalPostID {
		t.Fatalf("watermark = %#v, want community:post-1", batch.Watermark)
	}

	if len(batch.Tracking) != 1 || batch.Tracking[0].ContentID != testCanonicalPostID || batch.Tracking[0].DetectedAt != detectedAt {
		t.Fatalf("tracking = %#v", batch.Tracking)
	}

	if len(batch.Notifications) != 1 {
		t.Fatalf("notifications = %#v, want one", batch.Notifications)
	}

	notification := batch.Notifications[0]
	if !strings.Contains(notification.Payload, `"canonical_post_id":"community:post-1"`) {
		t.Fatalf("notification payload missing canonical id: %s", notification.Payload)
	}

	if !strings.Contains(notification.Payload, `"post_id":"post-1"`) {
		t.Fatalf("notification payload missing upstream post id: %s", notification.Payload)
	}

	if !strings.Contains(notification.Payload, `"published_at":"`+yttimestamp.Format(publishedAt)+`"`) {
		t.Fatalf("notification payload missing published_at: %s", notification.Payload)
	}
}

func TestWindowArtifactsPersistWholeWindowAndNotifyOnlyUnknownPosts(t *testing.T) {
	payload := contract.CommunityPayloadV1{
		ChannelID: testChannelID,
		Posts: []contract.CommunityPostV1{
			{PostID: "pinned", ChannelID: testChannelID},
			{PostID: "new-post", ChannelID: testChannelID},
			{PostID: "known-post", ChannelID: testChannelID},
		},
	}
	known := map[string]struct{}{
		"community:pinned":     {},
		"community:known-post": {},
	}

	batch := NormalizeWindow(&payload).Artifacts(true, known, time.Date(2026, time.August, 13, 8, 0, 0, 0, time.UTC))
	if len(batch.Posts) != 3 {
		t.Fatalf("persisted posts = %d, want 3", len(batch.Posts))
	}

	if len(batch.Notifications) != 1 || batch.Notifications[0].ContentID != "community:new-post" {
		t.Fatalf("notifications = %#v, want new-post only", batch.Notifications)
	}

	if len(batch.Tracking) != 1 || batch.Tracking[0].ContentID != "community:new-post" {
		t.Fatalf("tracking = %#v, want new-post only", batch.Tracking)
	}
}

func TestWindowArtifactsFirstWindowOmitsNotifications(t *testing.T) {
	payload := contract.CommunityPayloadV1{
		ChannelID: testChannelID,
		Posts:     []contract.CommunityPostV1{{PostID: testPostID, ChannelID: testChannelID, ContentText: "hello world"}},
	}
	batch := NormalizeWindow(&payload).Artifacts(false, nil, time.Date(2026, time.August, 13, 8, 0, 0, 0, time.UTC))

	if len(batch.Posts) != 1 || batch.Posts[0].PostID != testCanonicalPostID {
		t.Fatalf("posts = %#v", batch.Posts)
	}

	if len(batch.Notifications) != 0 || len(batch.Tracking) != 0 {
		t.Fatalf("first window notifications=%d tracking=%d, want 0", len(batch.Notifications), len(batch.Tracking))
	}

	if batch.Watermark == nil || batch.Watermark.LastContentID != testCanonicalPostID {
		t.Fatalf("watermark = %#v, want community:post-1", batch.Watermark)
	}
}
