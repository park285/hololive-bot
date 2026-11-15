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
	testTypeSourceGemini   = "gemini"
	testTypeSourcePlayer   = "player"
	reviewedTalkTitle      = "Prisuners, hello"
)

func TestClassifyBroadcastVideoReviewBoundaries(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, video, channel, topic, title string
		signals                            BroadcastTypeSignals
		typ                                broadcasttype.Type
		source                             string
	}{
		{name: "matching review", video: reviewedTalkVideoID, channel: reviewedTalkChannelID, title: reviewedTalkTitle, typ: broadcasttype.Talk, source: testTypeSourceReviewed},
		{name: "different channel", video: reviewedTalkVideoID, channel: "UCOtherDifferentChannel00", title: reviewedTalkTitle, typ: broadcasttype.Unknown, source: testTypeSourceUnknown},
		{name: "different video", video: "00000000000", channel: reviewedTalkChannelID, title: reviewedTalkTitle, typ: broadcasttype.Unknown, source: testTypeSourceUnknown},
		{name: "missing video", channel: reviewedTalkChannelID, title: reviewedTalkTitle, typ: broadcasttype.Unknown, source: testTypeSourceUnknown},
		{name: "known topic", video: reviewedTalkVideoID, channel: reviewedTalkChannelID, topic: "minecraft", typ: broadcasttype.Game, source: testTypeSourceTopic},
		{name: "known title", video: reviewedTalkVideoID, channel: reviewedTalkChannelID, title: "【歌枠】", typ: broadcasttype.Singing, source: testTypeSourceTitle},
		{name: "reviewed access priority", video: "raD2SDEffE8", channel: "UCzUNASdzI4PV5SlqtYwAkKQ", topic: "singing", typ: broadcasttype.Membership, source: testTypeSourceReviewed},
		{name: "reviewed access beats watchalong", video: "SrYExB6XHg8", channel: "UCLlJpxXt6L5d-XQ0cDdIyDQ", title: "【WINGMEN ONLY】FORREST GUMP WATCHPA", typ: broadcasttype.Membership, source: testTypeSourceReviewed},
		{name: "membership source retained", video: "raD2SDEffE8", channel: "UCzUNASdzI4PV5SlqtYwAkKQ", topic: "membersonly", typ: broadcasttype.Membership, source: testTypeSourceTopic},
		{name: "decision fills unknown", video: "00000000000", title: reviewedTalkTitle, signals: BroadcastTypeSignals{Decision: BroadcastTypeDecision{Type: broadcasttype.Talk, Confidence: 0.8}}, typ: broadcasttype.Talk, source: testTypeSourceGemini},
		{name: "decision at threshold", video: "00000000000", title: reviewedTalkTitle, signals: BroadcastTypeSignals{Decision: BroadcastTypeDecision{Type: broadcasttype.Other, Confidence: minBroadcastTypeDecisionConfidence}}, typ: broadcasttype.Other, source: testTypeSourceGemini},
		{name: "low confidence decision", video: "00000000000", title: reviewedTalkTitle, signals: BroadcastTypeSignals{Decision: BroadcastTypeDecision{Type: broadcasttype.Talk, Confidence: minBroadcastTypeDecisionConfidence - 0.01}}, typ: broadcasttype.Unknown, source: testTypeSourceUnknown},
		{name: "unknown decision", video: "00000000000", title: reviewedTalkTitle, signals: BroadcastTypeSignals{Decision: BroadcastTypeDecision{Type: broadcasttype.Unknown, Confidence: 0.9}}, typ: broadcasttype.Unknown, source: testTypeSourceUnknown},
		{name: "invalid decision", video: "00000000000", title: reviewedTalkTitle, signals: BroadcastTypeSignals{Decision: BroadcastTypeDecision{Type: "unsupported", Confidence: 0.9}}, typ: broadcasttype.Unknown, source: testTypeSourceUnknown},
		{name: "title beats decision", video: "00000000000", title: "【歌枠】", signals: BroadcastTypeSignals{Decision: BroadcastTypeDecision{Type: broadcasttype.Talk, Confidence: 0.99}}, typ: broadcasttype.Singing, source: testTypeSourceTitle},
		{name: "review beats decision", video: reviewedTalkVideoID, channel: reviewedTalkChannelID, title: reviewedTalkTitle, signals: BroadcastTypeSignals{Decision: BroadcastTypeDecision{Type: broadcasttype.Game, Confidence: 0.99}}, typ: broadcasttype.Talk, source: testTypeSourceReviewed},
		{name: "player members-only fills unknown", video: "00000000000", title: reviewedTalkTitle, signals: BroadcastTypeSignals{PlayerMembersOnly: true}, typ: broadcasttype.Membership, source: testTypeSourcePlayer},
		{name: "player members-only beats decision", video: "00000000000", title: reviewedTalkTitle, signals: BroadcastTypeSignals{PlayerMembersOnly: true, Decision: BroadcastTypeDecision{Type: broadcasttype.Talk, Confidence: 0.99}}, typ: broadcasttype.Membership, source: testTypeSourcePlayer},
		{name: "title keeps type over player members-only", video: "00000000000", title: "【歌枠】", signals: BroadcastTypeSignals{PlayerMembersOnly: true}, typ: broadcasttype.Singing, source: testTypeSourceTitle},
		{name: "review keeps type over player members-only", video: reviewedTalkVideoID, channel: reviewedTalkChannelID, title: reviewedTalkTitle, signals: BroadcastTypeSignals{PlayerMembersOnly: true}, typ: broadcasttype.Talk, source: testTypeSourceReviewed},
		{name: "member review beats decision", video: "raD2SDEffE8", channel: "UCzUNASdzI4PV5SlqtYwAkKQ", title: "さまーーーー", signals: BroadcastTypeSignals{Decision: BroadcastTypeDecision{Type: broadcasttype.Singing, Confidence: 0.99}}, typ: broadcasttype.Membership, source: testTypeSourceReviewed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := ClassifyBroadcastVideo(tc.video, tc.channel, tc.topic, tc.title, tc.signals)
			require.Equal(t, BroadcastClassification{Type: tc.typ, Source: tc.source}, got)
		})
	}

	require.Equal(t, broadcasttype.Unknown, ClassifyBroadcast("", reviewedTalkTitle))
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
		INSERT INTO youtube_live_sessions(video_id, channel_id, status, title, ended_at, last_seen_at, lifecycle_origin)
		VALUES ($1, $2, 'ENDED', 'Prisuners, hello', $3, $3, 'observed')
	`, reviewedTalkVideoID, reviewedTalkChannelID, endedAt)
	require.NoError(t, err)

	repo := &pgBroadcastHistoryRepository{pool: pool}
	entry, err := repo.GetEndedBroadcast(ctx, handlercore.BroadcastThumbnailQuery{VideoID: reviewedTalkVideoID})
	require.NoError(t, err)
	require.NotNil(t, entry)
	require.Equal(t, string(broadcasttype.Talk), entry.BroadcastType)
	require.Empty(t, entry.TopicID)
	require.Equal(t, reviewedTalkTitle, entry.Title)

	result, err := repo.ListEndedBroadcasts(ctx, &handlercore.BroadcastHistoryQuery{
		ChannelID: reviewedTalkChannelID, Type: string(broadcasttype.Talk), Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, result.Entries, 1)
	require.Equal(t, reviewedTalkVideoID, result.Entries[0].VideoID)
}

func TestPgBroadcastHistoryRepositoryDecisionClassification(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	endedAt := time.Date(2026, time.November, 13, 0, 0, 0, 0, time.UTC)

	const channelID = "UCdecisionTestChannel0000"

	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions(video_id, channel_id, status, title, ended_at, last_seen_at, lifecycle_origin)
		VALUES ('decided0001', $1, 'ENDED', 'にょ', $2, $2, 'observed'),
		       ('decided0002', $1, 'ENDED', '休憩', $2 - interval '1 hour', $2, 'observed'),
		       ('decided0003', $1, 'ENDED', 'a', $2 - interval '2 hours', $2, 'observed'),
		       ('decided0004', $1, 'ENDED', 'b', $2 - interval '3 hours', $2, 'observed'),
		       ('members0001', $1, 'ENDED', 'c', $2 - interval '4 hours', $2, 'observed'),
		       ('members0002', $1, 'ENDED', 'd', $2 - interval '5 hours', $2, 'observed'),
		       ('members0003', $1, 'ENDED', '【歌枠】', $2 - interval '6 hours', $2, 'observed')
	`, channelID, endedAt)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO youtube_live_time_finalizations(video_id, queue, outcome, completed_at)
		VALUES ('members0001', 'new', 'members_only', $1),
		       ('members0003', 'new', 'members_only', $1)
	`, endedAt)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO youtube_video_availability(video_id, channel_id, provider, identity_confirmed, availability, method,
		    evidence_sha256, scheduled_for, effective_at, observed_at, received_at)
		VALUES ('members0002', $1, 'youtubejs', true, 'MEMBERS_ONLY', 'player_members_only', repeat('a', 64), $2, $2, $2, $2)
	`, channelID, endedAt)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO youtube_broadcast_type_decisions(video_id, title, outcome, broadcast_type, confidence, model)
		VALUES ('decided0001', 'にょ', 'decided', 'talk', 0.82, 'gemini-decision-1'),
		       ('decided0002', '休憩 (old title)', 'decided', 'talk', 0.9, 'gemini-decision-1'),
		       ('decided0003', 'a', 'decided', 'talk', 0.29, 'gemini-decision-1'),
		       ('decided0004', 'b', 'blocked', NULL, NULL, 'gemini-decision-1')
	`)
	require.NoError(t, err)

	repo := &pgBroadcastHistoryRepository{pool: pool}

	for videoID, want := range map[string]broadcasttype.Type{
		"decided0001": broadcasttype.Talk,
		"decided0002": broadcasttype.Unknown,
		"decided0003": broadcasttype.Unknown,
		"decided0004": broadcasttype.Unknown,
		"members0001": broadcasttype.Membership,
		"members0002": broadcasttype.Membership,
		"members0003": broadcasttype.Singing,
	} {
		entry, getErr := repo.GetEndedBroadcast(ctx, handlercore.BroadcastThumbnailQuery{VideoID: videoID})
		require.NoError(t, getErr)
		require.Equal(t, string(want), entry.BroadcastType, videoID)
	}

	result, err := repo.ListEndedBroadcasts(ctx, &handlercore.BroadcastHistoryQuery{ChannelID: channelID, Type: string(broadcasttype.Talk), Limit: 5})
	require.NoError(t, err)
	require.Len(t, result.Entries, 1)
	require.Equal(t, "decided0001", result.Entries[0].VideoID)

	result, err = repo.ListEndedBroadcasts(ctx, &handlercore.BroadcastHistoryQuery{ChannelID: channelID, Type: string(broadcasttype.Membership), Limit: 5})
	require.NoError(t, err)
	require.Len(t, result.Entries, 2)
	require.Equal(t, "members0001", result.Entries[0].VideoID)
	require.Equal(t, "members0002", result.Entries[1].VideoID)
}
