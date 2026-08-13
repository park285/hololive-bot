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
	MaxClaimBatchSize = 100
	MaxAttempts       = 8
	maxParityBytes    = 16 << 10
	maxErrorCodeBytes = 128
	maxErrorTextBytes = 2048
)

var (
	ErrAuthorityMissing     = errors.New("source observation authority fence is missing")
	ErrAuthorityInactive    = errors.New("source observation authority is inactive")
	ErrStaleGeneration      = errors.New("source observation authority generation is stale")
	ErrClaimLost            = errors.New("source observation claim was lost")
	ErrObservationCollision = errors.New("source observation identity has conflicting payload")
	ErrInvalidRepository    = errors.New("source observation repository is not configured")
)

type AuthorityFence struct {
	SourceKind contract.SourceKind
	Mode       contract.AuthorityMode
	Generation int64
}

type Observation struct {
	ID             int64
	SourceKind     contract.SourceKind
	SourceKey      string
	ObservationKey string
	SchemaVersion  int16
	Generation     int64
	ObservedAt     time.Time
	Completeness   contract.Completeness
	Continuity     contract.Continuity
	Payload        json.RawMessage
	PayloadSHA256  string
	AttemptCount   int
	LeaseOwner     string
	LeaseToken     string
	LeaseExpiresAt time.Time
}

func (o Observation) Envelope() contract.Envelope {
	return contract.Envelope{
		SourceKind:      o.SourceKind,
		SourceKey:       o.SourceKey,
		ObservationKey: o.ObservationKey,
		SchemaVersion:  o.SchemaVersion,
		Generation:     o.Generation,
		ObservedAt:     o.ObservedAt,
		Completeness:   o.Completeness,
		Continuity:     o.Continuity,
		Payload:        o.Payload,
		PayloadSHA256:  o.PayloadSHA256,
	}
}

type PublishResult struct {
	Changed       bool
	Inserted      bool
	ObservationID int64
	Fence         AuthorityFence
}

type ClaimOptions struct {
	SourceKind    contract.SourceKind
	ConsumerName  string
	LeaseOwner    string
	Limit         int
	LeaseDuration time.Duration
}

type ClaimedBatch struct {
	Fence        AuthorityFence
	ConsumerName string
	Observations []Observation
}

type Completion struct {
	ConsumerName       string
	ObservationID      int64
	SourceKind         contract.SourceKind
	LeaseToken         string
	ExpectedGeneration int64
	ParityStatus       contract.ParityStatus
	ParityDetail       json.RawMessage
}

type RetryInput struct {
	ObservationID int64
	SourceKind    contract.SourceKind
	LeaseToken    string
	Delay         time.Duration
	ErrorCode     string
	ErrorDetail   string
}

type DeadLetterInput struct {
	ObservationID int64
	SourceKind    contract.SourceKind
	LeaseToken    string
	ErrorCode     string
	ErrorDetail   string
}

func (o ClaimOptions) validate() error {
	if err := validateSourceKind(o.SourceKind); err != nil {
		return fmt.Errorf("validate source observation claim: %w", err)
	}
	if err := validateText("consumer name", o.ConsumerName, 128); err != nil {
		return err
	}
	if err := validateText("lease owner", o.LeaseOwner, 128); err != nil {
		return err
	}
	if o.Limit <= 0 || o.Limit > MaxClaimBatchSize {
		return fmt.Errorf("validate source observation claim: limit must be between 1 and %d", MaxClaimBatchSize)
	}
	if o.LeaseDuration < time.Second || o.LeaseDuration > 10*time.Minute {
		return fmt.Errorf("validate source observation claim: lease duration must be between 1 second and 10 minutes")
	}
	return nil
}

func (c Completion) validate() error {
	if err := validateText("consumer name", c.ConsumerName, 128); err != nil {
		return err
	}
	if c.ObservationID <= 0 {
		return fmt.Errorf("validate source observation completion: observation id must be positive")
	}
	if err := validateSourceKind(c.SourceKind); err != nil {
		return fmt.Errorf("validate source observation completion: %w", err)
	}
	if !lowercaseHexToken(c.LeaseToken) {
		return fmt.Errorf("validate source observation completion: lease token must be 64 lowercase hexadecimal characters")
	}
	if c.ExpectedGeneration <= 0 {
		return fmt.Errorf("validate source observation completion: expected generation must be positive")
	}
	if !c.ParityStatus.Valid() {
		return fmt.Errorf("validate source observation completion: invalid parity status %q", c.ParityStatus)
	}
	if err := validateParityDetail(c.ParityDetail); err != nil {
		return err
	}
	return nil
}

func (r RetryInput) validate() error {
	if r.ObservationID <= 0 {
		return fmt.Errorf("validate source observation retry: observation id must be positive")
	}
	if err := validateSourceKind(r.SourceKind); err != nil {
		return fmt.Errorf("validate source observation retry: %w", err)
	}
	if !lowercaseHexToken(r.LeaseToken) {
		return fmt.Errorf("validate source observation retry: lease token must be 64 lowercase hexadecimal characters")
	}
	if r.Delay < 0 || r.Delay > 24*time.Hour {
		return fmt.Errorf("validate source observation retry: delay must be between zero and 24 hours")
	}
	return validateErrorFields("retry", r.ErrorCode, r.ErrorDetail)
}

func (d DeadLetterInput) validate() error {
	if d.ObservationID <= 0 {
		return fmt.Errorf("validate source observation dead letter: observation id must be positive")
	}
	if err := validateSourceKind(d.SourceKind); err != nil {
		return fmt.Errorf("validate source observation dead letter: %w", err)
	}
	if !lowercaseHexToken(d.LeaseToken) {
		return fmt.Errorf("validate source observation dead letter: lease token must be 64 lowercase hexadecimal characters")
	}
	return validateErrorFields("dead letter", d.ErrorCode, d.ErrorDetail)
}

func validateParityDetail(detail json.RawMessage) error {
	if len(detail) == 0 {
		return nil
	}
	if len(detail) > maxParityBytes {
		return fmt.Errorf("validate source observation completion: parity detail exceeds %d bytes", maxParityBytes)
	}
	var object map[string]any
	if err := json.Unmarshal(detail, &object); err != nil {
		return fmt.Errorf("validate source observation completion: parity detail must be a JSON object: %w", err)
	}
	if object == nil {
		return fmt.Errorf("validate source observation completion: parity detail must be a JSON object")
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
