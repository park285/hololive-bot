package dispatchoutbox

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestExpectRowsAffected_PartialBatchReturnsNil(t *testing.T) {
	t.Parallel()

	// MarkSent/MarkSending은 concurrent workers가 동일한 row를 처리할 때
	// RowsAffected < len(ids) 가 정상 상황 — nil이어야 함
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
			name:    "partial match — RowsAffected < len(ids) should return nil for MarkSent/MarkSending",
			got:     1,
			want:    3,
			wantErr: false,
		},
		{
			name:    "zero rows out of many — should return nil for MarkSent/MarkSending",
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

func TestWarnRowsAffected_EmitsMetricOnPartial(t *testing.T) {
	before := testutil.ToFloat64(alarmDispatchPGTransitionPartialTotal)

	// exact match — metric 증가 없음
	_ = warnRowsAffected(3, 3, "mark sent", nil)
	if got := testutil.ToFloat64(alarmDispatchPGTransitionPartialTotal); got != before {
		t.Errorf("exact match incremented metric: got %v, want %v", got, before)
	}

	// zero-of-zero — metric 증가 없음
	_ = warnRowsAffected(0, 0, "mark sending", nil)
	if got := testutil.ToFloat64(alarmDispatchPGTransitionPartialTotal); got != before {
		t.Errorf("zero-of-zero incremented metric: got %v, want %v", got, before)
	}

	// partial — metric 1 증가
	_ = warnRowsAffected(1, 3, "mark sent", nil)
	if got := testutil.ToFloat64(alarmDispatchPGTransitionPartialTotal); got != before+1 {
		t.Errorf("partial did not increment metric: got %v, want %v", got, before+1)
	}

	// 0-of-many — metric 또 1 증가
	_ = warnRowsAffected(0, 5, "mark sending", nil)
	if got := testutil.ToFloat64(alarmDispatchPGTransitionPartialTotal); got != before+2 {
		t.Errorf("zero-of-many did not increment metric: got %v, want %v", got, before+2)
	}
}
