package handlers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/broadcasttype"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	dbtest "github.com/kapu/hololive-dbtest"
)

const (
	reviewedTalkVideoID    = "JstSA5yRsj8"
	reviewedTalkChannelID  = "UCOyYb1c43VlX9rc_lT6NKQw"
	testTypeSourceReviewed = "reviewed"
)

func TestClassifyBroadcastVideoReviewBoundaries(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, video, channel, topic, title string
		typ                                broadcasttype.Type
		source                             string
	}{
		{name: "matching review", video: reviewedTalkVideoID, channel: reviewedTalkChannelID, title: "Prisuners, hello", typ: broadcasttype.Talk, source: testTypeSourceReviewed},
		{name: "different channel", video: reviewedTalkVideoID, channel: "UCOtherDifferentChannel00", title: "Prisuners, hello", typ: broadcasttype.Unknown, source: testTypeSourceUnknown},
		{name: "different video", video: "00000000000", channel: reviewedTalkChannelID, title: "Prisuners, hello", typ: broadcasttype.Unknown, source: testTypeSourceUnknown},
		{name: "missing video", channel: reviewedTalkChannelID, title: "Prisuners, hello", typ: broadcasttype.Unknown, source: testTypeSourceUnknown},
		{name: "known topic", video: reviewedTalkVideoID, channel: reviewedTalkChannelID, topic: "minecraft", typ: broadcasttype.Game, source: testTypeSourceTopic},
		{name: "known title", video: reviewedTalkVideoID, channel: reviewedTalkChannelID, title: "【歌枠】", typ: broadcasttype.Singing, source: testTypeSourceTitle},
		{name: "reviewed access priority", video: "raD2SDEffE8", channel: "UCzUNASdzI4PV5SlqtYwAkKQ", topic: "singing", typ: broadcasttype.Membership, source: testTypeSourceReviewed},
		{name: "reviewed access beats watchalong", video: "SrYExB6XHg8", channel: "UCLlJpxXt6L5d-XQ0cDdIyDQ", title: "【WINGMEN ONLY】FORREST GUMP WATCHPA", typ: broadcasttype.Membership, source: testTypeSourceReviewed},
		{name: "membership source retained", video: "raD2SDEffE8", channel: "UCzUNASdzI4PV5SlqtYwAkKQ", topic: "membersonly", typ: broadcasttype.Membership, source: testTypeSourceTopic},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := ClassifyBroadcastVideo(tc.video, tc.channel, tc.topic, tc.title)
			require.Equal(t, BroadcastClassification{Type: tc.typ, Source: tc.source}, got)
		})
	}

	require.Equal(t, broadcasttype.Unknown, ClassifyBroadcast("", "Prisuners, hello"))
}

func TestValidateBroadcastVideoReviews(t *testing.T) {
	t.Parallel()

	valid := broadcastVideoReview{
		ChannelID: reviewedTalkChannelID, Type: broadcasttype.Talk,
		ReviewedAt: "2026-09-11T08:25:39Z", SourceURL: "https://www.youtube.com/watch?v=" + reviewedTalkVideoID,
		EvidenceKind: "creator_tags", Evidence: "creator tagged this video as free talk",
	}
	require.NoError(t, validateBroadcastVideoReviews(map[string]broadcastVideoReview{reviewedTalkVideoID: valid}))
	require.Error(t, validateBroadcastVideoReviews(map[string]broadcastVideoReview{"bad-id": valid}))

	cases := []struct {
		name   string
		change func(*broadcastVideoReview)
	}{
		{name: "channel", change: func(r *broadcastVideoReview) { r.ChannelID = "" }},
		{name: "unknown label", change: func(r *broadcastVideoReview) { r.Type = broadcasttype.Unknown }},
		{name: "invalid type", change: func(r *broadcastVideoReview) { r.Type = "unsupported" }},
		{name: "timestamp", change: func(r *broadcastVideoReview) { r.ReviewedAt = "yesterday" }},
		{name: "source identity", change: func(r *broadcastVideoReview) { r.SourceURL = "https://www.youtube.com/watch?v=00000000000" }},
		{name: "missing evidence", change: func(r *broadcastVideoReview) { r.Evidence = " " }},
		{name: "missing method", change: func(r *broadcastVideoReview) { r.EvidenceKind = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			review := valid
			tc.change(&review)
			require.Error(t, validateBroadcastVideoReviews(map[string]broadcastVideoReview{reviewedTalkVideoID: review}))
		})
	}
}

func TestPgBroadcastHistoryRepositoryReviewedClassification(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	endedAt := time.Date(2026, time.September, 11, 0, 0, 0, 0, time.UTC)

	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions(video_id, channel_id, status, title, ended_at, last_seen_at)
		VALUES ($1, $2, 'ENDED', 'Prisuners, hello', $3, $3)
	`, reviewedTalkVideoID, reviewedTalkChannelID, endedAt)
	require.NoError(t, err)

	repo := &pgBroadcastHistoryRepository{pool: pool}
	entry, err := repo.GetEndedBroadcast(ctx, handlercore.BroadcastThumbnailQuery{VideoID: reviewedTalkVideoID})
	require.NoError(t, err)
	require.NotNil(t, entry)
	require.Equal(t, string(broadcasttype.Talk), entry.BroadcastType)
	require.Equal(t, testTypeSourceReviewed, entry.BroadcastTypeSource)
	require.Empty(t, entry.TopicID)
	require.Equal(t, "Prisuners, hello", entry.Title)

	result, err := repo.ListEndedBroadcasts(ctx, &handlercore.BroadcastHistoryQuery{
		ChannelID: reviewedTalkChannelID, Type: string(broadcasttype.Talk), Limit: 1, IncludeAll: true,
	})
	require.NoError(t, err)
	require.Len(t, result.Entries, 1)
	require.Equal(t, reviewedTalkVideoID, result.Entries[0].VideoID)
}
