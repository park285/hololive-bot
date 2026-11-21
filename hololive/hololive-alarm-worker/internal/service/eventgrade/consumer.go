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

//go:embed queries/next_observation.sql
var nextObservationSQL string

//go:embed queries/advance_offset.sql
var advanceOffsetSQL string

var errRejected = errors.New("event grade observation rejected")

type observation struct {
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
	var result ConsumeResult

	if !region.Valid() || limit < 1 {
		return result, errors.New("invalid event grade consume request")
	}

	for range limit {
		consumed, rejected, err := r.consumeNext(ctx, region)
		if err != nil {
			return result, err
		}

		if !consumed {
			return result, nil
		}

		if rejected {
			result.Rejected++
		} else {
			result.Applied++
		}
	}

	return result, nil
}

func (r *Repository) consumeNext(ctx context.Context, region contract.GradeRegion) (consumed, rejected bool, err error) {
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var applied int64

		if scanErr := tx.QueryRow(ctx, lockOffsetSQL, region).Scan(&applied); scanErr != nil {
			return fmt.Errorf("lock event grade consumer offset: %w", scanErr)
		}

		var obs observation

		scanErr := tx.QueryRow(ctx, nextObservationSQL, region, applied+1).Scan(&obs.version, &obs.kind, &obs.startedAt, &obs.observedAt, &obs.payload)
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return nil
		}

		if scanErr != nil {
			return fmt.Errorf("read event grade observation %d: %w", applied+1, scanErr)
		}

		if obs.version != observationcontract.SchemaVersion {
			return fmt.Errorf("event grade observation %d has unsupported schema version %d", applied+1, obs.version)
		}

		savepoint, beginErr := tx.Begin(ctx)
		if beginErr != nil {
			return fmt.Errorf("begin event grade observation apply: %w", beginErr)
		}

		applyErr := r.applyObservation(ctx, savepoint, region, &obs)

		switch {
		case applyErr == nil:
			if commitErr := savepoint.Commit(ctx); commitErr != nil {
				return fmt.Errorf("commit event grade observation apply: %w", commitErr)
			}
		case rejectedApply(applyErr):
			if rollbackErr := savepoint.Rollback(ctx); rollbackErr != nil {
				return fmt.Errorf("discard rejected event grade observation: %w", rollbackErr)
			}

			rejected = true
		default:
			return fmt.Errorf("apply event grade observation %d: %w", applied+1, applyErr)
		}

		tag, advanceErr := tx.Exec(ctx, advanceOffsetSQL, region, applied+1)
		if advanceErr != nil {
			return fmt.Errorf("advance event grade consumer offset: %w", advanceErr)
		}

		if tag.RowsAffected() != 1 {
			return errors.New("event grade consumer offset moved concurrently")
		}

		consumed = true

		return nil
	})
	if err != nil {
		return false, false, fmt.Errorf("consume event grade observation: %w", err)
	}

	return consumed, rejected, nil
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
