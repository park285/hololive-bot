package dispatchoutbox

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// 재시도 중(attempt > 0)인 행의 payload를 해석하지 못하면 claim attempt 그대로 DLQ로 끝나야 한다. 이 attempt가 어긋나면
// DLQ 전이가 fence에 막혀 배치 반환과 재claim이 반복된다.
func TestConsumerDrainMovesUndecodableRetriedRowToDLQ(t *testing.T) {
	pool := dbtest.NewPool(t)
	repository := newTestRepository(t, pool)
	ctx := t.Context()

	_, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{receiptEnvelope("decode-dlq-room", "decode-dlq")}})
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE alarm_dispatch_deliveries SET status='retry', attempt_count=2`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE alarm_dispatch_events SET payload='[]'::jsonb`)
	require.NoError(t, err)

	consumer := mustNewConsumer(t, repository, &fakeClaimKeyReleaser{}, slog.Default(), WithWorkerID("decode-owner"))

	envelopes, err := consumer.DrainBatch(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, envelopes)

	var (
		status, code string
		attempts     int
		unowned      bool
	)

	require.NoError(t, pool.QueryRow(ctx, `SELECT status, last_error_code, attempt_count, locked_by IS NULL FROM alarm_dispatch_deliveries`).
		Scan(&status, &code, &attempts, &unowned))
	require.Equal(t, string(StatusDLQ), status)
	require.Equal(t, ErrorCodePayload, code)
	require.Equal(t, 2, attempts)
	require.True(t, unowned)
}
