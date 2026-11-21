package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/dbx"
)

type publishStreamRows struct {
	pgx.Rows

	ordinals  []int
	index     int
	streamErr error
	scanErr   error
	closed    bool
}

func (r *publishStreamRows) Next() bool {
	if r.index == len(r.ordinals) {
		return false
	}

	r.index++

	return true
}

func (r *publishStreamRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}

	if len(dest) != 7 {
		return fmt.Errorf("publish stream fixture: destination count %d", len(dest))
	}

	return errors.Join(
		assignPublishStreamDestination(dest[0], r.ordinals[r.index-1]),
		assignPublishStreamDestination(dest[1], int64(r.index)),
		assignPublishStreamDestination(dest[2], false),
		assignPublishStreamDestination(dest[3], false),
		assignPublishStreamDestination(dest[4], false),
		assignPublishStreamDestination(dest[5], (*time.Time)(nil)),
		assignPublishStreamDestination(dest[6], time.Unix(1, 0)),
	)
}

func assignPublishStreamDestination[T any](destination any, value T) error {
	target, ok := destination.(*T)
	if !ok || target == nil {
		return fmt.Errorf("publish stream fixture: invalid destination %T", destination)
	}

	*target = value

	return nil
}

func (r *publishStreamRows) Err() error { return r.streamErr }
func (r *publishStreamRows) Close()     { r.closed = true }

type publishStreamTx struct {
	dbx.Tx

	rows *publishStreamRows
}

func (tx publishStreamTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return tx.rows, nil
}

func TestPublishSetStreamErrorPrecedesCompleteness(t *testing.T) {
	checkErr := &pgconn.PgError{Code: "23514", ConstraintName: "payload_check"}
	scanErr := errors.New("scan conversion failed")

	for _, tc := range []struct {
		name                     string
		ordinals                 []int
		streamErr, scanErr, want error
		reject                   bool
	}{
		{name: "zero rows", streamErr: checkErr, want: checkErr},
		{name: "partial rows", ordinals: []int{0}, streamErr: checkErr, want: checkErr},
		{name: "all rows", ordinals: []int{1, 0}, streamErr: checkErr, want: checkErr},
		{name: "canceled", ordinals: []int{0}, streamErr: context.Canceled, want: context.Canceled},
		{name: "deadline", streamErr: context.DeadlineExceeded, want: context.DeadlineExceeded},
		{name: "clean missing", ordinals: []int{0}, reject: true},
		{name: "duplicate before drain", ordinals: []int{0, 0}, streamErr: checkErr, reject: true},
		{name: "out of range before drain", ordinals: []int{2}, streamErr: checkErr, reject: true},
		{name: "negative ordinal", ordinals: []int{-1}, reject: true},
		{name: "scan before drain", ordinals: []int{0}, scanErr: scanErr, streamErr: checkErr, want: scanErr},
		{name: "reordered success", ordinals: []int{1, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := &publishStreamRows{ordinals: tc.ordinals, streamErr: tc.streamErr, scanErr: tc.scanErr}
			result, collision, err := publishObservationSet(t.Context(), publishStreamTx{rows: rows}, nil, 2)

			require.True(t, rows.closed)
			require.False(t, collision)

			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)

				if errors.Is(tc.want, checkErr) {
					got, ok := errors.AsType[*pgconn.PgError](err)
					require.True(t, ok)
					require.Same(t, checkErr, got)
				}

				return
			}

			if tc.reject {
				require.Error(t, err)
				require.NotErrorIs(t, err, checkErr)

				return
			}

			require.NoError(t, err)
			require.Len(t, result.Results, 2)
			require.Equal(t, 0, result.Results[0].Ordinal)
			require.Equal(t, 1, result.Results[1].Ordinal)
		})
	}
}
