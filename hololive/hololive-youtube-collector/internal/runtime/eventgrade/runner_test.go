package eventgrade

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	observation "github.com/kapu/hololive-shared/pkg/contracts/eventgrade"
)

const collectorInstance = "youtube-collector-c"

type fakeGame struct {
	mu        sync.Mutex
	now       time.Time
	borderErr error
	conflicts []string
	acked     [][]string
	borders   int
}

func (f *fakeGame) catalog(_ context.Context, region string) (catalogResponse, error) {
	pending := false
	chapters := []observation.Chapter{}
	bindings := []observation.Binding{}

	if region == "jp" {
		chapters = append(chapters, observation.Chapter{
			EventID: "ev1", ChapterID: "ch1", EventType: "normal",
			StartsAt: f.now.Add(-time.Hour), EndsAt: f.now.Add(time.Hour), EventEndsAt: f.now.Add(2 * time.Hour), RevealsAt: f.now.Add(2 * time.Hour),
		})
		bindings = append(bindings, observation.Binding{EventID: "ev1", ChapterID: "ch1"})
	}

	conflicts := []string{}

	if region == "jp" {
		conflicts = append(conflicts, f.conflicts...)
	}

	return catalogResponse{Version: 1, Region: region, Chapters: chapters, Bindings: bindings, Conflicts: conflicts, QuarantinePending: &pending}, nil
}

func (f *fakeGame) border(_ context.Context, req borderRequest) (borderResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.borders++
	if f.borderErr != nil {
		return borderResponse{}, f.borderErr
	}

	final := false
	score := int64(1234)
	now := time.Now().UTC().Truncate(time.Millisecond)

	return borderResponse{Version: 1, Border: border{
		Region: req.Region, EventID: req.EventID, ChapterID: req.ChapterID, BoardKind: observation.BoardKindScore,
		StartedAt: now, ObservedAt: now, Final: &final, Ranks: []observation.Rank{{Rank: 1, Score: &score}},
	}}, nil
}

func (f *fakeGame) acknowledge(_ context.Context, _ string, conflicts []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.acked = append(f.acked, conflicts)

	return nil
}

func collectorSession(t *testing.T) (*pgxpool.Pool, *pgxpool.Pool) {
	t.Helper()

	admin := dbtest.NewPool(t)

	return admin, dbtest.NewCollectorSessionPool(t, admin, CollectorRole)
}

func observations(t *testing.T, admin *pgxpool.Pool) []string {
	t.Helper()

	rows, err := admin.Query(t.Context(), "SELECT kind FROM public.event_grade_observations ORDER BY region, seq")
	require.NoError(t, err)

	var kinds []string

	for rows.Next() {
		var kind string

		require.NoError(t, rows.Scan(&kind))

		kinds = append(kinds, kind)
	}

	require.NoError(t, rows.Err())

	return kinds
}

func TestRunnerPublishesCatalogThenCutoffAndAcknowledgesAfterPublish(t *testing.T) {
	admin, session := collectorSession(t)
	require.NoError(t, VerifySession(t.Context(), session))

	game := &fakeGame{now: time.Now().UTC().Truncate(time.Millisecond), conflicts: []string{"gone"}}
	runner := NewRunner(session, game, collectorInstance, slog.New(slog.DiscardHandler))

	runner.Cycle(t.Context())

	require.Equal(t, []string{observation.KindCatalog, observation.KindCatalog, observation.KindCutoff, observation.KindCatalog}, observations(t, admin))
	require.Equal(t, [][]string{{"gone"}}, game.acked)

	runner.Cycle(t.Context())
	require.Len(t, observations(t, admin), 5)
}

func TestRunnerReleasesCutoffLeaseWhenCollectionFails(t *testing.T) {
	admin, session := collectorSession(t)
	game := &fakeGame{now: time.Now().UTC().Truncate(time.Millisecond), borderErr: errors.New("upstream unavailable")}
	runner := NewRunner(session, game, collectorInstance, slog.New(slog.DiscardHandler))

	runner.Cycle(t.Context())
	require.Equal(t, []string{observation.KindCatalog, observation.KindCatalog, observation.KindCatalog}, observations(t, admin))

	game.borderErr = nil

	require.NoError(t, runner.collectCutoffs(t.Context(), "jp"))
	require.Equal(t, 2, game.borders)
	require.Equal(t, []string{observation.KindCatalog, observation.KindCatalog, observation.KindCutoff, observation.KindCatalog}, observations(t, admin))
}

func TestVerifySessionRejectsOtherCollectorLogins(t *testing.T) {
	admin := dbtest.NewPool(t)
	other := dbtest.NewCollectorSessionPool(t, admin, "hololive_collector_d")

	require.Error(t, VerifySession(t.Context(), other))
}
