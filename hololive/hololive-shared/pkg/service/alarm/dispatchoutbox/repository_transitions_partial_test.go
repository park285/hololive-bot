package dispatchoutbox

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestWarnRowsAffected_PartialBatchReturnsNilForPostSendTransitions(t *testing.T) {
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
			name:    "partial match returns nil for post-send transition",
			got:     1,
			want:    3,
			wantErr: false,
		},
		{
			name:    "zero rows out of many returns nil for post-send transition",
			got:     0,
			want:    5,
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := warnRowsAffected(tc.got, tc.want, "test action", nil)
			if (err != nil) != tc.wantErr {
				t.Errorf("warnRowsAffected(%d, %d) error = %v, wantErr %v", tc.got, tc.want, err, tc.wantErr)
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

func TestWarnRowsAffected_EmitsMetricOnPartial(t *testing.T) {
	before := testutil.ToFloat64(alarmDispatchPGTransitionPartialTotal)

	// exact match — metric 증가 없음
	requireNoError(t, warnRowsAffected(3, 3, "mark sent", nil))
	if got := testutil.ToFloat64(alarmDispatchPGTransitionPartialTotal); got != before {
		t.Errorf("exact match incremented metric: got %v, want %v", got, before)
	}

	// zero-of-zero — metric 증가 없음
	requireNoError(t, warnRowsAffected(0, 0, "mark sending", nil))
	if got := testutil.ToFloat64(alarmDispatchPGTransitionPartialTotal); got != before {
		t.Errorf("zero-of-zero incremented metric: got %v, want %v", got, before)
	}

	// partial — metric 1 증가
	requireNoError(t, warnRowsAffected(1, 3, "mark sent", nil))
	if got := testutil.ToFloat64(alarmDispatchPGTransitionPartialTotal); got != before+1 {
		t.Errorf("partial did not increment metric: got %v, want %v", got, before+1)
	}

	// 0-of-many — metric 또 1 증가
	requireNoError(t, warnRowsAffected(0, 5, "mark sending", nil))
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
