package eventgrade

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/alarm"
)

//go:embed testdata/test_seed_request.sql
var seedRequestSQL string

//go:embed testdata/read_requests.sql
var readRequestsSQL string

//go:embed testdata/count_deliveries.sql
var countDeliveriesSQL string

func chapterAt(id string, end time.Time) Chapter {
	return Chapter{EventID: id, ChapterID: id + "-chapter", EventType: "relay", StartsAt: end.Add(-4 * time.Hour), EndsAt: end, EventEndsAt: end.Add(time.Minute), RevealsAt: end.Add(2 * time.Minute)}
}

func TestRegisterPinsOnlyFreshActiveChapterAndUpdatesScore(t *testing.T) {
	r := NewRepository(dbtest.NewPool(t))
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Second)
	chapters := []Chapter{chapterAt("old", now.Add(-time.Hour)), chapterAt("active", now.Add(time.Hour))}
	req := contract.EventGradeRequest{Region: "jp", RoomID: "12", UserID: "34", Score: 1}

	require.NoError(t, r.StoreCatalog(ctx, "jp", now.Add(-3*time.Minute), chapters))

	res, err := r.RegisterEventGrade(ctx, req)
	require.NoError(t, err)
	require.Equal(t, "unavailable", res.Status)
	require.NoError(t, r.StoreCatalog(ctx, "jp", now, chapters))

	res, err = r.RegisterEventGrade(ctx, req)
	require.NoError(t, err)
	require.Equal(t, "registered", res.Status)
	require.Equal(t, "relay", res.EventType)
	require.WithinDuration(t, chapters[1].EndsAt, res.EndsAt, 0)

	req.Score = 91
	_, err = r.RegisterEventGrade(ctx, req)
	require.NoError(t, err)

	var (
		n        int
		score    int64
		id       string
		outcomes []string
	)

	require.NoError(t, r.pool.QueryRow(ctx, readRequestsSQL, req.Region, nil).Scan(&n, &score, &id, &outcomes))
	require.Equal(t, 1, n)
	require.Equal(t, int64(91), score)
	require.Equal(t, "active", id)

	req.Region = "as"
	res, err = r.RegisterEventGrade(ctx, req)
	require.NoError(t, err)
	require.Equal(t, "unavailable", res.Status)
	require.NoError(t, r.StoreCatalog(ctx, "jp", now.Add(time.Microsecond), []Chapter{}))

	req.Region = "jp"
	res, err = r.RegisterEventGrade(ctx, req)
	require.NoError(t, err)
	require.Equal(t, "unavailable", res.Status)
}

func borderAt(c Chapter, region contract.GradeRegion, t time.Time, final bool) Border {
	return Border{EventID: c.EventID, ChapterID: c.ChapterID, Region: region, BoardKind: "marathon_score", StartedAt: t, ObservedAt: t, Final: new(final), Ranks: []RankScore{{100, new(int64(130))}, {1000, new(int64(70))}, {5000, new(int64(20))}, {500, new(int64(100))}}}
}

type writerPublisher struct{ outbox dispatchoutbox.Writer }

func (p writerPublisher) PublishDispatchBatch(ctx context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope) (dispatchoutbox.PublishBatchResult, error) {
	result, err := p.outbox.InsertBatch(ctx, dispatchoutbox.PublishBatchInput{Envelopes: envelopes})
	if err != nil {
		return result, fmt.Errorf("publish test event grades: %w", err)
	}

	return result, nil
}

type lostReceipt struct{ outbox dispatchoutbox.Writer }

func (w lostReceipt) PublishDispatchBatch(ctx context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope) (dispatchoutbox.PublishBatchResult, error) {
	if _, err := w.outbox.InsertBatch(ctx, dispatchoutbox.PublishBatchInput{Envelopes: envelopes}); err != nil {
		return dispatchoutbox.PublishBatchResult{}, fmt.Errorf("insert batch before simulated receipt loss: %w", err)
	}

	return dispatchoutbox.PublishBatchResult{}, errors.New("receipt lost")
}

func TestFinalizeUsesExactFinalSnapshotAndSurvivesLostReceipt(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	r := NewRepository(pool)
	outbox, err := dispatchoutbox.NewPgxRepository(dbtest.WrapPool(pool), nil)
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)
	c := chapterAt("requested", now.Add(-time.Hour))
	other := chapterAt("other", c.EndsAt)
	expired := chapterAt("expired", now.Add(-26*time.Hour))
	future := chapterAt("future", now.Add(time.Hour))

	for _, region := range []contract.GradeRegion{"jp", "us"} {
		require.NoError(t, r.StoreCatalog(ctx, region, now, []Chapter{c, other, expired, future}))
	}

	for i, score := range []int64{110, 100, 90, 70, 50, 20, 1} {
		_, err := pool.Exec(ctx, seedRequestSQL, "123", strconv.Itoa(i+1), score, "jp", c.EventID, c.ChapterID)
		require.NoError(t, err)
	}

	for _, cc := range []Chapter{expired, future} {
		_, err := pool.Exec(ctx, seedRequestSQL, "123", "99", 34, "jp", cc.EventID, cc.ChapterID)
		require.NoError(t, err)
	}

	require.NoError(t, r.StoreBorder(ctx, borderAt(c, "jp", now, false)))
	require.NoError(t, r.StoreBorder(ctx, borderAt(other, "jp", now, true)))
	require.NoError(t, r.StoreBorder(ctx, borderAt(c, "us", now, true)))
	require.Error(t, r.StoreBorder(ctx, borderAt(future, "jp", now, true)))
	require.NoError(t, r.Finalize(ctx, writerPublisher{outbox}))

	var (
		count    int
		score    int64
		id       string
		outcomes []string
	)

	require.NoError(t, pool.QueryRow(ctx, readRequestsSQL, "jp", expired.EventID).Scan(&count, &score, &id, &outcomes))
	require.Equal(t, []string{"unavailable"}, outcomes)
	require.NoError(t, pool.QueryRow(ctx, readRequestsSQL, "jp", c.EventID).Scan(&count, &score, &id, &outcomes))
	require.Empty(t, outcomes)
	require.NoError(t, r.StoreBorder(ctx, borderAt(c, "jp", now, true)))
	require.Error(t, r.Finalize(ctx, lostReceipt{outbox}))
	require.NoError(t, r.Finalize(ctx, writerPublisher{outbox}))
	require.NoError(t, r.Finalize(ctx, writerPublisher{outbox}))
	require.NoError(t, pool.QueryRow(ctx, readRequestsSQL, "jp", c.EventID).Scan(&count, &score, &id, &outcomes))
	require.Equal(t, []string{"top500", "boundary", "top1000", "boundary", "top5000", "boundary", "outside"}, outcomes)
	require.NoError(t, pool.QueryRow(ctx, countDeliveriesSQL, "123", "event_grade").Scan(&count))
	require.Equal(t, 8, count)
}

func TestCutoffShapeAllowsDynamicRanksButNotAmbiguity(t *testing.T) {
	rows := borderAt(Chapter{}, "jp", time.Time{}, true).Ranks
	ranks, cuts, ok := gradeCutoffs(rows)
	require.True(t, ok)
	require.Equal(t, []int32{100, 500, 1000, 5000}, ranks)
	require.Equal(t, []int64{130, 100, 70, 20}, cuts)

	_, _, ok = gradeCutoffs(append(rows, rows[3]))
	require.False(t, ok)

	_, _, ok = gradeCutoffs(rows[:0])
	require.False(t, ok)

	_, _, ok = gradeCutoffs([]RankScore{{Rank: 500, Score: new(int64(10))}, {Rank: 1000, Score: new(int64(20))}})
	require.False(t, ok)
}
