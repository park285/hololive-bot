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

const maxCheckpointCursorBytes = 16384

// 카운트·바이트·cursor 크기 상한은 preflightPublishBatch가 복사 전 가드로 이미 검증했다는
// 전제 위에서만 호출된다. 단독 호출 시 해당 상한은 검증되지 않는다.
func validatePublishBatch(input *PublishBatchInput) error {
	if err := validatePublishBatchObservations(input); err != nil {
		return err
	}
	bindings, err := observationCheckpointBindings(input.Observations)
	if err != nil {
		return err
	}
	return validatePublishBatchCheckpoints(input, bindings)
}

func validatePublishBatchCounts(input *PublishBatchInput) error {
	if len(input.Observations) < 1 || len(input.Observations) > MaxPublishBatchSize {
		return fmt.Errorf("%w: observation count must be between 1 and %d", ErrInvalidEnvelope, MaxPublishBatchSize)
	}
	if len(input.Checkpoint.Entries) != len(input.Observations) || len(input.Checkpoint.Entries) > MaxCheckpointCount {
		return fmt.Errorf("%w: checkpoint count must equal observation count and be at most %d", ErrInvalidEnvelope, MaxCheckpointCount)
	}
	if input.Checkpoint.CollectionLatency < 0 || input.Checkpoint.CollectionLatency > MaxCollectionLatency {
		return fmt.Errorf("%w: collection latency is outside the accepted range", ErrInvalidEnvelope)
	}
	return nil
}

func validatePublishBatchObservations(input *PublishBatchInput) error {
	seen := make(map[string]struct{}, len(input.Observations))
	for i := range input.Observations {
		if err := validatePublishBatchObservation(input, i, seen); err != nil {
			return err
		}
	}
	return nil
}

func validatePublishBatchObservation(input *PublishBatchInput, index int, seen map[string]struct{}) error {
	observation := &input.Observations[index]
	if observation.Lease != input.Lease {
		return fmt.Errorf("%w: observation %d lease proof mismatch", ErrInvalidEnvelope, index)
	}
	if err := observation.Validate(); err != nil {
		return fmt.Errorf("%w: observation %d: %w", ErrInvalidEnvelope, index, err)
	}
	identity := observationIdentity(observation)
	if _, ok := seen[identity]; ok {
		return fmt.Errorf("%w: duplicate batch identity", ErrInvalidEnvelope)
	}
	seen[identity] = struct{}{}
	return nil
}

func observationCheckpointBindings(observations []contract.Envelope) (map[checkpointBinding]struct{}, error) {
	bindings := make(map[checkpointBinding]struct{}, len(observations))
	for i := range observations {
		binding := checkpointBindingForObservation(&observations[i])
		if _, ok := bindings[binding]; ok {
			return nil, fmt.Errorf("%w: duplicate observation checkpoint identity", ErrInvalidEnvelope)
		}
		bindings[binding] = struct{}{}
	}
	return bindings, nil
}

func validatePublishBatchCheckpoints(input *PublishBatchInput, bindings map[checkpointBinding]struct{}) error {
	checkpointKeys := make(map[string]struct{}, len(input.Checkpoint.Entries))
	matched := make(map[checkpointBinding]struct{}, len(input.Checkpoint.Entries))
	for i := range input.Checkpoint.Entries {
		if err := validatePublishCheckpoint(&input.Checkpoint.Entries[i], i, bindings, checkpointKeys, matched); err != nil {
			return err
		}
	}
	if len(matched) != len(bindings) {
		return fmt.Errorf("%w: checkpoint entries are missing a batch observation binding", ErrInvalidEnvelope)
	}
	return nil
}

func validatePublishCheckpoint(
	entry *CheckpointEntry,
	index int,
	bindings map[checkpointBinding]struct{},
	checkpointKeys map[string]struct{},
	matched map[checkpointBinding]struct{},
) error {
	if err := validateCheckpointMetadata(entry, index); err != nil {
		return err
	}
	if err := validateCheckpointFields(entry, index); err != nil {
		return err
	}
	return bindPublishCheckpoint(entry, index, bindings, checkpointKeys, matched)
}

func validateCheckpointMetadata(entry *CheckpointEntry, index int) error {
	if !entry.Provider.Valid() || !entry.ObservationKind.Valid() || entry.ContractGeneration <= 0 ||
		!entry.Continuity.Valid() || entry.LastScheduledFor.IsZero() {
		return fmt.Errorf("%w: checkpoint %d metadata is invalid", ErrInvalidEnvelope, index)
	}
	return nil
}

func validateCheckpointFields(entry *CheckpointEntry, index int) error {
	if err := validateCheckpointTextFields(entry, index); err != nil {
		return err
	}
	return validateCheckpointCursor(entry, index)
}

func validateCheckpointTextFields(entry *CheckpointEntry, index int) error {
	for name, value := range map[string]string{
		"subject key": entry.SubjectKey, "observation key": entry.LastObservationKey,
		"scope sha256": entry.ScopeSHA256, "evidence sha256": entry.LastEvidenceSHA256,
	} {
		if err := validateCheckpointTextField(name, value, index); err != nil {
			return err
		}
	}
	return nil
}

func validateCheckpointTextField(name, value string, index int) error {
	if name == "scope sha256" || name == "evidence sha256" {
		if !lowercaseHexToken(value) {
			return fmt.Errorf("%w: checkpoint %d %s is invalid", ErrInvalidEnvelope, index, name)
		}
		return nil
	}
	limit := 512
	if name == "subject key" {
		limit = 256
	}
	if err := validateText(name, value, limit); err != nil {
		return fmt.Errorf("%w: checkpoint %d: %w", ErrInvalidEnvelope, index, err)
	}
	return nil
}

func validateCheckpointCursorSize(cursor []byte, index int) error {
	if len(cursor) > maxCheckpointCursorBytes {
		return fmt.Errorf("%w: checkpoint %d cursor is too large", ErrInvalidEnvelope, index)
	}
	return nil
}

func validateCheckpointCursor(entry *CheckpointEntry, index int) error {
	if len(entry.Cursor) == 0 {
		return nil
	}
	var cursor map[string]any
	if err := json.Unmarshal(entry.Cursor, &cursor); err != nil || cursor == nil {
		return fmt.Errorf("%w: checkpoint %d cursor must be an object", ErrInvalidEnvelope, index)
	}
	return nil
}

func bindPublishCheckpoint(
	entry *CheckpointEntry,
	index int,
	bindings map[checkpointBinding]struct{},
	checkpointKeys map[string]struct{},
	matched map[checkpointBinding]struct{},
) error {
	key := strings.Join([]string{string(entry.Provider), string(entry.ObservationKind), entry.SubjectKey, entry.ScopeSHA256}, "\x1f")
	if _, ok := checkpointKeys[key]; ok {
		return fmt.Errorf("%w: duplicate checkpoint identity", ErrInvalidEnvelope)
	}
	checkpointKeys[key] = struct{}{}
	binding := checkpointBindingForEntry(entry)
	if _, ok := bindings[binding]; !ok {
		return fmt.Errorf("%w: checkpoint %d is not bound to a batch observation", ErrInvalidEnvelope, index)
	}
	if _, ok := matched[binding]; ok {
		return fmt.Errorf("%w: checkpoint %d duplicates a batch observation binding", ErrInvalidEnvelope, index)
	}
	matched[binding] = struct{}{}
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

func checkpointBindingForObservation(observation *contract.Envelope) checkpointBinding {
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

func checkpointBindingForEntry(entry *CheckpointEntry) checkpointBinding {
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
	proof *contract.LeaseProof,
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

func observationIdentity(observation *contract.Envelope) string {
	return strings.Join([]string{
		string(observation.Provider), string(observation.ObservationKind), observation.SubjectKey,
		observation.ObservationKey, fmt.Sprint(observation.SchemaVersion), fmt.Sprint(observation.ContractGeneration),
	}, "\x1f")
}
