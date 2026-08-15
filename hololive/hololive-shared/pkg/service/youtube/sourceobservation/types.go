package sourceobservation

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

const (
	MaxPublishBatchSize  = 1024
	MaxPublishBatchBytes = 8 << 20
	MaxClaimBatchSize    = 100
	MaxCheckpointCount   = 1024
	MaxAttempts          = 64
	MaxReplayCount       = 16
	MaxCollectionLatency = 24 * time.Hour
	maxErrorCodeBytes    = 128
	maxErrorTextBytes    = 2048
)

var (
	ErrInvalidEnvelope      = errors.New("source observation envelope is invalid")
	ErrStaleContract        = errors.New("source observation contract is stale")
	ErrCollectionFenceLost  = errors.New("collection job fence was lost")
	ErrProjectionStale      = errors.New("collection projection is stale")
	ErrTargetDisabled       = errors.New("collection target is disabled")
	ErrClaimLost            = errors.New("source observation claim was lost")
	ErrObservationCollision = errors.New("source observation identity has conflicting evidence")
	ErrUnsupportedContract  = errors.New("source observation contract is unsupported")
	ErrInvalidRepository    = errors.New("source observation repository is not configured")
)

type ContractVersion struct {
	Provider   contract.Provider
	Kind       contract.ObservationKind
	Schema     int16
	Generation int64
}

type SupportedContractSet interface {
	Supports(ContractVersion) bool
}

type StaticSupportedContracts map[ContractVersion]struct{}

func (s StaticSupportedContracts) Supports(version ContractVersion) bool {
	_, ok := s[version]
	return ok
}

func InitialSupportedContracts() StaticSupportedContracts {
	result := make(StaticSupportedContracts)
	for _, version := range []ContractVersion{
		{contract.ProviderYouTubeJS, contract.KindCommunityPage, 1, 1},
		{contract.ProviderYouTubeJS, contract.KindVideoList, 1, 1},
		{contract.ProviderYouTubeJS, contract.KindShortsList, 1, 1},
		{contract.ProviderYouTubeJS, contract.KindLiveSnapshot, 1, 1},
		{contract.ProviderYouTubeJS, contract.KindViewerSample, 1, 1},
		{contract.ProviderYouTubeJS, contract.KindChannelStats, 1, 1},
		{contract.ProviderYouTubeJS, contract.KindChannelProfile, 1, 1},
		{contract.ProviderYouTubeJS, contract.KindChannelPhoto, 1, 1},
		{contract.ProviderHolodex, contract.KindLiveSnapshot, 1, 1},
		{contract.ProviderHolodex, contract.KindViewerSample, 1, 1},
		{contract.ProviderHolodex, contract.KindSchedule, 1, 1},
		{contract.ProviderHolodex, contract.KindChannelStats, 1, 1},
		{contract.ProviderHolodex, contract.KindChannelProfile, 1, 1},
		{contract.ProviderHolodex, contract.KindChannelPhoto, 1, 1},
		{contract.ProviderHololiveOfficial, contract.KindSchedule, 1, 1},
	} {
		result[version] = struct{}{}
	}
	return result
}

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
	Cursor             json.RawMessage
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
}

type PublishBatchResult struct {
	Results []PublishedObservation
}

type Observation struct {
	ID                   int64
	Provider             contract.Provider
	ObservationKind      contract.ObservationKind
	SubjectKey           string
	ObservationKey       string
	SchemaVersion        int16
	ContractGeneration   int64
	ScheduledFor         time.Time
	ObservedAt           time.Time
	SourceEventAt        *time.Time
	ReceivedAt           time.Time
	ScopeSHA256          string
	Completeness         contract.Completeness
	Continuity           contract.Continuity
	Payload              json.RawMessage
	PayloadSHA256        string
	EvidenceSHA256       string
	CollectorInstance    string
	JobKey               string
	CollectionJobKind    string
	FenceEpoch           int64
	ProjectionGeneration int64
	AttemptCount         int
	LeaseOwner           string
	LeaseToken           string
	LeaseExpiresAt       time.Time
	EffectiveAt          time.Time
	SourceEventFallback  bool
}

func (o Observation) ContractVersion() ContractVersion {
	return ContractVersion{Provider: o.Provider, Kind: o.ObservationKind, Schema: o.SchemaVersion, Generation: o.ContractGeneration}
}

func (o Observation) Envelope() contract.Envelope {
	return contract.Envelope{
		Provider: o.Provider, ObservationKind: o.ObservationKind, SubjectKey: o.SubjectKey,
		ObservationKey: o.ObservationKey, SchemaVersion: o.SchemaVersion,
		ContractGeneration: o.ContractGeneration, ScheduledFor: o.ScheduledFor,
		ObservedAt: o.ObservedAt, SourceEventAt: o.SourceEventAt, ScopeSHA256: o.ScopeSHA256,
		Completeness: o.Completeness, Continuity: o.Continuity, Payload: o.Payload,
		PayloadSHA256: o.PayloadSHA256, EvidenceSHA256: o.EvidenceSHA256,
		CollectorInstance: o.CollectorInstance,
		Lease: contract.LeaseProof{
			JobKey: o.JobKey, CollectionJobKind: o.CollectionJobKind,
			OwnerInstance: o.CollectorInstance, FenceEpoch: o.FenceEpoch,
			ProjectionGeneration: o.ProjectionGeneration, ScheduledFor: o.ScheduledFor,
		},
	}
}

type ClaimOptions struct {
	ConsumerName  string
	LeaseOwner    string
	Kinds         []contract.ObservationKind
	Limit         int
	LeaseDuration time.Duration
}

type ClaimedBatch struct {
	ConsumerName string
	Observations []Observation
}

type Claim struct {
	ConsumerName  string
	ObservationID int64
	LeaseToken    string
}

type Application struct {
	EntityKind string
	EntityKey  string
	Decision   string
}

type ReconcileResult struct {
	Applications        []Application
	Unsupported         bool
	EffectiveAt         time.Time
	SourceEventFallback bool
}

type RetryInput struct {
	ObservationID int64
	LeaseToken    string
	Delay         time.Duration
	ErrorCode     string
	ErrorDetail   string
}

type DeadLetterInput struct {
	ObservationID int64
	LeaseToken    string
	ErrorCode     string
	ErrorDetail   string
}

type ReplayInput struct {
	ObservationID int64
	RequestedBy   string
	Reason        string
}

type ReplayResult struct {
	RequestID     int64
	Applied       bool
	RejectionCode string
}

type RetentionQuery struct {
	Kind   contract.ObservationKind
	Before time.Time
	Limit  int
}

func (o ClaimOptions) validate() error {
	if err := validateText("consumer name", o.ConsumerName, 128); err != nil {
		return fmt.Errorf("validate source observation claim: %w", err)
	}
	if err := validateText("lease owner", o.LeaseOwner, 128); err != nil {
		return fmt.Errorf("validate source observation claim: %w", err)
	}
	if err := validateClaimKinds(o.Kinds); err != nil {
		return err
	}
	if o.Limit < 1 || o.Limit > MaxClaimBatchSize {
		return fmt.Errorf("validate source observation claim: limit must be between 1 and %d", MaxClaimBatchSize)
	}
	if o.LeaseDuration < time.Second || o.LeaseDuration > 10*time.Minute {
		return fmt.Errorf("validate source observation claim: lease duration must be between 1 second and 10 minutes")
	}
	return nil
}

func validateClaimKinds(kinds []contract.ObservationKind) error {
	if len(kinds) == 0 || len(kinds) > 9 {
		return fmt.Errorf("validate source observation claim: kind count must be between 1 and 9")
	}
	seen := make(map[contract.ObservationKind]struct{}, len(kinds))
	for _, kind := range kinds {
		if !kind.Valid() {
			return fmt.Errorf("validate source observation claim: invalid kind %q", kind)
		}
		if _, ok := seen[kind]; ok {
			return fmt.Errorf("validate source observation claim: duplicate kind %q", kind)
		}
		seen[kind] = struct{}{}
	}
	return nil
}

func validateErrorFields(action, code, detail string) error {
	if err := validateText("error code", code, maxErrorCodeBytes); err != nil {
		return fmt.Errorf("validate source observation %s: %w", action, err)
	}
	if len(detail) > maxErrorTextBytes {
		return fmt.Errorf("validate source observation %s: error detail exceeds %d bytes", action, maxErrorTextBytes)
	}
	return nil
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
