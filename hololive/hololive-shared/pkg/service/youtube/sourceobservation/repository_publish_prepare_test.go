package sourceobservation

import (
	"bytes"
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestClonePublishBatchInputCopiesLeaseObservationsAndCursors(t *testing.T) {
	t.Parallel()
	sourceEvent := time.Date(2026, 8, 14, 1, 0, 1, 0, time.UTC)
	input := &PublishBatchInput{
		Lease: contract.LeaseProof{JobKey: "job", OwnerInstance: "a", FenceEpoch: 1, ScheduledFor: sourceEvent},
		Checkpoint: CheckpointUpdate{
			Entries: []CheckpointEntry{{
				SubjectKey: "UC_TEST",
				Cursor:     []byte(`{"page":1}`),
			}},
			CollectionLatency: time.Second,
		},
		Observations: []contract.Envelope{{
			SubjectKey:    "UC_TEST",
			Payload:       []byte(`{"ok":true}`),
			SourceEventAt: &sourceEvent,
		}},
	}
	cloned, err := clonePublishBatchInput(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Lease.JobKey = "mutated"
	input.Observations[0].Payload[0] ^= 0xff
	input.Observations[0].SubjectKey = "mutated"
	input.Checkpoint.Entries[0].Cursor[0] ^= 0xff
	if cloned.Lease.JobKey != "job" || cloned.Observations[0].SubjectKey != "UC_TEST" {
		t.Fatalf("clone leaked identity mutation: %#v", cloned)
	}
	if bytes.Equal(cloned.Observations[0].Payload, input.Observations[0].Payload) {
		t.Fatal("payload mutation leaked into clone")
	}
	if bytes.Equal(cloned.Checkpoint.Entries[0].Cursor, input.Checkpoint.Entries[0].Cursor) {
		t.Fatal("cursor mutation leaked into clone")
	}
	if cloned.Observations[0].SourceEventAt == nil || cloned.Observations[0].SourceEventAt == input.Observations[0].SourceEventAt {
		t.Fatal("SourceEventAt pointer was shared")
	}
}

func TestPreparePublishBatchRejectsNilAndEmptyInput(t *testing.T) {
	t.Parallel()
	if _, err := preparePublishBatch(nil); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("nil input error = %v, want ErrInvalidEnvelope", err)
	}
	if _, err := preparePublishBatch(&PublishBatchInput{}); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("empty input error = %v, want ErrInvalidEnvelope", err)
	}
}

func TestPreflightPublishBatchRejectsOversizedInputBeforeClone(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		payload    []byte
		cursor     []byte
		wantDetail string
	}{
		{
			name:       "payload",
			payload:    make([]byte, contract.MaxPayloadBytes+1),
			wantDetail: "observation 0 payload is too large",
		},
		{
			name:       "cursor",
			payload:    []byte(`{}`),
			cursor:     make([]byte, maxCheckpointCursorBytes+1),
			wantDetail: "checkpoint 0 cursor is too large",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := &PublishBatchInput{
				Observations: []contract.Envelope{{Payload: test.payload}},
				Checkpoint:   CheckpointUpdate{Entries: []CheckpointEntry{{Cursor: test.cursor}}},
			}
			err := preflightPublishBatch(input)
			if err == nil {
				t.Fatal("preflight accepted oversized input")
			}
			if !errors.Is(err, ErrInvalidEnvelope) || !strings.Contains(err.Error(), test.wantDetail) {
				t.Fatalf("preflight error = %v, want %q", err, test.wantDetail)
			}
		})
	}
}

func TestPreflightPublishBatchRejectsOversizedAggregateBeforeClone(t *testing.T) {
	t.Parallel()
	const count = MaxPublishBatchBytes/contract.MaxPayloadBytes + 1
	input := &PublishBatchInput{
		Observations: make([]contract.Envelope, count),
		Checkpoint:   CheckpointUpdate{Entries: make([]CheckpointEntry, count)},
	}
	for i := range input.Observations {
		input.Observations[i].Payload = make([]byte, contract.MaxPayloadBytes)
	}
	err := preflightPublishBatch(input)
	if err == nil {
		t.Fatal("preflight accepted oversized aggregate")
	}
	if !errors.Is(err, ErrInvalidEnvelope) || !strings.Contains(err.Error(), "aggregate payload and cursor bytes exceed") {
		t.Fatalf("preflight error = %v, want aggregate byte bound", err)
	}
}

func TestValidatePublishBatchResultExactCardinalityAndOrdinal(t *testing.T) {
	t.Parallel()
	valid := PublishBatchResult{Results: []PublishedObservation{
		NewPublishedObservation(1, PublishInserted, 0),
		NewPublishedObservation(2, PublishDuplicate, 1),
	}}
	if err := ValidatePublishBatchResult(2, valid); err != nil {
		t.Fatal(err)
	}
	missing := clonePublishBatchResult(valid)
	missing.Results = missing.Results[:1]
	if err := ValidatePublishBatchResult(2, missing); err == nil {
		t.Fatal("missing row must fail")
	}
	dup := clonePublishBatchResult(valid)
	dup.Results[1].Ordinal = 0
	if err := ValidatePublishBatchResult(2, dup); err == nil {
		t.Fatal("duplicate ordinal must fail")
	}
	badOutcome := clonePublishBatchResult(valid)
	badOutcome.Results[0].Outcome = "WEIRD"
	if err := ValidatePublishBatchResult(2, badOutcome); err == nil {
		t.Fatal("impossible outcome must fail")
	}
	zeroID := clonePublishBatchResult(valid)
	zeroID.Results[1].ObservationID = 0
	if err := ValidatePublishBatchResult(2, zeroID); err == nil {
		t.Fatal("non-positive observation ID must fail")
	}
}

func clonePublishBatchResult(source PublishBatchResult) PublishBatchResult {
	return PublishBatchResult{Results: slices.Clone(source.Results)}
}

func TestERR003AtomicAndCompleteErrorSQLMatchGoTuples(t *testing.T) {
	t.Parallel()
	deferTuples := extractDurableFailureTuples(mustSQL("repository_job_defer_0082_82.sql"))
	if !failureTupleSetsEqual(deferTuples, contract.DeferFailureTuples()) {
		t.Fatalf("atomic defer SQL tuples = %#v, want %#v", deferTuples, contract.DeferFailureTuples())
	}
	completeTuples := extractDurableFailureTuples(mustSQL("repository_job_complete_error_0081_81.sql"))
	if !failureTupleSetsEqual(completeTuples, contract.CompleteErrorFailureTuples()) {
		t.Fatalf("complete-with-error SQL tuples = %#v, want %#v", completeTuples, contract.CompleteErrorFailureTuples())
	}
}

var durableFailureTuplePattern = regexp.MustCompile(`\(\s*'([a-z0-9_]+)'\s*,\s*'([A-Z_]+)'\s*\)`)

func extractDurableFailureTuples(sql string) []contract.FailureTuple {
	upper := strings.ToUpper(sql)
	valuesAt := strings.Index(upper, "VALUES")
	if valuesAt < 0 {
		return nil
	}
	matches := durableFailureTuplePattern.FindAllStringSubmatch(sql[valuesAt:], -1)
	tuples := make([]contract.FailureTuple, 0, len(matches))
	for _, match := range matches {
		tuples = append(tuples, contract.FailureTuple{
			Code:  contract.CollectionErrorCode(match[1]),
			Class: contract.FailureClass(match[2]),
		})
	}
	return tuples
}

func failureTupleSetsEqual(got, want []contract.FailureTuple) bool {
	g := slices.Clone(got)
	w := slices.Clone(want)
	slices.SortFunc(g, compareTestFailureTuple)
	slices.SortFunc(w, compareTestFailureTuple)
	return slices.Equal(g, w)
}

func compareTestFailureTuple(a, b contract.FailureTuple) int {
	if a.Code != b.Code {
		if a.Code < b.Code {
			return -1
		}
		return 1
	}
	if a.Class == b.Class {
		return 0
	}
	if a.Class < b.Class {
		return -1
	}
	return 1
}

func mustTestDeferInput(t *testing.T, code contract.CollectionErrorCode, class contract.FailureClass, detail string) DeferCollectionInput {
	t.Helper()
	diagnostic, err := contract.NewFailureDiagnostic(code, class, detail)
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := NewRetryDelaySchedule(200 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	input, err := NewDeferCollectionInput(diagnostic, RetryBounds{Minimum: 100 * time.Millisecond, Maximum: time.Second}, schedule)
	if err != nil {
		t.Fatal(err)
	}
	return input
}
