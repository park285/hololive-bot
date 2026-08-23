package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
)

func TestRetryableObservationErrorClassification(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "deadline exceeded", err: context.DeadlineExceeded, want: true},
		{name: "wrapped deadline exceeded", err: fmt.Errorf("consume: %w", context.DeadlineExceeded), want: true},
		{name: "serialization failure", err: &pgconn.PgError{Code: "40001"}, want: true},
		{name: "deadlock", err: &pgconn.PgError{Code: "40P01"}, want: true},
		{name: "lock not available", err: &pgconn.PgError{Code: "55P03"}, want: true},
		{name: "connection exception", err: &pgconn.PgError{Code: "08000"}, want: true},
		{name: "connection failure", err: fmt.Errorf("finalize: %w", &pgconn.PgError{Code: "08006"}), want: true},
		{name: "protocol violation", err: &pgconn.PgError{Code: "08P01"}, want: true},
		{name: "too many connections", err: &pgconn.PgError{Code: "53300"}, want: true},
		{name: "query canceled", err: &pgconn.PgError{Code: "57014"}, want: true},
		{name: "admin shutdown", err: &pgconn.PgError{Code: "57P01"}, want: true},
		{name: "crash shutdown", err: &pgconn.PgError{Code: "57P02"}, want: true},
		{name: "cannot connect now", err: &pgconn.PgError{Code: "57P03"}, want: true},
		{name: "broken pipe", err: &net.OpError{Op: "write", Net: "tcp", Err: syscall.EPIPE}, want: true},
		{name: "wrapped connection reset", err: fmt.Errorf("consume: %w", &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}), want: true},
		{name: "closed network connection", err: &net.OpError{Op: "write", Net: "tcp", Err: net.ErrClosed}, want: true},
		{name: "unexpected eof", err: fmt.Errorf("consume: %w", io.ErrUnexpectedEOF), want: true},
		{name: "eof", err: io.EOF, want: true},
		{name: "conn closed", err: fmt.Errorf("consume: %w", pgconn.ErrConnClosed), want: true},
		{name: "unique violation", err: &pgconn.PgError{Code: "23505"}, want: false},
		{name: "undefined table", err: &pgconn.PgError{Code: "42P01"}, want: false},
		{name: "invalid text representation", err: &pgconn.PgError{Code: "22P02"}, want: false},
		{name: "canceled", err: context.Canceled, want: false},
		{name: "claim lost", err: sourceobservation.ErrClaimLost, want: false},
		{name: "unknown", err: errors.New("unexpected canonical write failure"), want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := retryableObservationError(tc.err); got != tc.want {
				t.Fatalf("retryableObservationError(%v) = %t, want %t", tc.err, got, tc.want)
			}
		})
	}
}

func TestNonRetryableConsumeErrorDeadLettersWithoutExit(t *testing.T) {
	var retries atomic.Int64
	var deadLettered atomic.Int64
	var input sourceobservation.DeadLetterInput
	runtime := newTestRuntime(deadLetterClaimer{
		fakeClaimer: fakeClaimer{
			retry: func(context.Context, sourceobservation.RetryInput) (contract.Status, error) {
				retries.Add(1)
				return contract.StatusPending, nil
			},
		},
		deadLetter: func(_ context.Context, in sourceobservation.DeadLetterInput) error {
			deadLettered.Add(1)
			input = in
			return nil
		},
	}, fakeConsumer{
		consume: func(context.Context, sourceobservation.Claim) error {
			return &pgconn.PgError{Code: "22P02", Message: "invalid input syntax"}
		},
	})
	observation := sourceobservation.ClaimWork{
		ObservationID:   31,
		LeaseToken:      strings.Repeat("ab", 32),
		ObservationKind: contract.KindCommunityPage,
		SubjectKey:      "UC_POISON",
	}

	if err := runtime.processClaim(context.Background(), observation); err != nil {
		t.Fatalf("processClaim() error = %v, want nil after dead letter", err)
	}
	if deadLettered.Load() != 1 {
		t.Fatalf("dead letter calls = %d, want 1", deadLettered.Load())
	}
	if retries.Load() != 0 {
		t.Fatalf("poison observation retried %d time(s)", retries.Load())
	}
	if input.ObservationID != observation.ObservationID || input.LeaseToken != observation.LeaseToken {
		t.Fatalf("DeadLetter input = %#v", input)
	}
	if input.ErrorCode == "" || !strings.Contains(input.ErrorDetail, "22P02") {
		t.Fatalf("DeadLetter error fields = (%q, %q)", input.ErrorCode, input.ErrorDetail)
	}
	if !runtime.Degraded() {
		t.Fatal("immediate dead letter did not degrade the plane")
	}
	if _, ok := runtime.inFlight.Load(observation.Key()); ok {
		t.Fatal("dead-lettered observation remained in flight")
	}
}

func TestDeadLetterClaimLostDoesNotDegrade(t *testing.T) {
	runtime := newTestRuntime(deadLetterClaimer{
		deadLetter: func(context.Context, sourceobservation.DeadLetterInput) error {
			return sourceobservation.ErrClaimLost
		},
	}, fakeConsumer{
		consume: func(context.Context, sourceobservation.Claim) error {
			return errors.New("unexpected canonical write failure")
		},
	})
	observation := sourceobservation.ClaimWork{
		ObservationID:   32,
		LeaseToken:      strings.Repeat("cd", 32),
		ObservationKind: contract.KindCommunityPage,
		SubjectKey:      "UC_LOST",
	}

	if err := runtime.processClaim(context.Background(), observation); err != nil {
		t.Fatalf("processClaim() error = %v", err)
	}
	if runtime.Degraded() {
		t.Fatal("lost claim during dead letter degraded the plane")
	}
}

func TestDeadLetterWriteFailureFailsClosed(t *testing.T) {
	runtime := newTestRuntime(deadLetterClaimer{
		deadLetter: func(context.Context, sourceobservation.DeadLetterInput) error {
			return errors.New("dead letter write failed")
		},
	}, fakeConsumer{
		consume: func(context.Context, sourceobservation.Claim) error {
			return errors.New("unexpected canonical write failure")
		},
	})
	observation := sourceobservation.ClaimWork{
		ObservationID:   33,
		LeaseToken:      strings.Repeat("ef", 32),
		ObservationKind: contract.KindCommunityPage,
		SubjectKey:      "UC_DLQ_FAIL",
	}

	err := runtime.processClaim(context.Background(), observation)
	if err == nil || !strings.Contains(err.Error(), "dead letter observation 33") || !strings.Contains(err.Error(), "dead letter write failed") {
		t.Fatalf("processClaim() error = %v, want wrapped dead letter failure", err)
	}
	if _, ok := runtime.inFlight.Load(observation.Key()); ok {
		t.Fatal("observation remained in flight after dead letter failure")
	}
}

func TestConnectionLossConsumeErrorIsRetried(t *testing.T) {
	var retries atomic.Int64
	var deadLettered atomic.Int64
	runtime := newTestRuntime(deadLetterClaimer{
		fakeClaimer: fakeClaimer{
			retry: func(context.Context, sourceobservation.RetryInput) (contract.Status, error) {
				retries.Add(1)
				return contract.StatusPending, nil
			},
		},
		deadLetter: func(context.Context, sourceobservation.DeadLetterInput) error {
			deadLettered.Add(1)
			return nil
		},
	}, fakeConsumer{
		consume: func(context.Context, sourceobservation.Claim) error {
			return fmt.Errorf("finalize: %w", &net.OpError{Op: "write", Net: "tcp", Err: syscall.EPIPE})
		},
	})
	observation := sourceobservation.ClaimWork{
		ObservationID:   34,
		LeaseToken:      strings.Repeat("ab", 32),
		ObservationKind: contract.KindCommunityPage,
		SubjectKey:      "UC_CONN_LOSS",
	}

	if err := runtime.processClaim(context.Background(), observation); err != nil {
		t.Fatalf("processClaim() error = %v", err)
	}
	if retries.Load() != 1 || deadLettered.Load() != 0 {
		t.Fatalf("retries = %d, dead letters = %d; want 1, 0", retries.Load(), deadLettered.Load())
	}
	if runtime.Degraded() {
		t.Fatal("transient connection loss degraded the plane")
	}
}

type deadLetterClaimer struct {
	fakeClaimer
	deadLetter func(context.Context, sourceobservation.DeadLetterInput) error
}

func (c deadLetterClaimer) DeadLetter(ctx context.Context, input sourceobservation.DeadLetterInput) error {
	if c.deadLetter == nil {
		return nil
	}
	return c.deadLetter(ctx, input)
}
