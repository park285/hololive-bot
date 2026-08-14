package sourceobservation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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
	return dbx.InPgxTxWithResult(ctx, r.pool, func(tx dbx.Tx) (PublishBatchResult, error) {
		return r.publishBatchTx(ctx, tx, input)
	})
}

func (r *Repository) publishBatchTx(
	ctx context.Context,
	tx dbx.Tx,
	input PublishBatchInput,
) (PublishBatchResult, error) {
	if err := r.fenceVerifier.Verify(ctx, tx, input.Lease, input.Observations); err != nil {
		return PublishBatchResult{}, fmt.Errorf("publish source observation batch: verify job fence: %w", err)
	}
	for i := range input.Observations {
		if err := verifyCurrentContract(ctx, tx, input.Observations[i]); err != nil {
			return PublishBatchResult{}, fmt.Errorf("publish source observation batch: observation %d: %w", i, err)
		}
	}
	order := sortedObservationIndexes(input.Observations)
	for _, index := range order {
		if _, err := tx.Exec(
			ctx,
			mustSQL("repository_identity_advisory_lock_0005_05.sql"),
			observationIdentity(input.Observations[index]),
		); err != nil {
			return PublishBatchResult{}, fmt.Errorf("publish source observation batch: lock identity: %w", err)
		}
	}
	existing := make([]existingObservation, len(input.Observations))
	hasCollision := false
	for i := range input.Observations {
		candidate, err := loadExistingObservation(ctx, tx, input.Observations[i])
		if err != nil {
			return PublishBatchResult{}, err
		}
		existing[i] = candidate
		if candidate.found && candidate.evidenceSHA256 != input.Observations[i].EvidenceSHA256 {
			hasCollision = true
		}
	}
	if hasCollision {
		result := PublishBatchResult{Results: make([]PublishedObservation, len(input.Observations))}
		for i := range input.Observations {
			result.Results[i] = PublishedObservation{ObservationID: existing[i].id, Outcome: PublishCollision}
			if !existing[i].found || existing[i].evidenceSHA256 == input.Observations[i].EvidenceSHA256 {
				continue
			}
			if err := insertCollision(ctx, tx, existing[i], input.Observations[i]); err != nil {
				return PublishBatchResult{}, err
			}
		}
		if err := completeCollectionJob(ctx, tx, input.Lease, "observation_collision"); err != nil {
			return PublishBatchResult{}, err
		}
		return result, nil
	}

	result := PublishBatchResult{Results: make([]PublishedObservation, len(input.Observations))}
	for i := range input.Observations {
		if existing[i].found {
			result.Results[i] = PublishedObservation{ObservationID: existing[i].id, Outcome: PublishDuplicate}
			continue
		}
		observationID, err := insertObservation(ctx, tx, input.Observations[i])
		if err != nil {
			return PublishBatchResult{}, err
		}
		if _, err := tx.Exec(ctx, mustSQL("repository_queue_insert_0008_08.sql"), observationID); err != nil {
			return PublishBatchResult{}, fmt.Errorf("publish source observation batch: insert queue row: %w", err)
		}
		result.Results[i] = PublishedObservation{ObservationID: observationID, Outcome: PublishInserted}
	}
	for i := range input.Checkpoint.Entries {
		if err := upsertCheckpoint(ctx, tx, input.Checkpoint.Entries[i], input.Checkpoint.CollectionLatency); err != nil {
			return PublishBatchResult{}, err
		}
	}
	if err := completeCollectionJob(ctx, tx, input.Lease, ""); err != nil {
		return PublishBatchResult{}, err
	}
	return result, nil
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

func verifyCurrentContract(ctx context.Context, tx dbx.Tx, observation contract.Envelope) error {
	var schema int16
	var generation int64
	err := tx.QueryRow(
		ctx,
		mustSQL("repository_contract_current_0004_04.sql"),
		observation.Provider,
		observation.ObservationKind,
	).Scan(&schema, &generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrStaleContract
	}
	if err != nil {
		return fmt.Errorf("verify current observation contract: %w", err)
	}
	if schema != observation.SchemaVersion || generation != observation.ContractGeneration {
		return ErrStaleContract
	}
	return nil
}

type existingObservation struct {
	id             int64
	evidenceSHA256 string
	found          bool
}

func loadExistingObservation(
	ctx context.Context,
	tx dbx.Tx,
	observation contract.Envelope,
) (existingObservation, error) {
	var existing existingObservation
	err := tx.QueryRow(
		ctx,
		mustSQL("repository_observation_identity_0006_06.sql"),
		observation.Provider,
		observation.ObservationKind,
		observation.SubjectKey,
		observation.ObservationKey,
		observation.SchemaVersion,
		observation.ContractGeneration,
	).Scan(&existing.id, &existing.evidenceSHA256)
	if errors.Is(err, pgx.ErrNoRows) {
		return existingObservation{}, nil
	}
	if err != nil {
		return existingObservation{}, fmt.Errorf("publish source observation batch: preflight identity: %w", err)
	}
	existing.found = true
	return existing, nil
}

func insertObservation(ctx context.Context, tx dbx.Tx, observation contract.Envelope) (int64, error) {
	var observationID int64
	err := tx.QueryRow(
		ctx,
		mustSQL("repository_observation_insert_0007_07.sql"),
		observation.Provider,
		observation.ObservationKind,
		observation.SubjectKey,
		observation.ObservationKey,
		observation.SchemaVersion,
		observation.ContractGeneration,
		observation.ScheduledFor,
		observation.ObservedAt,
		observation.SourceEventAt,
		observation.ScopeSHA256,
		observation.Completeness,
		observation.Continuity,
		string(observation.Payload),
		observation.PayloadSHA256,
		observation.EvidenceSHA256,
		observation.CollectorInstance,
		observation.Lease.JobKey,
		observation.Lease.CollectionJobKind,
		observation.Lease.FenceEpoch,
		observation.Lease.ProjectionGeneration,
	).Scan(&observationID)
	if err != nil {
		return 0, fmt.Errorf("publish source observation batch: insert immutable evidence: %w", err)
	}
	return observationID, nil
}

func insertCollision(
	ctx context.Context,
	tx dbx.Tx,
	existing existingObservation,
	attempted contract.Envelope,
) error {
	if _, err := tx.Exec(
		ctx,
		mustSQL("repository_collision_insert_0009_09.sql"),
		existing.id,
		attempted.Provider,
		attempted.ObservationKind,
		attempted.SubjectKey,
		attempted.ObservationKey,
		attempted.SchemaVersion,
		attempted.ContractGeneration,
		existing.evidenceSHA256,
		attempted.EvidenceSHA256,
		attempted.PayloadSHA256,
		attempted.CollectorInstance,
		attempted.Lease.JobKey,
		attempted.Lease.FenceEpoch,
	); err != nil {
		return fmt.Errorf("publish source observation batch: insert collision audit: %w", err)
	}
	return nil
}

func upsertCheckpoint(
	ctx context.Context,
	tx dbx.Tx,
	entry CheckpointEntry,
	collectionLatency time.Duration,
) error {
	var cursor any
	if len(entry.Cursor) > 0 {
		cursor = string(entry.Cursor)
	}
	if _, err := tx.Exec(
		ctx,
		mustSQL("repository_checkpoint_upsert_0010_10.sql"),
		entry.Provider,
		entry.ObservationKind,
		entry.SubjectKey,
		entry.ScopeSHA256,
		entry.ContractGeneration,
		entry.LastObservationKey,
		entry.LastEvidenceSHA256,
		entry.LastScheduledFor,
		collectionLatency.Milliseconds(),
		entry.Continuity,
		cursor,
	); err != nil {
		return fmt.Errorf("publish source observation batch: upsert checkpoint: %w", err)
	}
	return nil
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

func sortedObservationIndexes(observations []contract.Envelope) []int {
	indexes := make([]int, len(observations))
	for i := range indexes {
		indexes[i] = i
	}
	sort.Slice(indexes, func(i, j int) bool {
		return observationIdentity(observations[indexes[i]]) < observationIdentity(observations[indexes[j]])
	})
	return indexes
}

func observationIdentity(observation contract.Envelope) string {
	return strings.Join([]string{
		string(observation.Provider), string(observation.ObservationKind), observation.SubjectKey,
		observation.ObservationKey, fmt.Sprint(observation.SchemaVersion), fmt.Sprint(observation.ContractGeneration),
	}, "\x1f")
}
