package dispatchoutbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"testing"
	"time"
)

type payloadCleanupRecorder struct {
	*consumerTestRepository

	terminalErr error
	releaseErr  error
	operations  []string
	released    []string
}

func (r *payloadCleanupRecorder) MoveToDLQ(ctx context.Context, updates []TerminalUpdate, worker string) error {
	r.operations = append(r.operations, "dlq")
	if r.terminalErr != nil {
		return fmt.Errorf("terminal update: %w", r.terminalErr)
	}

	return r.consumerTestRepository.MoveToDLQ(ctx, updates, worker)
}

func (r *payloadCleanupRecorder) DelMany(_ context.Context, keys []string) (int64, error) {
	r.operations = append(r.operations, "release")
	r.released = slices.Clone(keys)

	if r.releaseErr != nil {
		return 0, fmt.Errorf("delete claim keys: %w", r.releaseErr)
	}

	return int64(len(keys)), nil
}

func TestConsumerRejectsPayloadBeforeReleasingClaims(t *testing.T) {
	t.Parallel()

	for _, fixture := range []struct {
		name   string
		record Record
	}{
		{name: "invalid JSON", record: Record{Payload: []byte("{")}},
		{name: "invalid delivery context", record: Record{Payload: []byte("{}"), DeliveryContext: []byte("{")}},
		{name: "missing event", record: Record{EventID: 1}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()

			for _, failure := range []string{"none", "terminal ownership mismatch", "release unavailable"} {
				t.Run(failure, func(t *testing.T) {
					t.Parallel()

					checkPayloadCleanup(t, fixture.record, failure)
				})
			}
		})
	}
}

func checkPayloadCleanup(t *testing.T, record Record, failure string) {
	t.Helper()

	record.ID = 7
	record.ClaimKeys = []string{"notified:claim:room-1:stream-1:100:live", "alarm:dispatch:claim:unrelated"}

	recorder := &payloadCleanupRecorder{consumerTestRepository: &consumerTestRepository{
		claimDueFunc: func(context.Context, string, int, time.Duration) ([]*Record, error) {
			return []*Record{&record}, nil
		},
	}}
	wantOperations := []string{"dlq", "release"}

	var wantErr error

	switch failure {
	case "terminal ownership mismatch":
		wantErr = errors.New(failure)
		recorder.terminalErr = wantErr
		wantOperations = []string{"dlq"}
	case "release unavailable":
		wantErr = errors.New(failure)
		recorder.releaseErr = wantErr
	}

	consumer := NewConsumer(recorder, slog.New(slog.DiscardHandler), WithClaimKeyReleaser(recorder))
	envelopes, err := consumer.DrainBatch(t.Context(), 1)

	if !errors.Is(err, wantErr) || len(envelopes) != 0 {
		t.Fatalf("DrainBatch() = %v, %v; want no envelopes and %v", envelopes, err, wantErr)
	}

	if !slices.Equal(recorder.operations, wantOperations) {
		t.Fatalf("operations = %v, want %v", recorder.operations, wantOperations)
	}

	if recorder.terminalErr == nil && !slices.Equal(recorder.released, record.ClaimKeys[:1]) {
		t.Fatalf("released = %v, want only the rejected delivery's allowed claim key", recorder.released)
	}
}

func TestConsumerAcceptsPayloadWithoutReleasingClaims(t *testing.T) {
	t.Parallel()

	record := &Record{ID: 9, Payload: []byte("{}"), ClaimKeys: []string{"notified:claim:room-2:stream-1:100:live"}}
	recorder := &payloadCleanupRecorder{consumerTestRepository: &consumerTestRepository{
		claimDueFunc: func(context.Context, string, int, time.Duration) ([]*Record, error) {
			return []*Record{record}, nil
		},
	}}
	consumer := NewConsumer(recorder, slog.New(slog.DiscardHandler), WithClaimKeyReleaser(recorder))
	envelopes, err := consumer.DrainBatch(t.Context(), 1)

	if err != nil || len(envelopes) != 1 || len(recorder.operations) != 0 {
		t.Fatalf("DrainBatch() = %v, %v; operations = %v", envelopes, err, recorder.operations)
	}

	if !slices.Equal(envelopes[0].ClaimKeys, record.ClaimKeys) {
		t.Fatalf("valid envelope lost its claim keys: %v", envelopes[0].ClaimKeys)
	}
}
