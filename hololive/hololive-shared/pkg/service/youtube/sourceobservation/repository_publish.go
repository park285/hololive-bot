package sourceobservation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

type sqlPublishFenceVerifier struct {
	jobs JobContractSet
}

func (v sqlPublishFenceVerifier) Verify(
	ctx context.Context,
	tx dbx.Tx,
	proof contract.LeaseProof,
	observations []contract.Envelope,
) error {
	var provider string
	var collectionJobKind string
	var jobClass string
	var jobSubject string
	err := tx.QueryRow(
		ctx,
		mustSQL("repository_publish_fence_0001_01.sql"),
		proof.JobKey,
		proof.OwnerInstance,
		proof.FenceEpoch,
		proof.ProjectionGeneration,
		proof.ScheduledFor,
	).Scan(&provider, &collectionJobKind, &jobClass, &jobSubject)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCollectionFenceLost
	}
	if err != nil {
		return fmt.Errorf("verify collection job fence: %w", err)
	}
	if collectionJobKind != proof.CollectionJobKind {
		return ErrCollectionFenceLost
	}
	definition, ok := v.jobs.Definition(collectionJobKind)
	if !ok || definition.Class != jobClass || definition.FixedSubject != "" && definition.FixedSubject != jobSubject {
		return ErrCollectionFenceLost
	}
	var generation int64
	err = tx.QueryRow(
		ctx,
		mustSQL("repository_projection_current_0002_02.sql"),
		proof.ProjectionGeneration,
	).Scan(&generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrProjectionStale
	}
	if err != nil {
		return fmt.Errorf("verify current collection projection: %w", err)
	}
	subjects := make([]string, len(observations))
	kinds := make([]string, len(observations))
	for i := range observations {
		observation := observations[i]
		if provider != string(observation.Provider) ||
			!v.jobs.Allows(collectionJobKind, observation.Provider, observation.ObservationKind) {
			return fmt.Errorf("verify collection job emission %d: %w", i, ErrTargetDisabled)
		}
		if definition.Membership == JobMembershipExactSubject && observation.SubjectKey != jobSubject {
			return fmt.Errorf("verify collection job membership %d: %w", i, ErrTargetDisabled)
		}
		if definition.Membership != JobMembershipExactSubject && definition.Membership != JobMembershipCurrentProjection {
			return fmt.Errorf("verify collection job membership %d: %w", i, ErrTargetDisabled)
		}
		subjects[i] = observation.SubjectKey
		kinds[i] = string(observation.ObservationKind)
	}
	var allEnabled bool
	err = tx.QueryRow(
		ctx,
		mustSQL("repository_target_enabled_0003_03.sql"),
		proof.ProjectionGeneration,
		subjects,
		kinds,
	).Scan(&allEnabled)
	if err != nil {
		return fmt.Errorf("verify collection targets: %w", err)
	}
	if !allEnabled {
		return fmt.Errorf("verify collection targets: %w", ErrTargetDisabled)
	}
	return nil
}

func (r *Repository) PublishBatch(
	ctx context.Context,
	input PublishBatchInput,
) (PublishBatchResult, error) {
	if err := r.validate(); err != nil {
		return PublishBatchResult{}, err
	}
	if err := validatePublishBatch(input); err != nil {
		return PublishBatchResult{}, fmt.Errorf("publish source observation batch: %w", err)
	}
	encoded, contracts, err := encodePublishBatch(input)
	if err != nil {
		return PublishBatchResult{}, err
	}
	return dbx.InPgxTxWithResult(ctx, r.pool, func(tx dbx.Tx) (PublishBatchResult, error) {
		return r.publishBatchTx(ctx, tx, input, encoded, contracts)
	})
}

func (r *Repository) publishBatchTx(
	ctx context.Context,
	tx dbx.Tx,
	input PublishBatchInput,
	encoded []byte,
	contracts []byte,
) (PublishBatchResult, error) {
	if err := r.fenceVerifier.Verify(ctx, tx, input.Lease, input.Observations); err != nil {
		return PublishBatchResult{}, fmt.Errorf("publish source observation batch: verify job fence: %w", err)
	}
	if err := verifyCurrentContracts(ctx, tx, contracts); err != nil {
		return PublishBatchResult{}, err
	}
	result, collision, err := publishObservationSet(ctx, tx, encoded, len(input.Observations))
	if err != nil {
		return PublishBatchResult{}, err
	}
	errorCode := ""
	if collision {
		errorCode = "observation_collision"
	}
	if err := completeCollectionJob(ctx, tx, input.Lease, errorCode); err != nil {
		return PublishBatchResult{}, err
	}
	return result, nil
}

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
	Payload              json.RawMessage          `json:"payload"`
	PayloadSHA256        string                   `json:"payload_sha256"`
	EvidenceSHA256       string                   `json:"evidence_sha256"`
	CollectorInstance    string                   `json:"collector_instance"`
	JobKey               string                   `json:"job_key"`
	CollectionJobKind    string                   `json:"collection_job_kind"`
	FenceEpoch           int64                    `json:"fence_epoch"`
	ProjectionGeneration int64                    `json:"projection_generation"`
	CollectionLatencyMS  int64                    `json:"collection_latency_ms"`
	Cursor               json.RawMessage          `json:"cursor"`
}

type publishContractRow struct {
	Provider           contract.Provider        `json:"provider"`
	ObservationKind    contract.ObservationKind `json:"observation_kind"`
	SchemaVersion      int16                    `json:"schema_version"`
	ContractGeneration int64                    `json:"contract_generation"`
}

func encodePublishBatch(input PublishBatchInput) ([]byte, []byte, error) {
	checkpoints := make(map[checkpointBinding]CheckpointEntry, len(input.Checkpoint.Entries))
	for i := range input.Checkpoint.Entries {
		entry := input.Checkpoint.Entries[i]
		checkpoints[checkpointBindingForEntry(entry)] = entry
	}
	aggregateBytes := 0
	for i := range input.Observations {
		observation := input.Observations[i]
		checkpoint := checkpoints[checkpointBindingForObservation(observation)]
		inputBytes := len(observation.Payload) + len(checkpoint.Cursor)
		if inputBytes > MaxPublishBatchBytes-aggregateBytes {
			return nil, nil, fmt.Errorf(
				"publish source observation batch: %w: aggregate payload and cursor bytes exceed %d",
				ErrInvalidEnvelope,
				MaxPublishBatchBytes,
			)
		}
		aggregateBytes += inputBytes
	}
	rows := make([]publishBatchRow, len(input.Observations))
	contracts := make([]publishContractRow, len(input.Observations))
	for i := range input.Observations {
		observation := input.Observations[i]
		checkpoint := checkpoints[checkpointBindingForObservation(observation)]
		rows[i] = publishBatchRow{
			Ordinal: i, Identity: observationIdentity(observation),
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
			CollectionLatencyMS: input.Checkpoint.CollectionLatency.Milliseconds(), Cursor: checkpoint.Cursor,
		}
		contracts[i] = publishContractRow{
			Provider: observation.Provider, ObservationKind: observation.ObservationKind,
			SchemaVersion: observation.SchemaVersion, ContractGeneration: observation.ContractGeneration,
		}
	}
	encoded, err := json.Marshal(rows)
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
	contractEncoded, err := json.Marshal(contracts)
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
	result := PublishBatchResult{Results: make([]PublishedObservation, want)}
	seen := make([]bool, want)
	collision := false
	for rows.Next() {
		var ordinal int
		var observationID int64
		var outcome PublishOutcome
		if err := rows.Scan(&ordinal, &observationID, &outcome); err != nil {
			return PublishBatchResult{}, false, fmt.Errorf("publish source observation batch: scan set result: %w", err)
		}
		if ordinal < 0 || ordinal >= want || seen[ordinal] ||
			outcome != PublishInserted && outcome != PublishDuplicate && outcome != PublishCollision {
			return PublishBatchResult{}, false, fmt.Errorf("publish source observation batch: invalid set result")
		}
		seen[ordinal] = true
		result.Results[ordinal] = PublishedObservation{ObservationID: observationID, Outcome: outcome}
		collision = collision || outcome == PublishCollision
	}
	if err := rows.Err(); err != nil {
		return PublishBatchResult{}, false, fmt.Errorf("publish source observation batch: read set result: %w", err)
	}
	for i := range seen {
		if !seen[i] {
			return PublishBatchResult{}, false, fmt.Errorf("publish source observation batch: incomplete set result")
		}
	}
	return result, collision, nil
}

func validatePublishBatch(input PublishBatchInput) error {
	if len(input.Observations) < 1 || len(input.Observations) > MaxPublishBatchSize {
		return fmt.Errorf("%w: observation count must be between 1 and %d", ErrInvalidEnvelope, MaxPublishBatchSize)
	}
	if len(input.Checkpoint.Entries) != len(input.Observations) || len(input.Checkpoint.Entries) > MaxCheckpointCount {
		return fmt.Errorf("%w: checkpoint count must equal observation count and be at most %d", ErrInvalidEnvelope, MaxCheckpointCount)
	}
	if input.Checkpoint.CollectionLatency < 0 || input.Checkpoint.CollectionLatency > MaxCollectionLatency {
		return fmt.Errorf("%w: collection latency is outside the accepted range", ErrInvalidEnvelope)
	}
	seen := make(map[string]struct{}, len(input.Observations))
	for i := range input.Observations {
		observation := input.Observations[i]
		if observation.Lease != input.Lease {
			return fmt.Errorf("%w: observation %d lease proof mismatch", ErrInvalidEnvelope, i)
		}
		if err := observation.Validate(); err != nil {
			return fmt.Errorf("%w: observation %d: %v", ErrInvalidEnvelope, i, err)
		}
		identity := observationIdentity(observation)
		if _, ok := seen[identity]; ok {
			return fmt.Errorf("%w: duplicate batch identity", ErrInvalidEnvelope)
		}
		seen[identity] = struct{}{}
	}
	observationBindings := make(map[checkpointBinding]struct{}, len(input.Observations))
	for i := range input.Observations {
		binding := checkpointBindingForObservation(input.Observations[i])
		if _, ok := observationBindings[binding]; ok {
			return fmt.Errorf("%w: duplicate observation checkpoint identity", ErrInvalidEnvelope)
		}
		observationBindings[binding] = struct{}{}
	}
	checkpointKeys := make(map[string]struct{}, len(input.Checkpoint.Entries))
	matchedObservations := make(map[checkpointBinding]struct{}, len(input.Checkpoint.Entries))
	for i := range input.Checkpoint.Entries {
		entry := input.Checkpoint.Entries[i]
		if !entry.Provider.Valid() || !entry.ObservationKind.Valid() || entry.ContractGeneration <= 0 ||
			!entry.Continuity.Valid() || entry.LastScheduledFor.IsZero() {
			return fmt.Errorf("%w: checkpoint %d metadata is invalid", ErrInvalidEnvelope, i)
		}
		for name, value := range map[string]string{
			"subject key": entry.SubjectKey, "observation key": entry.LastObservationKey,
			"scope sha256": entry.ScopeSHA256, "evidence sha256": entry.LastEvidenceSHA256,
		} {
			limit := 512
			if name == "subject key" {
				limit = 256
			}
			if name == "scope sha256" || name == "evidence sha256" {
				if !lowercaseHexToken(value) {
					return fmt.Errorf("%w: checkpoint %d %s is invalid", ErrInvalidEnvelope, i, name)
				}
				continue
			}
			if err := validateText(name, value, limit); err != nil {
				return fmt.Errorf("%w: checkpoint %d: %v", ErrInvalidEnvelope, i, err)
			}
		}
		if len(entry.Cursor) > 16384 {
			return fmt.Errorf("%w: checkpoint %d cursor is too large", ErrInvalidEnvelope, i)
		}
		if len(entry.Cursor) > 0 {
			var cursor map[string]any
			if err := json.Unmarshal(entry.Cursor, &cursor); err != nil || cursor == nil {
				return fmt.Errorf("%w: checkpoint %d cursor must be an object", ErrInvalidEnvelope, i)
			}
		}
		key := strings.Join([]string{string(entry.Provider), string(entry.ObservationKind), entry.SubjectKey, entry.ScopeSHA256}, "\x1f")
		if _, ok := checkpointKeys[key]; ok {
			return fmt.Errorf("%w: duplicate checkpoint identity", ErrInvalidEnvelope)
		}
		checkpointKeys[key] = struct{}{}
		binding := checkpointBindingForEntry(entry)
		if _, ok := observationBindings[binding]; !ok {
			return fmt.Errorf("%w: checkpoint %d is not bound to a batch observation", ErrInvalidEnvelope, i)
		}
		if _, ok := matchedObservations[binding]; ok {
			return fmt.Errorf("%w: checkpoint %d duplicates a batch observation binding", ErrInvalidEnvelope, i)
		}
		matchedObservations[binding] = struct{}{}
	}
	if len(matchedObservations) != len(observationBindings) {
		return fmt.Errorf("%w: checkpoint entries are missing a batch observation binding", ErrInvalidEnvelope)
	}
	return nil
}

type checkpointBinding struct {
	Provider           contract.Provider
	ObservationKind    contract.ObservationKind
	SubjectKey         string
	ScopeSHA256        string
	ContractGeneration int64
	ObservationKey     string
	EvidenceSHA256     string
	ScheduledFor       time.Time
	Continuity         contract.Continuity
}

func checkpointBindingForObservation(observation contract.Envelope) checkpointBinding {
	return checkpointBinding{
		Provider:           observation.Provider,
		ObservationKind:    observation.ObservationKind,
		SubjectKey:         observation.SubjectKey,
		ScopeSHA256:        observation.ScopeSHA256,
		ContractGeneration: observation.ContractGeneration,
		ObservationKey:     observation.ObservationKey,
		EvidenceSHA256:     observation.EvidenceSHA256,
		ScheduledFor:       observation.ScheduledFor.UTC(),
		Continuity:         observation.Continuity,
	}
}

func checkpointBindingForEntry(entry CheckpointEntry) checkpointBinding {
	return checkpointBinding{
		Provider:           entry.Provider,
		ObservationKind:    entry.ObservationKind,
		SubjectKey:         entry.SubjectKey,
		ScopeSHA256:        entry.ScopeSHA256,
		ContractGeneration: entry.ContractGeneration,
		ObservationKey:     entry.LastObservationKey,
		EvidenceSHA256:     entry.LastEvidenceSHA256,
		ScheduledFor:       entry.LastScheduledFor.UTC(),
		Continuity:         entry.Continuity,
	}
}

func completeCollectionJob(
	ctx context.Context,
	tx dbx.Tx,
	proof contract.LeaseProof,
	errorCode string,
) error {
	var code any
	if errorCode != "" {
		code = errorCode
	}
	var jobKey string
	err := tx.QueryRow(
		ctx,
		mustSQL("repository_job_complete_0011_11.sql"),
		proof.JobKey,
		proof.OwnerInstance,
		proof.FenceEpoch,
		proof.ProjectionGeneration,
		proof.ScheduledFor,
		code,
	).Scan(&jobKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCollectionFenceLost
	}
	if err != nil {
		return fmt.Errorf("publish source observation batch: complete collection job: %w", err)
	}
	return nil
}

func observationIdentity(observation contract.Envelope) string {
	return strings.Join([]string{
		string(observation.Provider), string(observation.ObservationKind), observation.SubjectKey,
		observation.ObservationKey, fmt.Sprint(observation.SchemaVersion), fmt.Sprint(observation.ContractGeneration),
	}, "\x1f")
}
