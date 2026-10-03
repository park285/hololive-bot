package sourceobservation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/schedule"
	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	publishkit "github.com/kapu/hololive-youtube-collector/testkit/sourceobservation"
)

func TestScheduleConsumerRetainsNewestItemAcrossReverseObservationOrder(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderHololiveOfficial, contract.KindSchedule, "global:hololive-schedule", "official_schedule")
	initial := proof.ScheduledFor
	consumer := NewConsumerWithGraces(NewRepository(pool), 0, 0)
	item := contract.ScheduleItemV1{
		ExternalID: testVideoID, VideoID: testVideoID, ChannelID: testChannelID,
		Title: "Latest title", ScheduledAt: initial.Add(time.Hour), CollaboTalentNames: []string{"Latest guest"}, IsLive: true,
	}

	for _, sample := range []struct {
		delta time.Duration
		stale bool
	}{
		{delta: 10 * time.Minute},
		{delta: 10 * time.Minute}, // 같은 payload도 최신 관측 시각은 앞으로 이동해야 한다.
		{delta: -5 * time.Minute, stale: true},
		{delta: -10 * time.Minute, stale: true},
	} {
		proof = advanceLease(ctx, t, pool, &proof, sample.delta)

		incoming := item

		if sample.stale {
			incoming.Title = "Older title"
			incoming.CollaboTalentNames = []string{"Older guest"}
			incoming.ScheduledAt = initial.Add(2 * time.Hour)
			incoming.IsLive = false
		}

		_, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(scheduleEnvelope(t, &proof, incoming)))
		require.NoError(t, err)
		require.NoError(t, consumer.Consume(ctx, liveClaimOptions()))
	}

	var (
		title     string
		names     []string
		observed  *time.Time
		scheduled time.Time
		isLive    bool
	)

	require.NoError(t, pool.QueryRow(ctx, `
        SELECT title,collabo_talent_names,observed_at,scheduled_at,is_live
        FROM youtube_schedule_items WHERE external_id=$1
    `, testVideoID).Scan(&title, &names, &observed, &scheduled, &isLive))
	require.Equal(t, item.Title, title)
	require.Equal(t, item.CollaboTalentNames, names)
	require.True(t, scheduled.Equal(item.ScheduledAt))
	require.True(t, isLive)
	require.NotNil(t, observed)
	require.True(t, observed.Equal(initial.Add(20*time.Minute)))
}

func TestScheduleItemUnknownAndEqualObservationClocks(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	observed := time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC)
	_, err := pool.Exec(ctx, `INSERT INTO youtube_schedule_items(group_key,provider,external_id,title,scheduled_at,collabo_talent_names) VALUES('legacy-group','hololive_official','legacy-item','Legacy',$1,ARRAY['Legacy guest'])`, observed)
	require.NoError(t, err)

	item := schedule.Item{GroupKey: "legacy-group", ExternalID: "legacy-item", Title: "Current", ScheduledAt: observed.Add(time.Hour), CollaboTalentNames: []string{"Current guest"}}
	observation := Observation{Provider: contract.ProviderHololiveOfficial, EffectiveAt: observed}

	require.NoError(t, dbx.InPgxTx(ctx, pool, func(tx dbx.Tx) error {
		return persistScheduleDecision(ctx, tx, &observation, &schedule.Decision{Items: []schedule.Item{item}})
	}))

	item.Title = "Equal-time conflict"
	item.CollaboTalentNames = []string{"Conflicting guest"}

	require.NoError(t, dbx.InPgxTx(ctx, pool, func(tx dbx.Tx) error {
		return persistScheduleDecision(ctx, tx, &observation, &schedule.Decision{Items: []schedule.Item{item}})
	}))

	var (
		title string
		names []string
		clock time.Time
	)

	require.NoError(t, pool.QueryRow(ctx, `SELECT title,collabo_talent_names,observed_at FROM youtube_schedule_items WHERE external_id='legacy-item'`).Scan(&title, &names, &clock))
	require.Equal(t, "Current", title)
	require.Equal(t, []string{"Current guest"}, names)
	require.True(t, clock.Equal(observed))
}
