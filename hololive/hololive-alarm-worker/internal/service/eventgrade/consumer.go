package eventgrade

import (
	"context"
	_ "embed"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	contract "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	observationcontract "github.com/kapu/hololive-shared/pkg/contracts/eventgrade"
)

//go:embed queries/lock_offset.sql
var lockOffsetSQL string

//go:embed queries/next_observations.sql
var nextObservationsSQL string

//go:embed queries/advance_offset.sql
var advanceOffsetSQL string

var errRejected = errors.New("event grade observation rejected")

type observation struct {
	seq        int64
	version    int16
	kind       string
	startedAt  time.Time
	observedAt time.Time
	payload    []byte
}

type ConsumeResult struct {
	Applied  int
	Rejected int
}

func (r *Repository) ConsumeRegion(ctx context.Context, region contract.GradeRegion, limit int) (ConsumeResult, error) {
	if !region.Valid() || limit < 1 {
		return ConsumeResult{}, errors.New("invalid event grade consume request")
	}

	var batch consumedBatch

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var batchErr error

		batch, batchErr = r.consumeBatch(ctx, tx, region, limit)

		return batchErr
	})
	if err != nil {
		return ConsumeResult{}, fmt.Errorf("consume event grade observations: %w", err)
	}

	if batch.stopErr != nil {
		return batch.result, fmt.Errorf("consume event grade observations: %w", batch.stopErr)
	}

	return batch.result, nil
}

type consumedBatch struct {
	result  ConsumeResult
	stopErr error
}

func (r *Repository) consumeBatch(ctx context.Context, tx pgx.Tx, region contract.GradeRegion, limit int) (consumedBatch, error) {
	var (
		out       consumedBatch
		applied   int64
		published int64
	)

	if err := tx.QueryRow(ctx, lockOffsetSQL, region).Scan(&applied, &published); err != nil {
		return out, fmt.Errorf("lock event grade consumer offset: %w", err)
	}

	observations, err := readObservations(ctx, tx, region, applied, limit)
	if err != nil {
		return out, err
	}

	last := applied

	for i := range observations {
		rejected, consumeErr := r.consumeObservation(ctx, tx, region, last+1, &observations[i])
		if consumeErr != nil {
			out.stopErr = consumeErr

			break
		}

		last++

		if rejected {
			out.result.Rejected++
		} else {
			out.result.Applied++
		}
	}

	if out.stopErr == nil && len(observations) < limit && last < published {
		out.stopErr = fmt.Errorf("event grade observation %d is missing before published %d", last+1, published)
	}

	if last == applied {
		return out, out.stopErr
	}

	tag, err := tx.Exec(ctx, advanceOffsetSQL, region, applied, last)
	if err != nil {
		return out, fmt.Errorf("advance event grade consumer offset: %w", err)
	}

	if tag.RowsAffected() != 1 {
		return out, errors.New("event grade consumer offset moved concurrently")
	}

	return out, nil
}

func readObservations(ctx context.Context, tx pgx.Tx, region contract.GradeRegion, applied int64, limit int) ([]observation, error) {
	rows, err := tx.Query(ctx, nextObservationsSQL, region, applied, limit)
	if err != nil {
		return nil, fmt.Errorf("read event grade observations after %d: %w", applied, err)
	}

	observations, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (observation, error) {
		var obs observation

		scanErr := row.Scan(&obs.seq, &obs.version, &obs.kind, &obs.startedAt, &obs.observedAt, &obs.payload)

		return obs, scanErr
	})
	if err != nil {
		return nil, fmt.Errorf("read event grade observations after %d: %w", applied, err)
	}

	return observations, nil
}

func (r *Repository) consumeObservation(ctx context.Context, tx pgx.Tx, region contract.GradeRegion, seq int64, obs *observation) (rejected bool, err error) {
	if obs.seq != seq {
		return false, fmt.Errorf("event grade observation %d is missing before %d", seq, obs.seq)
	}

	if obs.version != observationcontract.SchemaVersion {
		return false, fmt.Errorf("event grade observation %d has unsupported schema version %d", seq, obs.version)
	}

	savepoint, err := tx.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin event grade observation apply: %w", err)
	}

	applyErr := r.applyObservation(ctx, savepoint, region, obs)

	switch {
	case applyErr == nil:
		if err := savepoint.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit event grade observation apply: %w", err)
		}

		return false, nil
	case rejectedApply(applyErr):
		if err := savepoint.Rollback(ctx); err != nil {
			return false, fmt.Errorf("discard rejected event grade observation: %w", err)
		}

		return true, nil
	default:
		if err := savepoint.Rollback(ctx); err != nil {
			return false, errors.Join(fmt.Errorf("apply event grade observation %d: %w", seq, applyErr), fmt.Errorf("discard failed event grade observation: %w", err))
		}

		return false, fmt.Errorf("apply event grade observation %d: %w", seq, applyErr)
	}
}

func rejectedApply(err error) bool {
	if errors.Is(err, errRejected) {
		return true
	}

	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		class := pgErr.Code[:2]
		return class == "22" || class == "23"
	}

	return false
}

func (r *Repository) applyObservation(ctx context.Context, tx pgx.Tx, region contract.GradeRegion, obs *observation) error {
	switch obs.kind {
	case observationcontract.KindCatalog:
		var body observationcontract.CatalogObservation

		if err := jsonv2.Unmarshal(obs.payload, &body); err != nil {
			return fmt.Errorf("%w: decode catalog: %w", errRejected, err)
		}

		chapters, bindings := catalogFromObservation(&body)
		if err := validateCatalogSnapshot(region, obs.observedAt, chapters, bindings, body.Conflicts); err != nil {
			return err
		}

		return r.storeCatalogSnapshotTx(ctx, tx, region, obs.observedAt, chapters, bindings, body.Conflicts, body.QuarantineMissing)
	case observationcontract.KindCutoff:
		var body observationcontract.CutoffObservation

		if err := jsonv2.Unmarshal(obs.payload, &body); err != nil {
			return fmt.Errorf("%w: decode cutoff: %w", errRejected, err)
		}

		border := Border{
			Region: region, EventID: body.EventID, ChapterID: body.ChapterID, BoardKind: body.BoardKind,
			StartedAt: obs.startedAt, ObservedAt: obs.observedAt, Final: body.Final, Ranks: ranksFromObservation(body.Ranks),
		}

		ranks, scores, err := validateBorder(&border)
		if err != nil {
			return err
		}

		return r.storeBorderTx(ctx, tx, &border, ranks, scores)
	default:
		return fmt.Errorf("%w: unknown observation kind %q", errRejected, obs.kind)
	}
}

func catalogFromObservation(body *observationcontract.CatalogObservation) ([]Chapter, []ChapterBinding) {
	if body.Chapters == nil || body.Bindings == nil {
		return nil, nil
	}

	chapters := make([]Chapter, len(body.Chapters))
	for i := range body.Chapters {
		chapters[i] = Chapter(body.Chapters[i])
	}

	bindings := make([]ChapterBinding, len(body.Bindings))
	for i, b := range body.Bindings {
		bindings[i] = ChapterBinding(b)
	}

	return chapters, bindings
}

func ranksFromObservation(ranks []observationcontract.Rank) []RankScore {
	if ranks == nil {
		return nil
	}

	out := make([]RankScore, len(ranks))
	for i, rank := range ranks {
		out[i] = RankScore(rank)
	}

	return out
}
