package sourceobservation

import (
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

type publishBatchRow struct {
	Ordinal              int                      `json:"ordinal"`
	Identity             string                   `json:"identity"`
	Provider             contract.Provider        `json:"provider"`
	ObservationKind      contract.ObservationKind `json:"observation_kind"`
	SubjectKey           string                   `json:"subject_key"`
	ObservationKey       string                   `json:"observation_key"`
	SchemaVersion        int16                    `json:"schema_version"`
	ContractGeneration   int64                    `json:"contract_generation"`
	ScheduledFor         time.Time                `json:"scheduled_for"`
	ObservedAt           time.Time                `json:"observed_at"`
	SourceEventAt        *time.Time               `json:"source_event_at"`
	ScopeSHA256          string                   `json:"scope_sha256"`
	Completeness         contract.Completeness    `json:"completeness"`
	Continuity           contract.Continuity      `json:"continuity"`
	Payload              jsontext.Value           `json:"payload"`
	PayloadSHA256        string                   `json:"payload_sha256"`
	EvidenceSHA256       string                   `json:"evidence_sha256"`
	CollectorInstance    string                   `json:"collector_instance"`
	JobKey               string                   `json:"job_key"`
	CollectionJobKind    string                   `json:"collection_job_kind"`
	FenceEpoch           int64                    `json:"fence_epoch"`
	ProjectionGeneration int64                    `json:"projection_generation"`
	CollectionLatencyMS  int64                    `json:"collection_latency_ms"`
	Cursor               jsontext.Value           `json:"cursor"`
}

type publishContractRow struct {
	Provider           contract.Provider        `json:"provider"`
	ObservationKind    contract.ObservationKind `json:"observation_kind"`
	SchemaVersion      int16                    `json:"schema_version"`
	ContractGeneration int64                    `json:"contract_generation"`
}

func encodePublishBatch(input *PublishBatchInput) (observationJSON, contractJSON []byte, err error) {
	rows, contracts := encodePublishBatchRows(input, checkpointEntriesByBinding(input.Checkpoint.Entries))

	out1, out2, err := marshalPublishBatch(rows, contracts)
	if err != nil {
		return out1, out2, fmt.Errorf("marshal publish batch: %w", err)
	}

	return out1, out2, nil
}

func checkpointEntriesByBinding(entries []CheckpointEntry) map[checkpointBinding]CheckpointEntry {
	checkpoints := make(map[checkpointBinding]CheckpointEntry, len(entries))
	for i := range entries {
		checkpoints[checkpointBindingForEntry(&entries[i])] = entries[i]
	}

	return checkpoints
}

func encodePublishBatchRows(input *PublishBatchInput, checkpoints map[checkpointBinding]CheckpointEntry) (batchRows []publishBatchRow, contractRows []publishContractRow) {
	rows := make([]publishBatchRow, len(input.Observations))
	contracts := make([]publishContractRow, len(input.Observations))

	for i := range input.Observations {
		observation := &input.Observations[i]
		checkpoint := checkpoints[checkpointBindingForObservation(observation)]

		rows[i] = newPublishBatchRow(i, observation, checkpoint.Cursor, input.Checkpoint.CollectionLatency)
		contracts[i] = publishContractRow{
			Provider: observation.Provider, ObservationKind: observation.ObservationKind,
			SchemaVersion: observation.SchemaVersion, ContractGeneration: observation.ContractGeneration,
		}
	}

	return rows, contracts
}

func newPublishBatchRow(
	ordinal int,
	observation *contract.Envelope,
	cursor jsontext.Value,
	latency time.Duration,
) publishBatchRow {
	return publishBatchRow{
		Ordinal: ordinal, Identity: observationIdentity(observation),
		Provider: observation.Provider, ObservationKind: observation.ObservationKind,
		SubjectKey: observation.SubjectKey, ObservationKey: observation.ObservationKey,
		SchemaVersion: observation.SchemaVersion, ContractGeneration: observation.ContractGeneration,
		ScheduledFor: observation.ScheduledFor, ObservedAt: observation.ObservedAt,
		SourceEventAt: observation.SourceEventAt, ScopeSHA256: observation.ScopeSHA256,
		Completeness: observation.Completeness, Continuity: observation.Continuity,
		Payload: observation.Payload, PayloadSHA256: observation.PayloadSHA256,
		EvidenceSHA256: observation.EvidenceSHA256, CollectorInstance: observation.CollectorInstance,
		JobKey: observation.Lease.JobKey, CollectionJobKind: observation.Lease.CollectionJobKind,
		FenceEpoch: observation.Lease.FenceEpoch, ProjectionGeneration: observation.Lease.ProjectionGeneration,
		CollectionLatencyMS: latency.Milliseconds(), Cursor: cursor,
	}
}

func marshalPublishBatch(rows []publishBatchRow, contracts []publishContractRow) (observationJSON, contractJSON []byte, err error) {
	encoded, err := jsonv2.Marshal(rows)
	if err != nil {
		return nil, nil, fmt.Errorf("publish source observation batch: encode set: %w", err)
	}

	if len(encoded) > MaxPublishBatchBytes {
		return nil, nil, fmt.Errorf(
			"publish source observation batch: %w: encoded batch exceeds %d bytes",
			ErrInvalidEnvelope,
			MaxPublishBatchBytes,
		)
	}

	contractEncoded, err := jsonv2.Marshal(contracts)
	if err != nil {
		return nil, nil, fmt.Errorf("publish source observation batch: encode contracts: %w", err)
	}

	return encoded, contractEncoded, nil
}

func verifyCurrentContracts(ctx context.Context, tx dbx.Tx, encoded []byte) error {
	var current bool

	if err := tx.QueryRow(ctx, mustSQL("repository_contract_batch_current_0031_31.sql"), string(encoded)).Scan(&current); err != nil {
		return fmt.Errorf("publish source observation batch: verify current contracts: %w", err)
	}

	if !current {
		return fmt.Errorf("publish source observation batch: %w", ErrStaleContract)
	}

	return nil
}

func publishObservationSet(
	ctx context.Context,
	tx dbx.Tx,
	encoded []byte,
	want int,
) (PublishBatchResult, bool, error) {
	rows, err := tx.Query(ctx, mustSQL("repository_publish_set_0032_32.sql"), string(encoded))
	if err != nil {
		return PublishBatchResult{}, false, fmt.Errorf("publish source observation batch: execute set: %w", err)
	}
	defer rows.Close()

	result, collision, err := collectPublishSetRows(rows, want)
	if err != nil {
		return PublishBatchResult{}, false, fmt.Errorf("collect publish set rows: %w", err)
	}

	if err := rows.Err(); err != nil {
		return PublishBatchResult{}, false, fmt.Errorf("publish source observation batch: read set result: %w", err)
	}

	return result, collision, nil
}

func collectPublishSetRows(rows pgx.Rows, want int) (PublishBatchResult, bool, error) {
	result := PublishBatchResult{Results: make([]PublishedObservation, want)}
	seen := make([]bool, want)
	collision := false

	for rows.Next() {
		ordinal, observationID, outcome, err := scanPublishSetRow(rows)
		if err != nil {
			return PublishBatchResult{}, false, fmt.Errorf("scan publish set row: %w", err)
		}

		if err := recordPublishSetRow(&result, seen, ordinal, observationID, outcome, want); err != nil {
			return PublishBatchResult{}, false, fmt.Errorf("record publish set row: %w", err)
		}

		collision = collision || outcome == PublishCollision
	}

	if err := ensurePublishSetComplete(seen); err != nil {
		return result, collision, fmt.Errorf("ensure publish set complete: %w", err)
	}

	return result, collision, nil
}

func scanPublishSetRow(rows pgx.Rows) (rowOrdinal int, rowObservationID int64, rowOutcome PublishOutcome, rowErr error) {
	var (
		ordinal       int
		observationID int64
		outcome       PublishOutcome
	)

	if err := rows.Scan(&ordinal, &observationID, &outcome); err != nil {
		return 0, 0, "", fmt.Errorf("publish source observation batch: scan set result: %w", err)
	}

	return ordinal, observationID, outcome, nil
}

func recordPublishSetRow(
	result *PublishBatchResult,
	seen []bool,
	ordinal int,
	observationID int64,
	outcome PublishOutcome,
	want int,
) error {
	if invalidPublishSetRow(ordinal, seen, outcome, want) {
		return errors.New("publish source observation batch: invalid set result")
	}

	seen[ordinal] = true
	result.Results[ordinal] = NewPublishedObservation(observationID, outcome, ordinal)

	return nil
}

func invalidPublishSetRow(ordinal int, seen []bool, outcome PublishOutcome, want int) bool {
	return ordinal < 0 || ordinal >= want || seen[ordinal] || !validPublishOutcome(outcome)
}

func validPublishOutcome(outcome PublishOutcome) bool {
	return outcome == PublishInserted || outcome == PublishDuplicate || outcome == PublishCollision
}

func ensurePublishSetComplete(seen []bool) error {
	for i := range seen {
		if !seen[i] {
			return errors.New("publish source observation batch: incomplete set result")
		}
	}

	return nil
}
