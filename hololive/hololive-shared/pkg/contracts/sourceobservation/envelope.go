package sourceobservation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

const (
	CommunitySchemaVersionV1 int16 = 1
	MaxPayloadBytes                = 1 << 20
)

type SourceKind string

const SourceKindYouTubeCommunity SourceKind = "youtube_community"

type Completeness string

const (
	CompletenessCompleteWindow Completeness = "COMPLETE_WINDOW"
	CompletenessPartialWindow  Completeness = "PARTIAL_WINDOW"
)

type Continuity string

const (
	ContinuityContiguous    Continuity = "CONTIGUOUS"
	ContinuityGapUnresolved Continuity = "GAP_UNRESOLVED"
)

type AuthorityMode string

const (
	AuthorityModeLegacy        AuthorityMode = "legacy"
	AuthorityModeShadow        AuthorityMode = "shadow"
	AuthorityModeAuthoritative AuthorityMode = "authoritative"
)

type Status string

const (
	StatusPending    Status = "PENDING"
	StatusProcessing Status = "PROCESSING"
	StatusProcessed  Status = "PROCESSED"
	StatusDeadLetter Status = "DEAD_LETTER"
)

type ParityStatus string

const (
	ParityStatusNotChecked ParityStatus = "NOT_CHECKED"
	ParityStatusMatch      ParityStatus = "MATCH"
	ParityStatusMismatch   ParityStatus = "MISMATCH"
)

type Envelope struct {
	SourceKind      SourceKind      `json:"source_kind"`
	SourceKey       string          `json:"source_key"`
	ObservationKey string          `json:"observation_key"`
	SchemaVersion  int16           `json:"schema_version"`
	Generation     int64           `json:"generation"`
	ObservedAt     time.Time       `json:"observed_at"`
	Completeness   Completeness    `json:"completeness"`
	Continuity     Continuity      `json:"continuity"`
	Payload        json.RawMessage `json:"payload"`
	PayloadSHA256  string          `json:"payload_sha256"`
}

var lowercaseSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (e Envelope) Validate() error {
	_, err := e.ValidateAndCanonicalPayload()
	return err
}

func (e Envelope) ValidateAndCanonicalPayload() ([]byte, error) {
	if e.SourceKind != SourceKindYouTubeCommunity {
		return nil, fmt.Errorf("validate source observation envelope: unsupported source kind %q", e.SourceKind)
	}
	if err := validateBoundedText("source key", e.SourceKey, 256); err != nil {
		return nil, err
	}
	if err := validateBoundedText("observation key", e.ObservationKey, 512); err != nil {
		return nil, err
	}
	if e.SchemaVersion != CommunitySchemaVersionV1 {
		return nil, fmt.Errorf("validate source observation envelope: unsupported schema version %d", e.SchemaVersion)
	}
	if e.Generation <= 0 {
		return nil, fmt.Errorf("validate source observation envelope: generation must be positive")
	}
	if e.ObservedAt.IsZero() {
		return nil, fmt.Errorf("validate source observation envelope: observed at is zero")
	}
	if !e.Completeness.Valid() {
		return nil, fmt.Errorf("validate source observation envelope: invalid completeness %q", e.Completeness)
	}
	if !e.Continuity.Valid() {
		return nil, fmt.Errorf("validate source observation envelope: invalid continuity %q", e.Continuity)
	}
	if !lowercaseSHA256Pattern.MatchString(e.PayloadSHA256) {
		return nil, fmt.Errorf("validate source observation envelope: payload sha256 must be 64 lowercase hexadecimal characters")
	}
	canonicalPayload, err := CanonicalizeJSON(e.Payload)
	if err != nil {
		return nil, fmt.Errorf("validate source observation envelope: canonicalize payload: %w", err)
	}
	if actual := SHA256Hex(canonicalPayload); actual != e.PayloadSHA256 {
		return nil, fmt.Errorf("validate source observation envelope: payload sha256 mismatch")
	}
	if err := validatePayloadForEnvelope(e, canonicalPayload); err != nil {
		return nil, err
	}
	return canonicalPayload, nil
}

func validatePayloadForEnvelope(envelope Envelope, canonicalPayload []byte) error {
	switch envelope.SourceKind {
	case SourceKindYouTubeCommunity:
		var payload CommunityPayloadV1
		if err := decodeStrictJSON(canonicalPayload, &payload); err != nil {
			return fmt.Errorf("validate source observation envelope: decode community payload: %w", err)
		}
		if err := payload.Validate(envelope.SourceKey); err != nil {
			return fmt.Errorf("validate source observation envelope: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("validate source observation envelope: unsupported source kind %q", envelope.SourceKind)
	}
}

func CanonicalizeJSON(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("payload is empty")
	}
	if len(raw) > MaxPayloadBytes {
		return nil, fmt.Errorf("payload exceeds %d bytes", MaxPayloadBytes)
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode json: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode canonical json: %w", err)
	}
	if len(canonical) > MaxPayloadBytes {
		return nil, fmt.Errorf("canonical payload exceeds %d bytes", MaxPayloadBytes)
	}
	return canonical, nil
}

func decodeStrictJSON(raw []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	return ensureJSONEOF(decoder)
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if err == nil {
		return fmt.Errorf("decode json: trailing value")
	}
	if err != io.EOF {
		return fmt.Errorf("decode json trailing data: %w", err)
	}
	return nil
}

func SHA256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func (c Completeness) Valid() bool {
	return c == CompletenessCompleteWindow || c == CompletenessPartialWindow
}

func (c Continuity) Valid() bool {
	return c == ContinuityContiguous || c == ContinuityGapUnresolved
}

func (m AuthorityMode) Valid() bool {
	return m == AuthorityModeLegacy || m == AuthorityModeShadow || m == AuthorityModeAuthoritative
}

func (s Status) Valid() bool {
	return s == StatusPending || s == StatusProcessing || s == StatusProcessed || s == StatusDeadLetter
}

func (p ParityStatus) Valid() bool {
	return p == ParityStatusNotChecked || p == ParityStatusMatch || p == ParityStatusMismatch
}

func validateBoundedText(name, value string, maxLength int) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("validate source observation envelope: %s is empty", name)
	}
	if len(trimmed) > maxLength {
		return fmt.Errorf("validate source observation envelope: %s exceeds %d bytes", name, maxLength)
	}
	if trimmed != value {
		return fmt.Errorf("validate source observation envelope: %s must not contain surrounding whitespace", name)
	}
	return nil
}
