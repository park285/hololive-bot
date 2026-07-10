package dispatchoutbox

import (
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestValidatePostSendRowsAffected_ReturnsTypedPartialError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		got     int64
		want    int
		wantErr bool
	}{
		{
			name:    "exact match returns nil",
			got:     3,
			want:    3,
			wantErr: false,
		},
		{
			name:    "zero of zero returns nil",
			got:     0,
			want:    0,
			wantErr: false,
		},
		{
			name:    "partial match returns observable post-send error",
			got:     1,
			want:    3,
			wantErr: true,
		},
		{
			name:    "zero rows out of many returns observable post-send error",
			got:     0,
			want:    5,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validatePostSendRowsAffected(tc.got, tc.want, "test action", nil)
			if (err != nil) != tc.wantErr {
				t.Errorf("validatePostSendRowsAffected(%d, %d) error = %v, wantErr %v", tc.got, tc.want, err, tc.wantErr)
			}
			if tc.wantErr {
				var partialErr *PartialTransitionError
				if !errors.As(err, &partialErr) {
					t.Fatalf("error = %T %v, want *PartialTransitionError", err, err)
				}
				if partialErr.Updated != tc.got || partialErr.Expected != int64(tc.want) {
					t.Fatalf("PartialTransitionError = %+v, want updated=%d expected=%d", partialErr, tc.got, tc.want)
				}
			}
		})
	}
}

func TestExpectRowsAffected_BlocksPartialMarkSending_140eb6c4(t *testing.T) {
	t.Parallel()

	if err := expectRowsAffected(1, 3, "mark dispatch deliveries sending"); err == nil {
		t.Fatal("partial MarkSending update must fail before external send")
	}
	if err := expectRowsAffected(0, 5, "mark dispatch deliveries sending"); err == nil {
		t.Fatal("zero-row MarkSending update must fail before external send")
	}
	requireNoError(t, expectRowsAffected(3, 3, "mark dispatch deliveries sending"))
}

func TestValidatePostSendRowsAffected_EmitsMetricOnPartial(t *testing.T) {
	before := testutil.ToFloat64(alarmDispatchPGTransitionPartialTotal)

	// exact match — metric 증가 없음
	requireNoError(t, validatePostSendRowsAffected(3, 3, "mark sent", nil))
	if got := testutil.ToFloat64(alarmDispatchPGTransitionPartialTotal); got != before {
		t.Errorf("exact match incremented metric: got %v, want %v", got, before)
	}

	// zero-of-zero — metric 증가 없음
	requireNoError(t, validatePostSendRowsAffected(0, 0, "mark sending", nil))
	if got := testutil.ToFloat64(alarmDispatchPGTransitionPartialTotal); got != before {
		t.Errorf("zero-of-zero incremented metric: got %v, want %v", got, before)
	}

	// partial — metric 1 증가
	if err := validatePostSendRowsAffected(1, 3, "mark sent", nil); err == nil {
		t.Fatal("partial update error = nil")
	}
	if got := testutil.ToFloat64(alarmDispatchPGTransitionPartialTotal); got != before+1 {
		t.Errorf("partial did not increment metric: got %v, want %v", got, before+1)
	}

	// 0-of-many — metric 또 1 증가
	if err := validatePostSendRowsAffected(0, 5, "mark sending", nil); err == nil {
		t.Fatal("zero-of-many update error = nil")
	}
	if got := testutil.ToFloat64(alarmDispatchPGTransitionPartialTotal); got != before+2 {
		t.Errorf("zero-of-many did not increment metric: got %v, want %v", got, before+2)
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
