package sourceobservation

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"strings"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

const (
	MaxPublishBatchBytes = 8 << 20
	MaxCheckpointCount   = 1024
)

var (
	ErrInvalidEnvelope   = errors.New("source observation envelope is invalid")
	ErrStaleContract     = errors.New("source observation contract is stale")
	ErrInvalidRepository = errors.New("source observation repository is not configured")
)

type CheckpointEntry struct {
	Provider           contract.Provider
	ObservationKind    contract.ObservationKind
	SubjectKey         string
	ScopeSHA256        string
	ContractGeneration int64
	LastObservationKey string
	LastEvidenceSHA256 string
	LastScheduledFor   time.Time
	Continuity         contract.Continuity
	Cursor             jsontext.Value
}

type CheckpointUpdate struct {
	Entries           []CheckpointEntry
	CollectionLatency time.Duration
}

type PublishBatchInput struct {
	Lease        contract.LeaseProof
	Checkpoint   CheckpointUpdate
	Observations []contract.Envelope
}

type PublishOutcome string

const (
	PublishInserted  PublishOutcome = "INSERTED"
	PublishDuplicate PublishOutcome = "DUPLICATE"
	PublishCollision PublishOutcome = "COLLISION"
)

type PublishedObservation struct {
	ObservationID int64
	Outcome       PublishOutcome
	Ordinal       int
	// AcceptedInterval은 이 발행이 같은 checkpoint를 실제로 전진시켰을 때 직전 durable 수락 이후 경과 시간이다.
	// 최초 checkpoint·충돌·checkpoint가 바뀌지 않은 동일 slot 재생에서는 HasAcceptedInterval이 false다.
	AcceptedInterval    time.Duration
	HasAcceptedInterval bool
}

func NewPublishedObservation(observationID int64, outcome PublishOutcome, ordinal int) PublishedObservation {
	return PublishedObservation{ObservationID: observationID, Outcome: outcome, Ordinal: ordinal}
}

type PublishBatchResult struct {
	Results []PublishedObservation
	// EncodedBytes는 발행 SQL에 보낸 관측 JSON 크기다. MaxPublishBatchBytes 상한 근접 빈도를 관측하는 데 쓴다.
	EncodedBytes int
}

func validateText(name, value string, maxLength int) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is empty", name)
	}

	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must not contain surrounding whitespace", name)
	}

	if len(value) > maxLength {
		return fmt.Errorf("%s exceeds %d bytes", name, maxLength)
	}

	return nil
}

func lowercaseHexToken(value string) bool {
	if len(value) != 64 {
		return false
	}

	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}

	return true
}
