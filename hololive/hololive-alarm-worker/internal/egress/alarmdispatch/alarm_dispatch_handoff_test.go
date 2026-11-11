package alarmdispatch

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v5/iris"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/handoffcheck"
	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
)

const (
	testHandoffIrisRequestID = "iris-request-1"
	testHandoffHorizon       = 10 * time.Minute
	handoffOpDLQ             = "dlq"
)

type handoffCall struct {
	op         string
	unitID     int64
	step       time.Duration
	horizon    time.Duration
	retryAfter time.Duration
	code       string
}

type handoffTestConsumer struct {
	mu     sync.Mutex
	claims []dispatchoutbox.HandoffClaim
	calls  []handoffCall
}

func (c *handoffTestConsumer) ClaimHandoffDue(context.Context, int, time.Duration) ([]dispatchoutbox.HandoffClaim, error) {
	claims := c.claims

	c.claims = nil

	return claims, nil
}

func (c *handoffTestConsumer) record(call handoffCall) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.calls = append(c.calls, call)
}

func (c *handoffTestConsumer) CompleteHandoff(_ context.Context, claim *dispatchoutbox.HandoffClaim) error {
	c.record(handoffCall{op: "complete", unitID: claim.UnitID})

	return nil
}

func (c *handoffTestConsumer) RescheduleHandoff(_ context.Context, claim *dispatchoutbox.HandoffClaim, step, horizon time.Duration) error {
	c.record(handoffCall{op: "reschedule", unitID: claim.UnitID, step: step, horizon: horizon})

	return nil
}

func (c *handoffTestConsumer) ReissueAfterHandoff(_ context.Context, claim *dispatchoutbox.HandoffClaim, retryAfter time.Duration, code, _ string) error {
	c.record(handoffCall{op: "reissue", unitID: claim.UnitID, retryAfter: retryAfter, code: code})

	return nil
}

func (c *handoffTestConsumer) DLQAfterHandoff(_ context.Context, claim *dispatchoutbox.HandoffClaim, code, _ string) error {
	c.record(handoffCall{op: handoffOpDLQ, unitID: claim.UnitID, code: code})

	return nil
}

func (c *handoffTestConsumer) QuarantineAfterHandoff(_ context.Context, claim *dispatchoutbox.HandoffClaim, code, _ string) error {
	c.record(handoffCall{op: "quarantine", unitID: claim.UnitID, code: code})

	return nil
}

type handoffTestStatusClient struct {
	mu       sync.Mutex
	queried  []string
	snapshot func(requestID string) (*iris.ReplyStatusSnapshot, error)
}

func (c *handoffTestStatusClient) GetReplyStatus(_ context.Context, requestID string) (*iris.ReplyStatusSnapshot, error) {
	c.mu.Lock()

	c.queried = append(c.queried, requestID)
	c.mu.Unlock()

	return c.snapshot(requestID)
}

func handoffStatus(state string) func(string) (*iris.ReplyStatusSnapshot, error) {
	return func(requestID string) (*iris.ReplyStatusSnapshot, error) {
		return &iris.ReplyStatusSnapshot{RequestID: requestID, State: state}, nil
	}
}

func newHandoffTestReconciler(t *testing.T, consumer *handoffTestConsumer, client *handoffTestStatusClient) *HandoffReconciler {
	t.Helper()

	reconciler, err := NewHandoffReconciler(consumer, client, HandoffReconcilerConfig{Horizon: testHandoffHorizon, PollInterval: time.Second}, nil)
	require.NoError(t, err)

	return reconciler
}

func TestHandoffReconcilerRoutesObservationToLedgerAction(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		status     func(string) (*iris.ReplyStatusSnapshot, error)
		generation int
		attempt    int
		elapsed    time.Duration
		want       handoffCall
	}{
		{name: "completed", status: handoffStatus("handoff_completed"), want: handoffCall{op: "complete"}},
		{name: "failed with budget", status: handoffStatus("failed"), attempt: 1, want: handoffCall{op: "reissue", retryAfter: 10 * time.Second, code: handoffcheck.CodeFailed}},
		{name: "failed without attempt budget", status: handoffStatus("failed"), attempt: alarmDispatchMaxAttempts - 1, want: handoffCall{op: handoffOpDLQ, code: handoffcheck.CodeFailed}},
		{name: "failed at last generation", status: handoffStatus("failed"), generation: iris.ReplyReissueMaxGenerations, want: handoffCall{op: handoffOpDLQ, code: handoffcheck.CodeFailed}},
		{name: "queued before horizon", status: handoffStatus("queued"), elapsed: 30 * time.Second, want: handoffCall{op: "reschedule", step: time.Minute, horizon: testHandoffHorizon}},
		{name: "query error before horizon", status: func(string) (*iris.ReplyStatusSnapshot, error) {
			return nil, &iris.HTTPError{StatusCode: http.StatusServiceUnavailable}
		}, elapsed: 5 * time.Second, want: handoffCall{op: "reschedule", step: 15 * time.Second, horizon: testHandoffHorizon}},
		{name: "queued at horizon", status: handoffStatus("queued"), elapsed: testHandoffHorizon, want: handoffCall{op: "quarantine", code: handoffcheck.CodeUnconfirmed}},
		{name: "status missing", status: func(string) (*iris.ReplyStatusSnapshot, error) {
			return nil, &iris.HTTPError{StatusCode: http.StatusNotFound}
		}, want: handoffCall{op: "quarantine", code: handoffcheck.CodeStatusMissing}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			consumer := &handoffTestConsumer{claims: []dispatchoutbox.HandoffClaim{{
				UnitID:        7,
				IrisRequestID: testHandoffIrisRequestID,
				Generation:    tc.generation,
				AttemptCount:  tc.attempt,
				Elapsed:       tc.elapsed,
			}}}
			client := &handoffTestStatusClient{snapshot: tc.status}

			claimed, err := newHandoffTestReconciler(t, consumer, client).RunOnce(t.Context())
			require.NoError(t, err)
			require.Equal(t, 1, claimed)
			require.Equal(t, []string{testHandoffIrisRequestID}, client.queried)

			tc.want.unitID = 7
			require.Equal(t, []handoffCall{tc.want}, consumer.calls)
		})
	}
}

// handoffOutcomeLevels는 reconciler가 단위별 정산 결과로 남긴 로그 수준을 send_unit_id별로 모은다.
type handoffOutcomeLevels struct {
	mu     sync.Mutex
	levels map[int64]slog.Level
}

func newHandoffOutcomeLevels() *handoffOutcomeLevels {
	return &handoffOutcomeLevels{levels: make(map[int64]slog.Level)}
}

func (*handoffOutcomeLevels) Enabled(context.Context, slog.Level) bool { return true }

func (h *handoffOutcomeLevels) Handle(_ context.Context, record slog.Record) error {
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key != "send_unit_id" {
			return true
		}

		h.mu.Lock()
		defer h.mu.Unlock()

		h.levels[attr.Value.Int64()] = record.Level

		return false
	})

	return nil
}

func (h *handoffOutcomeLevels) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *handoffOutcomeLevels) WithGroup(string) slog.Handler { return h }

func (h *handoffOutcomeLevels) snapshot() map[int64]slog.Level {
	h.mu.Lock()
	defer h.mu.Unlock()

	return maps.Clone(h.levels)
}

// failingHandoffConsumer의 완료·dlq 정산은 err를 돌려준다.
type failingHandoffConsumer struct {
	handoffTestConsumer

	err error
}

func (c *failingHandoffConsumer) CompleteHandoff(context.Context, *dispatchoutbox.HandoffClaim) error {
	return c.err
}

func (c *failingHandoffConsumer) DLQAfterHandoff(context.Context, *dispatchoutbox.HandoffClaim, string, string) error {
	return c.err
}

// 저장소의 alarm dispatch 실패 sentinel이 엔진 분류로 이어진다. 확인이 닫힌 미스와 DLQ commit 뒤 claim 키 해제
// 실패는 다시 판정할 확인이 없으므로 오류이고, 확인을 닫지 않은 소유 상실 미스는 다음 확인이 다시 판정하므로 경고다.
func TestHandoffReconcilerClassifiesSettlementFailures(t *testing.T) {
	t.Parallel()

	sent, quarantined := string(dispatchoutbox.StatusSent), string(dispatchoutbox.StatusQuarantined)
	memberMiss := &sendfence.MissError{Action: "handoff_complete", Expected: 2, Rows: []sendfence.RowMiss{
		{ID: 1, Reason: sendfence.ReasonStatusChanged, Status: quarantined},
		{ID: 2, Reason: sendfence.ReasonPeerMissed, Status: sent},
	}}
	tokenMiss := &sendfence.MissError{Action: "handoff_complete", Expected: 2, Rows: []sendfence.RowMiss{
		{ID: 1, Reason: sendfence.ReasonVersionChanged, Status: sent},
		{ID: 2, Reason: sendfence.ReasonVersionChanged, Status: sent},
	}}

	cases := []struct {
		name       string
		status     string
		generation int
		err        error
		want       slog.Level
	}{
		{
			name:   "check closed",
			status: "handoff_completed",
			err:    fmt.Errorf("complete alarm handoff check: %w", fmt.Errorf("%w: %w", dispatchoutbox.ErrHandoffCheckClosed, memberMiss)),
			want:   slog.LevelError,
		},
		{
			name:   "claim lost",
			status: "handoff_completed",
			err:    fmt.Errorf("complete alarm handoff check: %w", tokenMiss),
			want:   slog.LevelWarn,
		},
		{
			name:       "dlq claim keys not released by deadline",
			status:     "failed",
			generation: iris.ReplyReissueMaxGenerations,
			err:        fmt.Errorf("release alarm handoff dlq claim keys: %w: %w", dispatchoutbox.ErrHandoffClaimKeysNotReleased, context.DeadlineExceeded),
			want:       slog.LevelError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			consumer := &failingHandoffConsumer{
				claims: []dispatchoutbox.HandoffClaim{{UnitID: 7, IrisRequestID: testHandoffIrisRequestID, Generation: tc.generation}},
				err:    tc.err,
			}
			levels := newHandoffOutcomeLevels()
			reconciler, err := NewHandoffReconciler(consumer, &handoffTestStatusClient{snapshot: handoffStatus(tc.status)},
				HandoffReconcilerConfig{Horizon: testHandoffHorizon, PollInterval: time.Second}, slog.New(levels))
			require.NoError(t, err)

			claimed, err := reconciler.RunOnce(t.Context())
			require.NoError(t, err)
			require.Equal(t, 1, claimed)
			require.Equal(t, map[int64]slog.Level{7: tc.want}, levels.snapshot())
		})
	}
}
