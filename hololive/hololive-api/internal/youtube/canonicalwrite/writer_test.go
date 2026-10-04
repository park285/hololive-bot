package canonicalwrite

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func buildVideosBatch() ([]*domain.YouTubeVideo, []*domain.YouTubeNotificationOutbox) {
	videos := make([]*domain.YouTubeVideo, 0, batchMaxSize+5)
	notifications := make([]*domain.YouTubeNotificationOutbox, 0, batchMaxSize+5)

	for i := range batchMaxSize + 5 {
		videoID := fmt.Sprintf("video-%03d", i)

		videos = append(videos, &domain.YouTubeVideo{
			VideoID:   videoID,
			ChannelID: testChannelID,
			Title:     "title-" + videoID,
			IsShort:   i%2 == 0,
			ViewCount: int64(100 + i),
		})
		notifications = append(notifications, &domain.YouTubeNotificationOutbox{
			Kind:      domain.OutboxKindNewVideo,
			ChannelID: testChannelID,
			ContentID: videoID,
			Payload:   `{"video_id":"` + videoID + `"}`,
			Status:    domain.OutboxStatusPending,
		})
	}

	return videos, notifications
}

func TestPersistVideosTxChunksVideosOutboxAndWatermark(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()

	videos, notifications := buildVideosBatch()

	require.NoError(t, commitVideos(ctx, pool, videos, notifications, nil, &domain.YouTubeContentWatermark{
		ChannelID:     testChannelID,
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: "video-054",
	}))

	require.EqualValues(t, batchMaxSize+5, countRows(t, pool, `SELECT COUNT(*) FROM youtube_videos`))
	require.EqualValues(t, batchMaxSize+5, countRows(t, pool, `SELECT COUNT(*) FROM youtube_notification_outbox`))

	watermark := getRow[domain.YouTubeContentWatermark](t, pool,
		`SELECT `+watermarkColumns+` FROM youtube_content_watermarks WHERE channel_id = $1 AND watermark_type = $2`, testChannelID, domain.WatermarkTypeVideo)
	require.Equal(t, "video-054", watermark.LastContentID)

	// 다시 관측한 영상은 갱신하고, 같은 (kind, content_id) outbox는 ON CONFLICT DO NOTHING으로 하나만 남긴다.
	require.NoError(t, commitVideos(ctx, pool, []*domain.YouTubeVideo{{
		VideoID:   "video-000",
		ChannelID: testChannelID,
		Title:     "title-video-000",
		ViewCount: 999,
	}}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindNewVideo,
		ChannelID: testChannelID,
		ContentID: "video-000",
		Payload:   `{"video_id":"video-000"}`,
		Status:    domain.OutboxStatusPending,
	}}, nil, &domain.YouTubeContentWatermark{
		ChannelID:     testChannelID,
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: "video-000",
	}))

	updated := getRow[domain.YouTubeVideo](t, pool, `SELECT `+videoColumns+` FROM youtube_videos WHERE video_id = $1`, "video-000")
	require.EqualValues(t, 999, updated.ViewCount)
	require.EqualValues(t, batchMaxSize+5, countRows(t, pool, `SELECT COUNT(*) FROM youtube_notification_outbox`))
}

func TestPersistVideosTxAllowsDifferentKindsForSameContentID(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()

	notifications := []*domain.YouTubeNotificationOutbox{
		{
			Kind:      domain.OutboxKindNewVideo,
			ChannelID: testChannelID,
			ContentID: testVideoID,
			Payload:   `{"video_id":"video-1","kind":"video"}`,
			Status:    domain.OutboxStatusPending,
		},
		{
			Kind:      domain.OutboxKindNewShort,
			ChannelID: testChannelID,
			ContentID: testVideoID,
			Payload:   `{"canonical_post_id":"short:video-1","video_id":"video-1","kind":"short"}`,
			Status:    domain.OutboxStatusPending,
		},
		{
			Kind:      domain.OutboxKindNewVideo,
			ChannelID: testChannelID,
			ContentID: testVideoID,
			Payload:   `{"video_id":"video-1","kind":"video-duplicate"}`,
			Status:    domain.OutboxStatusPending,
		},
	}

	require.NoError(t, commitVideos(ctx, pool, []*domain.YouTubeVideo{{
		VideoID:   testVideoID,
		ChannelID: testChannelID,
		Title:     "title-video-1",
		ViewCount: 1,
	}}, notifications, nil, &domain.YouTubeContentWatermark{
		ChannelID:     testChannelID,
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: testVideoID,
	}))

	outbox := selectRows[domain.YouTubeNotificationOutbox](t, pool, `SELECT `+outboxColumns+` FROM youtube_notification_outbox`)
	require.Len(t, outbox, 2)

	kinds := make([]domain.OutboxKind, 0, len(outbox))
	for _, row := range outbox {
		kinds = append(kinds, row.Kind)
	}

	require.ElementsMatch(t, []domain.OutboxKind{domain.OutboxKindNewVideo, domain.OutboxKindNewShort}, kinds)
}

func TestPersistVideosTxConcurrentOutboxInsertIsIdempotent(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()

	runPersist := func() error {
		return commitVideos(ctx, pool, []*domain.YouTubeVideo{{
			VideoID:   "video-race",
			ChannelID: testChannelID,
			Title:     "title-video-race",
			ViewCount: 1,
		}}, []*domain.YouTubeNotificationOutbox{{
			Kind:      domain.OutboxKindNewVideo,
			ChannelID: testChannelID,
			ContentID: "video-race",
			Payload:   `{"video_id":"video-race"}`,
			Status:    domain.OutboxStatusPending,
		}}, nil, &domain.YouTubeContentWatermark{
			ChannelID:     testChannelID,
			WatermarkType: domain.WatermarkTypeVideo,
			Initialized:   true,
			LastContentID: "video-race",
		})
	}

	var wg sync.WaitGroup

	errs := make(chan error, 2)

	for range 2 {
		wg.Go(func() {
			errs <- runPersist()
		})
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	require.EqualValues(t, 1, countRows(t, pool,
		`SELECT COUNT(*) FROM youtube_notification_outbox WHERE kind = $1 AND content_id = $2`, domain.OutboxKindNewVideo, "video-race"))
}

func TestPersistVideosTxPrimaryAndBackfillSameContentLeaveOneOutboxRow(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	video := &domain.YouTubeVideo{
		VideoID:   "short-backfill",
		ChannelID: testChannelID,
		Title:     "title-short-backfill",
		IsShort:   true,
		ViewCount: 1,
	}
	watermark := &domain.YouTubeContentWatermark{
		ChannelID:     testChannelID,
		WatermarkType: domain.WatermarkTypeShort,
		Initialized:   true,
		LastContentID: "short-backfill",
	}

	for range 2 {
		require.NoError(t, commitVideos(ctx, pool, []*domain.YouTubeVideo{video}, []*domain.YouTubeNotificationOutbox{{
			Kind:      domain.OutboxKindNewShort,
			ChannelID: testChannelID,
			ContentID: "short:short-backfill",
			Payload:   shortPayload(t, video, "short:short-backfill"),
			Status:    domain.OutboxStatusPending,
		}}, nil, watermark))
	}

	require.EqualValues(t, 1, countRows(t, pool,
		`SELECT COUNT(*) FROM youtube_notification_outbox WHERE kind = $1 AND content_id = $2`, domain.OutboxKindNewShort, "short:short-backfill"))
}

func TestPersistVideosTxPersistsShortPublishedAt(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	rawPublishedAt := time.Date(2026, time.April, 10, 10, 11, 12, 123000000, time.FixedZone("KST", 9*60*60))
	canonicalPublishedAt := yttimestamp.Normalize(rawPublishedAt)
	shortVideo := &domain.YouTubeVideo{
		VideoID:     testShortID,
		ChannelID:   testChannelID,
		Title:       testShortTitle,
		IsShort:     true,
		PublishedAt: &canonicalPublishedAt,
		ViewCount:   42,
	}

	require.NoError(t, commitVideos(ctx, pool, []*domain.YouTubeVideo{shortVideo}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindNewShort,
		ChannelID: testChannelID,
		ContentID: testShortID,
		Payload:   shortPayload(t, shortVideo, testCanonicalShortFromShortID),
		Status:    domain.OutboxStatusPending,
	}}, nil, &domain.YouTubeContentWatermark{
		ChannelID:     testChannelID,
		WatermarkType: domain.WatermarkTypeShort,
		Initialized:   true,
		LastContentID: testCanonicalShortFromShortID,
	}))

	stored := getRow[domain.YouTubeVideo](t, pool, `SELECT `+videoColumns+` FROM youtube_videos WHERE video_id = $1`, testShortID)
	require.NotNil(t, stored.PublishedAt)
	require.Equal(t, yttimestamp.Format(canonicalPublishedAt), stored.PublishedAt.UTC().Format(time.RFC3339Nano))

	outbox := getRow[domain.YouTubeNotificationOutbox](t, pool,
		`SELECT `+outboxColumns+` FROM youtube_notification_outbox WHERE kind = $1 AND content_id = $2`, domain.OutboxKindNewShort, testCanonicalShortFromShortID)
	require.Contains(t, outbox.Payload, `"canonical_post_id": "short:short-1"`)
	require.Contains(t, outbox.Payload, `"published_at": "`+yttimestamp.Format(canonicalPublishedAt)+`"`)

	watermark := getRow[domain.YouTubeContentWatermark](t, pool,
		`SELECT `+watermarkColumns+` FROM youtube_content_watermarks WHERE channel_id = $1 AND watermark_type = $2`, testChannelID, domain.WatermarkTypeShort)
	require.Equal(t, testCanonicalShortFromShortID, watermark.LastContentID)
}

func TestPersistVideosTxPreservesExistingPublishedAt(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	firstPublishedAt := time.Date(2026, time.April, 10, 1, 11, 12, 0, time.UTC)
	laterPublishedAt := firstPublishedAt.Add(5 * time.Minute)
	watermark := &domain.YouTubeContentWatermark{
		ChannelID:     testChannelID,
		WatermarkType: domain.WatermarkTypeShort,
		Initialized:   true,
		LastContentID: "short:short-stable",
	}

	require.NoError(t, commitVideos(ctx, pool, []*domain.YouTubeVideo{{
		VideoID:     "short-stable",
		ChannelID:   testChannelID,
		Title:       "title-short-stable",
		IsShort:     true,
		PublishedAt: &firstPublishedAt,
		ViewCount:   42,
	}}, nil, nil, watermark))
	require.NoError(t, commitVideos(ctx, pool, []*domain.YouTubeVideo{{
		VideoID:     "short-stable",
		ChannelID:   testChannelID,
		Title:       "title-short-stable",
		IsShort:     true,
		PublishedAt: &laterPublishedAt,
		ViewCount:   43,
	}}, nil, nil, watermark))

	stored := getRow[domain.YouTubeVideo](t, pool, `SELECT `+videoColumns+` FROM youtube_videos WHERE video_id = $1`, "short-stable")
	require.NotNil(t, stored.PublishedAt)
	require.Equal(t, firstPublishedAt, stored.PublishedAt.UTC())
	require.EqualValues(t, 43, stored.ViewCount)
}

func TestPersistVideosTxRejectsShortPublishedAtStorageRuleMismatch(t *testing.T) {
	pool := dbtest.NewPool(t)
	publishedAt := time.Date(2026, time.April, 10, 1, 11, 12, 0, time.UTC)

	err := commitVideos(t.Context(), pool, []*domain.YouTubeVideo{{
		VideoID:     testShortID,
		ChannelID:   testChannelID,
		Title:       testShortTitle,
		IsShort:     true,
		PublishedAt: &publishedAt,
	}}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindNewShort,
		ChannelID: testChannelID,
		ContentID: testShortID,
		Payload:   `{"canonical_post_id":"short:short-1","video_id":"short-1","published_at":"2026-04-10T10:11:12+09:00"}`,
		Status:    domain.OutboxStatusPending,
	}}, nil, &domain.YouTubeContentWatermark{ChannelID: testChannelID, WatermarkType: domain.WatermarkTypeShort, Initialized: true, LastContentID: testShortID})
	require.ErrorContains(t, err, "payload published_at mismatch")
	require.Zero(t, countRows(t, pool, `SELECT COUNT(*) FROM youtube_videos`))
}

func TestPersistVideosTxRejectsShortCanonicalPostIDMismatch(t *testing.T) {
	pool := dbtest.NewPool(t)
	publishedAt := time.Date(2026, time.April, 10, 1, 11, 12, 0, time.UTC)

	err := commitVideos(t.Context(), pool, []*domain.YouTubeVideo{{
		VideoID:     testShortID,
		ChannelID:   testChannelID,
		Title:       testShortTitle,
		IsShort:     true,
		PublishedAt: &publishedAt,
	}}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindNewShort,
		ChannelID: testChannelID,
		ContentID: testShortID,
		Payload:   `{"canonical_post_id":"short:short-other","video_id":"short-1","published_at":"2026-04-10T01:11:12Z"}`,
		Status:    domain.OutboxStatusPending,
	}}, nil, &domain.YouTubeContentWatermark{ChannelID: testChannelID, WatermarkType: domain.WatermarkTypeShort, Initialized: true, LastContentID: testShortID})
	require.ErrorContains(t, err, "payload canonical_post_id mismatch")
}

func TestPersistVideosTxCollectsSourcePostsWithoutTrackingRows(t *testing.T) {
	pool := dbtest.NewPool(t)
	publishedAt := time.Date(2026, time.April, 10, 1, 11, 12, 0, time.UTC)

	require.NoError(t, commitVideos(t.Context(), pool, []*domain.YouTubeVideo{{
		VideoID:     testShortID,
		ChannelID:   testChannelID,
		Title:       testShortTitle,
		IsShort:     true,
		PublishedAt: &publishedAt,
	}}, nil, nil, &domain.YouTubeContentWatermark{ChannelID: testChannelID, WatermarkType: domain.WatermarkTypeShort, Initialized: false, LastContentID: testShortID}))

	sourcePost := getRow[domain.YouTubeCommunityShortsSourcePost](t, pool,
		`SELECT `+sourcePostColumns+` FROM youtube_community_shorts_source_posts WHERE kind = $1 AND post_id = $2`, domain.OutboxKindNewShort, testCanonicalShortFromShortID)
	require.Equal(t, testChannelID, sourcePost.ChannelID)
	require.NotNil(t, sourcePost.ActualPublishedAt)
	require.Equal(t, publishedAt, sourcePost.ActualPublishedAt.UTC())
	require.False(t, sourcePost.DetectedAt.IsZero())
}

func communityTestPost(postID string, publishedAt *time.Time, likeCount, commentCount int64) *domain.YouTubeCommunityPost {
	return &domain.YouTubeCommunityPost{
		PostID:        postID,
		ChannelID:     testChannelID,
		AuthorName:    testAuthorName,
		ContentText:   testContentText,
		PublishedText: testPublishedText,
		PublishedAt:   publishedAt,
		LikeCount:     likeCount,
		CommentCount:  commentCount,
	}
}

func communityWatermark(lastContentID string) *domain.YouTubeContentWatermark {
	return &domain.YouTubeContentWatermark{
		ChannelID:     testChannelID,
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: lastContentID,
	}
}

func TestPersistCommunityPostsTxPersistsPostOutboxAndWatermark(t *testing.T) {
	pool := dbtest.NewPool(t)
	publishedAt := time.Date(2026, time.April, 10, 1, 11, 12, 0, time.UTC)
	post := communityTestPost(testPostID, &publishedAt, 10, 2)

	require.NoError(t, commitCommunityPosts(t.Context(), pool, []*domain.YouTubeCommunityPost{post}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindCommunityPost,
		ChannelID: testChannelID,
		ContentID: testPostID,
		Payload:   communityPayload(t, post, testPostID, testCanonicalPostID),
		Status:    domain.OutboxStatusPending,
	}}, nil, communityWatermark(testPostID)))

	stored := getRow[domain.YouTubeCommunityPost](t, pool, `SELECT `+postColumns+` FROM youtube_community_posts WHERE post_id = $1`, testPostID)
	require.EqualValues(t, 10, stored.LikeCount)
	require.EqualValues(t, 2, stored.CommentCount)
	require.NotNil(t, stored.PublishedAt)
	require.Equal(t, publishedAt, stored.PublishedAt.UTC())

	outbox := getRow[domain.YouTubeNotificationOutbox](t, pool,
		`SELECT `+outboxColumns+` FROM youtube_notification_outbox WHERE kind = $1 AND content_id = $2`, domain.OutboxKindCommunityPost, testPostID)
	require.Contains(t, outbox.Payload, `"canonical_post_id": "community:post-1"`)
	require.Contains(t, outbox.Payload, `"published_at": "`+publishedAt.Format(time.RFC3339Nano)+`"`)

	watermark := getRow[domain.YouTubeContentWatermark](t, pool,
		`SELECT `+watermarkColumns+` FROM youtube_content_watermarks WHERE channel_id = $1 AND watermark_type = $2`, testChannelID, domain.WatermarkTypeCommunityPost)
	require.Equal(t, testPostID, watermark.LastContentID)
}

func TestPersistCommunityPostsTxPreservesExistingPublishedAt(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	firstPublishedAt := time.Date(2026, time.April, 10, 1, 11, 12, 0, time.UTC)
	laterPublishedAt := firstPublishedAt.Add(5 * time.Minute)

	require.NoError(t, commitCommunityPosts(ctx, pool, []*domain.YouTubeCommunityPost{communityTestPost("post-stable", &firstPublishedAt, 10, 2)}, nil, nil, communityWatermark("post-stable")))
	require.NoError(t, commitCommunityPosts(ctx, pool, []*domain.YouTubeCommunityPost{communityTestPost("post-stable", &laterPublishedAt, 11, 3)}, nil, nil, communityWatermark("post-stable")))

	stored := getRow[domain.YouTubeCommunityPost](t, pool, `SELECT `+postColumns+` FROM youtube_community_posts WHERE post_id = $1`, "post-stable")
	require.NotNil(t, stored.PublishedAt)
	require.Equal(t, firstPublishedAt, stored.PublishedAt.UTC())
	require.EqualValues(t, 11, stored.LikeCount)
	require.EqualValues(t, 3, stored.CommentCount)
}

func TestPersistCommunityPostsTxBackfillsPublishedAt(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	publishedAt := time.Date(2026, time.April, 10, 1, 11, 12, 0, time.UTC)

	require.NoError(t, commitCommunityPosts(ctx, pool, []*domain.YouTubeCommunityPost{communityTestPost(testPostID, nil, 10, 2)}, nil, nil, communityWatermark(testPostID)))
	require.NoError(t, commitCommunityPosts(ctx, pool, []*domain.YouTubeCommunityPost{communityTestPost(testPostID, &publishedAt, 11, 3)}, nil, nil, communityWatermark(testPostID)))

	stored := getRow[domain.YouTubeCommunityPost](t, pool, `SELECT `+postColumns+` FROM youtube_community_posts WHERE post_id = $1`, testPostID)
	require.NotNil(t, stored.PublishedAt)
	require.Equal(t, publishedAt, stored.PublishedAt.UTC())
	require.EqualValues(t, 11, stored.LikeCount)
	require.EqualValues(t, 3, stored.CommentCount)
}

type communityPostRowVersion struct {
	ctid        string
	lastSeenAt  time.Time
	publishedAt *time.Time
	likeCount   int64
}

const reobservedCommunityPostID = "post-reobserved"

// 불변 재관측은 기존 행 버전을 그대로 두고, 카운터 변화나 published_at 보강이 있을 때만 새 행 버전을 쓴다.
func TestPersistCommunityPostsTxSkipsUnchangedReobservation(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	publishedAt := time.Date(2026, time.April, 10, 1, 11, 12, 0, time.UTC)
	observe := func(publishedAt *time.Time, likeCount int64) {
		t.Helper()

		require.NoError(t, commitCommunityPosts(ctx, pool, []*domain.YouTubeCommunityPost{
			communityTestPost(reobservedCommunityPostID, publishedAt, likeCount, 2),
		}, nil, nil, communityWatermark(reobservedCommunityPostID)))
	}
	readVersion := func() communityPostRowVersion {
		t.Helper()

		var version communityPostRowVersion

		require.NoError(t, pool.QueryRow(ctx, `
			SELECT ctid::text, last_seen_at, published_at, like_count
			FROM youtube_community_posts
			WHERE post_id = $1
		`, reobservedCommunityPostID).Scan(&version.ctid, &version.lastSeenAt, &version.publishedAt, &version.likeCount))

		return version
	}

	observe(nil, 10)

	first := readVersion()

	observe(nil, 10)
	require.Equal(t, first, readVersion(), "unchanged reobservation must not write a new row version")

	observe(&publishedAt, 10)

	enriched := readVersion()
	require.NotEqual(t, first.ctid, enriched.ctid)
	require.NotNil(t, enriched.publishedAt)
	require.Equal(t, publishedAt, enriched.publishedAt.UTC())

	observe(&publishedAt, 11)

	counted := readVersion()
	require.NotEqual(t, enriched.ctid, counted.ctid)
	require.EqualValues(t, 11, counted.likeCount)
	require.True(t, counted.lastSeenAt.After(first.lastSeenAt))
}

func TestPersistCommunityPostsTxRejectsPublishedAtStorageRuleMismatchBeforeWrite(t *testing.T) {
	pool := dbtest.NewPool(t)
	publishedAt := time.Date(2026, time.April, 10, 1, 11, 12, 0, time.UTC)

	err := commitCommunityPosts(t.Context(), pool, []*domain.YouTubeCommunityPost{communityTestPost(testPostID, &publishedAt, 10, 2)}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindCommunityPost,
		ChannelID: testChannelID,
		ContentID: testPostID,
		Payload:   `{"canonical_post_id":"community:post-1","post_id":"post-1","published_at":"2026-04-10T10:11:12+09:00"}`,
		Status:    domain.OutboxStatusPending,
	}}, nil, communityWatermark(testPostID))
	require.ErrorContains(t, err, "payload published_at mismatch")
	require.Zero(t, countRows(t, pool, `SELECT COUNT(*) FROM youtube_community_posts`))
	require.Zero(t, countRows(t, pool, `SELECT COUNT(*) FROM youtube_notification_outbox`))
}

func TestPersistCommunityPostsTxRejectsCanonicalPostIDMismatch(t *testing.T) {
	pool := dbtest.NewPool(t)
	publishedAt := time.Date(2026, time.April, 10, 1, 11, 12, 0, time.UTC)

	err := commitCommunityPosts(t.Context(), pool, []*domain.YouTubeCommunityPost{communityTestPost(testPostID, &publishedAt, 10, 2)}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindCommunityPost,
		ChannelID: testChannelID,
		ContentID: testPostID,
		Payload:   `{"canonical_post_id":"community:post-other","post_id":"post-1","published_at":"2026-04-10T01:11:12Z"}`,
		Status:    domain.OutboxStatusPending,
	}}, nil, communityWatermark(testPostID))
	require.ErrorContains(t, err, "payload canonical_post_id mismatch")
}

func TestPersistCommunityPostsTxUpsertsAlarmState(t *testing.T) {
	pool := dbtest.NewPool(t)
	publishedAt := time.Date(2026, time.April, 10, 1, 11, 12, 0, time.UTC)
	detectedAt := publishedAt.Add(20 * time.Second)
	post := communityTestPost(testPostID, &publishedAt, 10, 2)

	require.NoError(t, commitCommunityPosts(t.Context(), pool, []*domain.YouTubeCommunityPost{post}, []*domain.YouTubeNotificationOutbox{{
		Kind:      domain.OutboxKindCommunityPost,
		ChannelID: testChannelID,
		ContentID: testPostID,
		Payload:   communityPayload(t, post, testPostID, testCanonicalPostID),
		Status:    domain.OutboxStatusPending,
	}}, []*domain.YouTubeContentAlarmTracking{{
		Kind:              domain.OutboxKindCommunityPost,
		ContentID:         testPostID,
		ChannelID:         testChannelID,
		ActualPublishedAt: &publishedAt,
		DetectedAt:        detectedAt,
	}}, nil))

	state := getRow[domain.YouTubeCommunityShortsAlarmState](t, pool,
		`SELECT `+alarmStateColumns+` FROM youtube_community_shorts_alarm_states WHERE kind = $1 AND post_id = $2`, domain.OutboxKindCommunityPost, testCanonicalPostID)
	require.Equal(t, testPostID, state.ContentID)
	require.Equal(t, testChannelID, state.ChannelID)
	require.NotNil(t, state.ActualPublishedAt)
	require.Equal(t, publishedAt, state.ActualPublishedAt.UTC())
	require.Equal(t, detectedAt, state.DetectedAt.UTC())
	require.Nil(t, state.AuthorizedAt)
	require.Nil(t, state.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusDetected, state.DeliveryStatus)
}
