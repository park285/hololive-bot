package sourceobservation

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/content"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

func persistContentClocks(
	ctx context.Context,
	tx dbx.Tx,
	loaded map[string]content.EntityState,
	clocks []content.EntityState,
) error {
	statements := make([]dbx.Statement, 0, len(clocks))

	for i := range clocks {
		clock := &clocks[i]
		if clock.LastPositiveValueSHA256 == "" {
			continue
		}

		coverage, err := content.MarshalCoverage(clock.LastPositiveCoverage)
		if err != nil {
			return fmt.Errorf("marshal content clock coverage: %w", err)
		}

		if stored, ok := loaded[clock.VideoID]; ok && stored.LastPositiveValueSHA256 != "" {
			unchanged, compareErr := sameStoredContentClock(&stored, clock, coverage)
			if compareErr != nil {
				return fmt.Errorf("compare content clock: %w", compareErr)
			}

			if unchanged {
				continue
			}
		}

		statements = append(statements, contentClockStatement(clock, coverage))
	}

	if err := dbx.ExecStatements(ctx, tx, statements); err != nil {
		return fmt.Errorf("upsert content evidence clocks: %w", err)
	}

	return nil
}

func contentClockStatement(clock *content.EntityState, coverage []byte) dbx.Statement {
	var lastAbs any

	if clock.LastAbsenceObservationID != 0 {
		lastAbs = clock.LastAbsenceObservationID
	}

	return dbx.Statement{
		Operation: "upsert content evidence clock",
		SQL:       contentClockStatementSQL,
		Args: []any{
			clock.VideoID,
			clock.FirstPositiveEffectiveAt,
			clock.Clock.LastPositiveEffectiveAt,
			clock.Clock.LastPositiveReceivedAt,
			clock.LastPositiveValueSHA256,
			clock.LastPositiveScopeSHA256,
			coverage,
			clock.Clock.LastNegativeEffectiveAt,
			clock.LastNegativeReceivedAt,
			clock.FirstAbsenceScheduledFor,
			clock.SecondAbsenceScheduledFor,
			lastAbs,
			clock.Clock.MissingSinceEffectiveAt,
			clock.ConsecutiveAbsenceSlots,
			clock.WithdrawnAt,
			clock.NoveltyPending,
		},
	}
}

func sameStoredContentClock(stored, next *content.EntityState, nextCoverage []byte) (bool, error) {
	storedCoverage, err := content.MarshalCoverage(stored.LastPositiveCoverage)
	if err != nil {
		return false, fmt.Errorf("marshal stored content clock coverage: %w", err)
	}

	storedArgs := contentClockStatement(stored, storedCoverage).Args
	nextArgs := contentClockStatement(next, nextCoverage).Args

	for i := range nextArgs {
		same, err := sameContentClockArg(storedArgs[i], nextArgs[i])
		if err != nil {
			return false, fmt.Errorf("compare content clock arg $%d: %w", i+1, err)
		}

		if !same {
			return false, nil
		}
	}

	return true, nil
}

func sameContentClockArg(stored, next any) (bool, error) {
	switch storedValue := stored.(type) {
	case time.Time:
		nextValue, ok := next.(time.Time)

		return ok && storedValue.Equal(nextValue), nil
	case *time.Time:
		nextValue, ok := next.(*time.Time)

		return ok && sameClockTime(storedValue, nextValue), nil
	case []byte:
		nextValue, ok := next.([]byte)

		return ok && bytes.Equal(storedValue, nextValue), nil
	case nil, string, int, int64, bool:
		return stored == next, nil
	default:
		return false, fmt.Errorf("unsupported content clock arg type %T", stored)
	}
}

func sameClockTime(stored, next *time.Time) bool {
	if stored == nil || next == nil {
		return stored == nil && next == nil
	}

	return stored.Equal(*next)
}
