package sourceobservation

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/content"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

// persistContentClocks는 Decision.Clocks 전체 스냅샷 중 로드 시점 값과 달라진 clock만 한 번의 배치로 저장한다.
// Decision.Clocks는 reducer 수렴 계약상 전체 상태를 유지하므로 변경 판별은 persist 계층이 로드 상태와 비교해 소유한다.
// 판별이 놓친 동일 값은 0037의 IS DISTINCT FROM 가드가 다시 걸러 새 튜플을 만들지 않는다.
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
		SQL:       mustSQL("repository_content_clock_upsert_0037_37.sql"),
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
		},
	}
}

// sameStoredContentClock은 0037에 보낼 인자가 로드 값으로 만든 인자와 모두 같은지 판별한다.
// Go가 같다고 판정한 clock은 문장을 보내지 않으므로 SQL 가드가 되살릴 수 없다. 그래서 두 인자 목록을
// 같은 contentClockStatement로 만들어, 0037에 열을 추가해도 비교에서 빠지지 않게 한다.
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

// sameContentClockArg는 0037 인자 하나를 DB에 저장될 값 기준으로 비교한다. 시각은 location과 무관하게 Equal로 비교한다.
// 비교 규칙이 없는 형식을 같다고 보면 변경이 조용히 유실되므로 오류로 드러낸다.
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
	case nil, string, int, int64:
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
