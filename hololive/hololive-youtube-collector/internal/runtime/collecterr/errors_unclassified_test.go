package collecterr

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

func TestIsUnclassifiedSeparatesDefaultBucketFromExplicitClass(t *testing.T) {
	hint, hintErr := NewRetryAfterHint(time.Second)
	if hintErr != nil {
		t.Fatalf("NewRetryAfterHint() error = %v", hintErr)
	}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "explicit internal invariant", err: Wrap(Internal, ClassInternal, errors.New("invariant")), want: false},
		{name: "explicit transient", err: Wrap(Failed, ClassTransient, errors.New("upstream")), want: false},
		{name: "raw deadline is classified as timeout", err: context.DeadlineExceeded, want: false},
		{name: "raw eof is classified as transient", err: io.EOF, want: false},
		{name: "raw unknown", err: errors.New("acquire connection"), want: true},
		{name: "FromContext catch-all", err: FromContext(errors.New("read body")), want: true},
		{name: "FromContext socket deadline", err: FromContext(os.ErrDeadlineExceeded), want: true},
		{name: "WithRetry keeps unclassified base", err: WithRetry(FromContext(errors.New("read body")), hint), want: true},
		{name: "WithRetry keeps classified base", err: WithRetry(Wrap(Internal, ClassInternal, errors.New("invariant")), hint), want: false},
		{name: "WithRetry with invalid hint keeps unclassified base", err: WithRetry(FromContext(errors.New("read body")), RetryHint{}), want: true},
		{name: "wrapped unclassified", err: errors.Join(errors.New("outer"), FromContext(errors.New("read body"))), want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IsUnclassified(test.err); got != test.want {
				t.Fatalf("IsUnclassified() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestUnclassifiedErrorsKeepDurableFailureTuple(t *testing.T) {
	diagnostic := DiagnosticOf(FromContext(errors.New("read body")))

	if diagnostic.Code() != Internal || diagnostic.Class() != ClassInternal {
		t.Fatalf("DiagnosticOf() = %s/%s, want %s/%s — 미분류 표식이 계약 tuple을 바꿨다",
			diagnostic.Code(), diagnostic.Class(), Internal, ClassInternal)
	}
}
