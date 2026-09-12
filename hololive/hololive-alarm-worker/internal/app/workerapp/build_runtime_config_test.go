package workerapp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/constants"
	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type advanceMinutesCRUD struct {
	domain.AlarmCRUD

	apply func(context.Context, int) []int
}

func (c advanceMinutesCRUD) UpdateAlarmAdvanceMinutes(ctx context.Context, minutes int) []int {
	return c.apply(ctx, minutes)
}

type buildContextKey struct{}

func TestAdvanceMinutesHandlerOutlivesBuildContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), buildContextKey{}, "trace"))

	var appliedDone <-chan struct{}

	handler := buildAlarmAdvanceMinutesHandler(ctx, advanceMinutesCRUD{apply: func(ctx context.Context, minutes int) []int {
		require.NoError(t, ctx.Err())
		require.Equal(t, "trace", ctx.Value(buildContextKey{}))
		require.Equal(t, 12, minutes)

		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.Positive(t, time.Until(deadline))
		require.LessOrEqual(t, time.Until(deadline), constants.RequestTimeout.AdminRequest)

		appliedDone = ctx.Done()

		return []int{12, 3, 1}
	}}, nil)

	cancel()
	handler(contractssettings.AlarmAdvanceMinutesPayloadV1{Minutes: 12})
	require.NotNil(t, appliedDone)

	select {
	case <-appliedDone:
	default:
		t.Fatal("apply context was not released")
	}
}
