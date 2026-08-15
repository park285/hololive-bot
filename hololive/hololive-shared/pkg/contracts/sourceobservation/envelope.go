package sourceobservation

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	SchemaVersionV1                 int16 = 1
	CommunitySchemaVersionV1              = SchemaVersionV1
	MaxPayloadBytes                       = 1 << 20
	DefaultMaxSourceEventFutureSkew       = 5 * time.Minute
	MaxSourceEventFutureSkew              = 15 * time.Minute
)

type Provider string

const (
	ProviderHolodex          Provider = "holodex"
	ProviderYouTubeJS        Provider = "youtubejs"
	ProviderHololiveOfficial Provider = "hololive_official"
)

type ObservationKind string

const (
	KindCommunityPage  ObservationKind = "community_page"
	KindVideoList      ObservationKind = "video_list"
	KindShortsList     ObservationKind = "shorts_list"
	KindLiveSnapshot   ObservationKind = "live_snapshot"
	KindViewerSample   ObservationKind = "viewer_sample"
	KindChannelStats   ObservationKind = "channel_stats"
	KindChannelProfile ObservationKind = "channel_profile"
	KindChannelPhoto   ObservationKind = "channel_photo"
	KindSchedule       ObservationKind = "schedule_snapshot"
)

type Completeness string

const (
	CompletenessComplete Completeness = "COMPLETE"
	CompletenessPartial  Completeness = "PARTIAL"
	CompletenessUnknown  Completeness = "UNKNOWN"
)

type Continuity string

const (
	ContinuityContiguous    Continuity = "CONTIGUOUS"
	ContinuityGapUnresolved Continuity = "GAP_UNRESOLVED"
	ContinuityNotApplicable Continuity = "NOT_APPLICABLE"
)

type Status string

const (
	StatusPending    Status = "PENDING"
	StatusProcessing Status = "PROCESSING"
	StatusProcessed  Status = "PROCESSED"
	StatusDeadLetter Status = "DEAD_LETTER"
)

type LeaseProof struct {
	JobKey               string    `json:"job_key"`
	CollectionJobKind    string    `json:"collection_job_kind"`
	OwnerInstance        string    `json:"owner_instance"`
	FenceEpoch           int64     `json:"fence_epoch"`
	ProjectionGeneration int64     `json:"projection_generation"`
	ScheduledFor         time.Time `json:"scheduled_for"`
}

type Envelope struct {
	Provider           Provider        `json:"provider"`
	ObservationKind    ObservationKind `json:"observation_kind"`
	SubjectKey         string          `json:"subject_key"`
	ObservationKey     string          `json:"observation_key"`
	SchemaVersion      int16           `json:"schema_version"`
	ContractGeneration int64           `json:"contract_generation"`
	ScheduledFor       time.Time       `json:"scheduled_for"`
	ObservedAt         time.Time       `json:"observed_at"`
	SourceEventAt      *time.Time      `json:"source_event_at,omitempty"`
	ScopeSHA256        string          `json:"scope_sha256"`
	Completeness       Completeness    `json:"completeness"`
	Continuity         Continuity      `json:"continuity"`
	Payload            json.RawMessage `json:"payload"`
	PayloadSHA256      string          `json:"payload_sha256"`
	EvidenceSHA256     string          `json:"evidence_sha256"`
	CollectorInstance  string          `json:"collector_instance"`
	Lease              LeaseProof      `json:"lease"`
}

type EvidenceDigestV1 struct {
	Provider           Provider        `json:"provider"`
	ObservationKind    ObservationKind `json:"observation_kind"`
	SubjectKey         string          `json:"subject_key"`
	ObservationKey     string          `json:"observation_key"`
	SchemaVersion      int16           `json:"schema_version"`
	ContractGeneration int64           `json:"contract_generation"`
	ScheduledFor       time.Time       `json:"scheduled_for"`
	SourceEventAt      *time.Time      `json:"source_event_at,omitempty"`
	ScopeSHA256        string          `json:"scope_sha256"`
	Completeness       Completeness    `json:"completeness"`
	Continuity         Continuity      `json:"continuity"`
	PayloadSHA256      string          `json:"payload_sha256"`
}

type ObservationClock struct {
	ObservationKind ObservationKind
	ScheduledFor    time.Time
	SourceEventAt   *time.Time
	ReceivedAt      time.Time
}

var lowercaseSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func DecodeEnvelopeStrict(raw []byte) (Envelope, error) {
	if len(raw) == 0 || len(raw) > MaxPayloadBytes+8192 {
		return Envelope{}, fmt.Errorf("decode source observation envelope: size is outside the accepted range")
	}
	var envelope Envelope
	if err := decodeStrictJSON(raw, &envelope); err != nil {
		return Envelope{}, fmt.Errorf("decode source observation envelope: %w", err)
	}
	if err := envelope.Validate(); err != nil {
		return Envelope{}, err
	}
	return envelope, nil
}

func PrepareEnvelope(envelope Envelope) (Envelope, error) {
	canonicalPayload, canonicalScope, err := canonicalPayloadAndScope(
		envelope.ObservationKind,
		envelope.SubjectKey,
		envelope.Completeness,
		envelope.Payload,
	)
	if err != nil {
		return Envelope{}, fmt.Errorf("prepare source observation envelope: %w", err)
	}
	envelope.ScheduledFor = envelope.ScheduledFor.UTC()
	envelope.ObservedAt = envelope.ObservedAt.UTC()
	if envelope.SourceEventAt != nil {
		sourceEventAt := envelope.SourceEventAt.UTC()
		envelope.SourceEventAt = &sourceEventAt
	}
	envelope.Lease.ScheduledFor = envelope.Lease.ScheduledFor.UTC()
	envelope.Payload = canonicalPayload
	envelope.ScopeSHA256 = SHA256Hex(canonicalScope)
	envelope.PayloadSHA256 = SHA256Hex(canonicalPayload)
	envelope.ObservationKey = ObservationKeyForEnvelope(envelope, canonicalScope)
	evidenceSHA256, err := envelope.expectedEvidenceSHA256()
	if err != nil {
		return Envelope{}, fmt.Errorf("prepare source observation envelope: %w", err)
	}
	envelope.EvidenceSHA256 = evidenceSHA256
	if err := envelope.Validate(); err != nil {
		return Envelope{}, err
	}
	return envelope, nil
}

func (e Envelope) Validate() error {
	_, err := e.ValidateAndCanonicalPayload()
	return err
}

func (e Envelope) ValidateAndCanonicalPayload() ([]byte, error) {
	if err := e.validateEnvelopeIdentity(); err != nil {
		return nil, err
	}
	if err := e.validateEnvelopeClock(); err != nil {
		return nil, err
	}
	if err := e.validateEnvelopeLeaseAndHashes(); err != nil {
		return nil, err
	}
	return e.verifyCanonicalPayload()
}

func (e Envelope) validateEnvelopeIdentity() error {
	if !e.Provider.Valid() {
		return fmt.Errorf("validate source observation envelope: unsupported provider %q", e.Provider)
	}
	if !e.ObservationKind.Valid() {
		return fmt.Errorf("validate source observation envelope: unsupported observation kind %q", e.ObservationKind)
	}
	if err := validateBoundedText("subject key", e.SubjectKey, 256); err != nil {
		return err
	}
	if err := validateBoundedText("observation key", e.ObservationKey, 512); err != nil {
		return err
	}
	if e.SchemaVersion != SchemaVersionV1 {
		return fmt.Errorf("validate source observation envelope: unsupported schema version %d", e.SchemaVersion)
	}
	if e.ContractGeneration <= 0 {
		return fmt.Errorf("validate source observation envelope: contract generation must be positive")
	}
	return nil
}

func (e Envelope) validateEnvelopeClock() error {
	if e.ScheduledFor.IsZero() || e.ObservedAt.IsZero() {
		return fmt.Errorf("validate source observation envelope: scheduled for and observed at must be non-zero")
	}
	if e.SourceEventAt != nil && e.SourceEventAt.IsZero() {
		return fmt.Errorf("validate source observation envelope: source event at must not point to zero time")
	}
	if e.SourceEventAt != nil && !KindAllowsSourceEventTime(e.ObservationKind) {
		return fmt.Errorf("validate source observation envelope: observation kind does not allow source event time")
	}
	if !e.Completeness.Valid() {
		return fmt.Errorf("validate source observation envelope: invalid completeness %q", e.Completeness)
	}
	if !e.Continuity.Valid() {
		return fmt.Errorf("validate source observation envelope: invalid continuity %q", e.Continuity)
	}
	return nil
}

func (e Envelope) validateEnvelopeLeaseAndHashes() error {
	if err := e.Lease.validate(e.ScheduledFor); err != nil {
		return err
	}
	if err := validateBoundedText("collector instance", e.CollectorInstance, 128); err != nil {
		return err
	}
	return validateEnvelopeSHA256s(e)
}

func validateEnvelopeSHA256s(e Envelope) error {
	for name, value := range map[string]string{
		"scope sha256":    e.ScopeSHA256,
		"payload sha256":  e.PayloadSHA256,
		"evidence sha256": e.EvidenceSHA256,
	} {
		if !lowercaseSHA256Pattern.MatchString(value) {
			return fmt.Errorf("validate source observation envelope: %s must be 64 lowercase hexadecimal characters", name)
		}
	}
	return nil
}

func (e Envelope) verifyCanonicalPayload() ([]byte, error) {
	canonicalPayload, canonicalScope, err := canonicalPayloadAndScope(e.ObservationKind, e.SubjectKey, e.Completeness, e.Payload)
	if err != nil {
		return nil, fmt.Errorf("validate source observation envelope: %w", err)
	}
	if err := e.matchCanonicalDigests(canonicalPayload, canonicalScope); err != nil {
		return nil, err
	}
	return canonicalPayload, nil
}

func (e Envelope) matchCanonicalDigests(canonicalPayload, canonicalScope []byte) error {
	if actual := SHA256Hex(canonicalScope); actual != e.ScopeSHA256 {
		return fmt.Errorf("validate source observation envelope: scope sha256 mismatch")
	}
	if actual := SHA256Hex(canonicalPayload); actual != e.PayloadSHA256 {
		return fmt.Errorf("validate source observation envelope: payload sha256 mismatch")
	}
	if expected := ObservationKeyForEnvelope(e, canonicalScope); expected != e.ObservationKey {
		return fmt.Errorf("validate source observation envelope: observation key mismatch")
	}
	expectedEvidenceSHA256, err := e.expectedEvidenceSHA256()
	if err != nil {
		return fmt.Errorf("validate source observation envelope: %w", err)
	}
	if expectedEvidenceSHA256 != e.EvidenceSHA256 {
		return fmt.Errorf("validate source observation envelope: evidence sha256 mismatch")
	}
	return nil
}

func (e Envelope) expectedEvidenceSHA256() (string, error) {
	digest := EvidenceDigestV1{
		Provider:           e.Provider,
		ObservationKind:    e.ObservationKind,
		SubjectKey:         e.SubjectKey,
		ObservationKey:     e.ObservationKey,
		SchemaVersion:      e.SchemaVersion,
		ContractGeneration: e.ContractGeneration,
		ScheduledFor:       e.ScheduledFor.UTC(),
		SourceEventAt:      e.SourceEventAt,
		ScopeSHA256:        e.ScopeSHA256,
		Completeness:       e.Completeness,
		Continuity:         e.Continuity,
		PayloadSHA256:      e.PayloadSHA256,
	}
	if digest.SourceEventAt != nil {
		sourceEventAt := digest.SourceEventAt.UTC()
		digest.SourceEventAt = &sourceEventAt
	}
	canonical, err := canonicalJSON(digest)
	if err != nil {
		return "", fmt.Errorf("canonicalize evidence digest: %w", err)
	}
	return SHA256Hex(canonical), nil
}

func (proof LeaseProof) validate(scheduledFor time.Time) error {
	if err := validateBoundedText("lease job key", proof.JobKey, 512); err != nil {
		return err
	}
	if err := validateBoundedText("collection job kind", proof.CollectionJobKind, 128); err != nil {
		return err
	}
	if err := validateBoundedText("lease owner instance", proof.OwnerInstance, 128); err != nil {
		return err
	}
	if proof.FenceEpoch <= 0 || proof.ProjectionGeneration <= 0 {
		return fmt.Errorf("validate source observation envelope: lease fence and projection generation must be positive")
	}
	if proof.ScheduledFor.IsZero() || !proof.ScheduledFor.Equal(scheduledFor) {
		return fmt.Errorf("validate source observation envelope: lease scheduled slot mismatch")
	}
	return nil
}

func (p Provider) Valid() bool {
	return p == ProviderHolodex || p == ProviderYouTubeJS || p == ProviderHololiveOfficial
}

func (k ObservationKind) Valid() bool {
	switch k {
	case KindCommunityPage, KindVideoList, KindShortsList, KindLiveSnapshot,
		KindViewerSample, KindChannelStats, KindChannelProfile, KindChannelPhoto, KindSchedule:
		return true
	default:
		return false
	}
}

func (c Completeness) Valid() bool {
	return c == CompletenessComplete || c == CompletenessPartial || c == CompletenessUnknown
}

func (c Continuity) Valid() bool {
	return c == ContinuityContiguous || c == ContinuityGapUnresolved || c == ContinuityNotApplicable
}

func (s Status) Valid() bool {
	return s == StatusPending || s == StatusProcessing || s == StatusProcessed || s == StatusDeadLetter
}

func NegativeEligible(completeness Completeness, continuity Continuity) bool {
	return completeness == CompletenessComplete &&
		(continuity == ContinuityContiguous || continuity == ContinuityNotApplicable)
}

func KindAllowsSourceEventTime(kind ObservationKind) bool {
	switch kind {
	case KindCommunityPage, KindLiveSnapshot, KindViewerSample, KindChannelProfile, KindChannelPhoto, KindSchedule:
		return true
	default:
		return false
	}
}

func ValidateMaxSourceEventFutureSkew(value time.Duration) error {
	if value < 0 || value > MaxSourceEventFutureSkew {
		return fmt.Errorf("source event future skew must be between zero and %s", MaxSourceEventFutureSkew)
	}
	return nil
}

func SourceEventAtAllowed(observation ObservationClock, maxFutureSkew time.Duration) bool {
	if observation.SourceEventAt == nil || observation.ReceivedAt.IsZero() ||
		!KindAllowsSourceEventTime(observation.ObservationKind) || ValidateMaxSourceEventFutureSkew(maxFutureSkew) != nil {
		return false
	}
	return !observation.SourceEventAt.After(observation.ReceivedAt.Add(maxFutureSkew))
}

func EffectiveAt(observation ObservationClock, maxFutureSkew time.Duration) (time.Time, bool) {
	if SourceEventAtAllowed(observation, maxFutureSkew) {
		return observation.SourceEventAt.UTC(), false
	}
	return observation.ScheduledFor.UTC(), observation.SourceEventAt != nil
}

func validateBoundedText(name, value string, maxLength int) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("validate source observation envelope: %s is empty", name)
	}
	if len(value) > maxLength {
		return fmt.Errorf("validate source observation envelope: %s exceeds %d bytes", name, maxLength)
	}
	if trimmed != value {
		return fmt.Errorf("validate source observation envelope: %s must not contain surrounding whitespace", name)
	}
	return nil
}
