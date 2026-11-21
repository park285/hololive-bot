package eventgrade

import (
	_ "embed"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/alarm"
)

//go:embed testdata/snapshot.sql
var readSnapshotSQL string

//go:embed testdata/reject_test_rank.sql
var rejectTestRankSQL string

func requireSnapshot(t *testing.T, r *Repository, b Border) {
	t.Helper()

	var (
		ranks       []int32
		scores      []int64
		first, last time.Time
	)

	err := r.pool.QueryRow(t.Context(), readSnapshotSQL, b.Region, b.EventID, b.ChapterID, *b.Final).Scan(&ranks, &scores, &first, &last)
	require.NoError(t, err)

	wantRanks, wantScores, ok := gradeCutoffs(b.Ranks)
	require.True(t, ok)
	require.Equal(t, wantRanks, ranks)
	require.Equal(t, wantScores, scores)
	require.True(t, first.Equal(b.ObservedAt))
	require.True(t, last.Equal(b.ObservedAt))
}

func TestFinalBorderFreezesOneWholeSnapshot(t *testing.T) {
	r := NewRepository(dbtest.NewPool(t))
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Second)
	c := chapterAt("contest", now.Add(-time.Hour))
	require.NoError(t, r.StoreCatalog(ctx, "jp", now, []Chapter{c}))

	a := borderAt(c, "jp", now.Add(-time.Minute), true)
	b := a

	b.Ranks = []RankScore{a.Ranks[0], {777, new(int64(88))}}
	b.ObservedAt = now.Add(-time.Second)

	type attempt struct {
		border Border
		err    error
	}

	start := make(chan struct{})
	results := make(chan attempt, 2)

	for _, candidate := range []Border{a, b} {
		go func() {
			<-start

			results <- attempt{candidate, r.StoreBorder(ctx, candidate)}
		}()
	}

	close(start)

	x, y := <-results, <-results
	if x.err != nil {
		x, y = y, x
	}

	require.NoError(t, x.err)
	require.Error(t, y.err)
	requireSnapshot(t, r, x.border)

	replay := x.border

	replay.Ranks = slices.Clone(x.border.Ranks)
	slices.Reverse(replay.Ranks)

	replay.ObservedAt = now
	require.NoError(t, r.StoreBorder(ctx, replay))

	for _, ranks := range [][]RankScore{
		x.border.Ranks[:1],
		append(slices.Clone(x.border.Ranks), RankScore{9001, new(int64(1))}),
		{{3, new(int64(20))}},
	} {
		replay.Ranks = ranks
		require.Error(t, r.StoreBorder(ctx, replay))
	}

	replay.Ranks = slices.Clone(x.border.Ranks)
	if len(replay.Ranks) == 0 {
		t.Fatal("empty accepted border")
	}

	replay.Ranks[0].Score = new(*replay.Ranks[0].Score + 1)
	require.Error(t, r.StoreBorder(ctx, replay))
	requireSnapshot(t, r, x.border)
}

func TestProvisionalBorderReplacesRanksAndRollsBackFailedInsert(t *testing.T) {
	r := NewRepository(dbtest.NewPool(t))
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Second)
	c := chapterAt("contest", now.Add(time.Hour))
	require.NoError(t, r.StoreCatalog(ctx, "jp", now, []Chapter{c}))

	b := borderAt(c, "jp", now.Add(-time.Minute), false)
	require.NoError(t, r.StoreBorder(ctx, b))

	newer := b

	newer.Ranks = b.Ranks[:2]
	newer.ObservedAt = now.Add(-time.Second)
	require.NoError(t, r.StoreBorder(ctx, newer))
	require.Error(t, r.StoreBorder(ctx, b))
	requireSnapshot(t, r, newer)

	_, err := r.pool.Exec(ctx, rejectTestRankSQL)
	require.NoError(t, err)

	b.Ranks = []RankScore{b.Ranks[0], {777, new(int64(88))}}
	b.ObservedAt = now
	require.Error(t, r.StoreBorder(ctx, b))
	requireSnapshot(t, r, newer)
}

func TestCatalogQuarantinesRegionChapterRebinding(t *testing.T) {
	r := NewRepository(dbtest.NewPool(t))
	ctx := t.Context()
	now := time.Now().UTC().Add(-time.Second).Truncate(time.Millisecond)
	c := chapterAt("a", now.Add(time.Hour))
	other := c

	other.EventID = "b"
	require.Error(t, r.StoreCatalog(ctx, "jp", now, []Chapter{c, other}))
	require.NoError(t, r.StoreCatalog(ctx, "jp", now.Add(-time.Second), []Chapter{c}))
	require.NoError(t, r.StoreCatalog(ctx, "jp", now, []Chapter{other}))
	require.NotContains(t, collectionTargets(t, r), c.ChapterID)
	require.NoError(t, r.StoreCatalog(ctx, "jp", now.Add(time.Millisecond), []Chapter{c}))
	require.Contains(t, collectionTargets(t, r), c.ChapterID)
	require.NoError(t, r.StoreCatalog(ctx, "us", now, []Chapter{other}))
}

//go:embed testdata/collectable_chapters.sql
var collectableChaptersSQL string

func collectionTargets(t *testing.T, r *Repository) map[string]time.Time {
	t.Helper()

	rows, err := r.pool.Query(t.Context(), collectableChaptersSQL, "jp")
	require.NoError(t, err)

	defer rows.Close()

	targets := make(map[string]time.Time)

	for rows.Next() {
		var (
			event, chapter string
			due, binding   time.Time
		)

		require.NoError(t, rows.Scan(&event, &chapter, &due, &binding))
		require.NotContains(t, targets, chapter)

		targets[chapter] = due.UTC()
	}

	require.NoError(t, rows.Err())

	return targets
}

func TestTargetsCollectUnrequestedChaptersInFinalWindow(t *testing.T) {
	r := NewRepository(dbtest.NewPool(t))
	now := time.Now().UTC().Truncate(time.Second)
	active := chapterAt("active", now.Add(time.Hour))
	due := chapterAt("due", now.Add(-time.Hour))
	late := chapterAt("late", now.Add(-26*time.Hour))
	waiting := chapterAt("waiting", now.Add(-time.Minute))
	require.NoError(t, r.StoreCatalog(t.Context(), "jp", now, []Chapter{active, due, late, waiting}))
	require.Equal(t, map[string]time.Time{active.ChapterID: active.RevealsAt, due.ChapterID: due.RevealsAt}, collectionTargets(t, r))
	require.NoError(t, r.StoreBorder(t.Context(), borderAt(due, "jp", now, true)))
	require.Equal(t, map[string]time.Time{active.ChapterID: active.RevealsAt}, collectionTargets(t, r))
}

func TestPostponementUsesCurrentScheduleForRegistrationCollectionAndDecision(t *testing.T) {
	ctx := t.Context()
	r := NewRepository(dbtest.NewPool(t))
	now := time.Now().UTC().Truncate(time.Second)
	c := chapterAt("postponed", now.Add(-26*time.Hour))
	require.NoError(t, r.StoreCatalog(ctx, "jp", now.Add(-3*time.Second), []Chapter{c}))

	req := contract.EventGradeRequest{Region: "jp", RoomID: "12", UserID: "34", Score: 90}
	_, err := r.pool.Exec(ctx, seedRequestSQL, req.RoomID, req.UserID, 10, req.Region, c.EventID, c.ChapterID)
	require.NoError(t, err)
	require.NoError(t, r.StoreBorder(ctx, borderAt(c, "jp", c.RevealsAt, true)))

	c = chapterAt(c.EventID, now.Add(time.Hour))
	c.EventEndsAt = c.RevealsAt.Add(time.Hour)
	require.NoError(t, r.StoreCatalog(ctx, "jp", now.Add(-2*time.Second), []Chapter{c}))

	decision, err := r.pool.Exec(ctx, decideSQL, []string{"jp"})
	require.NoError(t, err)
	require.Zero(t, decision.RowsAffected())
	require.Equal(t, map[string]time.Time{c.ChapterID: c.EventEndsAt}, collectionTargets(t, r))

	res, err := r.RegisterEventGrade(ctx, req)
	require.NoError(t, err)
	require.Equal(t, "registered", res.Status)
	require.True(t, res.EndsAt.Equal(c.EndsAt))
	require.True(t, res.RevealsAt.Equal(c.EventEndsAt))

	c = chapterAt(c.EventID, now.Add(-time.Hour))
	require.NoError(t, r.StoreCatalog(ctx, "jp", now, []Chapter{c}))
	require.Equal(t, map[string]time.Time{c.ChapterID: c.RevealsAt}, collectionTargets(t, r))

	decision, err = r.pool.Exec(ctx, decideSQL, []string{"jp"})
	require.NoError(t, err)
	require.Zero(t, decision.RowsAffected())
	require.NoError(t, r.StoreBorder(ctx, borderAt(c, "jp", now, true)))

	decision, err = r.pool.Exec(ctx, decideSQL, []string{"jp"})
	require.NoError(t, err)
	require.Equal(t, int64(1), decision.RowsAffected())
}

func TestRescheduleInvalidatesFinalStartedBeforeCorrectedReveal(t *testing.T) {
	ctx := t.Context()
	r := NewRepository(dbtest.NewPool(t))
	now := time.Now().UTC().Add(-time.Second).Truncate(time.Millisecond)
	c := chapterAt("straddled", now.Add(-time.Minute))

	c.EventEndsAt = c.EndsAt
	c.RevealsAt = now.Add(-10 * time.Second)
	require.NoError(t, r.StoreCatalog(ctx, "jp", now.Add(-200*time.Millisecond), []Chapter{c}))

	old := borderAt(c, "jp", now, true)

	old.StartedAt = now.Add(-5 * time.Second)
	require.NoError(t, r.StoreBorder(ctx, old))

	c.RevealsAt = now.Add(-2 * time.Second)
	require.NoError(t, r.StoreCatalog(ctx, "jp", now.Add(-100*time.Millisecond), []Chapter{c}))

	corrected := borderAt(c, "jp", now, true)

	corrected.StartedAt = now.Add(-time.Second)
	require.NoError(t, r.StoreBorder(ctx, corrected))
	requireSnapshot(t, r, corrected)
}

func TestBindingConflictPersistsAcrossEmptyCatalogUntilFreshConfirmation(t *testing.T) {
	ctx := t.Context()
	r := NewRepository(dbtest.NewPool(t))
	now := time.Now().UTC().Add(-time.Second).Truncate(time.Millisecond)
	c := chapterAt("conflicted", now.Add(-time.Hour))
	require.NoError(t, r.StoreCatalog(ctx, "jp", now, []Chapter{c}))
	require.Contains(t, collectionTargets(t, r), c.ChapterID)
	require.NoError(t, r.StoreBorder(ctx, borderAt(c, "jp", now, true)))

	require.NoError(t, r.StoreCatalogSnapshot(ctx, "jp", now.Add(100*time.Millisecond), []Chapter{}, []ChapterBinding{}, []string{c.ChapterID}))
	require.Error(t, r.StoreBorder(ctx, borderAt(c, "jp", now.Add(150*time.Millisecond), true)))
	require.NotContains(t, collectionTargets(t, r), c.ChapterID)

	require.NoError(t, r.StoreCatalogSnapshot(ctx, "jp", now.Add(200*time.Millisecond), []Chapter{}, []ChapterBinding{}, []string{}))
	require.NotContains(t, collectionTargets(t, r), c.ChapterID)

	require.NoError(t, r.StoreCatalog(ctx, "jp", now.Add(300*time.Millisecond), []Chapter{c}))
	require.Contains(t, collectionTargets(t, r), c.ChapterID)
}

func TestPersistedBindingReassignmentQuarantinesOnlyChangedChapter(t *testing.T) {
	ctx := t.Context()
	r := NewRepository(dbtest.NewPool(t))
	now := time.Now().UTC().Add(-time.Second).Truncate(time.Millisecond)
	c := chapterAt("original", now.Add(-time.Hour))
	require.NoError(t, r.StoreCatalog(ctx, "jp", now, []Chapter{c}))

	other := chapterAt("other", c.EndsAt)
	bindings := []ChapterBinding{
		{EventID: "replacement", ChapterID: c.ChapterID},
		{EventID: other.EventID, ChapterID: other.ChapterID},
	}
	require.NoError(t, r.StoreCatalogSnapshot(ctx, "jp", now.Add(100*time.Millisecond), []Chapter{other}, bindings, []string{}))

	targets := collectionTargets(t, r)
	require.NotContains(t, targets, c.ChapterID)
	require.Contains(t, targets, other.ChapterID)
}

func TestInterruptedCatalogQuarantinesUnconfirmedPersistedBindings(t *testing.T) {
	ctx := t.Context()
	r := NewRepository(dbtest.NewPool(t))
	now := time.Now().UTC().Add(-time.Second).Truncate(time.Millisecond)
	c := chapterAt("interrupted", now.Add(-time.Hour))
	require.NoError(t, r.StoreCatalog(ctx, "jp", now, []Chapter{c}))

	require.NoError(t, r.storeCatalogSnapshot(ctx, "jp", now.Add(time.Millisecond), []Chapter{}, []ChapterBinding{}, []string{}, true))
	require.NotContains(t, collectionTargets(t, r), c.ChapterID)
}

func TestExpiryPreservesTimelyFinalSnapshot(t *testing.T) {
	ctx := t.Context()
	r := NewRepository(dbtest.NewPool(t))
	now := time.Now().UTC().Truncate(time.Second)
	c := chapterAt("timely-final", now.Add(-25*time.Hour))
	require.NoError(t, r.StoreCatalog(ctx, "jp", now, []Chapter{c}))

	collected := c.RevealsAt.Add(23*time.Hour + 59*time.Minute)
	require.NoError(t, r.StoreBorder(ctx, borderAt(c, "jp", collected, true)))

	_, err := r.pool.Exec(ctx, seedRequestSQL, "1", "2", 90, "jp", c.EventID, c.ChapterID)
	require.NoError(t, err)

	expired, err := r.pool.Exec(ctx, expireSQL, "jp")
	require.NoError(t, err)
	require.Zero(t, expired.RowsAffected())

	decided, err := r.pool.Exec(ctx, decideSQL, []string{"jp"})
	require.NoError(t, err)
	require.Equal(t, int64(1), decided.RowsAffected())
}
