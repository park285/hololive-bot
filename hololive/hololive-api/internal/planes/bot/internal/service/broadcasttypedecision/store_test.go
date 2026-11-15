package broadcasttypedecision

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/broadcasttype"
	"github.com/kapu/hololive-api/internal/service/geminidecision"
	dbtest "github.com/kapu/hololive-dbtest"
)

func TestPgStoreSelectsUndecidedEndedBroadcastsAndSavesEveryType(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	store := pgStore{pool: pool}
	now := time.Date(2026, time.November, 14, 12, 0, 0, 0, time.UTC)

	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions(video_id, channel_id, status, title, ended_at, last_seen_at, lifecycle_origin)
		VALUES ('pending0001', 'UCchannel', 'ENDED', 'にょ', $1, $1, 'observed'),
		       ('decided0001', 'UCchannel', 'ENDED', 'a', $1 - interval '1 minute', $1, 'observed'),
		       ('retitled001', 'UCchannel', 'ENDED', 'new title', $1 - interval '2 minutes', $1, 'observed'),
		       ('fallback001', 'UCchannel', 'ENDED', NULL, $1 - interval '3 minutes', $1, 'observed'),
		       ('livenow0001', 'UCchannel', 'LIVE', 'b', NULL, $1, 'observed'),
		       ('tooold00001', 'UCchannel', 'ENDED', 'c', $1 - interval '10 days', $1, 'observed')
	`, now)
	require.NoError(t, err)

	insertNotYetAskableBroadcasts(t, pool, now)

	_, err = pool.Exec(ctx, `
		INSERT INTO alarm_dispatch_events(event_key, payload_hash, alarm_type, channel_id, stream_id, category, payload)
		VALUES ('broadcast-type-decision-fallback', repeat('a', 64), 'LIVE'::alarm_type, 'UCchannel', 'fallback001', '',
		        '{"notification":{"stream":{"topic_id":"Rubinite","thumbnail":"https://i.ytimg.com/vi/fallback001/hqdefault.jpg"}}}'::jsonb)
	`)
	require.NoError(t, err)

	talk := string(broadcasttype.Talk)
	require.NoError(t, store.save(ctx, decision{
		VideoID: "decided0001", Title: "a", Outcome: outcomeDecided, Type: &talk, Confidence: new(0.9), Model: geminidecision.Model,
	}))
	require.NoError(t, store.save(ctx, decision{VideoID: "retitled001", Title: "old title", Outcome: outcomeBlocked, Model: geminidecision.Model}))

	page, err := store.candidates(ctx, now.Add(-recentWindow), now.Add(-endGrace), nil, "", 10)
	require.NoError(t, err)
	require.Equal(t, []candidate{
		{VideoID: "pending0001", ChannelID: "UCchannel", Title: "にょ", SortAt: now},
		{VideoID: "retitled001", ChannelID: "UCchannel", Title: "new title", SortAt: now.Add(-2 * time.Minute)},
		{
			VideoID: "fallback001", ChannelID: "UCchannel", TopicID: "Rubinite",
			ThumbnailURL: "https://i.ytimg.com/vi/fallback001/hqdefault.jpg", SortAt: now.Add(-3 * time.Minute),
		},
	}, normalizeTimes(page))

	next, err := store.candidates(ctx, now.Add(-recentWindow), now.Add(-endGrace), &page[0].SortAt, page[0].VideoID, 1)
	require.NoError(t, err)
	require.Equal(t, "retitled001", next[0].VideoID)

	for typ := range typeCriteria {
		value := string(typ)
		require.NoError(t, store.save(ctx, decision{
			VideoID: "pending0001", Title: "にょ", Outcome: outcomeDecided, Type: &value, Confidence: new(0.5), Model: geminidecision.Model,
		}), value)
	}

	unknown := string(broadcasttype.Unknown)
	require.Error(t, store.save(ctx, decision{
		VideoID: "pending0001", Title: "にょ", Outcome: outcomeDecided, Type: &unknown, Confidence: new(0.5), Model: geminidecision.Model,
	}), "미분류는 답으로 저장하지 않는다")
}

func insertNotYetAskableBroadcasts(t *testing.T, pool *pgxpool.Pool, now time.Time) {
	t.Helper()

	ctx := t.Context()

	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions(video_id, channel_id, status, title, ended_at, last_seen_at, lifecycle_origin, status_observed_at)
		VALUES ('memberend01', 'UCchannel', 'ENDED', 'd', $1::timestamptz - interval '4 minutes', $1, 'observed', NULL),
		       ('memberview1', 'UCchannel', 'ENDED', 'e', $1::timestamptz - interval '5 minutes', $1, 'observed', NULL),
		       ('justended01', 'UCchannel', 'ENDED', 'f', $1::timestamptz - interval '6 minutes', $1, 'observed', $1::timestamptz - interval '30 minutes')
	`, now)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO youtube_live_time_finalizations(video_id, queue, outcome, completed_at)
		VALUES ('memberend01', 'new', 'members_only', $1)
	`, now)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO youtube_video_availability(video_id, channel_id, provider, identity_confirmed, availability, method,
		    evidence_sha256, scheduled_for, effective_at, observed_at, received_at)
		VALUES ('memberview1', 'UCchannel', 'youtubejs', true, 'MEMBERS_ONLY', 'player_members_only', repeat('a', 64), $1, $1, $1, $1)
	`, now)
	require.NoError(t, err)
}

func normalizeTimes(page []candidate) []candidate {
	for i := range page {
		page[i].SortAt = page[i].SortAt.UTC()
	}

	return page
}
