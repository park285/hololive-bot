package community

import (
	"strings"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func TestBuildPostArtifactsKeepsCanonicalIDAndNotificationPayload(t *testing.T) {
	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	detectedAt := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	post, tracking, notification := BuildPostArtifacts(
		"UC_TEST",
		&parser.CommunityPost{
			PostID:       "post-1",
			AuthorName:   "Author",
			ContentText:  "hello world",
			PublishedAt:  &publishedAt,
			LikeCount:    1,
			CommentCount: 2,
		},
		true,
		detectedAt,
		nil,
	)
	if post == nil || post.PostID != "community:post-1" {
		t.Fatalf("post = %#v, want canonical community:post-1", post)
	}
	if tracking == nil || tracking.ContentID != "community:post-1" || tracking.DetectedAt != detectedAt {
		t.Fatalf("tracking = %#v", tracking)
	}
	if notification == nil {
		t.Fatal("expected notification")
	}
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

func TestPayloadFromPostsRoundTripsParserFields(t *testing.T) {
	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	posts := []*parser.CommunityPost{{
		PostID:         "post-1",
		UpstreamPostID: "up-1",
		AuthorID:       "UC_AUTHOR",
		AuthorName:     "Author",
		ContentText:    "hello world",
		PublishedText:  "1 day ago",
		PublishedAt:    &publishedAt,
		LikeCount:      3,
		CommentCount:   4,
		VideoID:        "video-1",
		AuthorPhoto:    []parser.Thumbnail{{URL: "https://img.test/a.jpg", Width: 88, Height: 88}},
	}}
	payload := PayloadFromPosts("UC_TEST", posts, 10, true)
	if payload.ChannelID != "UC_TEST" || len(payload.Posts) != 1 {
		t.Fatalf("payload = %#v", payload)
	}
	if payload.Posts[0].ChannelID != "UC_TEST" || payload.Posts[0].PostID != "post-1" {
		t.Fatalf("mapped post = %#v", payload.Posts[0])
	}
	if payload.Coverage.ChannelID != "UC_TEST" || !payload.Coverage.Exhausted {
		t.Fatalf("coverage = %#v", payload.Coverage)
	}
	roundTrip := PostsFromPayload(payload.Posts)
	if len(roundTrip) != 1 || roundTrip[0].PostID != "post-1" || roundTrip[0].VideoID != "video-1" {
		t.Fatalf("round trip = %#v", roundTrip)
	}
}

func TestCollectNewPostsStopsAtCanonicalWatermark(t *testing.T) {
	posts := []*parser.CommunityPost{
		{PostID: "post-2"},
		{PostID: "post-1"},
	}
	got := CollectNewPosts(posts, &domain.YouTubeContentWatermark{LastContentID: "community:post-1"}, true)
	if len(got) != 1 || got[0].PostID != "post-2" {
		t.Fatalf("new posts = %#v, want post-2 only", got)
	}
}

func TestArtifactsFromPayloadMatchPollerCanonicalRules(t *testing.T) {
	publishedAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	detectedAt := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	payload := contract.CommunityPayloadV1{
		ChannelID: "UC_TEST",
		Posts: []contract.CommunityPostV1{{
			PostID:       "post-1",
			ChannelID:    "UC_TEST",
			AuthorName:   "Author",
			ContentText:  "hello world",
			PublishedAt:  &publishedAt,
			LikeCount:    1,
			CommentCount: 2,
		}},
	}
	batch := ArtifactsFromPayload(&payload, true, &domain.YouTubeContentWatermark{LastContentID: "old-post"}, detectedAt, nil)
	if len(batch.Posts) != 1 || batch.Posts[0].PostID != "community:post-1" {
		t.Fatalf("posts = %#v", batch.Posts)
	}
	if batch.Watermark == nil || batch.Watermark.LastContentID != "community:post-1" {
		t.Fatalf("watermark = %#v, want community:post-1", batch.Watermark)
	}
	if len(batch.Notifications) != 1 || len(batch.Tracking) != 1 {
		t.Fatalf("notifications=%d tracking=%d, want 1", len(batch.Notifications), len(batch.Tracking))
	}
}

func TestArtifactsFromPayloadFirstWindowOmitsNotifications(t *testing.T) {
	payload := contract.CommunityPayloadV1{
		ChannelID: "UC_TEST",
		Posts:     []contract.CommunityPostV1{{PostID: "post-1", ChannelID: "UC_TEST", ContentText: "hello world"}},
	}
	batch := ArtifactsFromPayload(&payload, false, nil, time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC), nil)
	if len(batch.Posts) != 1 || batch.Posts[0].PostID != "community:post-1" {
		t.Fatalf("posts = %#v", batch.Posts)
	}
	if len(batch.Notifications) != 0 || len(batch.Tracking) != 0 {
		t.Fatalf("first window notifications=%d tracking=%d, want 0", len(batch.Notifications), len(batch.Tracking))
	}
	if batch.Watermark == nil || batch.Watermark.LastContentID != "community:post-1" {
		t.Fatalf("watermark = %#v, want community:post-1", batch.Watermark)
	}
}

func TestNormalizeKeywordsAndMatch(t *testing.T) {
	keywords := NormalizeKeywords([]string{" HoloLive ", "", "STREAM", "stream"})
	if len(keywords) != 2 || keywords[0] != "hololive" || keywords[1] != "stream" {
		t.Fatalf("keywords = %#v", keywords)
	}
	if !MatchesKeywords("Tonight's HOLOLIVE schedule", keywords) {
		t.Fatal("expected hololive match")
	}
	if MatchesKeywords("unrelated post", keywords) {
		t.Fatal("unexpected keyword match")
	}
}
