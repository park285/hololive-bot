package dispatchoutbox

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
)

func requireNoError(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRouteFailures_ValidatesTargetStatusBeforeSQL(t *testing.T) {
	t.Parallel()

	repository := &PgxRepository{}
	err := repository.RouteFailures(t.Context(), []FailureUpdate{
		{ID: 5, AttemptCount: 1, TargetStatus: StatusSending},
	}, "worker-1")

	if err == nil || !strings.Contains(err.Error(), "unsupported target status") {
		t.Fatalf("RouteFailures() error = %v, want unsupported target status rejection", err)
	}

	requireNoError(t, repository.RouteFailures(t.Context(), nil, "worker-1"))
	requireNoError(t, repository.RouteSendingFailures(t.Context(), nil, "worker-1"))
}

func TestFenceMissMetricCountsEachReasonOncePerTransition(t *testing.T) {
	statusLabel := alarmDispatchPGTransitionPartialTotal.WithLabelValues(fenceActionMarkSent, string(sendfence.ReasonStatusChanged))
	ownerLabel := alarmDispatchPGTransitionPartialTotal.WithLabelValues(fenceActionMarkSent, string(sendfence.ReasonOwnerChanged))
	statusBefore := testutil.ToFloat64(statusLabel)
	ownerBefore := testutil.ToFloat64(ownerLabel)

	observePGFenceMiss(&sendfence.MissError{
		Action:   fenceActionMarkSent,
		Expected: 3,
		Rows: []sendfence.RowMiss{
			{ID: 1, Reason: sendfence.ReasonStatusChanged},
			{ID: 2, Reason: sendfence.ReasonStatusChanged},
			{ID: 3, Reason: sendfence.ReasonOwnerChanged},
		},
	})

	if got := testutil.ToFloat64(statusLabel); got != statusBefore+1 {
		t.Errorf("status_changed = %v, want %v", got, statusBefore+1)
	}

	if got := testutil.ToFloat64(ownerLabel); got != ownerBefore+1 {
		t.Errorf("owner_changed = %v, want %v", got, ownerBefore+1)
	}
}
