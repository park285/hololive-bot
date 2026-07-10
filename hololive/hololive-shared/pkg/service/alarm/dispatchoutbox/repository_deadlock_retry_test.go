package dispatchoutbox

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunPublishBatchWithDeadlockRetry_RetriesOnceOn40P01(t *testing.T) {
	t.Parallel()

	result := PublishBatchResult{RequestedEvents: 2}
	attempts := 0
	publishResult, err := runPublishBatchWithDeadlockRetry(&result, func() (PublishBatchResult, error) {
		attempts++
		result.InsertedEvents++
		result.HashConflictEvents++
		if attempts == 1 {
			return PublishBatchResult{}, &pgconn.PgError{Code: "40P01", Message: "deadlock detected"}
		}
		return result, nil
	})

	require.NoError(t, err)
	assert.Equal(t, 2, attempts)
	assert.Equal(t, 1, publishResult.InsertedEvents,
		"재시도 전에 첫 시도의 카운터 반영을 스냅샷으로 되돌려 이중 집계를 막아야 한다")
	assert.Equal(t, 1, publishResult.HashConflictEvents)
	assert.Equal(t, 2, publishResult.RequestedEvents)
}

func TestRunPublishBatchWithDeadlockRetry_DoesNotRetryNonDeadlockError(t *testing.T) {
	t.Parallel()

	cause := errors.New("connection reset")
	result := PublishBatchResult{}
	attempts := 0
	_, err := runPublishBatchWithDeadlockRetry(&result, func() (PublishBatchResult, error) {
		attempts++
		return PublishBatchResult{}, cause
	})

	require.ErrorIs(t, err, cause)
	assert.Equal(t, 1, attempts)
}

func TestRunPublishBatchWithDeadlockRetry_RetriesOnlyOnce(t *testing.T) {
	t.Parallel()

	deadlock := &pgconn.PgError{Code: "40P01", Message: "deadlock detected"}
	result := PublishBatchResult{}
	attempts := 0
	_, err := runPublishBatchWithDeadlockRetry(&result, func() (PublishBatchResult, error) {
		attempts++
		return PublishBatchResult{}, deadlock
	})

	require.Error(t, err)
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.NotNil(t, pgErr)
	assert.Equal(t, "40P01", pgErr.Code)
	assert.Equal(t, 2, attempts, "40P01 재시도는 정확히 1회여야 한다")
}

func TestTerminalStatusSQL_DLQCoversSendingRows(t *testing.T) {
	t.Parallel()

	_, statusFilter := terminalStatusSQL(StatusDLQ)
	assert.Equal(t, "status IN ('leased','sending')", statusFilter,
		"post-send retry 소진(persistSendingRetry)과 MarkSending 보상의 DLQ 분기는 'sending' 행에서 발생한다")
}
