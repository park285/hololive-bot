package eventgrade

import (
	_ "embed"
	jsonv2 "encoding/json/v2"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	observationcontract "github.com/kapu/hololive-shared/pkg/contracts/eventgrade"
)

//go:embed testdata/insert_observation.sql
var insertObservationSQL string

//go:embed testdata/request_outcome.sql
var requestOutcomeSQL string

//go:embed testdata/applied_seq.sql
var appliedSeqSQL string

//go:embed testdata/inflight_lease.sql
var inflightLeaseSQL string

//go:embed testdata/release_lease.sql
var releaseLeaseSQL string

//go:embed testdata/drop_schema_version_check.sql
var dropSchemaVersionCheckSQL string

//go:embed testdata/bump_schema_version.sql
var bumpSchemaVersionSQL string

//go:embed testdata/delete_observation.sql
var deleteObservationSQL string

func outboxPublisher(t *testing.T, r *Repository) writerPublisher {
	t.Helper()

	outbox, err := dispatchoutbox.NewPgxRepository(dbtest.WrapPool(r.pool), nil)
	require.NoError(t, err)

	return writerPublisher{outbox}
}

func publishObservation(t *testing.T, r *Repository, kind string, started, observed time.Time, payload any) int64 {
	t.Helper()

	body, err := jsonv2.Marshal(payload)
	require.NoError(t, err)

	var seq int64

	require.NoError(t, r.pool.QueryRow(t.Context(), insertObservationSQL, "jp", kind, started, observed, string(body)).Scan(&seq))

	return seq
}

func cutoffPayload(c Chapter, final bool, score int64) observationcontract.CutoffObservation {
	return observationcontract.CutoffObservation{EventID: c.EventID, ChapterID: c.ChapterID, BoardKind: observationcontract.BoardKindScore, Final: &final, Ranks: []observationcontract.Rank{{Rank: 1, Score: &score}}}
}

func appliedSeq(t *testing.T, r *Repository) int64 {
	t.Helper()

	var seq int64

	require.NoError(t, r.pool.QueryRow(t.Context(), appliedSeqSQL, "jp").Scan(&seq))

	return seq
}

func requestOutcome(t *testing.T, r *Repository, id int64) string {
	t.Helper()

	var outcome string

	require.NoError(t, r.pool.QueryRow(t.Context(), requestOutcomeSQL, id).Scan(&outcome))

	return outcome
}

func TestConsumeAppliesObservationsInOrderAndSkipsRejected(t *testing.T) {
	r := NewRepository(dbtest.NewPool(t))
	now := time.Now().UTC().Truncate(time.Millisecond)
	c := chapterAt("consumed", now.Add(time.Hour))

	publishObservation(t, r, "catalog", now.Add(-time.Second), now, observationcontract.CatalogObservation{
		Chapters: []observationcontract.Chapter{observationcontract.Chapter(c)}, Bindings: []observationcontract.Binding{{EventID: c.EventID, ChapterID: c.ChapterID}}, Conflicts: []string{},
	})
	publishObservation(t, r, "cutoff", now, now, cutoffPayload(Chapter{EventID: "unknown", ChapterID: "unknown"}, false, 5))
	publishObservation(t, r, "cutoff", now, now, cutoffPayload(c, false, 7))

	result, err := r.ConsumeRegion(t.Context(), "jp", 10)
	require.NoError(t, err)
	require.Equal(t, ConsumeResult{Applied: 2, Rejected: 1}, result)
	require.Equal(t, int64(3), appliedSeq(t, r))
	require.Contains(t, collectionTargets(t, r), c.ChapterID)

	result, err = r.ConsumeRegion(t.Context(), "jp", 10)
	require.NoError(t, err)
	require.Equal(t, ConsumeResult{}, result)
}

func TestConsumeAcceptsHelperWirePayload(t *testing.T) {
	r := NewRepository(dbtest.NewPool(t))
	now := time.Now().UTC().Truncate(time.Millisecond)
	c := chapterAt("wire", now.Add(time.Hour))

	catalog := `{"chapters":[{"event_id":"` + c.EventID + `","chapter_id":"` + c.ChapterID + `","event_type":"normal","starts_at":"` +
		c.StartsAt.Format(time.RFC3339Nano) + `","ends_at":"` + c.EndsAt.Format(time.RFC3339Nano) + `","event_ends_at":"` +
		c.EventEndsAt.Format(time.RFC3339Nano) + `","reveals_at":"` + c.RevealsAt.Format(time.RFC3339Nano) +
		`"}],"bindings":[{"event_id":"` + c.EventID + `","chapter_id":"` + c.ChapterID + `"}],"conflicts":[],"quarantine_missing":false}`
	cutoff := `{"event_id":"` + c.EventID + `","chapter_id":"` + c.ChapterID + `","board_kind":"marathon_score","final":false,"ranks":[{"rank":1,"score":"9007199254740993"},{"rank":10,"score":"5"}]}`

	for _, body := range []struct{ kind, payload string }{{"catalog", catalog}, {"cutoff", cutoff}} {
		var seq int64

		require.NoError(t, r.pool.QueryRow(t.Context(), insertObservationSQL, "jp", body.kind, now, now, body.payload).Scan(&seq))
	}

	result, err := r.ConsumeRegion(t.Context(), "jp", 10)
	require.NoError(t, err)
	require.Equal(t, ConsumeResult{Applied: 2}, result)
}

func TestConsumeStopsRegionOnUnsupportedSchemaVersion(t *testing.T) {
	r := NewRepository(dbtest.NewPool(t))
	now := time.Now().UTC().Truncate(time.Millisecond)

	_, err := r.pool.Exec(t.Context(), dropSchemaVersionCheckSQL)
	require.NoError(t, err)

	seq := publishObservation(t, r, "catalog", now, now, observationcontract.CatalogObservation{Chapters: []observationcontract.Chapter{}, Bindings: []observationcontract.Binding{}, Conflicts: []string{}})

	_, err = r.pool.Exec(t.Context(), bumpSchemaVersionSQL, "jp", seq)
	require.NoError(t, err)

	_, err = r.ConsumeRegion(t.Context(), "jp", 10)
	require.ErrorContains(t, err, "unsupported schema version")
	require.Zero(t, appliedSeq(t, r))
}

func TestConsumeCommitsBatchPrefixBeforeUnsupportedSchemaVersion(t *testing.T) {
	r := NewRepository(dbtest.NewPool(t))
	now := time.Now().UTC().Truncate(time.Millisecond)
	empty := observationcontract.CatalogObservation{Chapters: []observationcontract.Chapter{}, Bindings: []observationcontract.Binding{}, Conflicts: []string{}}

	_, err := r.pool.Exec(t.Context(), dropSchemaVersionCheckSQL)
	require.NoError(t, err)

	publishObservation(t, r, "catalog", now, now, empty)
	publishObservation(t, r, "cutoff", now, now, cutoffPayload(Chapter{EventID: "absent", ChapterID: "absent"}, false, 5))

	seq := publishObservation(t, r, "catalog", now, now, empty)
	publishObservation(t, r, "catalog", now, now, empty)

	_, err = r.pool.Exec(t.Context(), bumpSchemaVersionSQL, "jp", seq)
	require.NoError(t, err)

	result, err := r.ConsumeRegion(t.Context(), "jp", 10)
	require.ErrorContains(t, err, "unsupported schema version")
	require.Equal(t, ConsumeResult{Applied: 1, Rejected: 1}, result)
	require.Equal(t, seq-1, appliedSeq(t, r))
}

func TestConsumeStopsAtSequenceGap(t *testing.T) {
	r := NewRepository(dbtest.NewPool(t))
	now := time.Now().UTC().Truncate(time.Millisecond)
	empty := observationcontract.CatalogObservation{Chapters: []observationcontract.Chapter{}, Bindings: []observationcontract.Binding{}, Conflicts: []string{}}

	first := publishObservation(t, r, "catalog", now, now, empty)
	gap := publishObservation(t, r, "catalog", now, now, empty)
	publishObservation(t, r, "catalog", now, now, empty)

	_, err := r.pool.Exec(t.Context(), deleteObservationSQL, "jp", gap)
	require.NoError(t, err)

	result, err := r.ConsumeRegion(t.Context(), "jp", 10)
	require.ErrorContains(t, err, "is missing before")
	require.Equal(t, ConsumeResult{Applied: 1}, result)
	require.Equal(t, first, appliedSeq(t, r))
}

func TestConsumeLimitsBatchAndResumes(t *testing.T) {
	r := NewRepository(dbtest.NewPool(t))
	now := time.Now().UTC().Truncate(time.Millisecond)
	empty := observationcontract.CatalogObservation{Chapters: []observationcontract.Chapter{}, Bindings: []observationcontract.Binding{}, Conflicts: []string{}}

	for i := range 3 {
		observed := now.Add(time.Duration(i-3) * time.Second)
		publishObservation(t, r, "catalog", observed, observed, empty)
	}

	result, err := r.ConsumeRegion(t.Context(), "jp", 2)
	require.NoError(t, err)
	require.Equal(t, ConsumeResult{Applied: 2}, result)
	require.Equal(t, int64(2), appliedSeq(t, r))

	result, err = r.ConsumeRegion(t.Context(), "jp", 2)
	require.NoError(t, err)
	require.Equal(t, ConsumeResult{Applied: 1}, result)
	require.Equal(t, int64(3), appliedSeq(t, r))
}

func TestFinalizeWaitsForTimelyFinalPublishedBeforeDeadline(t *testing.T) {
	r := NewRepository(dbtest.NewPool(t))
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Second)
	c := chapterAt("late-consumed", now.Add(-26*time.Hour))
	require.NoError(t, r.StoreCatalog(ctx, "jp", now, []Chapter{c}))

	var id int64

	require.NoError(t, r.pool.QueryRow(ctx, seedRequestSQL, "1", "2", 90, "jp", c.EventID, c.ChapterID).Scan(&id))

	collected := c.RevealsAt.Add(23 * time.Hour)
	publishObservation(t, r, "cutoff", collected, collected, cutoffPayload(c, true, 50))

	require.NoError(t, r.FinalizeRegions(ctx, outboxPublisher(t, r), []string{"jp"}))
	require.Empty(t, requestOutcome(t, r, id))

	_, err := r.ConsumeRegion(ctx, "jp", 10)
	require.NoError(t, err)
	require.NoError(t, r.FinalizeRegions(ctx, outboxPublisher(t, r), []string{"jp"}))
	require.Equal(t, "top1", requestOutcome(t, r, id))
}

func TestFinalizeDefersWhileCutoffLeaseIsInFlight(t *testing.T) {
	r := NewRepository(dbtest.NewPool(t))
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Second)
	c := chapterAt("in-flight", now.Add(-26*time.Hour))
	require.NoError(t, r.StoreCatalog(ctx, "jp", now, []Chapter{c}))

	var id int64

	require.NoError(t, r.pool.QueryRow(ctx, seedRequestSQL, "1", "2", 90, "jp", c.EventID, c.ChapterID).Scan(&id))

	_, err := r.pool.Exec(ctx, inflightLeaseSQL, contract.GradeRegion("jp"), c.EventID, c.ChapterID)
	require.NoError(t, err)

	require.NoError(t, r.FinalizeRegions(ctx, outboxPublisher(t, r), []string{"jp"}))
	require.Empty(t, requestOutcome(t, r, id))

	_, err = r.pool.Exec(ctx, releaseLeaseSQL)
	require.NoError(t, err)
	require.NoError(t, r.FinalizeRegions(ctx, outboxPublisher(t, r), []string{"jp"}))
	require.Equal(t, "unavailable", requestOutcome(t, r, id))
}
