package dispatchoutbox

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type batchCleanupRepository struct {
	*consumerTestRepository

	loadErr, terminalErr, releaseErr error
	releaseCalls                     int
	releasedIDs                      []int64
	releasedOwner                    string
	releaseContext                   func(context.Context)
}

func (r *batchCleanupRepository) LoadEventsByID(ctx context.Context, ids []int64) (map[int64]EventRecord, error) {
	if r.loadErr != nil {
		return nil, r.loadErr
	}

	return r.consumerTestRepository.LoadEventsByID(ctx, ids)
}

func (r *batchCleanupRepository) MoveToDLQ(ctx context.Context, updates []TerminalUpdate, owner string) error {
	if r.terminalErr != nil {
		return r.terminalErr
	}

	return r.consumerTestRepository.MoveToDLQ(ctx, updates, owner)
}

func (r *batchCleanupRepository) ReleaseLeased(ctx context.Context, ids []int64, owner string) error {
	r.releaseCalls++

	r.releasedIDs = slices.Clone(ids)
	r.releasedOwner = owner

	if r.releaseContext != nil {
		r.releaseContext(ctx)
	}

	return r.releaseErr
}

type batchCleanupTestCase struct {
	name                                     string
	loadErr, terminalErr, keyErr, cleanupErr error
	canceled, valid                          bool
	wantIDs                                  []int64
	wantKeyCalls                             int
}

func batchCleanupRecords(valid bool) []*Record {
	records := []*Record{
		{ID: 1, Status: StatusLeased, Payload: []byte("{}"), AttemptCount: 4, SendUnitID: 77, ClaimKeys: []string{"notified:claim:valid-before"}},
		{ID: 2, Status: StatusLeased, Payload: []byte("{"), AttemptCount: 4, SendUnitID: 77, ClaimKeys: []string{"notified:claim:invalid"}},
		{ID: 3, Status: StatusLeased, Payload: []byte("{}"), AttemptCount: 4, SendUnitID: 77, ClaimKeys: []string{"notified:claim:valid-after"}},
	}

	if valid {
		records[1].Payload = []byte("{}")
	}

	return records
}

func assertLiveCleanupContext(ctx context.Context, t *testing.T) <-chan struct{} {
	t.Helper()

	require.NoError(t, ctx.Err())

	deadline, ok := ctx.Deadline()
	require.True(t, ok)

	remaining := time.Until(deadline)
	require.Positive(t, remaining)

	require.LessOrEqual(t, remaining, 5*time.Second)

	return ctx.Done()
}

func assertBatchCleanupResult(
	t *testing.T,
	tt batchCleanupTestCase,
	repo *batchCleanupRepository,
	releaser *fakeClaimKeyReleaser,
	records []*Record,
	out []domain.AlarmQueueEnvelope,
	err error,
	cleanupDone <-chan struct{},
) {
	t.Helper()

	if tt.wantIDs != nil {
		require.Nil(t, out, "partial send-unit must never escape a failed batch")
		require.Error(t, err)
		require.Equal(t, 1, repo.releaseCalls)
		require.Equal(t, tt.wantIDs, repo.releasedIDs)
		require.Equal(t, "batch-owner", repo.releasedOwner)
		require.NotNil(t, cleanupDone)

		select {
		case <-cleanupDone:
		case <-time.After(time.Second):
			t.Fatal("cleanup context was not canceled after release")
		}
	} else {
		require.NoError(t, err)
		require.Zero(t, repo.releaseCalls)

		wantLen := 2

		if tt.valid {
			wantLen = 3
		}

		require.Len(t, out, wantLen)
	}

	for _, cause := range []error{tt.loadErr, tt.terminalErr, tt.keyErr, tt.cleanupErr} {
		if cause == nil {
			continue
		}

		require.ErrorIs(t, err, cause)
	}

	require.Equal(t, tt.wantKeyCalls, releaser.calls)

	if tt.wantKeyCalls > 0 {
		require.Equal(t, records[1].ClaimKeys, releaser.lastKeys)
	}

	for _, record := range records {
		require.Equal(t, 4, record.AttemptCount)
		require.Equal(t, int64(77), record.SendUnitID)
		require.Len(t, record.ClaimKeys, 1)
	}

	require.Zero(t, repo.requeuePreSendCalls)
}

func TestConsumerDrainBatchFailedBatchCleanup(t *testing.T) {
	terminalErr := errors.New("terminal transition unknown")
	keyErr := errors.New("claim key release failed")
	loadErr := errors.New("event load failed")
	cleanupErr := errors.New("lease release failed")
	tests := []batchCleanupTestCase{
		{name: "terminal failure returns whole batch", terminalErr: terminalErr, wantIDs: []int64{1, 2, 3}},
		{name: "terminal success excludes row despite key failure", keyErr: keyErr, wantIDs: []int64{1, 3}, wantKeyCalls: 1},
		{name: "load failure returns whole batch", loadErr: loadErr, wantIDs: []int64{1, 2, 3}},
		{name: "cleanup failure preserves both causes", loadErr: loadErr, cleanupErr: cleanupErr, wantIDs: []int64{1, 2, 3}},
		{name: "canceled request gets bounded live cleanup", loadErr: context.Canceled, canceled: true, wantIDs: []int64{1, 2, 3}},
		{name: "successful batch keeps leases", valid: true},
		{name: "successful rejection keeps remaining leases", wantKeyCalls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			records := batchCleanupRecords(tt.valid)
			repo := &batchCleanupRepository{consumerTestRepository: &consumerTestRepository{
				claimDueFunc: func(context.Context, string, int, time.Duration) ([]*Record, error) { return records, nil },
			}, loadErr: tt.loadErr, terminalErr: tt.terminalErr, releaseErr: tt.cleanupErr}

			var cleanupDone <-chan struct{}

			repo.releaseContext = func(ctx context.Context) {
				cleanupDone = assertLiveCleanupContext(ctx, t)
			}

			releaser := &fakeClaimKeyReleaser{err: tt.keyErr}

			consumer := NewConsumer(repo, nil, WithWorkerID("batch-owner"), WithClaimKeyReleaser(releaser))

			ctx, cancel := context.WithCancel(t.Context())

			defer cancel()

			if tt.canceled {
				cancel()
			}

			out, err := consumer.DrainBatch(ctx, 3)

			assertBatchCleanupResult(t, tt, repo, releaser, records, out, err, cleanupDone)
		})
	}
}
